// Package api stellt RAPTOR als HTTP-Schnittstelle bereit. Koordinaten kommen nur im Request-Body; Anfragen,
// Orte und Ziele werden nie protokolliert, Fehlermeldungen nennen nur den Fehlertyp.
package api

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
	"github.com/Lord3n20/SchnellWeg/server/internal/rt"
)

// Berlin ist die Zeitzone der Fahrpläne.
var Berlin = mustLoc("Europe/Berlin")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// DayBase ist der Bezugspunkt der GTFS-Zeiten eines Betriebstags: „Mittag minus 12 Stunden“, damit Zeiten an
// Tagen mit Zeitumstellung stimmen.
func DayBase(day time.Time) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, 12, 0, 0, 0, Berlin).Add(-12 * time.Hour)
}

// Plan ist ein geladener Betriebstag samt Routern. Die Router sind nicht nebenläufig nutzbar und liegen deshalb
// in einem Kanal; seine Größe begrenzt gleichzeitige Anfragen und damit den Speicher.
type Plan struct {
	TT      *raptor.Timetable
	Day     time.Time
	Loaded  time.Time
	routers chan *raptor.Router
}

// NewPlan legt n Router für tt an.
func NewPlan(tt *raptor.Timetable, day time.Time, n int) *Plan {
	p := &Plan{TT: tt, Day: day, Loaded: time.Now(), routers: make(chan *raptor.Router, n)}
	for i := 0; i < n; i++ {
		p.routers <- raptor.NewRouter(tt)
	}
	return p
}

// Server beantwortet Anfragen auf dem jeweils aktuellen Plan; Swap tauscht ihn ohne Ausfall.
type Server struct {
	plan      atomic.Pointer[Plan]
	AccessM   float64   // Suchradius für Zugangshalte (Luftlinie)
	Valhalla  *Valhalla // echte Zugangswege; nil = Luftlinie
	MaxTrips  int
	WaitLimit time.Duration  // so lange wartet eine Anfrage höchstens auf einen freien Router
	Live      *rt.Live       // Echtzeit-Feed, nil = nur Fahrplan
	LiveWait  time.Duration  // so lange wartet eine Anfrage höchstens auf einen Echtzeit-Abruf
	Geocode   *geocode.Index // Adresssuche, nil = aus
	MapDir    string         // Kartenstand des Jobs (Ordner mit regionen.json, Glob erlaubt), "" = aus
	DirectM   float64        // reiner Fußweg als Vergleich bis zu dieser Luftlinie, 0 = aus
	DirectMax time.Duration  // … und höchstens so lange (beim gewählten Tempo)
}

// Swap setzt einen neuen Plan; laufende Anfragen rechnen auf dem alten zu Ende.
func (s *Server) Swap(p *Plan) {
	if s.Live != nil {
		s.Live.SetTrips(tripIDs(p.TT))
	}
	s.plan.Store(p)
}

// Current liefert den aktuellen Plan oder nil.
func (s *Server) Current() *Plan { return s.plan.Load() }

// Handler liefert die Routen der API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/route", s.route)
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("POST /v1/departures", s.departures)
	mux.HandleFunc("GET /v1/stops", s.stops)
	mux.HandleFunc("POST /v1/search", s.search)
	mux.HandleFunc("GET /v1/map", s.mapIndex)
	mux.HandleFunc("GET /v1/map/{id}", s.mapFile)
	return mux
}

type point struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

func (p point) ok() bool {
	return p.Lat != nil && p.Lon != nil && *p.Lat >= -90 && *p.Lat <= 90 && *p.Lon >= -180 && *p.Lon <= 180
}

type routeReq struct {
	From  point      `json:"from"`
	To    point      `json:"to"`
	Time  *time.Time `json:"time"`  // Abfahrt frühestens; fehlt sie, gilt jetzt
	Fern  bool       `json:"fern"`  // Fernverkehr zulassen; sonst nur, was das Deutschlandsemesterticket abdeckt
	Debug bool       `json:"debug"` // Zeiten der Valhalla-Aufrufe aufschlüsseln (nur Messung, keine Orte)
	KMH   *float64   `json:"kmh"`   // Gehtempo (App-Einstellung, D-12); fehlt es, gilt BaseKMH
	Art   string     `json:"art"`   // fuss (Standard) | einrad | klapprad | fixie | rennrad | mtb (D-21, D-22); rad = fixie
}

// reichweite: Faktor für Zugangsradius und direkten Weg; schneller unterwegs = weiter, höchstens das Vierfache.
// Mit allen Linien (nicht nur Bahn) höchstens das 2,5-Fache: sonst kämen so viele Bushalte in die Valhalla-Matrix,
// dass sie über 100 ms braucht (Klapprad mit 4× gemessen 111 ms, D-22).
func (q routeReq) reichweite() float64 {
	if q.KMH == nil {
		return 1
	}
	grenze := 2.5
	if arten[q.Art].NurBahn {
		grenze = 4
	}
	return min(grenze, max(1, *q.KMH/BaseKMH))
}

// BaseKMH ist das Tempo, mit dem Fußwege und Zugangswege gerechnet sind (D-12: „gemütlich“).
const BaseKMH = 4.3

// walkScale liefert den Faktor für die Gehzeiten; ok=false bei unplausiblem Tempo.
func (q routeReq) walkScale() (float64, bool) {
	if q.KMH == nil {
		return 1, true
	}
	a, ok := arten[q.Art]
	if k := *q.KMH; ok && k >= a.Min && k <= a.Max {
		return BaseKMH / k, true
	}
	return 0, false
}

type stopOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type legOut struct {
	Mode     string    `json:"mode"` // walk | ride
	From     *stopOut  `json:"from"` // null = Start- bzw. Zielort
	To       *stopOut  `json:"to"`
	Dep      time.Time `json:"dep"`
	Arr      time.Time `json:"arr"`
	Line     string    `json:"line,omitempty"`
	Headsign string    `json:"headsign,omitempty"`
	TripID   string    `json:"trip_id,omitempty"`
	Fern     bool      `json:"fern,omitempty"`  // nicht im Deutschlandticket
	Rad      bool      `json:"rad,omitempty"`   // Fahrrad fährt mit: Fahrradticket nötig (D-21; nicht beim Klapprad)
	Risk     bool      `json:"risk,omitempty"`  // Anschluss an diese Fahrt laut Prognose gefährdet
	Shape    string    `json:"shape,omitempty"` // Fußweg: Wegform (encoded polyline, Genauigkeit 6)
	Stops    []string  `json:"stops,omitempty"` // Fahrt: Halte-IDs vom Ein- bis zum Ausstieg
	liveOut
}

type journeyOut struct {
	Trips int        `json:"trips"`
	Dep   time.Time  `json:"dep"`
	Arr   time.Time  `json:"arr"`
	ArrRT *time.Time `json:"arr_rt,omitempty"` // erwartete Ankunft laut Echtzeit
	RT    bool       `json:"rt,omitempty"`     // mindestens eine Fahrt mit Echtzeit
	Risk  string     `json:"risk,omitempty"`   // "anschluss" (Umstieg gefährdet) | "ausfall" (Fahrt/Halt fällt aus)
	Legs  []legOut   `json:"legs"`
}

type routeResp struct {
	Journeys []journeyOut  `json:"journeys"`
	Ms       float64       `json:"ms"`           // Rechenzeit RAPTOR
	WalkMs   float64       `json:"walk_ms"`      // Zugangswege gesamt (Halte-Suche + Valhalla)
	VhMs     [2]float64    `json:"vh_ms"`        // davon die Valhalla-Aufrufe für Start und Ziel (parallel)
	ShapeMs  float64       `json:"shape_ms"`     // Wegformen der ausgegebenen Fußabschnitte (Valhalla, parallel)
	Vh       *[2]WalkTrace `json:"vh,omitempty"` // nur mit "debug": true
	RT       bool          `json:"rt"`           // Echtzeitstand verfügbar (sonst nur Fahrplan)
}

func fail(w http.ResponseWriter, code int, kind string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": kind})
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := s.Current()
	if p == nil {
		fail(w, http.StatusServiceUnavailable, "fahrplan_laedt")
		return
	}
	var req routeReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	err := dec.Decode(&req)
	scale, okKMH := req.walkScale()
	art, okArt := arten[req.Art]
	if err != nil || !req.From.ok() || !req.To.ok() || !okKMH || !okArt {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	t := time.Now()
	if req.Time != nil {
		t = *req.Time
	}
	base := DayBase(p.Day)
	dep := t.Sub(base)
	if dep < 0 || dep >= 24*time.Hour {
		fail(w, http.StatusUnprocessableEntity, "anderer_betriebstag")
		return
	}
	tw := time.Now()
	var tr *[2]WalkTrace
	if req.Debug {
		tr = &[2]WalkTrace{}
	}
	liveC := make(chan *rt.Snapshot, 1) // Echtzeit parallel zu den Zugangswegen holen
	go func() { liveC <- s.live(r.Context()) }()
	directC := make(chan *journeyOut, 1) // reiner Fußweg, ebenfalls parallel
	weit, kmh := req.reichweite(), BaseKMH/scale
	var ks []Kosten
	if s.Valhalla != nil {
		ks = art.kosten(s.Valhalla.KMH, kmh, scale)
	}
	go func() { directC <- s.directWalk(r.Context(), req.From, req.To, t, scale, weit, ks) }()
	from, to, vh := s.access(r.Context(), p.TT, req.From, req.To, weit, scale, art.NurBahn, ks, tr)
	walkMs := float64(time.Since(tw).Microseconds()) / 1000
	if len(from) == 0 || len(to) == 0 {
		if d := <-directC; d != nil { // keine Haltestelle, aber zu Fuß erreichbar
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			json.NewEncoder(w).Encode(routeResp{Journeys: []journeyOut{*d}, WalkMs: walkMs, VhMs: vh, Vh: tr})
			return
		}
		fail(w, http.StatusUnprocessableEntity, "keine_haltestelle_in_reichweite")
		return
	}
	var rt *raptor.Router
	select {
	case rt = <-p.routers:
	case <-time.After(s.WaitLimit):
		fail(w, http.StatusServiceUnavailable, "ausgelastet")
		return
	case <-r.Context().Done():
		return
	}
	rt.NoFern, rt.WalkScale, rt.NurBahn = !req.Fern, scale, art.NurBahn
	t0 := time.Now()
	res := rt.Query(int32(dep/time.Second), from, to, s.MaxTrips)
	ms := float64(time.Since(t0).Microseconds()) / 1000
	p.routers <- rt
	snap := <-liveC
	out := routeResp{Journeys: []journeyOut{}, Ms: ms, WalkMs: walkMs, VhMs: vh, Vh: tr, RT: snap != nil}
	bus := make([]bool, len(res.Journeys)) // Verbindung nutzt einen Bus (für BusMalus)
	for k, j := range res.Journeys {
		for _, l := range j.Legs {
			bus[k] = bus[k] || l.Kind == raptor.Ride && !p.TT.Routes[l.Route].Bahn
		}
		jo := convert(p.TT, base, j)
		for i := range jo.Legs {
			jo.Legs[i].Rad = art.NurBahn && jo.Legs[i].Mode == "ride"
		}
		p.annotate(snap, j, &jo)
		rideStops(p.TT, j, &jo)
		out.Journeys = append(out.Journeys, jo)
	}
	ts := time.Now()
	s.shapes(r.Context(), p.TT, req.From, req.To, res.Journeys, out.Journeys, ks)
	out.ShapeMs = since(ts)
	if d := <-directC; d != nil && art.Rad {
		out.Journeys = radVergleich(out.Journeys, bus, *d, art.BusMalus)
		out.Journeys = append([]journeyOut{*d}, out.Journeys...)
	} else if d != nil {
		out.Journeys = append(out.Journeys, *d) // als Vergleich hinten, die App wählt zuerst die erste Verbindung
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}

// radVergleich behält beim Rad nur die Verbindungen, die gegenüber dem direkten Weg d etwas bringen: RAPTOR-Wege
// ohne Fahrt sind doppelt, solche mit Bus (bus[k]) müssen mindestens malus früher ankommen.
func radVergleich(js []journeyOut, bus []bool, d journeyOut, malus time.Duration) []journeyOut {
	var out []journeyOut
	for k, j := range js {
		if j.Trips > 0 && !(bus[k] && j.Arr.After(d.Arr.Add(-malus))) {
			out = append(out, j)
		}
	}
	return out
}

// convert übersetzt einen Weg in die Ausgabe. Der Zugangsweg wird ans Ende verschoben, sodass man so spät wie
// möglich losgeht und nicht an der Haltestelle wartet.
func convert(tt *raptor.Timetable, base time.Time, j raptor.Journey) journeyOut {
	legs := append([]raptor.Leg(nil), j.Legs...)
	if len(legs) > 1 && legs[0].Kind == raptor.Walk && legs[1].Kind == raptor.Ride {
		shift := legs[1].Dep - legs[0].Arr
		legs[0].Dep += shift
		legs[0].Arr += shift
	}
	at := func(sec int32) time.Time { return base.Add(time.Duration(sec) * time.Second) }
	stop := func(i int32) *stopOut {
		if i < 0 {
			return nil
		}
		return &stopOut{tt.Stops[i].ID, tt.Stops[i].Name}
	}
	out := journeyOut{Trips: j.Trips, Arr: at(j.Arr)}
	for _, l := range legs {
		lo := legOut{Mode: "walk", From: stop(l.From), To: stop(l.To), Dep: at(l.Dep), Arr: at(l.Arr)}
		if l.Kind == raptor.Ride {
			ti := tt.Routes[l.Route].Trips[l.Trip]
			lo.Mode, lo.Line, lo.Headsign, lo.TripID, lo.Fern = "ride", ti.Line, ti.Headsign, ti.ID, tt.Routes[l.Route].Fern
		}
		out.Legs = append(out.Legs, lo)
	}
	if len(out.Legs) > 0 {
		out.Dep = out.Legs[0].Dep
	}
	return out
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	p := s.Current()
	w.Header().Set("Content-Type", "application/json")
	if p == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"ready": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ready": true, "day": p.Day.Format("2006-01-02"), "loaded": p.Loaded.Format(time.RFC3339),
		"stops": len(p.TT.Stops), "routes": len(p.TT.Routes), "rt": s.liveStatus(),
	})
}

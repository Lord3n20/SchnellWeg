package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
	"github.com/Lord3n20/SchnellWeg/server/internal/rt"
)

// rawID entfernt das Feed-Präfix ("0:") einer Halte- oder Fahrt-ID; der Echtzeit-Feed kennt nur die rohen IDs.
func rawID(id string) string {
	if _, r, ok := strings.Cut(id, ":"); ok {
		return r
	}
	return id
}

// tripIDs sammelt die rohen Fahrt-IDs eines Fahrplans für den Echtzeit-Filter.
func tripIDs(tt *raptor.Timetable) map[string]struct{} {
	m := map[string]struct{}{}
	for _, r := range tt.Routes {
		for _, t := range r.Trips {
			m[rawID(t.ID)] = struct{}{}
		}
	}
	return m
}

// live holt den Echtzeitstand für eine Anfrage (nil = nur Fahrplan). Wartet höchstens das Zeitbudget.
func (s *Server) live(ctx context.Context) *rt.Snapshot {
	if s.Live == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.LiveWait)
	defer cancel()
	return s.Live.Get(ctx)
}

// tripLive liefert die Prognose einer Fahrt je Halt der Route (nil = keine Echtzeit für diese Fahrt).
type tripLive struct {
	canceled       bool
	arr, dep       []int32
	known, skipped []bool
}

func (p *Plan) tripLive(snap *rt.Snapshot, ri, ti int32) *tripLive {
	if snap == nil {
		return nil
	}
	r := &p.TT.Routes[ri]
	info := r.Trips[ti]
	day := p.Day
	if info.PrevDay {
		day = day.AddDate(0, 0, -1)
	}
	t := snap.Trips[rt.TripKey{ID: rawID(info.ID), Date: day.Format("20060102")}]
	if t == nil {
		return nil
	}
	stops := make([]string, len(r.Stops))
	for i, s := range r.Stops {
		stops[i] = rawID(p.TT.Stops[s].ID)
	}
	tl := &tripLive{canceled: t.Canceled}
	tl.arr, tl.dep, tl.known, tl.skipped = t.Delays(stops)
	return tl
}

// liveOut sind die Echtzeitfelder einer Abfahrt oder eines Fahrtabschnitts; ohne Prognose bleiben sie leer.
type liveOut struct {
	DepRT    *time.Time `json:"dep_rt,omitempty"` // erwartete Abfahrt
	ArrRT    *time.Time `json:"arr_rt,omitempty"` // erwartete Ankunft
	Canceled bool       `json:"canceled,omitempty"`
	Skipped  bool       `json:"skipped,omitempty"` // Halt entfällt
}

// annotate ergänzt die Fahrtabschnitte um Prognosen und markiert gefährdete Umstiege: Kommt die Fahrt laut
// Prognose später an, als die nächste (prognostiziert) abfährt, abzüglich Fußweg, gilt der Anschluss als
// gefährdet. Gerechnet wird weiter auf dem Fahrplan (D-15); die App zeigt die Warnung.
func (p *Plan) annotate(snap *rt.Snapshot, j raptor.Journey, out *journeyOut) {
	if snap == nil {
		return
	}
	base := DayBase(p.Day)
	at := func(sec int32) *time.Time { t := base.Add(time.Duration(sec) * time.Second); return &t }
	var lastArr int32 = -1 // prognostizierte Ankunft der vorigen Fahrt
	var walk int32         // Fußweg seitdem
	for i, l := range j.Legs {
		lo := &out.Legs[i]
		if l.Kind != raptor.Ride {
			walk += l.Arr - l.Dep
			continue
		}
		r := &p.TT.Routes[l.Route]
		from, to := stopPos(r, l.From, 0), int32(-1)
		if from >= 0 {
			to = stopPos(r, l.To, from+1)
		}
		dep, arr := l.Dep, l.Arr
		if tl := p.tripLive(snap, l.Route, l.Trip); tl != nil && from >= 0 && to >= 0 {
			out.RT = true
			lo.Canceled = tl.canceled
			lo.Skipped = tl.skipped[from] || tl.skipped[to]
			if tl.known[from] {
				dep += tl.dep[from]
				lo.DepRT = at(dep)
			}
			if tl.known[to] {
				arr += tl.arr[to]
				lo.ArrRT = at(arr)
			}
		}
		if lo.Canceled || lo.Skipped {
			out.Risk = "ausfall"
		} else if lastArr >= 0 && lastArr+walk > dep && out.Risk == "" {
			out.Risk = "anschluss"
			lo.Risk = true
		}
		lastArr, walk = arr, 0
	}
	// erwartete Ankunft am Ziel: letzte Fahrt + restlicher Fußweg
	if lastArr >= 0 && out.RT {
		t := *at(lastArr + walk)
		out.ArrRT = &t
	}
}

// stopPos sucht Halt s in der Route ab Position from (Routen können einen Halt mehrfach anfahren).
func stopPos(r *raptor.Route, s, from int32) int32 {
	for i := from; i < int32(len(r.Stops)); i++ {
		if r.Stops[i] == s {
			return i
		}
	}
	return -1
}

type depReq struct {
	Stops []string   `json:"stops"` // Halte-IDs wie in /v1/stops
	Time  *time.Time `json:"time"`
	N     int        `json:"n"` // Abfahrten je Halt, Standard 3
}

type depOut struct {
	Line     string    `json:"line"`
	Headsign string    `json:"headsign,omitempty"`
	Dep      time.Time `json:"dep"`
	TripID   string    `json:"trip_id"`
	Fern     bool      `json:"fern,omitempty"`
	liveOut
	delay int32 // zum Sortieren
}

const (
	maxDepStops = 30
	depWindow   = 2 * time.Hour
	depBack     = 30 * time.Minute // verspätete Fahrten mit früherer Planzeit noch zeigen
)

// departures liefert je Halt die nächsten Abfahrten (Fahrplan + Echtzeit). Der Server sieht nur Halte-IDs,
// nie einen Standort: welche Halte in der Nähe liegen, bestimmt die App aus /v1/stops.
func (s *Server) departures(w http.ResponseWriter, r *http.Request) {
	p := s.Current()
	if p == nil {
		fail(w, http.StatusServiceUnavailable, "fahrplan_laedt")
		return
	}
	var req depReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || len(req.Stops) == 0 || len(req.Stops) > maxDepStops || req.N < 0 || req.N > 10 {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	if req.N == 0 {
		req.N = 3
	}
	now := time.Now()
	if req.Time != nil {
		now = *req.Time
	}
	base := DayBase(p.Day)
	t0 := int32(now.Sub(base) / time.Second)
	if t0 < 0 || t0 >= 24*3600 {
		fail(w, http.StatusUnprocessableEntity, "anderer_betriebstag")
		return
	}
	snap := s.live(r.Context())
	tt := p.TT
	out := map[string][]depOut{}
	for _, id := range req.Stops {
		si, ok := tt.StopIndex[id]
		if !ok {
			out[id] = nil // unbekannt (anderer Fahrplanstand) → App lädt /v1/stops neu
			continue
		}
		var list []depOut
		for k, ri := range tt.StopRoutes[si] {
			rr := &tt.Routes[ri]
			pos := tt.StopPos[si][k]
			if int(pos) == len(rr.Stops)-1 {
				continue // Endhalt: keine Abfahrt
			}
			n := len(rr.Stops)
			for ti := 0; ti < rr.NTrips; ti++ {
				dep := rr.Dep[ti*n+int(pos)]
				if dep < t0-int32(depBack/time.Second) || dep > t0+int32(depWindow/time.Second) {
					continue
				}
				info := rr.Trips[ti]
				d := depOut{Line: info.Line, Headsign: info.Headsign, Dep: base.Add(time.Duration(dep) * time.Second),
					TripID: info.ID, Fern: rr.Fern}
				exp := dep
				if tl := p.tripLive(snap, ri, int32(ti)); tl != nil {
					d.Canceled, d.Skipped = tl.canceled, tl.skipped[pos]
					if tl.known[pos] {
						exp += tl.dep[pos]
						e := base.Add(time.Duration(exp) * time.Second)
						d.DepRT, d.delay = &e, tl.dep[pos]
					}
				}
				if exp < t0 && !d.Canceled {
					continue // schon weg
				}
				if dep < t0 && d.DepRT == nil {
					continue // Planzeit vorbei, keine Prognose
				}
				list = append(list, d)
			}
		}
		sort.Slice(list, func(a, b int) bool {
			ea, eb := list[a].Dep.Add(time.Duration(list[a].delay)*time.Second), list[b].Dep.Add(time.Duration(list[b].delay)*time.Second)
			return ea.Before(eb)
		})
		if len(list) > req.N {
			list = list[:req.N]
		}
		out[id] = list
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"departures": out, "rt": snap != nil})
}

// stops liefert alle Halte mit Abfahrten als [id, name, lat, lon] für die Halte-Datenbank der App. Ändert sich
// nur mit dem Fahrplan; ETag = Ladezeitpunkt des Fahrplans.
func (s *Server) stops(w http.ResponseWriter, r *http.Request) {
	p := s.Current()
	if p == nil {
		fail(w, http.StatusServiceUnavailable, "fahrplan_laedt")
		return
	}
	etag := `"` + p.Loaded.UTC().Format("20060102T150405") + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rows := make([][4]any, 0, len(p.TT.Stops))
	for i, st := range p.TT.Stops {
		if len(p.TT.StopRoutes[i]) == 0 {
			continue
		}
		rows = append(rows, [4]any{st.ID, st.Name, round6(st.Lat), round6(st.Lon)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"day": p.Day.Format("2006-01-02"), "stops": rows})
}

func round6(x float64) float64 { return float64(int64(x*1e6+0.5*sign(x))) / 1e6 }

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

// liveStatus beschreibt den Echtzeitstand für /v1/status, ohne einen Abruf auszulösen.
func (s *Server) liveStatus() map[string]any {
	if s.Live == nil {
		return map[string]any{"on": false}
	}
	m := map[string]any{"on": true, "error": s.Live.LastErr()}
	if snap := s.Live.Peek(); snap != nil {
		m["fetched"] = snap.Fetched.Format(time.RFC3339)
		m["feed"] = snap.FeedTime.Format(time.RFC3339)
		m["trips"] = len(snap.Trips)
	}
	return m
}

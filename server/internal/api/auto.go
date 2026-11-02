package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"time"
)

// autoZeit: lange Autorouten brauchen weit mehr als ein Zugangsweg (Aachen–Köln gemessen ~360 ms, D-32).
const autoZeit = 5 * time.Second

// autoFangM: Start und Ziel dürfen so weit neben einer Straße liegen. Mit Valhallas Standard (35 km) kam für Ziele
// außerhalb der Tiles eine Route, die irgendwo am Rand endet (Düsseldorf: 200, aber Ende ~20 km daneben).
const autoFangM = 2000

// autoRoute antwortet mit der Autofahrt Start → Ziel als einziger Verbindung (D-32): Valhalla-Kosten „auto“ ohne
// Fahrplan und ohne Verkehrslage, kmh begrenzt als top_speed. Ohne Weg (auch außerhalb der Tiles) 422 kein_autoweg.
func (s *Server) autoRoute(w http.ResponseWriter, r *http.Request, req routeReq, t time.Time) {
	opts := map[string]any{}
	if req.KMH != nil {
		opts["top_speed"] = *req.KMH
	}
	if req.Meiden != nil {
		req.Meiden.autoOpts(opts)
	}
	ks := []Kosten{{Costing: "auto", Opts: map[string]any{"auto": opts}, Faktor: 1, FangM: autoFangM}}
	if req.Meiden != nil && req.Meiden.Belag > 0 { // meiden, nicht verbieten: liegt das Ziel an einem Feldweg, trotzdem hin
		ohne := maps.Clone(opts)
		ohne["exclude_unpaved"] = true
		ks = append([]Kosten{{Costing: "auto", Opts: map[string]any{"auto": ohne}, Faktor: 1, FangM: autoFangM}}, ks...)
	}
	s.tuerZuTuer(w, r, req, t, ks, "drive", "kein_autoweg", "auto_aus")
}

// tuerZuTuer antwortet mit dem Weg Start → Ziel als einziger Verbindung, ohne Fahrplan und ohne Längengrenze: mit der
// ersten Kostenart aus ks, die einen Weg findet. Auto (D-32) und Profile, die den ÖV meiden (D-34).
func (s *Server) tuerZuTuer(w http.ResponseWriter, r *http.Request, req routeReq, t time.Time, ks []Kosten, mode, keinWeg, aus string) {
	if s.Valhalla == nil {
		fail(w, http.StatusServiceUnavailable, aus)
		return
	}
	t0 := time.Now()
	var weg WalkRoute
	err := errMatrix
	for _, k := range ks {
		if weg, err = s.Valhalla.route(r.Context(), *req.From.Lat, *req.From.Lon, *req.To.Lat, *req.To.Lon, k, autoZeit); err == nil {
			break
		}
	}
	if errors.Is(err, errMatrix) {
		fail(w, http.StatusUnprocessableEntity, keinWeg)
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, aus)
		return
	}
	var lage []meldungOut
	lageOK := false
	if mode == "drive" {
		kmh := 0.0
		if req.KMH != nil {
			kmh = *req.KMH
		}
		lage, lageOK = s.fahrzeitAnpassen(r.Context(), &weg, kmh)
	}
	dauer := time.Duration(weg.Sec+0.5) * time.Second
	dep := t.Truncate(time.Second)
	arr := dep.Add(dauer)
	if req.Ankunft { // so losfahren, dass man pünktlich ankommt
		dep, arr = dep.Add(-dauer), dep
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(routeResp{ShapeMs: since(t0), Journeys: []journeyOut{{Dep: dep, Arr: arr, Verkehr: lage, VerkehrOK: lageOK,
		Legs: []legOut{{Mode: mode, Dep: dep, Arr: arr, Shape: weg.Shape}}}}})
}

type vhTraceResp struct {
	Shape string `json:"shape"`
	Edges []struct {
		Length     float64 `json:"length"` // km
		Speed      float64 `json:"speed"`  // km/h
		SpeedLimit any     `json:"speed_limit"`
		RoadClass  string  `json:"road_class"`
		Von        int     `json:"begin_shape_index"`
		Bis        int     `json:"end_shape_index"`
	} `json:"edges"`
}

// kanten holt die Straßenabschnitte der Route; bei Fehlern leer.
func (v *Valhalla) kanten(ctx context.Context, shape string) (t vhTraceResp) {
	body, _ := json.Marshal(map[string]any{"encoded_polyline": shape, "shape_match": "edge_walk", "costing": "auto", "units": "km",
		"filters": map[string]any{"action": "include", "attributes": []string{"shape", "edge.length", "edge.speed", "edge.speed_limit",
			"edge.road_class", "edge.begin_shape_index", "edge.end_shape_index"}}})
	ctx, cancel := context.WithTimeout(ctx, autoZeit)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL+"/trace_attributes", bytes.NewReader(body))
	if err != nil {
		return t
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := v.Client.Do(hr)
	if err != nil {
		return t
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&t) != nil {
		return vhTraceResp{}
	}
	return t
}

// fahrzeitAnpassen rechnet Tempo und Verkehrslage in die Fahrzeit ein (D-35, D-38). Auf Autobahn ohne Limit gilt kmh, denn
// Valhalla rechnet dort pauschal 105 km/h und top_speed senkt nur; Limits gelten weiter, in Baustellen auch deren Limit, dazu kommt
// die gemeldete Verzögerung. Fehlen Kanten oder Meldungen, bleibt der jeweilige Teil weg: dann nur ungenauer, kein Fehler.
func (s *Server) fahrzeitAnpassen(ctx context.Context, weg *WalkRoute, kmh float64) ([]meldungOut, bool) {
	route := dekodieren(weg.Shape)
	var auf []Meldung
	ok := false
	if s.Verkehr != nil {
		var alle []Meldung
		alle, ok = s.Verkehr.Holen(ctx)
		auf = aufDerRoute(alle, route)
	}
	t := s.Valhalla.kanten(ctx, weg.Shape)
	form := route
	if t.Shape != "" {
		form = dekodieren(t.Shape)
	}
	for _, e := range t.Edges {
		if e.Speed <= 0 {
			continue
		}
		v := e.Speed
		if e.RoadClass == "motorway" && e.SpeedLimit == "unlimited" && kmh > v {
			v = kmh
		}
		if e.Von >= 0 && e.Bis > e.Von && e.Bis < len(form) {
			mitte := form[(e.Von+e.Bis)/2]
			rx, ry := richtung(form[e.Von], form[e.Bis])
			for _, m := range auf {
				if m.MaxKmh > 0 && float64(m.MaxKmh) < v {
					if _, drin := amWeg(mitte, rx, ry, m.Form); drin {
						v = float64(m.MaxKmh)
					}
				}
			}
		}
		weg.Sec -= e.Length * 3600 * (1/e.Speed - 1/v)
	}
	var out []meldungOut
	for _, m := range auf {
		weg.Sec += float64(m.VerzMin) * 60
		out = append(out, meldungOut{Art: m.Art, Titel: m.Titel, Richtung: m.Richtung, VerzMin: m.VerzMin, Kmh: m.Kmh, MaxKmh: m.MaxKmh,
			Gesperrt: m.Gesperrt, Lat: m.Form[0].lat, Lon: m.Form[0].lon})
	}
	return out, ok
}

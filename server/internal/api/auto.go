package api

import (
	"encoding/json"
	"errors"
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
	k := Kosten{Costing: "auto", Opts: map[string]any{"auto": opts}, Faktor: 1, FangM: autoFangM}
	s.tuerZuTuer(w, r, req, t, []Kosten{k}, "drive", "kein_autoweg", "auto_aus")
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
	dauer := time.Duration(weg.Sec+0.5) * time.Second
	dep := t.Truncate(time.Second)
	arr := dep.Add(dauer)
	if req.Ankunft { // so losfahren, dass man pünktlich ankommt
		dep, arr = dep.Add(-dauer), dep
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(routeResp{ShapeMs: since(t0), Journeys: []journeyOut{{Dep: dep, Arr: arr,
		Legs: []legOut{{Mode: mode, Dep: dep, Arr: arr, Shape: weg.Shape}}}}})
}

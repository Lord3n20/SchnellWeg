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
	if s.Valhalla == nil {
		fail(w, http.StatusServiceUnavailable, "auto_aus")
		return
	}
	opts := map[string]any{}
	if req.KMH != nil {
		opts["top_speed"] = *req.KMH
	}
	t0 := time.Now()
	k := Kosten{"auto", map[string]any{"auto": opts}, 1}
	weg, err := s.Valhalla.route(r.Context(), *req.From.Lat, *req.From.Lon, *req.To.Lat, *req.To.Lon, k, autoZeit)
	if errors.Is(err, errMatrix) {
		fail(w, http.StatusUnprocessableEntity, "kein_autoweg")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "auto_aus")
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
		Legs: []legOut{{Mode: "drive", Dep: dep, Arr: arr, Shape: weg.Shape}}}}})
}

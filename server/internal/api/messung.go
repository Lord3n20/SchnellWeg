package api

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"time"
)

// Messgang (D-44): Die App schickt nach dem Messen die gefahrene Strecke samt Metern und Sekunden, der Server gleicht sie mit der
// Karte ab und antwortet, wie viele Meter und Sekunden auf welcher Steigung und welchem Belag lagen. Die Punkte werden nicht
// gespeichert und nicht protokolliert (auch nicht in Fehlern).
const (
	messMaxPunkte = 16000 // Valhallas Grenze für eine Spur
	messMaxBytes  = 2 << 20
	messZeit      = 15 * time.Second
)

type messReq struct {
	Punkte [][4]float64 `json:"punkte"` // Breite, Länge, Meter und Sekunden seit dem vorigen Punkt
}

type messStueck struct {
	G int     `json:"g"` // Steigung in %, gerundet
	C uint8   `json:"c"` // Belagklasse, 0 = unauffällig
	M float64 `json:"m"`
	S float64 `json:"s"`
}

type messAbgleich struct {
	Edges []struct {
		WayID    uint64  `json:"way_id"`
		Steigung float64 `json:"weighted_grade"`
	} `json:"edges"`
	Punkte []struct {
		Kante int64  `json:"edge_index"`
		Typ   string `json:"type"`
	} `json:"matched_points"`
}

func (s *Server) messung(w http.ResponseWriter, r *http.Request) {
	var q messReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, messMaxBytes)).Decode(&q); err != nil || len(q.Punkte) < 2 || len(q.Punkte) > messMaxPunkte {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	if s.Valhalla == nil {
		fail(w, http.StatusServiceUnavailable, "weg_aus")
		return
	}
	a, err := s.Valhalla.abgleichen(r.Context(), q.Punkte)
	if err != nil || len(a.Punkte) != len(q.Punkte) {
		fail(w, http.StatusUnprocessableEntity, "spur_passt_nicht")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"stuecke": messAuswerten(q.Punkte, a, s.Valhalla.Belag.Aktuell())})
}

func (v *Valhalla) abgleichen(ctx context.Context, punkte [][4]float64) (a messAbgleich, err error) {
	form := make([]loc, len(punkte))
	for i, p := range punkte {
		form[i] = loc{p[0], p[1]}
	}
	body, _ := json.Marshal(map[string]any{"shape": form, "shape_match": "map_snap", "costing": "pedestrian", "units": "km",
		"filters": map[string]any{"action": "include", "attributes": []string{"edge.way_id", "edge.weighted_grade", "matched.edge_index", "matched.type"}}})
	ctx, cancel := context.WithTimeout(ctx, messZeit)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL+"/trace_attributes", bytes.NewReader(body))
	if err != nil {
		return a, err
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := v.Client.Do(hr)
	if err != nil {
		return a, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return a, errMatrix
	}
	return a, json.NewDecoder(res.Body).Decode(&a)
}

// messAuswerten summiert Meter und Sekunden je Steigung und Belag. Punkte ohne Treffer auf der Karte zählen nicht.
func messAuswerten(punkte [][4]float64, a messAbgleich, d Belagdaten) []messStueck {
	type schluessel struct {
		g int
		c uint8
	}
	summen := map[schluessel]*messStueck{}
	for i, p := range a.Punkte {
		if p.Typ == "unmatched" || p.Kante < 0 || p.Kante >= int64(len(a.Edges)) || punkte[i][3] <= 0 {
			continue
		}
		e := a.Edges[p.Kante]
		k := schluessel{int(math.Round(e.Steigung)), d.klasse(e.WayID)}
		if summen[k] == nil {
			summen[k] = &messStueck{G: k.g, C: k.c}
		}
		summen[k].M += punkte[i][2]
		summen[k].S += punkte[i][3]
	}
	out := make([]messStueck, 0, len(summen))
	for _, x := range summen {
		out = append(out, *x)
	}
	slices.SortFunc(out, func(a, b messStueck) int { return cmp.Or(cmp.Compare(a.C, b.C), cmp.Compare(a.G, b.G)) })
	return out
}

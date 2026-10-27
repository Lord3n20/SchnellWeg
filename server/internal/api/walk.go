package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Lord3n20/RWaysTH/server/internal/raptor"
)

// Valhalla rechnet Zugangs- und Abgangswege (erste/letzte Meile) als echte Fußwege. Die Kandidaten kommen wie
// bisher aus Luftlinie + PerRoute; Valhalla ersetzt nur ihre Gehzeiten. Fällt Valhalla aus oder liegt der Ort
// außerhalb der Tiles, bleibt es für diese Seite bei der Luftlinie (D-12).
type Valhalla struct {
	URL     string        // z. B. http://91c9b2fc-valhalla:8002
	KMH     float64       // Gehtempo, Standard 4,3
	MaxM    float64       // längere echte Wege entfallen (Umweg um Gleise, Autobahn …)
	Timeout time.Duration // je Matrix
	Client  *http.Client
}

type loc struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type matrixReq struct {
	Sources        []loc          `json:"sources"`
	Targets        []loc          `json:"targets"`
	Costing        string         `json:"costing"`
	CostingOptions map[string]any `json:"costing_options"`
}

type matrixResp struct {
	Rows [][]struct {
		Time     *float64 `json:"time"`
		Distance *float64 `json:"distance"` // km
	} `json:"sources_to_targets"`
}

var errMatrix = errors.New("valhalla: unerwartete Antwort")

// Walk ersetzt die Gehzeiten in acc durch echte Fußwege zwischen (lat, lon) und den Halten. toStops: Weg vom
// Ort zu den Halten (Zugang), sonst von den Halten zum Ort (Abgang). Halte ohne Weg oder mit zu langem Weg
// entfallen.
func (v *Valhalla) Walk(ctx context.Context, tt *raptor.Timetable, lat, lon float64, acc []raptor.Access, toStops bool) ([]raptor.Access, error) {
	if len(acc) == 0 {
		return acc, nil
	}
	here := []loc{{lat, lon}}
	stops := make([]loc, len(acc))
	for i, a := range acc {
		stops[i] = loc{tt.Stops[a.Stop].Lat, tt.Stops[a.Stop].Lon}
	}
	req := matrixReq{Sources: here, Targets: stops, Costing: "pedestrian",
		CostingOptions: map[string]any{"pedestrian": map[string]float64{"walking_speed": v.KMH}}}
	if !toStops {
		req.Sources, req.Targets = stops, here
	}
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL+"/sources_to_targets", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := v.Client.Do(hr)
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(io.Discard, res.Body) // bis EOF lesen, sonst schließt Go die Verbindung statt sie wiederzuverwenden
		res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return nil, errMatrix // z. B. Ort außerhalb der Tiles
	}
	var m matrixResp
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, err
	}
	// Zugang: 1 Zeile × n Halte; Abgang: n Zeilen × 1 Ort. Die Matrix wird in jedem Fall flach gelesen.
	var cells []struct{ t, d *float64 }
	for _, row := range m.Rows {
		for _, c := range row {
			cells = append(cells, struct{ t, d *float64 }{c.Time, c.Distance})
		}
	}
	if len(cells) != len(acc) {
		return nil, errMatrix
	}
	var out []raptor.Access
	for i, a := range acc {
		c := cells[i]
		if c.t == nil || c.d == nil || *c.d*1000 > v.MaxM {
			continue
		}
		out = append(out, raptor.Access{Stop: a.Stop, Sec: int32(*c.t + 0.5)})
	}
	return out, nil
}

// access liefert Zugangs- und Abgangshalte; mit Valhalla beide Matrizen gleichzeitig. vh: Dauer der beiden
// Valhalla-Aufrufe in ms (Start, Ziel) für die Messung.
func (s *Server) access(ctx context.Context, tt *raptor.Timetable, from, to point) (fa, ta []raptor.Access, vh [2]float64) {
	fa = tt.PerRoute(tt.Near(*from.Lat, *from.Lon, s.AccessM))
	ta = tt.PerRoute(tt.Near(*to.Lat, *to.Lon, s.AccessM))
	if s.Valhalla == nil {
		return fa, ta, vh
	}
	walk := func(p point, acc []raptor.Access, toStops bool, ms *float64) []raptor.Access {
		t0 := time.Now()
		defer func() { *ms = float64(time.Since(t0).Microseconds()) / 1000 }()
		if r, err := s.Valhalla.Walk(ctx, tt, *p.Lat, *p.Lon, acc, toStops); err == nil {
			return r
		}
		return acc
	}
	done := make(chan struct{})
	go func() {
		fa = walk(from, fa, true, &vh[0])
		close(done)
	}()
	ta = walk(to, ta, false, &vh[1])
	<-done
	return fa, ta, vh
}

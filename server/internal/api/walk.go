package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
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
	// Client ohne Keep-Alive: Valhallas HTTP-Server wird auf wiederverwendeten Verbindungen mit jeder Anfrage
	// langsamer (gemessen 15 → 27 ms nach 10 Anfragen); ein neuer Verbindungsaufbau kostet < 1 ms.
	Client *http.Client
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

// WalkTrace schlüsselt einen Valhalla-Aufruf zeitlich auf (ms), nur für die Messung ("debug": true).
type WalkTrace struct {
	N     int     `json:"n"`     // Halte in der Matrix
	DNS   float64 `json:"dns"`   // Namensauflösung
	Conn  float64 `json:"conn"`  // Verbindungsaufbau
	TTFB  float64 `json:"ttfb"`  // bis zum ersten Antwortbyte (ab Start)
	Total float64 `json:"total"` // bis die Antwort gelesen ist
}

func since(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000 }

// Walk ersetzt die Gehzeiten in acc durch echte Fußwege zwischen (lat, lon) und den Halten. toStops: Weg vom
// Ort zu den Halten (Zugang), sonst von den Halten zum Ort (Abgang). Halte ohne Weg oder mit zu langem Weg
// entfallen.
//
// tr (darf nil sein) erhält die Zeiten des Aufrufs.
func (v *Valhalla) Walk(ctx context.Context, tt *raptor.Timetable, lat, lon float64, acc []raptor.Access, toStops bool, tr *WalkTrace) ([]raptor.Access, error) {
	if len(acc) == 0 {
		return acc, nil
	}
	t0 := time.Now()
	if tr != nil {
		tr.N = len(acc)
		var d0, c0 time.Time
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			DNSStart:             func(httptrace.DNSStartInfo) { d0 = time.Now() },
			DNSDone:              func(httptrace.DNSDoneInfo) { tr.DNS = since(d0) },
			ConnectStart:         func(_, _ string) { c0 = time.Now() },
			ConnectDone:          func(_, _ string, _ error) { tr.Conn = since(c0) },
			GotFirstResponseByte: func() { tr.TTFB = since(t0) },
		})
		defer func() { tr.Total = since(t0) }()
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
	defer res.Body.Close()
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
// Valhalla-Aufrufe in ms (Start, Ziel) für die Messung; tr (nil = aus) schlüsselt sie weiter auf.
func (s *Server) access(ctx context.Context, tt *raptor.Timetable, from, to point, tr *[2]WalkTrace) (fa, ta []raptor.Access, vh [2]float64) {
	fa = tt.PerRoute(tt.Near(*from.Lat, *from.Lon, s.AccessM))
	ta = tt.PerRoute(tt.Near(*to.Lat, *to.Lon, s.AccessM))
	if s.Valhalla == nil {
		return fa, ta, vh
	}
	walk := func(p point, acc []raptor.Access, toStops bool, k int) []raptor.Access {
		t0 := time.Now()
		defer func() { vh[k] = since(t0) }()
		var t *WalkTrace
		if tr != nil {
			t = &tr[k]
		}
		if r, err := s.Valhalla.Walk(ctx, tt, *p.Lat, *p.Lon, acc, toStops, t); err == nil {
			return r
		}
		return acc
	}
	done := make(chan struct{})
	go func() {
		fa = walk(from, fa, true, 0)
		close(done)
	}()
	ta = walk(to, ta, false, 1)
	<-done
	return fa, ta, vh
}

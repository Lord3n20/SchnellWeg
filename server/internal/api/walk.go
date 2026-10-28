package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"slices"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
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

// Walk ersetzt die Gehzeiten in acc durch echte Wege (Kostenart k, Zeiten × k.Faktor) zwischen (lat, lon) und den Halten. toStops: Weg vom
// Ort zu den Halten (Zugang), sonst von den Halten zum Ort (Abgang). Halte ohne Weg oder mit zu langem Weg
// entfallen.
//
// maxM ersetzt MaxM, wenn größer (erweiterte Suche); die Matrix darf dann entsprechend länger brauchen.
// tr (darf nil sein) erhält die Zeiten des Aufrufs.
func (v *Valhalla) Walk(ctx context.Context, tt *raptor.Timetable, lat, lon float64, acc []raptor.Access, toStops bool, k Kosten, maxM float64, tr *WalkTrace) ([]raptor.Access, error) {
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
	req := matrixReq{Sources: here, Targets: stops, Costing: k.Costing, CostingOptions: k.Opts}
	if !toStops {
		req.Sources, req.Targets = stops, here
	}
	body, _ := json.Marshal(req)
	maxM = max(maxM, v.MaxM)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(float64(v.Timeout)*min(8, maxM/v.MaxM)))
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
		if c.t == nil || c.d == nil || *c.d*1000 > maxM {
			continue
		}
		out = append(out, raptor.Access{Stop: a.Stop, Sec: int32(*c.t*k.Faktor + 0.5)})
	}
	return out, nil
}

// access liefert Zugangs- und Abgangshalte; mit Valhalla beide Seiten gleichzeitig. vh: Dauer der beiden
// Valhalla-Aufrufe in ms (Start, Ziel) für die Messung; tr (nil = aus) schlüsselt sie weiter auf.
// weit vergrößert den Suchradius (schnellere Fortbewegung, D-21); nurBahn lässt nur Halte mit Schienenverkehr zu.
// Die Zeiten gelten fürs gewählte Tempo: Luftlinie × scale, Valhalla je Kostenart (die erste, die antwortet).
//
// Bleibt auf einer Seite kein Halt übrig, sucht sie mit doppeltem Radius weiter bis FarM (D-25): auch auf dem
// Land findet sich so der nächste Halt, nur mit langem Fußweg. Dann zählen nur die nächsten Halte (farSlack).
func (s *Server) access(ctx context.Context, tt *raptor.Timetable, from, to point, weit, scale float64, nurBahn bool, ks []Kosten, tr *[2]WalkTrace) (fa, ta []raptor.Access, vh [2]float64) {
	near := func(p point, r float64) []raptor.Access {
		acc := tt.Near(*p.Lat, *p.Lon, r)
		if nurBahn {
			acc = slices.DeleteFunc(acc, func(a raptor.Access) bool { return !tt.HatBahn(a.Stop) })
		}
		acc = tt.PerRoute(acc)
		if r > s.AccessM*weit {
			acc = nearest(acc)
		}
		for i := range acc {
			acc[i].Sec = int32(float64(acc[i].Sec)*scale + 0.5)
		}
		return acc
	}
	side := func(p point, toStops bool, k int) []raptor.Access {
		t0 := time.Now()
		defer func() { vh[k] = since(t0) }()
		var t *WalkTrace
		if tr != nil {
			t = &tr[k]
		}
		for r := s.AccessM * weit; ; r *= 2 {
			acc := near(p, r)
			if s.Valhalla != nil && len(acc) > 0 {
				for _, kk := range ks {
					if w, err := s.Valhalla.Walk(ctx, tt, *p.Lat, *p.Lon, acc, toStops, kk, 2*r, t); err == nil {
						acc = w
						break
					}
				}
			}
			if len(acc) > 0 || r >= s.FarM*weit || ctx.Err() != nil {
				return acc
			}
		}
	}
	done := make(chan struct{})
	go func() {
		fa = side(from, true, 0)
		close(done)
	}()
	ta = side(to, false, 1)
	<-done
	return fa, ta, vh
}

// farSlack: bei der erweiterten Suche zählen nur Halte bis zu dieser Gehzeit über dem nächsten (Luftlinie), höchstens
// farMax Stück; weiter entfernte bringen kaum etwas und machen die lange Valhalla-Matrix nur langsamer.
const (
	farSlack = 15 * 60
	farMax   = 12
)

func nearest(acc []raptor.Access) []raptor.Access {
	slices.SortFunc(acc, func(a, b raptor.Access) int { return int(a.Sec - b.Sec) })
	n := 0
	for n < len(acc) && n < farMax && acc[n].Sec <= acc[0].Sec+farSlack {
		n++
	}
	return acc[:n]
}

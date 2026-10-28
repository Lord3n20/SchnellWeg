package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Wegformen für die Karte: Fußabschnitte bekommen die Form des echten Wegs von Valhalla (encoded polyline,
// Genauigkeit 6 wie bei Valhalla), Fahrten die Folge ihrer Halte. Formen werden nur für die ausgegebenen
// Verbindungen geholt, nicht für RAPTOR-Kandidaten.

type routeLoc struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type vhRouteReq struct {
	Locations      []routeLoc     `json:"locations"`
	Costing        string         `json:"costing"`
	CostingOptions map[string]any `json:"costing_options"`
	DirectionsType string         `json:"directions_type"`
}

type vhRouteResp struct {
	Trip struct {
		Legs []struct {
			Shape string `json:"shape"`
		} `json:"legs"`
		Summary struct {
			Time   float64 `json:"time"`   // s
			Length float64 `json:"length"` // km
		} `json:"summary"`
	} `json:"trip"`
}

// WalkRoute ist ein Weg von Valhalla: Form, Zeit im gewählten Tempo und Länge.
type WalkRoute struct {
	Shape string
	Sec   float64
	M     float64
}

// Route holt den Weg zwischen zwei Punkten mit der ersten Kostenart aus ks, die einen findet.
func (v *Valhalla) Route(ctx context.Context, aLat, aLon, bLat, bLon float64, ks []Kosten) (r WalkRoute, err error) {
	err = errMatrix
	for _, k := range ks {
		if r, err = v.route(ctx, aLat, aLon, bLat, bLon, k); err == nil {
			return r, nil
		}
	}
	return r, err
}

func (v *Valhalla) route(ctx context.Context, aLat, aLon, bLat, bLon float64, k Kosten) (WalkRoute, error) {
	req := vhRouteReq{Locations: []routeLoc{{aLat, aLon}, {bLat, bLon}}, Costing: k.Costing, CostingOptions: k.Opts,
		DirectionsType: "none"}
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL+"/route", bytes.NewReader(body))
	if err != nil {
		return WalkRoute{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := v.Client.Do(hr)
	if err != nil {
		return WalkRoute{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return WalkRoute{}, errMatrix // kein Weg oder außerhalb der Tiles
	}
	var r vhRouteResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return WalkRoute{}, err
	}
	if len(r.Trip.Legs) != 1 || r.Trip.Legs[0].Shape == "" {
		return WalkRoute{}, errMatrix
	}
	return WalkRoute{r.Trip.Legs[0].Shape, r.Trip.Summary.Time * k.Faktor, r.Trip.Summary.Length * 1000}, nil
}

type pt struct{ lat, lon float64 }

// shapes ergänzt die Fußabschnitte aller Verbindungen um ihre Wegform; gleiche Wege werden einmal geholt, alle
// gleichzeitig. Ohne Valhalla oder bei Fehlern bleibt die Form leer (die App zeichnet dann gerade).
func (s *Server) shapes(ctx context.Context, tt *raptor.Timetable, from, to point, js []raptor.Journey, out []journeyOut, ks []Kosten) {
	if s.Valhalla == nil {
		return
	}
	where := func(stop int32, p point) pt {
		if stop < 0 {
			return pt{*p.Lat, *p.Lon}
		}
		return pt{tt.Stops[stop].Lat, tt.Stops[stop].Lon}
	}
	type pair struct{ a, b pt }
	want := map[pair][]*legOut{}
	for i, j := range js {
		for k, l := range j.Legs {
			if l.Kind != raptor.Walk {
				continue
			}
			p := pair{where(l.From, from), where(l.To, to)}
			if p.a != p.b {
				want[p] = append(want[p], &out[i].Legs[k])
			}
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for p, legs := range want {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Valhalla.Route(ctx, p.a.lat, p.a.lon, p.b.lat, p.b.lon, ks)
			if err != nil {
				return
			}
			mu.Lock()
			for _, l := range legs {
				l.Shape = r.Shape
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
}

// rideStops setzt bei Fahrtabschnitten die Halte vom Ein- bis zum Ausstieg (beide eingeschlossen).
func rideStops(tt *raptor.Timetable, j raptor.Journey, out *journeyOut) {
	for k, l := range j.Legs {
		if l.Kind != raptor.Ride {
			continue
		}
		r := &tt.Routes[l.Route]
		a := stopPos(r, l.From, 0)
		if a < 0 {
			continue
		}
		b := stopPos(r, l.To, a+1)
		if b < 0 {
			continue
		}
		ids := make([]string, 0, b-a+1)
		for _, st := range r.Stops[a : b+1] {
			ids = append(ids, tt.Stops[st].ID)
		}
		out.Legs[k].Stops = ids
	}
}

// directWalk liefert den reinen Fußweg Start → Ziel als eigene Verbindung (Vergleich in der App), wenn er
// höchstens s.DirectMax dauert. Mit Valhalla echter Weg samt Form, sonst Luftlinien-Schätzung ohne Form.
// scale: Gehtempo-Faktor wie bei den Zugangswegen; ks: Valhalla-Kostenarten der Fortbewegungsart (D-22).
func (s *Server) directWalk(ctx context.Context, from, to point, dep time.Time, scale, weit float64, ks []Kosten) *journeyOut {
	m := raptor.Dist(*from.Lat, *from.Lon, *to.Lat, *to.Lon)
	if s.DirectM <= 0 || m > s.DirectM*weit {
		return nil
	}
	sec, shape := float64(raptor.WalkSec(m))*scale, ""
	if s.Valhalla != nil {
		r, err := s.Valhalla.Route(ctx, *from.Lat, *from.Lon, *to.Lat, *to.Lon, ks)
		if err == nil {
			sec, shape = r.Sec, r.Shape
		}
	}
	d := time.Duration(sec+0.5) * time.Second
	if d > s.DirectMax {
		return nil
	}
	arr := dep.Add(d).Truncate(time.Second)
	dep = dep.Truncate(time.Second)
	return &journeyOut{Dep: dep, Arr: arr, Legs: []legOut{{Mode: "walk", Dep: dep, Arr: arr, Shape: shape}}}
}

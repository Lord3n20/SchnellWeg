package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Verkehrslage von der Autobahn-API (D-38): Staus, Sperrungen und Baustellen. Abgefragt werden immer alle Autobahnen
// der Region, nie etwas zur einzelnen Route; verglichen wird hier. Abruf nur bei Autorouten, Stand nur im RAM.

const (
	verkehrTTL   = 3 * time.Minute  // so lange gilt ein Stand als frisch
	verkehrAlt   = 30 * time.Minute // älter wird er nicht mehr benutzt
	verkehrPause = time.Minute      // nach einem Fehlschlag nicht gleich wieder
	verkehrGanz  = 8 * time.Second  // Budget eines Abrufs (auch ohne wartende Anfrage)
	verkehrWarte = 3 * time.Second  // so lange wartet eine Anfrage, danach gilt der vorige Stand
	verkehrParal = 4
	meldungM     = 60.0 // so weit neben der Route gilt eine Meldung noch als auf der Route
)

// Autobahnen im Gebiet der Tiles (config/region.geojson); die Box wirft den Rest dieser Straßen weg.
var verkehrStrassen = []string{"A1", "A3", "A4", "A44", "A46", "A57", "A59", "A61", "A544", "A553", "A555", "A559", "A560", "A562", "A565"}

const boxS, boxN, boxW, boxO = 50.2, 51.3, 5.7, 7.6

var verkehrDienste = []struct{ pfad, art string }{{"warning", "stau"}, {"closure", "sperrung"}, {"roadworks", "baustelle"}}

// Meldung ist ein Eintrag der Autobahn-API; Form verläuft in Fahrtrichtung.
type Meldung struct {
	Art, Titel, Richtung string
	VerzMin, Kmh, MaxKmh int
	Gesperrt             bool
	Form                 []pt
}

// Verkehr holt und hält die Meldungen.
type Verkehr struct {
	URL, UA string
	Client  *http.Client

	mu       sync.Mutex
	stand    map[string][]Meldung // je Straße + Dienst, fällt einer aus, bleibt sein voriger Stand
	geholt   time.Time
	versucht time.Time
	laeuft   chan struct{}
}

// Holen liefert die Meldungen; ok = false, wenn es keinen brauchbaren Stand gibt.
func (v *Verkehr) Holen(ctx context.Context) (alle []Meldung, ok bool) {
	v.mu.Lock()
	jetzt := time.Now()
	if jetzt.Sub(v.geholt) > verkehrTTL && jetzt.Sub(v.versucht) > verkehrPause && v.laeuft == nil {
		v.versucht = jetzt
		v.laeuft = make(chan struct{})
		go v.abrufen(v.laeuft)
	}
	warte := v.laeuft
	v.mu.Unlock()
	if warte != nil {
		t := time.NewTimer(verkehrWarte)
		defer t.Stop()
		select {
		case <-warte:
		case <-t.C:
		case <-ctx.Done():
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.geholt.IsZero() || time.Since(v.geholt) > verkehrAlt {
		return nil, false
	}
	for _, l := range v.stand {
		alle = append(alle, l...)
	}
	return alle, true
}

func (v *Verkehr) abrufen(fertig chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), verkehrGanz)
	defer cancel()
	var wg sync.WaitGroup
	frei := make(chan struct{}, verkehrParal)
	neu, treffer := map[string][]Meldung{}, 0
	var mu sync.Mutex
	for _, s := range verkehrStrassen {
		for _, d := range verkehrDienste {
			wg.Add(1)
			go func() {
				defer wg.Done()
				frei <- struct{}{}
				defer func() { <-frei }()
				l, err := v.dienst(ctx, s, d.pfad, d.art)
				if err != nil {
					return
				}
				mu.Lock()
				neu[s+"/"+d.pfad] = l
				treffer++
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stand == nil {
		v.stand = map[string][]Meldung{}
	}
	for k, l := range neu {
		v.stand[k] = l
	}
	if treffer > 0 {
		v.geholt = time.Now()
	}
	v.laeuft = nil
	close(fertig)
}

type vkEintrag struct {
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	Delay       string   `json:"delayTimeValue"`
	Speed       string   `json:"averageSpeed"`
	IsBlocked   string   `json:"isBlocked"`
	Future      bool     `json:"future"`
	Description []string `json:"description"`
	Geometry    struct {
		Coordinates [][]float64 `json:"coordinates"`
	} `json:"geometry"`
}

var vkMax = regexp.MustCompile(`Max\. (\d+) km/h`)

func (v *Verkehr) dienst(ctx context.Context, strasse, pfad, art string) ([]Meldung, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, v.URL+"/"+strasse+"/services/"+pfad, nil)
	if err != nil {
		return nil, err
	}
	hr.Header.Set("User-Agent", v.UA)
	res, err := v.Client.Do(hr)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	var j map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&j); err != nil {
		return nil, err
	}
	var roh []vkEintrag
	if err := json.Unmarshal(j[pfad], &roh); err != nil {
		return nil, err
	}
	out := []Meldung{}
	for _, e := range roh {
		form := make([]pt, 0, len(e.Geometry.Coordinates))
		for _, c := range e.Geometry.Coordinates {
			if len(c) == 2 {
				form = append(form, pt{c[1], c[0]})
			}
		}
		if e.Future || len(form) < 2 || form[0].lat < boxS || form[0].lat > boxN || form[0].lon < boxW || form[0].lon > boxO {
			continue
		}
		m := Meldung{Art: art, Titel: strings.Replace(e.Title, " | ", " ", 1), Richtung: strings.TrimSpace(e.Subtitle),
			Gesperrt: e.IsBlocked == "true" || art == "sperrung", Form: form}
		m.VerzMin, _ = strconv.Atoi(e.Delay)
		m.Kmh, _ = strconv.Atoi(e.Speed)
		if t := vkMax.FindStringSubmatch(strings.Join(e.Description, " ")); t != nil {
			m.MaxKmh, _ = strconv.Atoi(t[1])
		}
		out = append(out, m)
	}
	return out, nil
}

// dekodieren liest eine encoded polyline (Genauigkeit 6); was nicht passt, endet die Liste.
func dekodieren(s string) []pt {
	var out []pt
	var lat, lon int
	for i := 0; i < len(s); {
		var d [2]int
		for k := range d {
			shift, r := 0, 0
			for {
				if i >= len(s) {
					return out
				}
				b := int(s[i]) - 63
				i++
				r |= (b & 0x1f) << shift
				shift += 5
				if b < 0x20 {
					break
				}
			}
			if r&1 != 0 {
				d[k] = ^(r >> 1)
			} else {
				d[k] = r >> 1
			}
		}
		lat += d[0]
		lon += d[1]
		out = append(out, pt{float64(lat) / 1e6, float64(lon) / 1e6})
	}
	return out
}

// amWeg: liegt p höchstens meldungM neben weg, auf einem Stück, das etwa in Richtung (rx, ry) läuft? Richtungsfahrbahnen
// liegen oft näher beieinander als die Daten genau sind, deshalb zählt die Richtung.
func amWeg(p pt, rx, ry float64, weg []pt) (int, bool) {
	mLat, mLon := 111320.0, 111320.0*math.Cos(p.lat*math.Pi/180)
	for i := 0; i+1 < len(weg); i++ {
		ax, ay := (weg[i].lon-p.lon)*mLon, (weg[i].lat-p.lat)*mLat
		dx, dy := (weg[i+1].lon-weg[i].lon)*mLon, (weg[i+1].lat-weg[i].lat)*mLat
		l := dx*dx + dy*dy
		if l == 0 {
			continue
		}
		t := max(0, min(1, -(ax*dx+ay*dy)/l))
		x, y := ax+t*dx, ay+t*dy
		if x*x+y*y <= meldungM*meldungM && (dx*rx+dy*ry)/math.Sqrt(l) > 0.7 {
			return i, true
		}
	}
	return 0, false
}

// richtung ist der Einheitsvektor von a nach b in Metern.
func richtung(a, b pt) (float64, float64) {
	x, y := (b.lon-a.lon)*111320*math.Cos(a.lat*math.Pi/180), (b.lat-a.lat)*111320
	l := math.Hypot(x, y)
	if l == 0 {
		return 0, 0
	}
	return x / l, y / l
}

// aufRoute: mindestens die Hälfte der Meldung liegt in ihrer Fahrtrichtung auf der Route; Rückgabe ist die Stelle auf der Route.
func aufRoute(form, route []pt) (int, bool) {
	if len(form) < 2 || len(route) < 2 {
		return 0, false
	}
	schritt := max(1, (len(form)-1)/12)
	proben, treffer, erste := 0, 0, -1
	for i := 0; i+1 < len(form); i += schritt {
		rx, ry := richtung(form[i], form[i+1])
		if rx == 0 && ry == 0 {
			continue
		}
		proben++
		if at, ok := amWeg(form[i], rx, ry, route); ok {
			treffer++
			if erste < 0 {
				erste = at
			}
		}
	}
	return max(erste, 0), proben > 0 && treffer*2 >= proben
}

// meldungOut geht an die App.
type meldungOut struct {
	Art      string  `json:"art"` // stau | sperrung | baustelle
	Titel    string  `json:"titel"`
	Richtung string  `json:"richtung,omitempty"`
	VerzMin  int     `json:"verz_min,omitempty"`
	Kmh      int     `json:"kmh,omitempty"`     // Durchschnitt im Stau
	MaxKmh   int     `json:"max_kmh,omitempty"` // Baustelle
	Gesperrt bool    `json:"gesperrt,omitempty"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
}

// aufDerRoute wählt die Meldungen entlang der Route, in Reihenfolge der Fahrt. Dasselbe Stück melden oft mehrere Quellen:
// von überlappenden Staus bleibt der längste (sonst zählte die Verzögerung doppelt).
func aufDerRoute(alle []Meldung, route []pt) []Meldung {
	if len(route) < 2 {
		return nil
	}
	s, n, w, o := route[0].lat, route[0].lat, route[0].lon, route[0].lon
	for _, p := range route {
		s, n, w, o = min(s, p.lat), max(n, p.lat), min(w, p.lon), max(o, p.lon)
	}
	const rand = 0.001
	type fund struct {
		m  Meldung
		at int
	}
	var f []fund
	for _, m := range alle {
		drin := slices.ContainsFunc(m.Form, func(p pt) bool {
			return p.lat >= s-rand && p.lat <= n+rand && p.lon >= w-rand && p.lon <= o+rand
		})
		if !drin {
			continue
		}
		if at, ok := aufRoute(m.Form, route); ok {
			f = append(f, fund{m, at})
		}
	}
	slices.SortStableFunc(f, func(a, b fund) int { return b.m.VerzMin - a.m.VerzMin })
	var bleibt []fund
	for _, x := range f {
		doppelt := x.m.Art == "stau" && slices.ContainsFunc(bleibt, func(b fund) bool {
			if b.m.Art != "stau" {
				return false
			}
			_, ok := aufRoute(x.m.Form, b.m.Form)
			return ok
		})
		if !doppelt {
			bleibt = append(bleibt, x)
		}
	}
	slices.SortStableFunc(bleibt, func(a, b fund) int { return a.at - b.at })
	out := make([]Meldung, len(bleibt))
	for i, x := range bleibt {
		out[i] = x.m
	}
	return out
}

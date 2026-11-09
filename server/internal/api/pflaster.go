package api

import (
	"cmp"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Belag und Hang (D-44). Valhalla kennt zu Fuß keine Kosten für den Belag befestigter Wege und führt deshalb durchs
// Kopfsteinpflaster. Der Job valhalla legt die Wege samt Belagklasse neben die Tiles (1 = am schlechtesten). Das Profil sagt je
// Klasse, wie schnell man dort ist. Der Server rechnet die normale Route und Varianten, die die langsamen Beläge über kleine
// Sperrflächen umgehen, mit diesem Tempo durch und nimmt die schnellste.
const (
	belagRundenGesamt = 20      // so oft wird je Weg höchstens neu gefragt, über alle Varianten
	pflasterMaxBoxen  = 200     // Valhalla erlaubt 10 km Umfang, eine Box hat ~20 m
	pflasterBoxLat    = 0.00002 // ~2 m: trifft die Kante, aber kaum Nachbarwege
	pflasterBoxLon    = 0.00003
	pflasterFreiM     = 60 // am Start und am Ziel lässt sich Belag nicht umgehen
	pflasterPruefen   = time.Minute
	belagKlassen      = 5

	hangVoll    = 6.0 // % Steigung, ab der das volle Tempo bergauf/bergab gilt
	hangMaxKMH  = 200.0
	hangMaxVoll = 30.0
)

// Belagdaten: sortierte Way-IDs und je Weg die Klasse.
type Belagdaten struct {
	ids     []uint32
	klassen []uint8
}

// klasse liefert die Belagklasse des Wegs, 0 = unauffällig.
func (d Belagdaten) klasse(way uint64) uint8 {
	if way > math.MaxUint32 {
		return 0
	}
	i, drin := slices.BinarySearch(d.ids, uint32(way))
	if !drin {
		return 0
	}
	return d.klassen[i]
}

// Belagliste liest die Datei des Jobs (n IDs als uint32, dann n Klassen als uint8) und bemerkt neue Stände.
type Belagliste struct {
	Muster string // Pfad, Glob erlaubt

	mu       sync.Mutex
	daten    Belagdaten
	pfad     string
	stand    time.Time
	geprueft time.Time
}

// Aktuell liefert die Daten; bei Lesefehlern bleibt der letzte Stand.
func (l *Belagliste) Aktuell() Belagdaten {
	if l == nil {
		return Belagdaten{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.geprueft) < pflasterPruefen {
		return l.daten
	}
	l.geprueft = time.Now()
	treffer, _ := filepath.Glob(l.Muster)
	if len(treffer) == 0 {
		return l.daten
	}
	st, err := os.Stat(treffer[0]) // folgt dem Symlink current
	if err != nil || treffer[0] == l.pfad && st.ModTime().Equal(l.stand) {
		return l.daten
	}
	b, err := os.ReadFile(treffer[0])
	if err != nil || len(b)%5 != 0 {
		return l.daten
	}
	n := len(b) / 5
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	l.daten, l.pfad, l.stand = Belagdaten{ids, slices.Clone(b[4*n:])}, treffer[0], st.ModTime()
	return l.daten
}

// Hang rechnet das Tempo je nach Steigung: Eben bei 0 %, linear bis Auf bzw. Ab bei hangVoll, darüber gleich.
// Voll > 0 ersetzt hangVoll bergauf (z. B. die Steigung, ab der man das Longboard trägt).
type Hang struct{ Eben, Auf, Ab, Voll float64 }

func (h Hang) kmh(steigung float64) float64 {
	ziel, voll := h.Auf, hangVoll
	if steigung < 0 {
		ziel, steigung = h.Ab, -steigung
	} else if h.Voll > 0 {
		voll = h.Voll
	}
	return h.Eben + (ziel-h.Eben)*min(steigung/voll, 1)
}

// hang liefert die Tempi des Profils; nil, wenn keins gesetzt ist.
func (m *Meiden) hang(kmh float64) *Hang {
	if m.KmhAuf <= 0 && m.KmhAb <= 0 {
		return nil
	}
	h := Hang{Eben: kmh, Auf: m.KmhAuf, Ab: m.KmhAb, Voll: m.HangVoll}
	if h.Auf <= 0 {
		h.Auf = kmh
	}
	if h.Ab <= 0 {
		h.Ab = kmh
	}
	return &h
}

// kantenZeit summiert die Zeit über die Kanten: Tempo nach Steigung, auf einem Belag mit eigenem Tempo höchstens dieses.
// ok = false, wenn die Kanten nicht zu rechnen sind.
func (k Kosten) kantenZeit(t vhTraceResp, d Belagdaten) (sek, m float64, ok bool) {
	for _, e := range t.Edges {
		v := k.Eben
		if k.Hang != nil {
			v = k.Hang.kmh(e.Steigung)
		}
		if c := d.klasse(e.WayID); c > 0 && int(c) <= belagKlassen && k.BelagKmh[c] > 0 {
			v = min(v, k.BelagKmh[c])
		}
		if v <= 0 {
			return 0, 0, false
		}
		sek += e.Length * 3600 / v
		m += e.Length * 1000
	}
	return sek, m, m > 0
}

// belagRegeln sagt, ob das Profil für einen Belag ein eigenes Tempo hat.
func (k Kosten) belagRegeln() bool {
	return slices.ContainsFunc(k.BelagKmh[:], func(v float64) bool { return v > 0 })
}

// langsameKlassen: Klassen, die langsamer sind als die Ebene, die langsamste zuerst.
func (k Kosten) langsameKlassen() []uint8 {
	var l []uint8
	for c := uint8(1); c <= belagKlassen; c++ {
		if v := k.BelagKmh[c]; v > 0 && v < k.Eben {
			l = append(l, c)
		}
	}
	slices.SortFunc(l, func(a, b uint8) int { return cmp.Compare(k.BelagKmh[a], k.BelagKmh[b]) })
	return l
}

// belagStellen liefert kleine Sperrflächen auf den Abschnitten der Route, deren Klasse in meiden steht.
func belagStellen(t vhTraceResp, d Belagdaten, meiden []uint8, a, b pt) (boxen [][][2]float64) {
	form := dekodieren(t.Shape)
	for _, e := range t.Edges {
		if c := d.klasse(e.WayID); c == 0 || !slices.Contains(meiden, c) {
			continue
		}
		if e.Von < 0 || e.Bis <= e.Von || e.Bis >= len(form) {
			continue
		}
		p := form[(e.Von+e.Bis)/2]
		if raptor.Dist(p.lat, p.lon, a.lat, a.lon) < pflasterFreiM || raptor.Dist(p.lat, p.lon, b.lat, b.lon) < pflasterFreiM {
			continue
		}
		x, y := pflasterBoxLon, pflasterBoxLat
		boxen = append(boxen, [][2]float64{{p.lon - x, p.lat - y}, {p.lon + x, p.lat - y}, {p.lon + x, p.lat + y}, {p.lon - x, p.lat + y}, {p.lon - x, p.lat - y}})
	}
	return boxen
}

// belagVersuch ist ein Weg samt Kanten und seiner Zeit nach dem Profil.
type belagVersuch struct {
	weg WalkRoute
	t   vhTraceResp
	sek float64
}

// wegAnpassen holt den Weg und wendet Belag- und Hang-Tempo des Profils an. Schlägt ein späterer Schritt fehl, bleibt der
// beste bisherige Weg.
func (v *Valhalla) wegAnpassen(ctx context.Context, aLat, aLon, bLat, bLon float64, k Kosten, zeit time.Duration) (WalkRoute, error) {
	weg, err := v.route(ctx, aLat, aLon, bLat, bLon, k, zeit)
	if err != nil || !k.belagRegeln() && k.Hang == nil {
		return weg, err
	}
	t := v.kanten(ctx, weg.Shape, k.Costing)
	d := v.Belag.Aktuell()
	start := belagVersuch{weg, t, math.Inf(1)}
	if sek, _, ok := k.kantenZeit(t, d); ok {
		start.sek = sek
	}
	beste, runden := start, 0
	if len(d.ids) > 0 {
		klassen := k.langsameKlassen()
		for n := 1; n <= len(klassen); n++ { // erst nur den langsamsten Belag umgehen, dann auch die nächsten
			v.belagPass(ctx, aLat, aLon, bLat, bLon, k, zeit, d, klassen[:n], start, &beste, &runden)
		}
	}
	if sek, m, ok := k.kantenZeit(beste.t, d); ok && m >= 0.9*beste.weg.M {
		beste.weg.Sec = sek
	}
	return beste.weg, nil
}

// belagPass probiert ab start Wege ohne die Klassen in meiden und merkt sich den schnellsten. runden zählt über alle Varianten.
func (v *Valhalla) belagPass(ctx context.Context, aLat, aLon, bLat, bLon float64, k Kosten, zeit time.Duration, d Belagdaten, meiden []uint8, start belagVersuch, beste *belagVersuch, runden *int) {
	weg, t := start.weg, start.t
	for len(t.Edges) > 0 {
		if sek, _, ok := k.kantenZeit(t, d); ok && sek < beste.sek {
			*beste = belagVersuch{weg, t, sek}
		}
		boxen := belagStellen(t, d, meiden, pt{aLat, aLon}, pt{bLat, bLon})
		if len(boxen) == 0 || *runden >= belagRundenGesamt || len(k.Polygone)+len(boxen) > pflasterMaxBoxen {
			return
		}
		*runden++
		k.Polygone = slices.Concat(k.Polygone, boxen)
		neu, err := v.route(ctx, aLat, aLon, bLat, bLon, k, zeit)
		if err != nil || neu.Shape == weg.Shape {
			return
		}
		weg, t = neu, v.kanten(ctx, neu.Shape, k.Costing)
	}
}

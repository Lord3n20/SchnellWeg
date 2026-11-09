package api

import (
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
// Kopfsteinpflaster. Der Job valhalla legt die Wege samt Belagklasse neben die Tiles; der Server fragt die Route, sperrt die
// Stücke, die sie davon benutzt, und fragt neu. So bleiben es wenige kleine Sperrflächen statt aller solchen Wege der Region.
// Klasse k (1 = am schlechtesten) wird ab Stufe k × belagSchritt umgangen.
const (
	belagRundenGesamt  = 20      // so oft wird je Weg höchstens neu gefragt, über alle Stufen
	pflasterMaxBoxen   = 200     // Valhalla erlaubt 10 km Umfang, eine Box hat ~20 m
	pflasterBoxLat     = 0.00002 // ~2 m: trifft die Kante, aber kaum Nachbarwege
	pflasterBoxLon     = 0.00003
	pflasterFreiM      = 60 // am Start und am Ziel lässt sich Pflaster nicht umgehen
	belagUmwegStandard = 50 // % länger darf der Weg werden …
	belagExtraM        = 10 // … oder so viele Meter je %, was mehr erlaubt
	belagUmwegMax      = 300
	pflasterPruefen    = time.Minute
	belagSchritt       = 0.2

	hangVoll    = 6.0 // % Steigung, ab der das volle Tempo bergauf/bergab gilt
	hangMaxKMH  = 200.0
	hangMaxVoll = 30.0
)

// Belagdaten: sortierte Way-IDs und je Weg die Klasse.
type Belagdaten struct {
	ids     []uint32
	klassen []uint8
}

// umgehen sagt, ob der Weg bei dieser Stufe zu meiden ist.
func (d Belagdaten) umgehen(way uint64, stufe float64) bool {
	if way > math.MaxUint32 {
		return false
	}
	i, drin := slices.BinarySearch(d.ids, uint32(way))
	return drin && float64(d.klassen[i])*belagSchritt <= stufe+1e-9
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

// hangZeit ersetzt die Zeit durch die Summe je Kante. Deckt die Antwort den Weg nicht ab, bleibt Valhallas Zeit.
func hangZeit(weg *WalkRoute, t vhTraceResp, h Hang) {
	var sek, m float64
	for _, e := range t.Edges {
		v := h.kmh(e.Steigung)
		if v <= 0 {
			return
		}
		sek += e.Length * 3600 / v
		m += e.Length * 1000
	}
	if m >= 0.9*weg.M && m > 0 {
		weg.Sec = sek
	}
}

// pflasterStellen liefert kleine Sperrflächen auf den Pflasterstücken der Route und deren Länge in m.
func pflasterStellen(t vhTraceResp, d Belagdaten, stufe float64, a, b pt) (boxen [][][2]float64, m float64) {
	form := dekodieren(t.Shape)
	for _, e := range t.Edges {
		if !d.umgehen(e.WayID, stufe) {
			continue
		}
		m += e.Length * 1000
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
	return boxen, m
}

// wegAnpassen holt den Weg und wendet Pflaster-Umgehung und Hang-Tempo des Profils an. Schlägt ein späterer Schritt fehl,
// bleibt der beste bisherige Weg: lieber übers Pflaster als gar kein Weg.
func (v *Valhalla) wegAnpassen(ctx context.Context, aLat, aLon, bLat, bLon float64, k Kosten, zeit time.Duration) (WalkRoute, error) {
	weg, err := v.route(ctx, aLat, aLon, bLat, bLon, k, zeit)
	if err != nil || k.BelagMeiden < belagSchritt && k.Hang == nil {
		return weg, err
	}
	t := v.kanten(ctx, weg.Shape, k.Costing)
	if d := v.Belag.Aktuell(); k.BelagMeiden >= belagSchritt && len(d.ids) > 0 {
		p := float64(k.BelagUmweg)
		if p <= 0 {
			p = belagUmwegStandard
		}
		grenze := max(weg.M*(1+p/100), weg.M+p*belagExtraM)
		// Reicht der Umweg nicht für alle gewählten Beläge, wenigstens den schlimmsten ausweichen.
		start := belagVersuch{weg, t, false}
		ergebnis, runden := start, 0
		for stufe, erste := k.BelagMeiden, true; stufe+1e-9 >= belagSchritt; stufe, erste = stufe-belagSchritt, false {
			e := v.belagPass(ctx, aLat, aLon, bLat, bLon, k, zeit, d, stufe, grenze, start, &runden)
			if erste || e.sauber {
				ergebnis = e
			}
			if e.sauber {
				break
			}
		}
		weg, t = ergebnis.weg, ergebnis.t
	}
	if k.Hang != nil {
		hangZeit(&weg, t, *k.Hang)
	}
	return weg, nil
}

// belagVersuch ist ein Weg samt Kanten; sauber: auf ihm liegt bei der Stufe nichts mehr, was sich umgehen ließe.
type belagVersuch struct {
	weg    WalkRoute
	t      vhTraceResp
	sauber bool
}

// belagPass sucht ab start den Weg mit dem wenigsten Belag der Stufe innerhalb der Grenze. runden zählt über alle Stufen.
func (v *Valhalla) belagPass(ctx context.Context, aLat, aLon, bLat, bLon float64, k Kosten, zeit time.Duration, d Belagdaten, stufe, grenze float64, start belagVersuch, runden *int) belagVersuch {
	weg, t := start.weg, start.t
	beste, besteM := start, math.Inf(1)
	for len(t.Edges) > 0 {
		boxen, m := pflasterStellen(t, d, stufe, pt{aLat, aLon}, pt{bLat, bLon})
		innen := weg.M <= grenze
		if innen && (m < besteM || len(boxen) == 0) {
			beste, besteM = belagVersuch{weg, t, len(boxen) == 0}, m
		}
		if len(boxen) == 0 || !innen || *runden >= belagRundenGesamt || len(k.Polygone)+len(boxen) > pflasterMaxBoxen {
			break
		}
		*runden++
		k.Polygone = slices.Concat(k.Polygone, boxen)
		neu, err := v.route(ctx, aLat, aLon, bLat, bLon, k, zeit)
		if err != nil || neu.Shape == weg.Shape {
			break
		}
		weg, t = neu, v.kanten(ctx, neu.Shape, k.Costing)
	}
	return beste
}

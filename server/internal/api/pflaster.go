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

// Pflaster und Hang (D-44). Valhalla kennt zu Fuß keine Kosten für den Belag befestigter Wege und führt deshalb durchs
// Kopfsteinpflaster. Der Job valhalla legt die betroffenen Wege neben die Tiles; der Server fragt die Route, sperrt die
// Stücke, die sie davon benutzt, und fragt neu. So bleiben es wenige kleine Sperrflächen statt aller Pflasterwege der Region.
const (
	pflasterRunden   = 8       // so oft wird höchstens neu gefragt
	pflasterMaxBoxen = 200     // Valhalla erlaubt 10 km Umfang, eine Box hat ~20 m
	pflasterBoxLat   = 0.00002 // ~2 m: trifft die Kante, aber kaum Nachbarwege
	pflasterBoxLon   = 0.00003
	pflasterFreiM    = 60  // am Start und am Ziel lässt sich Pflaster nicht umgehen
	pflasterUmweg    = 1.5 // so viel länger darf der Weg werden …
	pflasterExtraM   = 500 // … oder so viele Meter, was mehr erlaubt
	pflasterPruefen  = time.Minute

	hangVoll    = 6.0 // % Steigung, ab der das volle Tempo bergauf/bergab gilt
	hangMaxKMH  = 200.0
	hangMaxVoll = 30.0
)

// Pflasterliste liest die sortierten Way-IDs aus der Datei des Jobs und bemerkt neue Stände.
type Pflasterliste struct {
	Muster string // Pfad, Glob erlaubt

	mu       sync.Mutex
	ids      []uint32
	pfad     string
	stand    time.Time
	geprueft time.Time
}

// Aktuell liefert die IDs; bei Lesefehlern bleibt der letzte Stand.
func (l *Pflasterliste) Aktuell() []uint32 {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.geprueft) < pflasterPruefen {
		return l.ids
	}
	l.geprueft = time.Now()
	treffer, _ := filepath.Glob(l.Muster)
	if len(treffer) == 0 {
		return l.ids
	}
	st, err := os.Stat(treffer[0]) // folgt dem Symlink current
	if err != nil || treffer[0] == l.pfad && st.ModTime().Equal(l.stand) {
		return l.ids
	}
	b, err := os.ReadFile(treffer[0])
	if err != nil || len(b)%4 != 0 {
		return l.ids
	}
	ids := make([]uint32, len(b)/4)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	l.ids, l.pfad, l.stand = ids, treffer[0], st.ModTime()
	return l.ids
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
func pflasterStellen(t vhTraceResp, ids []uint32, a, b pt) (boxen [][][2]float64, m float64) {
	form := dekodieren(t.Shape)
	for _, e := range t.Edges {
		if e.WayID > math.MaxUint32 {
			continue
		}
		if _, drin := slices.BinarySearch(ids, uint32(e.WayID)); !drin {
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
	if err != nil || !k.Pflaster && k.Hang == nil {
		return weg, err
	}
	t := v.kanten(ctx, weg.Shape, k.Costing)
	if ids := v.Pflaster.Aktuell(); k.Pflaster && len(ids) > 0 {
		grenze := max(weg.M*pflasterUmweg, weg.M+pflasterExtraM)
		beste, besteT, besteM := weg, t, math.Inf(1)
		for runde := 0; len(t.Edges) > 0; runde++ {
			boxen, m := pflasterStellen(t, ids, pt{aLat, aLon}, pt{bLat, bLon})
			if m < besteM && weg.M <= grenze {
				beste, besteT, besteM = weg, t, m
			}
			if len(boxen) == 0 || weg.M > grenze || runde == pflasterRunden || len(k.Polygone)+len(boxen) > pflasterMaxBoxen {
				break
			}
			k.Polygone = slices.Concat(k.Polygone, boxen)
			neu, err := v.route(ctx, aLat, aLon, bLat, bLon, k, zeit)
			if err != nil || neu.Shape == weg.Shape {
				break
			}
			weg, t = neu, v.kanten(ctx, neu.Shape, k.Costing)
		}
		weg, t = beste, besteT
	}
	if k.Hang != nil {
		hangZeit(&weg, t, *k.Hang)
	}
	return weg, nil
}

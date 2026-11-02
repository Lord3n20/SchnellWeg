package api

import (
	"math"
	"slices"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Mehrere Verbindungen und knappe Verbindungen (D-31). Eine RAPTOR-Abfrage liefert nur die früheste Ankunft je
// Umstiegszahl – meist genau eine Verbindung. Darum wird nach der frühesten Losgehzeit eine Minute später neu
// gefragt, bis optionenN Verbindungen da sind. Fährt ein dichter Bus zum selben Zug, liefert jede Abfrage nur eine
// spätere Variante mit gleicher Ankunft; dann wächst der Schritt, bis wieder eine neue Ankunft kommt (eine Verbindung
// dazwischen kann dabei fehlen, die schnellste aus der ersten Abfrage nie). Mit "knapp" kommt zusätzlich, was man mit
// Losgehen bis zu knappSek vorher erwischt hätte, wenn es mit schnellerem Zugangsweg noch reicht und früher ankommt.
const (
	optionenN       = 4   // so viele ÖV-Verbindungen höchstens
	optionenAbfr    = 10  // so viele RAPTOR-Abfragen höchstens (je ~1–50 ms)
	knappSek        = 150 // so viel früher hätte man höchstens losgehen müssen (Betreiber: „1–2 min“)
	knappFaktor     = 2.0 // Zugangsweg höchstens so viel schneller als beim gewählten Tempo
	eilFaktorProfil = 1.5 // eigene Profile: knapp höchstens so viel schneller als ihr Tempo (D-34)
	laufenKMH       = 12  // zu Fuß höchstens so schnell (Laufen) – art.Max (8) ist die Grenze für die Einstellung
)

// option ist eine Verbindung; eil > 0: nur mit diesem Tempo (km/h) auf dem Zugangsweg erreichbar.
type option struct {
	j   raptor.Journey
	eil float64
}

// optionen sucht ab dep bis zu optionenN Verbindungen, sortiert nach Ankunft; vorn ggf. knappe (kmh: gewähltes
// Tempo, grenze: höchstes Tempo für knappe Verbindungen, 0 = keine knappen).
func optionen(rt *raptor.Router, dep int32, from, to []raptor.Access, maxTrips int, kmh, grenze float64) []option {
	var alle []raptor.Journey
	t, zuletzt, schritt := dep, dep, int32(60)
	for q := 0; q < optionenAbfr && len(ungeschlagen(alle)) < optionenN; q++ {
		res := rt.Query(t, from, to, maxTrips)
		if len(res.Journeys) == 0 {
			if schritt == 60 {
				break
			}
			t, schritt = zuletzt+60, 60 // vielleicht zu weit gesprungen, etwa über den letzten Bus
			continue
		}
		naechste := int32(math.MaxInt32)
		neu := false
		for _, j := range res.Journeys {
			neu = neu || !slices.ContainsFunc(alle, func(o raptor.Journey) bool { return o.Arr == j.Arr })
			// Ein Weg ohne Fahrt reicht: je Minute später losgehen wäre sonst eine neue „Verbindung“ und nähme echten den Platz
			doppelt := func(o raptor.Journey) bool { return gleich(o, j) || o.Trips == 0 && j.Trips == 0 }
			if !slices.ContainsFunc(alle, doppelt) {
				alle = append(alle, j)
			}
			naechste = min(naechste, abfahrt(j))
		}
		if neu {
			schritt = max(schritt/2, 60) // der nächste Zug ist vermutlich ähnlich weit weg
		} else {
			schritt *= 2
		}
		zuletzt = max(naechste, t)
		t = zuletzt + schritt
	}
	var out []option
	for _, j := range ungeschlagen(alle) {
		out = append(out, option{j: j})
	}
	slices.SortStableFunc(out, func(a, b option) int { return int(a.j.Arr-b.j.Arr)*16 + a.j.Trips - b.j.Trips })
	out = out[:min(len(out), optionenN)]
	if grenze <= 0 || len(out) == 0 {
		return out
	}
	frueh := out[0].j.Arr
	var knapp []option
	for _, j := range rt.Query(dep-knappSek, from, to, maxTrips).Journeys {
		if e := eilTempo(j, dep, kmh); e > 0 && e <= grenze && j.Arr < frueh {
			knapp = append(knapp, option{j: j, eil: e})
		}
	}
	return append(knapp, out...)
}

// ungeschlagen: die Verbindungen aus js, die keine andere aus js in allem schlägt.
func ungeschlagen(js []raptor.Journey) []raptor.Journey {
	var out []raptor.Journey
	for _, j := range js {
		if !slices.ContainsFunc(js, func(o raptor.Journey) bool { return besser(o, j) }) {
			out = append(out, j)
		}
	}
	return out
}

// eilTempo: welches Tempo bräuchte man auf dem Zugangsweg von j, wenn man erst um dep losgeht? 0 = j ist normal
// erreichbar, gar nicht erreichbar oder bräuchte mehr als knappFaktor. Auf 0,5 km/h aufgerundet.
func eilTempo(j raptor.Journey, dep int32, kmh float64) float64 {
	if len(j.Legs) < 2 || j.Legs[0].Kind != raptor.Walk || j.Legs[1].Kind != raptor.Ride || abfahrt(j) >= dep {
		return 0
	}
	weg, frei := j.Legs[0].Arr-j.Legs[0].Dep, j.Legs[1].Dep-dep
	if frei <= 0 || float64(weg) > knappFaktor*float64(frei) {
		return 0
	}
	return math.Ceil(kmh*float64(weg)/float64(frei)*2) / 2
}

// besser: o geht nicht früher los, kommt nicht später an, steigt nicht öfter um und ist in einem davon besser.
func besser(o, j raptor.Journey) bool {
	ao, aj := abfahrt(o), abfahrt(j)
	return ao >= aj && o.Arr <= j.Arr && o.Trips <= j.Trips && (ao > aj || o.Arr < j.Arr || o.Trips < j.Trips)
}

// gleich: dieselben Fahrten mit denselben Ein- und Ausstiegen.
func gleich(a, b raptor.Journey) bool {
	return a.Arr == b.Arr && slices.EqualFunc(fahrten(a), fahrten(b), func(x, y raptor.Leg) bool { return x == y })
}

func fahrten(j raptor.Journey) []raptor.Leg {
	var out []raptor.Leg
	for _, l := range j.Legs {
		if l.Kind == raptor.Ride {
			out = append(out, l)
		}
	}
	return out
}

package api

import (
	"math"
	"slices"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Mehrere Verbindungen und knappe Verbindungen (D-31). Eine RAPTOR-Abfrage liefert nur die früheste Ankunft je
// Umstiegszahl – meist genau eine Verbindung. Darum wird nach der frühesten Losgehzeit eine Minute später neu
// gefragt, bis optionenN Verbindungen da sind. Mit "knapp" kommt zusätzlich, was man mit Losgehen bis zu knappSek
// vorher erwischt hätte, wenn es mit schnellerem Zugangsweg noch reicht und früher ankommt.
const (
	optionenN    = 4   // so viele ÖV-Verbindungen höchstens
	optionenAbfr = 5   // so viele RAPTOR-Abfragen höchstens (je ~1–50 ms)
	knappSek     = 150 // so viel früher hätte man höchstens losgehen müssen (Betreiber: „1–2 min“)
	knappFaktor  = 2.0 // Zugangsweg höchstens so viel schneller als beim gewählten Tempo
	laufenKMH    = 12  // zu Fuß höchstens so schnell (Laufen) – art.Max (8) ist die Grenze für die Einstellung
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
	t := dep
	for q := 0; q < optionenAbfr && len(alle) < optionenN; q++ {
		res := rt.Query(t, from, to, maxTrips)
		if len(res.Journeys) == 0 {
			break
		}
		naechste := int32(math.MaxInt32)
		for _, j := range res.Journeys {
			if !slices.ContainsFunc(alle, func(o raptor.Journey) bool { return gleich(o, j) }) {
				alle = append(alle, j)
			}
			naechste = min(naechste, abfahrt(j))
		}
		t = max(naechste, t) + 60
	}
	var out []option
	for _, j := range alle {
		if !slices.ContainsFunc(alle, func(o raptor.Journey) bool { return besser(o, j) }) {
			out = append(out, option{j: j})
		}
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

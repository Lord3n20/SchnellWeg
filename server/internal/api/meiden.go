package api

import (
	"math"
	"time"
)

// Meiden beschreibt ein Profil aus der App (D-34): Name und Tempo bleiben auf dem Handy, der Server erfährt nur, was
// die Route meiden soll. Stufen 0–1: 0 = egal (Valhallas Standard), 1 = so weit wie möglich meiden.
type Meiden struct {
	Treppen       float64   `json:"treppen"`
	Belag         float64   `json:"belag"` // Feldwege und Radtyp
	Hauptstrassen float64   `json:"hauptstrassen"`
	Steigung      float64   `json:"steigung"`
	Faehren       float64   `json:"faehren"`
	Dunkel        float64   `json:"dunkel"`      // unbeleuchtete Wege (nur Fußgänger-Kosten)
	Autobahn      float64   `json:"autobahn"`    // nur Auto
	Maut          float64   `json:"maut"`        // nur Auto
	Fusswege      bool      `json:"fusswege"`    // Straßen und Radwege statt Fußwege (Fahrradkosten)
	NurBahn       bool      `json:"bus"`         // Busse nie: nur Schiene
	BusMin        int       `json:"bus_min"`     // Verbindungen mit Bus nur, wenn sie so viele Minuten vor dem direkten Weg ankommen
	OEV           bool      `json:"oev"`         // kein Bus und keine Bahn: nur der direkte Weg, ohne Längengrenze
	Umstiege      *int      `json:"umstiege"`    // höchstens so viele Umstiege
	Anlieger      bool      `json:"anlieger"`    // zu Fuß auch durch Zufahrten nur für Anlieger, wenn es kürzer ist (D-43)
	Pflaster      bool      `json:"pflaster"`    // App 0.2.4–0.2.6, ohne Wirkung
	BelagUmweg    int       `json:"belag_umweg"` // App 0.2.7–0.2.8, ohne Wirkung
	BelagKmh      []float64 `json:"belag_kmh"`   // Tempo je Belagklasse 1–5, 0 = wie normal (D-44)
	KmhAuf        float64   `json:"kmh_auf"`     // Tempo bergauf ab 6 % Steigung, 0 = wie in der Ebene (D-44)
	KmhAb         float64   `json:"kmh_ab"`      // Tempo bergab ab 6 % Gefälle, 0 = wie in der Ebene (D-44)
	HangVoll      float64   `json:"hang_voll"`   // Steigung in %, ab der kmh_auf voll gilt, 0 = 6 % (D-44)
}

// stufenMaxSek: step_penalty bei Treppen = 1 (Rennrad bisher, D-30).
const stufenMaxSek = 600

// anliegerSek: destination_only_penalty für Fußwege bei „anlieger“ statt Valhallas 600 s (D-43).
const anliegerSek = 60

func (m *Meiden) gueltig() bool {
	for _, x := range []float64{m.Treppen, m.Belag, m.Hauptstrassen, m.Steigung, m.Faehren, m.Dunkel, m.Autobahn, m.Maut} {
		if !(x >= 0 && x <= 1) {
			return false
		}
	}
	for _, x := range []float64{m.KmhAuf, m.KmhAb} {
		if !(x >= 0 && x <= hangMaxKMH) {
			return false
		}
	}
	if len(m.BelagKmh) != 0 && len(m.BelagKmh) != belagKlassen {
		return false
	}
	for _, x := range m.BelagKmh {
		if !(x >= 0 && x <= hangMaxKMH) {
			return false
		}
	}
	if !(m.HangVoll >= 0 && m.HangVoll <= hangMaxVoll) {
		return false
	}
	return m.BusMin >= 0 && (m.Umstiege == nil || *m.Umstiege >= 0)
}

// art: Tempo frei wie bei Custom; mit Bus-Minuten oder Fahrradkosten wird wie beim Rad mit dem direkten Weg verglichen.
func (m *Meiden) art() Art {
	a := Art{Max: math.Inf(1), Rad: m.Fusswege || m.BusMin > 0, NurBahn: m.NurBahn, Belag: m.Belag, Stufen: m.Treppen * stufenMaxSek,
		BusMalus: time.Duration(m.BusMin) * time.Minute, Meiden: m}
	if m.Fusswege {
		a.RadTyp = "Hybrid"
		if m.Belag >= 0.8 {
			a.RadTyp = "Road"
		} else if m.Belag == 0 {
			a.RadTyp = "Mountain"
		}
	}
	return a
}

// setzen trägt den Valhalla-Wert ein: Standard × (1 − Stufe); bei Stufe 0 bleibt Valhallas Standard.
func setzen(o map[string]any, name string, standard, stufe float64) {
	if stufe > 0 {
		o[name] = standard * (1 - stufe)
	}
}

func (m *Meiden) fussOpts(o map[string]any) {
	setzen(o, "use_hills", 0.5, m.Steigung)
	setzen(o, "use_ferry", 0.5, m.Faehren)
	setzen(o, "use_tracks", 0.5, m.Belag)
	if m.Dunkel > 0 {
		o["use_lit"] = m.Dunkel
	}
	if m.Anlieger {
		o["destination_only_penalty"] = anliegerSek
	}
}

func (m *Meiden) radOpts(o map[string]any) {
	setzen(o, "use_roads", 0.6, m.Hauptstrassen)
	setzen(o, "use_hills", 0.5, m.Steigung)
	setzen(o, "use_ferry", 0.5, m.Faehren)
}

func (m *Meiden) autoOpts(o map[string]any) {
	setzen(o, "use_highways", 1, m.Autobahn)
	setzen(o, "use_tolls", 0.5, m.Maut)
	setzen(o, "use_ferry", 0.5, m.Faehren)
}

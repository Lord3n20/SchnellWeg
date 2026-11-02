package api

import (
	"math"
	"time"
)

// Fortbewegungsarten (D-21, D-22, D-30). Zu Fuß und Einrad rechnen mit Valhallas Fußgänger-Kosten (Wege wie zu
// Fuß) und skalieren die Zeit aufs gewählte Tempo. Die Räder fahren mit Fahrrad-Kosten ihres Typs (Straßen und
// Radwege, keine reinen Fußwege, keine Treppen) und fallen nur dann auf Fußwege mit Treppenstrafe zurück, wenn es
// sonst keinen Weg gibt (Ziel mitten in der Fußgängerzone, Bahnsteig nur über Treppen).
type Art struct {
	Min, Max float64 // km/h; Min gilt ausschließlich, wenn er 0 ist (Custom: jedes Tempo über 0)
	Rad      bool    // Fahrrad: direkter Weg steht vorn, ÖV nur, wenn nötig
	NurBahn  bool    // nur Schiene, Fahrten brauchen ein Fahrradticket
	RadTyp   string  // Valhalla bicycle_type (Road | Hybrid | City | Mountain); "" = nur Fußgänger-Kosten
	Belag    float64 // avoid_bad_surfaces 0–1 (Rennrad meidet Schotter, MTB nicht)
	Stufen   float64 // step_penalty für die Fußgänger-Kosten in s (0 = Treppen wie zu Fuß)
	// BusMalus: Verbindungen mit Bus entfallen, wenn sie nicht mindestens so viel früher ankommen als der direkte
	// Weg (Klapprad: ein paar Stationen Bus sind kaum schneller als selbst fahren, Betreiber 28.10.).
	BusMalus time.Duration
	Auto     bool    // Autofahrt von Tür zu Tür ohne ÖV, kmh = Höchsttempo (D-32)
	Meiden   *Meiden // Profil aus der App statt fester Art (D-34)
}

var arten = map[string]Art{
	"":       {Min: 2, Max: 8},
	"fuss":   {Min: 2, Max: 8},
	"einrad": {Min: 0, Max: math.Inf(1)}, // App „Custom“: frei wählbar (Enzo 28.10.), 0 und weniger ergäbe keine Wegzeit
	// zusammengeklappt in Bus und Bahn, kein Ticket
	"klapprad": {Min: 6, Max: 30, Rad: true, BusMalus: 5 * time.Minute, RadTyp: "City", Belag: 0.5, Stufen: 120},
	"fixie":    {Min: 6, Max: 40, Rad: true, NurBahn: true, RadTyp: "Hybrid", Belag: 0.5, Stufen: 300},
	"rad":      {Min: 6, Max: 40, Rad: true, NurBahn: true, RadTyp: "Hybrid", Belag: 0.5, Stufen: 300}, // App vor D-22 = Fixie
	"rennrad":  {Min: 10, Max: 45, Rad: true, NurBahn: true, RadTyp: "Road", Belag: 0.8, Stufen: 600},
	"mtb":      {Min: 6, Max: 35, Rad: true, NurBahn: true, RadTyp: "Mountain", Stufen: 180}, // überall lang, Treppen nur, wenn's viel spart
	"auto":     {Min: 30, Max: 500, Auto: true},
}

const radMinKMH, radMaxKMH = 5.0, 60.0 // Bereich von Valhallas cycling_speed

// Kosten ist eine Valhalla-Kostenart samt Faktor, mit dem ihre Zeiten zur Zeit im gewählten Tempo werden.
type Kosten struct {
	Costing  string
	Opts     map[string]any
	Faktor   float64
	FangM    int            // Fangradius auf die Straße in m, 0 = Valhallas Standard (35 km)
	Schritte bool           // Abbiegehinweise mitliefern (Wege von Tür zu Tür)
	Polygone [][][2]float64 // Bereiche, die die Route meidet (Umfahrung, D-39)
}

// kosten liefert die Valhalla-Kostenarten für Art a in der Reihenfolge, in der sie versucht werden.
// scale: Faktor auf Fußzeiten bei walkKMH (BaseKMH / Tempo).
func (a Art) kosten(walkKMH, kmh, scale float64) []Kosten {
	fuss := map[string]any{"walking_speed": walkKMH}
	if a.Stufen > 0 {
		fuss["step_penalty"] = a.Stufen
	}
	if m := a.Meiden; m != nil {
		m.fussOpts(fuss)
	}
	k := []Kosten{{Costing: "pedestrian", Opts: map[string]any{"pedestrian": fuss}, Faktor: scale}}
	if a.RadTyp != "" {
		// Außerhalb 5–60 km/h nähme Valhalla sein Standardtempo: dort den Randwert schicken und die Zeit skalieren
		vh := min(radMaxKMH, max(radMinKMH, kmh))
		rad := map[string]any{"bicycle_type": a.RadTyp, "cycling_speed": vh, "use_roads": 0.6, "avoid_bad_surfaces": a.Belag}
		if m := a.Meiden; m != nil {
			m.radOpts(rad)
		}
		k = append([]Kosten{{Costing: "bicycle", Opts: map[string]any{"bicycle": rad}, Faktor: vh / kmh}}, k...)
	}
	return k
}

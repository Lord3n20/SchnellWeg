package api

import "time"

// Fortbewegungsarten (D-21, D-22). Zu Fuß, Einrad, Klapprad, Fixie und Mountainbike rechnen mit Valhallas
// Fußgänger-Kosten (Wege wie zu Fuß, Treppen trag- bzw. schiebbar) und skalieren die Zeit aufs gewählte Tempo;
// das Rennrad fährt mit Fahrrad-Kosten (Straßen und Radwege, keine Fußwege, keine Treppen) und fällt nur dann auf
// Fußwege mit hoher Treppenstrafe zurück, wenn es sonst keinen Weg gibt.
type Art struct {
	Min, Max float64 // km/h
	Rad      bool    // Fahrrad: direkter Weg steht vorn, ÖV nur, wenn nötig
	NurBahn  bool    // nur Schiene, Fahrten brauchen ein Fahrradticket
	Strasse  bool    // Valhalla-Fahrradkosten (Rennrad)
	Stufen   float64 // step_penalty für die Fußgänger-Kosten in s (0 = Treppen wie zu Fuß)
	// BusMalus: Verbindungen mit Bus entfallen, wenn sie nicht mindestens so viel früher ankommen als der direkte
	// Weg (Klapprad: ein paar Stationen Bus sind kaum schneller als selbst fahren, Betreiber 28.10.).
	BusMalus time.Duration
}

var arten = map[string]Art{
	"":         {Min: 2, Max: 8},
	"fuss":     {Min: 2, Max: 8},
	"einrad":   {Min: 4, Max: 20},
	"klapprad": {Min: 6, Max: 30, Rad: true, BusMalus: 3 * time.Minute}, // zusammengeklappt in Bus und Bahn, kein Ticket
	"fixie":    {Min: 6, Max: 40, Rad: true, NurBahn: true},
	"rad":      {Min: 6, Max: 40, Rad: true, NurBahn: true}, // App vor D-22 = Fixie
	"rennrad":  {Min: 10, Max: 45, Rad: true, NurBahn: true, Strasse: true, Stufen: 600},
	"mtb":      {Min: 6, Max: 35, Rad: true, NurBahn: true, Stufen: 180}, // überall lang, Treppen nur, wenn's viel spart
}

// Kosten ist eine Valhalla-Kostenart samt Faktor, mit dem ihre Zeiten zur Zeit im gewählten Tempo werden.
type Kosten struct {
	Costing string
	Opts    map[string]any
	Faktor  float64
}

// kosten liefert die Valhalla-Kostenarten für Art a in der Reihenfolge, in der sie versucht werden.
// scale: Faktor auf Fußzeiten bei walkKMH (BaseKMH / Tempo).
func (a Art) kosten(walkKMH, kmh, scale float64) []Kosten {
	fuss := map[string]any{"walking_speed": walkKMH}
	if a.Stufen > 0 {
		fuss["step_penalty"] = a.Stufen
	}
	k := []Kosten{{"pedestrian", map[string]any{"pedestrian": fuss}, scale}}
	if a.Strasse {
		rad := map[string]any{"bicycle_type": "Road", "cycling_speed": kmh, "use_roads": 0.6, "avoid_bad_surfaces": 0.8}
		k = append([]Kosten{{"bicycle", map[string]any{"bicycle": rad}, 1}}, k...)
	}
	return k
}

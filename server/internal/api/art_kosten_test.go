package api

import "testing"

func TestKostenRadTyp(t *testing.T) {
	for name, typ := range map[string]string{"klapprad": "City", "fixie": "Hybrid", "rad": "Hybrid", "rennrad": "Road", "mtb": "Mountain"} {
		k := arten[name].kosten(5, 20, 0.25)
		if len(k) != 2 || k[0].Costing != "bicycle" || k[1].Costing != "pedestrian" {
			t.Fatalf("%s: %+v", name, k)
		}
		if got := k[0].Opts["bicycle"].(map[string]any)["bicycle_type"]; got != typ {
			t.Errorf("%s: bicycle_type %v, erwartet %s", name, got, typ)
		}
		if k[0].Faktor != 1 || k[1].Faktor != 0.25 {
			t.Errorf("%s: Faktoren %v/%v", name, k[0].Faktor, k[1].Faktor)
		}
	}
	for _, name := range []string{"", "fuss", "einrad"} {
		if k := arten[name].kosten(5, 5, 1); len(k) != 1 || k[0].Costing != "pedestrian" {
			t.Errorf("%q: %+v", name, k)
		}
	}
}

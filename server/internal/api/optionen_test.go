package api

import (
	"testing"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// netz baut Busse A→B (5 min) und Züge B→C (40 min), Abfahrten in Sekunden.
func netz(busse, zuege []int32) *raptor.Timetable {
	tt := &raptor.Timetable{Stops: []raptor.Stop{{ID: "A", Name: "A"}, {ID: "B", Name: "B"}, {ID: "C", Name: "C"}}}
	route := func(a, b int32, ab []int32, fahrt int32, line string) raptor.Route {
		r := raptor.Route{Stops: []int32{a, b}, NTrips: len(ab)}
		for _, d := range ab {
			r.Arr = append(r.Arr, d, d+fahrt)
			r.Dep = append(r.Dep, d, d+fahrt)
			r.Trips = append(r.Trips, raptor.TripInfo{ID: line, Line: line})
		}
		return r
	}
	tt.Routes = []raptor.Route{route(0, 1, busse, 300, "21"), route(1, 2, zuege, 2400, "RE9")}
	tt.StopRoutes = [][]int32{{0}, {0, 1}, {1}}
	tt.StopPos = [][]int32{{0}, {1, 0}, {1}}
	tt.Footpaths = [][]raptor.Footpath{nil, nil, nil}
	return tt
}

func takt(von, bis, schritt int32) []int32 {
	var out []int32
	for d := von; d <= bis; d += schritt {
		out = append(out, d)
	}
	return out
}

// Am Start A nur der Bus, zu Fuß käme man nicht zum Zug.
var (
	abA = []raptor.Access{{Stop: 0, Sec: 60}}
	anC = []raptor.Access{{Stop: 2, Sec: 60}}
)

// Dichter Bus zum stündlichen Zug: Die späteren Busse zum selben Zug dürfen die Abfragen nicht aufbrauchen, sonst
// bleibt nur eine Verbindung (Betrieb: Elisenbrunnen→Düren bei 4,3 km/h).
func TestOptionenDichterZubringer(t *testing.T) {
	const h = 3600
	tt := netz(takt(8*h, 13*h, 120), takt(8*h+40*60, 13*h, h))
	out := optionen(raptor.NewRouter(tt), 8*h-120, abA, anC, 3, 4.3, 0)
	if len(out) < 3 {
		t.Fatalf("erwartet mindestens 3 Verbindungen, bekam %d", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i].j.Arr <= out[i-1].j.Arr {
			t.Errorf("Option %d kommt nicht später an: %d nach %d", i, out[i].j.Arr, out[i-1].j.Arr)
		}
	}
}

// Springt der wachsende Schritt über den letzten Bus, muss dessen Verbindung trotzdem kommen.
func TestOptionenLetzterBus(t *testing.T) {
	const h = 3600
	tt := netz(takt(8*h, 8*h+10*60, 120), []int32{8*h + 13*60, 8*h + 20*60})
	out := optionen(raptor.NewRouter(tt), 8*h-120, abA, anC, 3, 4.3, 0)
	if len(out) != 2 || out[1].j.Arr != 8*h+60*60+60 {
		t.Fatalf("erwartet beide Züge, bekam %d: %+v", len(out), out)
	}
}

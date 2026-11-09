package api

import (
	"testing"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

const fahrtStart = 8 * 3600

// Linie mit fünf Halten, je 500 m und 90 s auseinander; eine Fahrt ab 08:00, 20 s Halt.
func fahrtTT() *raptor.Timetable {
	tt := &raptor.Timetable{}
	r := raptor.Route{NTrips: 1, Trips: []raptor.TripInfo{{ID: "t", Line: "33"}}}
	for i := int32(0); i < 5; i++ {
		tt.Stops = append(tt.Stops, raptor.Stop{ID: string(rune('A' + i)), Name: string(rune('A' + i)), Lat: 50.77 + float64(i)*0.0045, Lon: 6.08})
		tt.StopRoutes = append(tt.StopRoutes, []int32{0})
		tt.StopPos = append(tt.StopPos, []int32{i})
		r.Stops = append(r.Stops, i)
		an := int32(fahrtStart) + i*110
		r.Arr = append(r.Arr, an)
		r.Dep = append(r.Dep, an+20)
	}
	tt.Routes = []raptor.Route{r}
	return tt
}

// spur fährt von Halt a nach b: [steht] Sekunden an jedem Halt, [fahrt] Sekunden je Abschnitt, Beginn zur Sekunde [t], Punkt alle 4 s.
func spur(tt *raptor.Timetable, base time.Time, a, b int, t, steht, fahrt int, vorher int) [][4]float64 {
	var out [][4]float64
	add := func(lat float64, sek int, v float64) {
		out = append(out, [4]float64{lat, 6.08, float64(base.Unix()) + float64(sek), v})
	}
	for s := 0; s < vorher; s += 4 {
		add(tt.Stops[a].Lat, t-vorher+s, 0)
	}
	for i := a; i <= b; i++ {
		for s := 0; s < steht; s += 4 {
			add(tt.Stops[i].Lat, t+s, 0)
		}
		t += steht
		if i == b {
			break
		}
		for s := 0; s < fahrt; s += 4 {
			add(tt.Stops[i].Lat+(tt.Stops[i+1].Lat-tt.Stops[i].Lat)*float64(s)/float64(fahrt), t+s, 500/float64(fahrt))
		}
		t += fahrt
	}
	return out
}

func TestFahrtErkannt(t *testing.T) {
	tt, base := fahrtTT(), DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	f := fahrtenSuchen(tt, base, spur(tt, base, 0, 4, fahrtStart, 20, 90, 120))
	if len(f) != 1 || !f[0].Sicher || f[0].Linie != "33" || !f[0].Bus || f[0].Von.ID != "A" || f[0].Nach.ID != "E" {
		t.Fatalf("%+v", f)
	}
	if f[0].M < 1900 || f[0].WarteS < 100 {
		t.Errorf("Meter/Warten: %+v", f[0])
	}
}

func TestLongboardImBusTaktIstKeineFahrt(t *testing.T) {
	tt, base := fahrtTT(), DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	// fährt zur selben Zeit dieselbe Strecke, hält aber an keinem Halt
	if f := fahrtenSuchen(tt, base, spur(tt, base, 0, 4, fahrtStart, 0, 110, 0)); len(f) != 0 {
		t.Fatalf("%+v", f)
	}
}

func TestNurZweiHalteMitWartenIstUnsicher(t *testing.T) {
	tt, base := fahrtTT(), DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	f := fahrtenSuchen(tt, base, spur(tt, base, 0, 1, fahrtStart, 20, 90, 120))
	if len(f) != 1 || f[0].Sicher {
		t.Fatalf("%+v", f)
	}
}

func TestAndereZeitIstKeineFahrt(t *testing.T) {
	tt, base := fahrtTT(), DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	if f := fahrtenSuchen(tt, base, spur(tt, base, 0, 4, fahrtStart+3600, 20, 90, 120)); len(f) != 0 {
		t.Fatalf("%+v", f)
	}
}

func TestSchnelleBahnOhneZwischenhalt(t *testing.T) {
	tt, base := fahrtTT(), DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	// 2 km in 80 s: ein Zug, im Schnitt 25 m/s
	r := &tt.Routes[0]
	r.Bahn = true
	for i := int32(0); i < 5; i++ {
		r.Arr[i] = fahrtStart + 20 + i*20
		r.Dep[i] = r.Arr[i]
	}
	p := [][4]float64{}
	for i := 0; i <= 20; i++ {
		lat := tt.Stops[0].Lat + (tt.Stops[4].Lat-tt.Stops[0].Lat)*float64(i)/20
		p = append(p, [4]float64{lat, 6.08, float64(base.Unix()) + fahrtStart + 20 + float64(i)*4, 25})
	}
	f := fahrtenSuchen(tt, base, p)
	if len(f) == 0 || !f[0].Sicher || f[0].Bus {
		t.Fatalf("%+v", f)
	}
}

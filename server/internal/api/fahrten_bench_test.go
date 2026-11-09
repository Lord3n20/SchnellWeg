package api

import (
	"math/rand"
	"testing"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Größe wie der NRW-Fahrplan (rund 200 000 Halte), dazu ein ungünstiger Korridor: 400 dicht liegende Halte entlang der Spur,
// durch die 60 Linien mit je 200 Fahrten laufen. Spur: zwei Stunden GPS im Sekundentakt.
func fahrtBenchDaten() (*raptor.Timetable, time.Time, [][4]float64) {
	const korridor, linien, fahrtenJe, halteJe = 400, 60, 200, 80
	rnd := rand.New(rand.NewSource(1))
	tt := &raptor.Timetable{}
	for i := 0; i < 200000; i++ {
		lat, lon := 50.3+rnd.Float64()*2.2, 5.9+rnd.Float64()*3.5
		if i < korridor {
			lat, lon = 50.5+float64(i)*0.0009, 6.08
		}
		tt.Stops = append(tt.Stops, raptor.Stop{ID: "h", Name: "h", Lat: lat, Lon: lon})
	}
	tt.StopRoutes, tt.StopPos = make([][]int32, len(tt.Stops)), make([][]int32, len(tt.Stops))
	for l := 0; l < linien; l++ {
		r := raptor.Route{NTrips: fahrtenJe}
		erster := l * (korridor - halteJe) / linien
		for j := 0; j < halteJe; j++ {
			h := int32(erster + j)
			r.Stops = append(r.Stops, h)
			tt.StopRoutes[h], tt.StopPos[h] = append(tt.StopRoutes[h], int32(l)), append(tt.StopPos[h], int32(j))
		}
		for t := 0; t < fahrtenJe; t++ {
			r.Trips = append(r.Trips, raptor.TripInfo{ID: "t", Line: "x"})
			for j := 0; j < halteJe; j++ {
				an := int32(6*3600 + t*300 + l*17 + erster*18 + j*18)
				r.Arr, r.Dep = append(r.Arr, an), append(r.Dep, an+2)
			}
		}
		tt.Routes = append(tt.Routes, r)
	}
	base := DayBase(time.Date(2026, 11, 9, 0, 0, 0, 0, Berlin))
	var spur [][4]float64
	for s := 0; s < 7200; s++ {
		v := 5.5
		if s%18 < 3 {
			v = 0
		}
		spur = append(spur, [4]float64{50.5 + float64(s)*0.00005, 6.08, float64(base.Unix()) + float64(8*3600+s), v})
	}
	return tt, base, spur
}

func BenchmarkFahrtenSuchen(b *testing.B) {
	tt, base, spur := fahrtBenchDaten()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := fahrtenSuchen(tt, base, spur)
		b.ReportMetric(float64(len(n)), "fahrten")
	}
}

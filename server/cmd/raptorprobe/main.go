// raptorprobe lädt einen GTFS-Auszug für einen Tag und misst RAPTOR-Anfragen zwischen neutralen Orten.
//
//	raptorprobe -gtfs sr_gtfs.zip,de_rv.zip -day 2026-10-28
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/Lord3n20/RWaysTH/server/internal/raptor"
)

var places = map[string][2]float64{
	"hbf": {50.7677, 6.0912}, "elisen": {50.7745, 6.0855}, "superc": {50.7780, 6.0780},
	"eschweiler": {50.8165, 6.2725}, "stolberg": {50.7700, 6.2270}, "herzogenrath": {50.8700, 6.0940},
	"monschau": {50.5550, 6.2400}, "uniklinik": {50.7760, 6.0450}, "koeln": {50.9430, 6.9585},
}

// box der Valhalla-Tiles (StädteRegion, wie scripts/measure/spike): lon/lat min, lon/lat max.
var box = [4]float64{5.95, 50.48, 6.45, 50.93}

var pairs = [][2]string{{"hbf", "elisen"}, {"hbf", "superc"}, {"hbf", "eschweiler"}, {"superc", "uniklinik"},
	{"hbf", "herzogenrath"}, {"stolberg", "monschau"}, {"elisen", "uniklinik"}, {"hbf", "koeln"}}

func vm(key string) string {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key) {
			return strings.Join(strings.Fields(l)[1:], " ")
		}
	}
	return "?"
}

func clock(s int32) string {
	if s < 0 {
		return "-" + clock(-s)
	}
	if s >= 86400 {
		return clock(s-86400) + " (+1)"
	}
	return fmt.Sprintf("%02d:%02d", s/3600, s/60%60)
}

func name(tt *raptor.Timetable, s int32) string {
	if s < 0 {
		return "Ort"
	}
	return tt.Stops[s].Name
}

// check prüft, ob ein Weg lückenlos und zeitlich aufsteigend ist.
func check(j raptor.Journey, dep int32, arr int32) string {
	t, at := dep, int32(-1)
	for i, l := range j.Legs {
		if l.Dep < t || l.Arr < l.Dep || (i > 0 && l.From != at) {
			return fmt.Sprintf("Abschnitt %d passt nicht", i)
		}
		t, at = l.Arr, l.To
	}
	if at != -1 || t != arr || j.Arr != arr {
		return "Ende passt nicht"
	}
	return ""
}

func show(tt *raptor.Timetable, j raptor.Journey) {
	for _, l := range j.Legs {
		if l.Kind == raptor.Walk {
			fmt.Printf("      %s–%s  zu Fuß %s → %s\n", clock(l.Dep), clock(l.Arr), name(tt, l.From), name(tt, l.To))
			continue
		}
		ti := tt.Routes[l.Route].Trips[l.Trip]
		v := ti.Line
		if ti.Headsign != "" {
			v += " Ri. " + ti.Headsign
		}
		if ti.PrevDay {
			v += " (Vortag)"
		}
		fmt.Printf("      %s–%s  %s: %s → %s\n", clock(l.Dep), clock(l.Arr), v, name(tt, l.From), name(tt, l.To))
	}
}

func main() {
	gtfs := flag.String("gtfs", "sr_gtfs.zip", "GTFS-Zip(s), durch Komma getrennt")
	dayS := flag.String("day", time.Now().Format("2006-01-02"), "Betriebstag")
	walk := flag.Float64("walk", 400, "max. Umstiegsfußweg in m (Luftlinie)")
	access := flag.Float64("access", 800, "max. Zugangsweg in m (Luftlinie)")
	legs := flag.Bool("legs", false, "Weg mit den wenigsten Fahrten ausgeben")
	pairsOut := flag.String("pairs-out", "", "Kandidatenpaare (Quelle in der StädteRegion-Box) für den Valhalla-Fußweg-Job schreiben und enden")
	accessOut := flag.String("access-out", "", "Zugangshalte der neutralen Orte als JSON für bench.py access schreiben und enden")
	perRoute := flag.Bool("per-route", false, "Zugang nur über den nächsten Halt je Muster")
	footpaths := flag.String("footpaths", "", "Valhalla-Fußwege (CSV aus footpaths.py) statt Luftlinie, soweit vorhanden; Wege über 2×walk entfallen")
	flag.Parse()
	day, _ := time.Parse("2006-01-02", *dayS)

	t0 := time.Now()
	tt, err := raptor.Load(strings.Split(*gtfs, ","), day, *walk)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
	near := func(p [2]float64) []raptor.Access {
		acc := tt.Near(p[0], p[1], *access)
		if *perRoute {
			acc = tt.PerRoute(acc)
		}
		return acc
	}
	if *accessOut != "" {
		type st struct {
			ID       string
			Lat, Lon float64
		}
		out := map[string]any{}
		for k, p := range places {
			var ss []st
			for _, a := range near(p) {
				s := tt.Stops[a.Stop]
				ss = append(ss, st{s.ID, s.Lat, s.Lon})
			}
			out[k] = map[string]any{"lat": p[0], "lon": p[1], "stops": ss}
		}
		b, _ := json.Marshal(out)
		if err := os.WriteFile(*accessOut, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		return
	}
	if *pairsOut != "" {
		f, err := os.Create(*pairsOut)
		if err == nil {
			var n int
			n, err = tt.WritePairs(f, box)
			f.Close()
			fmt.Println(n, "Paare geschrieben")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		return
	}
	if *footpaths != "" {
		f, err := os.Open(*footpaths)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		src, n, err := tt.ReadFootpaths(f, int(2**walk))
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		fmt.Printf("Valhalla-Fußwege: %d Quellen ersetzt, %d Fußwege\n", src, n)
	}
	debug.FreeOSMemory() // Ladeabfall zurückgeben, damit RSS den Betriebsbedarf zeigt
	nfp, ntrip := 0, 0
	for _, f := range tt.Footpaths {
		nfp += len(f)
	}
	for _, r := range tt.Routes {
		ntrip += r.NTrips
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("Laden %v: %d Halte, %d Muster, %d Fahrten, %d Fußwege · Heap %d MB · RSS %s · Spitze %s\n",
		time.Since(t0).Round(time.Millisecond), len(tt.Stops), len(tt.Routes), ntrip, nfp, ms.HeapInuse>>20, vm("VmRSS"), vm("VmHWM"))

	r := raptor.NewRouter(tt)
	var lat []float64
	nj, bad := 0, 0
	for _, dep := range []int32{30 * 60, 7*3600 + 30*60, 12 * 3600, 17*3600 + 15*60, 22*3600 + 30*60} {
		for _, p := range pairs {
			a, b := places[p[0]], places[p[1]]
			from, to := near(a), near(b)
			for rep := 0; rep < 3; rep++ {
				s := time.Now()
				res := r.Query(dep, from, to, 6)
				lat = append(lat, float64(time.Since(s).Microseconds())/1000)
				if rep == 0 {
					for _, j := range res.Journeys {
						nj++
						if e := check(j, dep, res.ArrByTrips[j.Trips]); e != "" {
							bad++
							fmt.Printf("  FEHLER %s → %s ab %s, %d Fahrten: %s\n", p[0], p[1], clock(dep), j.Trips, e)
							show(tt, j)
						}
					}
				}
				if rep == 0 && (dep == 7*3600+30*60 || dep == 30*60) {
					var out []string
					for k, v := range res.ArrByTrips {
						if k > 0 && v < 1<<30 && (k == 1 || v < res.ArrByTrips[k-1]) {
							out = append(out, fmt.Sprintf("%d Fahrt(en) → %s", k, clock(v)))
						}
					}
					fmt.Printf("  %s → %s ab %s (%d/%d Zugangshalte): %s\n", p[0], p[1], clock(dep), len(from), len(to), strings.Join(out, ", "))
					if *legs && len(res.Journeys) > 0 {
						show(tt, res.Journeys[0])
					}
				}
			}
		}
	}
	sort.Float64s(lat)
	fmt.Printf("Wege geprüft: %d, fehlerhaft: %d\n", nj, bad)
	fmt.Printf("RAPTOR (max. 6 Fahrten, 5 Uhrzeiten × %d Paare × 3): n=%d Median %.1f ms, p95 %.1f ms, max %.1f ms · RSS %s · Spitze %s\n",
		len(pairs), len(lat), lat[len(lat)/2], lat[len(lat)*95/100], lat[len(lat)-1], vm("VmRSS"), vm("VmHWM"))
}

// rtprobe misst Abruf und Filterung des gtfs.de-Echtzeitfeeds (Phase 0, D-05).
//
//	rtprobe -trips trips.txt [-url URL | -file rt.pb] [-n 3]
//
// trips.txt: eine trip_id pro Zeile (Regionsfahrten). Ausgabe nur Zahlen, keine Fahrtinhalte.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	gtfs "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"

	"github.com/Lord3n20/SchnellWeg/server/internal/rt"
)

const userAgent = "SchnellWeg/0.0 (+https://github.com/Lord3n20/SchnellWeg)"

func main() {
	tripsPath := flag.String("trips", "", "Datei mit trip_ids der Region")
	url := flag.String("url", "https://realtime.gtfs.de/realtime-free.pb", "Feed-URL")
	file := flag.String("file", "", "lokale Datei statt URL")
	runs := flag.Int("n", 1, "Durchläufe (mind. 30 s Abstand bei URL)")
	flag.Parse()

	trips, err := readTrips(*tripsPath)
	if err != nil {
		fail(err)
	}
	fmt.Printf("Regionsfahrten: %d · GOMAXPROCS %d\n", len(trips), runtime.GOMAXPROCS(0))
	client := &http.Client{Timeout: 6 * time.Second}

	for i := 0; i < *runs; i++ {
		if i > 0 && *file == "" {
			time.Sleep(30 * time.Second)
		}
		var r io.ReadCloser
		start := time.Now()
		if *file != "" {
			r, err = os.Open(*file)
		} else {
			r, err = get(client, *url)
		}
		if err != nil {
			fail(err)
		}
		cpu0 := cpuTime()
		kept := make([]*gtfs.TripUpdate, 0, 1024)
		st, err := rt.Filter(r, trips, func(tu *gtfs.TripUpdate) { kept = append(kept, tu) })
		r.Close()
		if err != nil {
			fail(err)
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		fmt.Printf("Lauf %d: %.1f MB, %d Einträge, %d TripUpdates, %d behalten · Wand %v · CPU %v · Heap %d MB · VmHWM %s\n",
			i+1, float64(st.Bytes)/1e6, st.Entities, st.TripUpdates, st.Kept,
			time.Since(start).Round(time.Millisecond), (cpuTime() - cpu0).Round(time.Millisecond),
			ms.HeapInuse>>20, vmHWM())
		runtime.KeepAlive(kept)
	}
}

func get(c *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func readTrips(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			m[t] = struct{}{}
		}
	}
	return m, sc.Err()
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func vmHWM() string {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmHWM:") {
			return strings.Join(strings.Fields(l)[1:], " ")
		}
	}
	return "?"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "Fehler:", err)
	os.Exit(1)
}

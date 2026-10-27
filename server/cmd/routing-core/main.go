// routing-core lädt den Fahrplan des heutigen Betriebstags und beantwortet Routenanfragen per HTTP.
// Um Mitternacht lädt er den nächsten Tag im Hintergrund und schaltet ohne Ausfall um.
//
//	routing-core -gtfs sr_gtfs.zip,de_rv.zip,de_fv.zip:fern -footpaths footpaths.csv -listen :8090
//
// Als HA-Add-on: routing-core -options /data/options.json -dir /config (Optionen heißen wie die Flags).
// Mit gtfs_dir (Glob erlaubt, z. B. /addon_configs/*_routing_jobs/gtfs/current) kommen die Fahrpläne aus dem
// Job-Add-on; über den Symlink current liest der Tageswechsel um Mitternacht immer den neuesten Stand.
//
// Protokolliert werden nur Ladezeiten, Speicher und Fehlertypen, nie Anfragen.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/api"
	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
	"github.com/Lord3n20/SchnellWeg/server/internal/rt"
	_ "time/tzdata" // Image ohne Zeitzonen-Dateien
)

// applyOptions übernimmt Werte aus einer JSON-Datei (HA-Add-on-Optionen) für alle Flags, die nicht auf der
// Kommandozeile stehen.
func applyOptions(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var opts map[string]any
	if err := json.Unmarshal(b, &opts); err != nil {
		return err
	}
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for k, v := range opts {
		if set[k] || flag.Lookup(k) == nil {
			continue
		}
		if err := flag.Set(k, fmt.Sprint(v)); err != nil {
			return fmt.Errorf("Option %s: %w", k, err)
		}
	}
	return nil
}

func main() {
	gtfs := flag.String("gtfs", "sr_gtfs.zip", "GTFS-Zip(s), durch Komma getrennt")
	footpaths := flag.String("footpaths", "", "Valhalla-Fußwege (CSV, Job footpaths), soweit vorhanden; Glob erlaubt, ohne Treffer gilt die Luftlinie")
	walk := flag.Float64("walk", 400, "max. Umstiegsfußweg in m (Luftlinie); Valhalla-Wege bis 2×walk")
	access := flag.Float64("access", 800, "Suchradius für Zugangshalte in m (Luftlinie); echte Wege bis 2×access")
	valhalla := flag.String("valhalla", "", "Valhalla-URL für echte Zugangswege (leer = Luftlinie)")
	kmh := flag.Float64("kmh", 4.3, "Gehtempo für Zugangswege in km/h")
	rtURL := flag.String("rt", "https://realtime.gtfs.de/realtime-free.pb", "GTFS-RT-Feed (leer = nur Fahrplan); Abruf nur bei Anfragen, höchstens alle 30 s")
	rtWait := flag.Duration("rt_wait", 6*time.Second, "so lange wartet eine Anfrage höchstens auf den Echtzeit-Abruf")
	mapPath := flag.String("map", "", "Kartenausschnitt (PMTiles, Job map) für /v1/map; Glob erlaubt, wird beim Start aufgelöst")
	geo := flag.String("geocode", "", "Suchliste (TSV, Job geocode) für /v1/search; Glob erlaubt, ohne Treffer keine Suche")
	listen := flag.String("listen", ":8090", "Adresse der API")
	par := flag.Int("par", 2, "gleichzeitige Anfragen (je ein Router)")
	dayS := flag.String("day", "", "Betriebstag fest vorgeben (Test), sonst heute mit Wechsel um Mitternacht")
	dir := flag.String("dir", "", "Ordner, auf den sich relative Pfade in -gtfs/-footpaths beziehen")
	gtfsDir := flag.String("gtfs_dir", "", "Ordner für relative Pfade in -gtfs statt -dir (Glob erlaubt, erster Treffer)")
	options := flag.String("options", "", "JSON-Datei mit Optionen (HA-Add-on: /data/options.json)")
	flag.Parse()
	log.SetFlags(0)
	if *options != "" {
		if err := applyOptions(*options); err != nil {
			log.Fatal("Fehler in den Optionen: ", err)
		}
	}
	rel := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(*dir, p)
	}
	gdir := *dir
	if *gtfsDir != "" {
		m, _ := filepath.Glob(*gtfsDir)
		if len(m) == 0 {
			log.Fatal("Fehler: gtfs_dir nicht gefunden – erst das Job-Add-on mit job gtfs laufen lassen")
		}
		gdir = m[0]
	}
	var feeds []string
	for _, f := range strings.Split(*gtfs, ",") {
		if f = strings.TrimSpace(f); !filepath.IsAbs(f) {
			f = filepath.Join(gdir, f)
		}
		feeds = append(feeds, f)
	}
	*footpaths = rel(*footpaths)
	if strings.ContainsAny(*footpaths, "*?[") {
		m, _ := filepath.Glob(*footpaths)
		if len(m) == 0 {
			log.Print("Keine Fußwege gefunden, Umstiege mit Luftlinie")
			*footpaths = ""
		} else {
			*footpaths = m[0]
		}
	}

	load := func(day time.Time) (*api.Plan, error) {
		t0 := time.Now()
		tt, err := raptor.Load(feeds, day, *walk)
		if err != nil {
			return nil, err
		}
		if *footpaths != "" {
			f, err := os.Open(*footpaths)
			if err != nil {
				return nil, err
			}
			_, _, err = tt.ReadFootpaths(f, int(2**walk))
			f.Close()
			if err != nil {
				return nil, err
			}
		}
		p := api.NewPlan(tt, day, *par)
		runtime.GC()
		debug.FreeOSMemory()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		log.Printf("Fahrplan %s geladen in %v: %d Halte, %d Muster, Heap %d MB",
			day.Format("2006-01-02"), time.Since(t0).Round(time.Millisecond), len(tt.Stops), len(tt.Routes), ms.HeapInuse>>20)
		return p, nil
	}

	var geoIx *geocode.Index
	if m, _ := filepath.Glob(rel(*geo)); *geo != "" && len(m) > 0 {
		t0 := time.Now()
		f, err := os.Open(m[0])
		if err != nil {
			log.Fatal("Fehler beim Laden der Suchliste: ", err)
		}
		ix, err := geocode.Load(f)
		f.Close()
		if err != nil {
			log.Fatal("Fehler beim Laden der Suchliste: ", err)
		}
		geoIx = ix
		log.Printf("Suchliste geladen in %v: %d Einträge", time.Since(t0).Round(time.Millisecond), len(ix.Entries))
	} else if *geo != "" {
		log.Print("Keine Suchliste gefunden, /v1/search ist aus")
	}
	if m, _ := filepath.Glob(rel(*mapPath)); *mapPath != "" && len(m) > 0 {
		*mapPath = m[0]
	} else if *mapPath != "" && strings.ContainsAny(*mapPath, "*?[") {
		// Glob ohne Treffer: Karte kommt mit dem nächsten Job-Lauf; Ordner auflösen, sobald er existiert
		log.Print("Noch keine Karte gefunden, /v1/map antwortet 503")
		*mapPath = ""
	}
	srv := &api.Server{Geocode: geoIx, MapPath: *mapPath, AccessM: *access, MaxTrips: 6, WaitLimit: 2 * time.Second, LiveWait: *rtWait}
	if *rtURL != "" {
		// Zeitbudget der Quelle 6 s je Versuch (CLAUDE.md), Stände älter als 5 min gelten nicht mehr
		srv.Live = &rt.Live{URL: *rtURL, Client: &http.Client{Timeout: 6 * time.Second}, MinAge: 30 * time.Second,
			MaxAge: 5 * time.Minute, UA: "SchnellWeg (+https://github.com/Lord3n20/SchnellWeg)"}
	}
	if *valhalla != "" {
		srv.Valhalla = &api.Valhalla{URL: strings.TrimRight(*valhalla, "/"), KMH: *kmh, MaxM: 2 * *access,
			Timeout: 300 * time.Millisecond, Client: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	}
	today := func() time.Time {
		y, m, d := time.Now().In(api.Berlin).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, api.Berlin)
	}
	day := today()
	if *dayS != "" {
		d, err := time.ParseInLocation("2006-01-02", *dayS, api.Berlin)
		if err != nil {
			log.Fatal("Fehler: -day ungültig")
		}
		day = d
	}
	p, err := load(day)
	if err != nil {
		log.Fatal("Fehler beim Laden: ", err)
	}
	srv.Swap(p)

	if *dayS == "" {
		go func() {
			for {
				next := today().AddDate(0, 0, 1)
				time.Sleep(time.Until(next))
				for try := 0; ; try++ {
					p, err := load(next)
					if err == nil {
						srv.Swap(p) // alter Plan wird frei, sobald die letzte Anfrage darauf fertig ist
						break
					}
					log.Print("Fehler beim Tageswechsel: ", err)
					if try == 5 {
						break // alter Tag bleibt; Anfragen bekommen „anderer_betriebstag“
					}
					time.Sleep(time.Minute)
				}
			}
		}()
	}

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.New(os.Stderr, "", 0),
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(c)
	}()
	log.Printf("API auf %s", *listen)
	if err := hs.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal("Fehler: ", err)
	}
}

// routing-jobs baut die Daten für routing-core und Valhalla. Ein Aufruf arbeitet die Jobs nacheinander ab und
// beendet sich; gestartet wird er nur von einer HA-Automation (kein Scheduler im Container).
//
//	routing-jobs -job gtfs,osm,valhalla -dir /config
//
// Jobs:
//
//	gtfs      Fahrpläne von gtfs.de laden (ETag), Nahverkehr auf gtfs_box schneiden, mit dem RAPTOR-Lader prüfen
//	          → gtfs/current/{sr_gtfs,de_rv,de_fv}.zip
//	osm       nur wenn der Stand älter als osm_tage ist: Geofabrik-Ausschnitte laden (ETag + md5), mit osmconvert auf config/region.geojson schneiden
//	          zusammenführen und mit osmium neu nummerieren → osm/current/region.osm.pbf
//	valhalla  Valhalla-Tiles aus osm/current bauen und mit Testrouten prüfen → valhalla/current/tiles.tar;
//	          danach wird das Valhalla-Add-on neu gestartet, falls es läuft
//	footpaths Umstiegswege zwischen nahen Halten (GTFS-Stand) mit einem kurz gestarteten Valhalla auf dem
//	          Tile-Stand rechnen (D-12) → footpaths/current/footpaths.csv
//	geocode   Suchliste (Adressen, Straßen, Orte, Einrichtungen in gtfs_box) aus dem OSM-Stand
//	          → geocode/current/geocode.tsv
//	map       Kartenausschnitt (Protomaps-Basiskarte, PMTiles) für die App, höchstens alle osm_tage
//	          → map/current/region.pmtiles
//
// Hat gtfs, footpaths oder geocode einen neuen Stand gebracht, wird am Ende routing-core neu gestartet, falls es läuft.
//
// Jeder Job prüft vorher freien Speicher und Platz, misst den anonymen Speicher der cgroup, bricht beim
// Überschreiten des Budgets ab und schaltet erst nach erfolgreicher Prüfung um (vorheriger Stand bleibt).
// Unveränderte Eingänge → Job wird übersprungen. Ergebnis als HA-Benachrichtigung.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Lord3n20/RWaysTH/server/internal/jobs"
)

var (
	dir         = flag.String("dir", "/config", "Ablage (HA: Konfigurationsordner des Job-Add-ons)")
	jobList     = flag.String("job", "gtfs,osm,valhalla,footpaths,geocode,map", "Jobs, durch Komma getrennt, in dieser Reihenfolge")
	sources     = flag.String("osm_sources", "", "Geofabrik-Pfade ohne -latest.osm.pbf, durch Komma getrennt")
	region      = flag.String("region", "/etc/rwaysth/region.geojson", "Regionspolygon (GeoJSON)")
	maxMem      = flag.Int64("max_mem_mb", 2048, "Speicherspitze je Job in MB (zusätzlich ≤ MemAvailable − 1 GB)")
	concurrency = flag.Int("concurrency", 4, "Threads für den Valhalla-Bau (≤ 4)")
	minDisk     = flag.Int64("min_free_disk_mb", 6000, "nötiger freier Platz vor dem Start")
	force       = flag.Bool("force", false, "auch bei unveränderten Eingängen neu bauen")
	gtfsBox     = flag.String("gtfs_box", "5.95,50.48,6.45,50.93", "Nahverkehr: Fahrten mit Halt in dieser Box (minlon,minlat,maxlon,maxlat)")
	mapBox      = flag.String("map_box", "5.95,50.48,6.45,50.93", "Kartenausschnitt (minlon,minlat,maxlon,maxlat)")
	mapZoom     = flag.Int("map_zoom", 15, "höchste Zoomstufe der Karte")
	osmDays     = flag.Int("osm_tage", 7, "OSM + Valhalla erst neu bauen, wenn der Stand so alt ist (0 = immer prüfen)")
	options     = flag.String("options", "", "JSON-Datei mit Optionen (HA-Add-on: /data/options.json)")
)

// osmFormat ändert sich, wenn der osm-Job anders baut; dann wird trotz gleicher Quellen neu gebaut.
const osmFormat = "renumber"

// errSkip meldet: Eingang unverändert, nichts zu tun.
var errSkip = errors.New("unverändert")

func main() {
	flag.Parse()
	log.SetFlags(0)
	if *options != "" {
		if err := applyOptions(*options); err != nil {
			log.Fatal("Fehler in den Optionen: ", err)
		}
	}
	*concurrency = min(max(*concurrency, 1), 4)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sup := jobs.NewSupervisor()

	var report []string
	failed, coreData := false, false
	for _, name := range strings.Split(*jobList, ",") {
		name = strings.TrimSpace(name)
		run, ok := map[string]func(context.Context, *jobs.Watch) (string, error){
			"gtfs": jobGTFS, "osm": jobOSM, "valhalla": jobValhalla, "footpaths": jobFootpaths, "geocode": jobGeocode, "map": jobMap,
		}[name]
		if !ok {
			log.Fatalf("Fehler: unbekannter Job %q", name)
		}
		line, err := runJob(ctx, name, run)
		log.Print(line)
		report = append(report, line)
		if err != nil && !errors.Is(err, errSkip) {
			failed = true
			break // spätere Jobs bauen auf früheren auf
		}
		if name == "valhalla" && err == nil {
			restartSibling(ctx, sup, "valhalla", &report)
		}
		coreData = coreData || (err == nil && (name == "gtfs" || name == "footpaths" || name == "geocode"))
	}
	if coreData { // einmal am Ende, auch wenn ein späterer Job fehlschlug: der neue Stand ist geprüft
		restartSibling(ctx, sup, "routing_core", &report)
	}
	title := "RWaysTH-Daten aktualisiert"
	if failed {
		title = "RWaysTH-Datenjob fehlgeschlagen"
	}
	if err := sup.Notify(ctx, "rwaysth_jobs", title, strings.Join(report, "\n")); err != nil {
		log.Print("Benachrichtigung fehlgeschlagen: ", err)
	}
	if failed {
		os.Exit(1)
	}
}

// runJob prüft Speicher und Platz, überwacht den Job und liefert eine Berichtszeile.
func runJob(parent context.Context, name string, run func(context.Context, *jobs.Watch) (string, error)) (string, error) {
	t0 := time.Now()
	avail, err := jobs.MemAvailableMB()
	if err != nil {
		return fmt.Sprintf("%s: Fehler: Speicher nicht lesbar", name), err
	}
	budget := jobs.Budget(*maxMem, avail)
	if budget < 1024 {
		return fmt.Sprintf("%s: abgebrochen vor Start, nur %d MB verfügbar (Budget %d MB)", name, avail, budget),
			errors.New("zu wenig Speicher")
	}
	if free, err := jobs.FreeDisk(*dir); err == nil && free < *minDisk {
		return fmt.Sprintf("%s: abgebrochen vor Start, nur %d MB Platz frei", name, free), errors.New("zu wenig Platz")
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	w := jobs.NewWatch(ctx, budget, func(used int64) {
		cancel(fmt.Errorf("Speicherbudget %d MB überschritten (%d MB)", budget, used))
	})
	info, err := run(ctx, w)
	w.Sample()
	if c := context.Cause(ctx); c != nil && err != nil {
		err = c
	}
	took := time.Since(t0).Round(time.Second)
	switch {
	case errors.Is(err, errSkip):
		return fmt.Sprintf("%s: übersprungen (%s), %v", name, info, took), err
	case err != nil:
		return fmt.Sprintf("%s: Fehler: %v (Spitze %d MB, %v)", name, err, w.PeakMB(), took), err
	}
	return fmt.Sprintf("%s: ok, %s (Spitze %d MB von %d, %v)", name, info, w.PeakMB(), budget, took), nil
}

// cmd startet ein Programm mit niedriger Priorität in eigener Prozessgruppe; bei Abbruch (Speicherbudget,
// SIGTERM) wird die ganze Gruppe beendet. Ausgaben landen in logf, nicht im Add-on-Protokoll.
func cmd(ctx context.Context, logf, name string, args ...string) error {
	c := exec.CommandContext(ctx, "nice", append([]string{"-n", "19", "ionice", "-c3", name}, args...)...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	if logf != "" {
		f, err := os.OpenFile(logf, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		c.Stdout, c.Stderr = f, f
	}
	c.Env = append(os.Environ(), "GOMAXPROCS=4")
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return fmt.Errorf("%s: %w (Details in %s)", name, err, filepath.Base(logf))
	}
	return nil
}

// jobOSM lädt die Geofabrik-Ausschnitte und schneidet sie auf das Regionspolygon.
func jobOSM(ctx context.Context, w *jobs.Watch) (string, error) {
	if info, fresh := fresh("osm"); fresh {
		return info, errSkip
	}
	var paths []string
	for _, s := range strings.Split(*sources, ",") {
		if s = strings.Trim(strings.TrimSpace(s), "/"); s != "" {
			paths = append(paths, s)
		}
	}
	if len(paths) == 0 {
		return "", errors.New("keine osm_sources")
	}
	raw := filepath.Join(*dir, "raw")
	os.MkdirAll(raw, 0o755)
	changed := false
	var files []string
	for _, p := range paths {
		dst := filepath.Join(raw, strings.ReplaceAll(p, "/", "_")+".osm.pbf")
		u := "https://download.geofabrik.de/" + p + "-latest.osm.pbf"
		ch, err := jobs.Fetch(ctx, u, u+".md5", dst)
		if err != nil {
			return "", fmt.Errorf("Download %s: %w", p, err)
		}
		changed = changed || ch
		files = append(files, dst)
	}
	// Signatur der Eingänge: ETags + Polygon. Gleich wie im aktuellen Stand → überspringen.
	sig := osmFormat + ";" + inputSig(files, *region)
	cur := jobs.Current(*dir, "osm")
	if !*force && !changed && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "Quellen unverändert", errSkip
	}

	f, err := os.Open(*region)
	if err != nil {
		return "", err
	}
	poly, err := jobs.PolyFromGeoJSON(f)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("Region: %w", err)
	}
	r, err := jobs.NewRelease(*dir, "osm", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	polyFile := filepath.Join(r.Dir, "region.poly")
	if err := os.WriteFile(polyFile, []byte(poly), 0o644); err != nil {
		return "", err
	}
	logf := filepath.Join(r.Dir, "osm.log")
	var cuts []string
	for i, src := range files {
		out := filepath.Join(r.Dir, fmt.Sprintf("teil%d.o5m", i))
		// D-10: osmconvert mit begrenztem Hash statt osmium (das hält ID-Mengen über den ganzen ID-Raum).
		if err := cmd(ctx, logf, "osmconvert", src, "-B="+polyFile, "--complete-ways", "--hash-memory=400", "-o="+out); err != nil {
			return "", err
		}
		cuts = append(cuts, out)
	}
	merged := filepath.Join(r.Dir, "zusammen.osm.pbf")
	if err := cmd(ctx, logf, "osmconvert", append(cuts, "-o="+merged)...); err != nil {
		return "", err
	}
	for _, c := range cuts {
		os.Remove(c)
	}
	// IDs ab 1 neu nummerieren: Valhalla legt Bitsets über den ganzen OSM-ID-Raum an (gemessen 27.10.:
	// 2,9 GB beim Tile-Bau mit Original-IDs, 1,2 GB nach renumber). Puffergrößen per OSMIUM_* im Image.
	pbf := filepath.Join(r.Dir, "region.osm.pbf")
	if err := cmd(ctx, logf, "osmium", "renumber", "-o", pbf, merged); err != nil {
		return "", err
	}
	os.Remove(merged)
	nodes, ways, err := osmStats(ctx, pbf)
	if err != nil {
		return "", err
	}
	// Prüfung: plausible Größe und kein Einbruch gegenüber dem bisherigen Stand.
	if nodes < 1_000_000 {
		return "", fmt.Errorf("Prüfung: nur %d Knoten", nodes)
	}
	if prev, _ := strconv.ParseInt(jobs.ReadStamp(cur, "knoten"), 10, 64); prev > 0 && nodes < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Knoten, vorher %d (−10 %% Grenze)", nodes, prev)
	}
	st, _ := os.Stat(pbf)
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "knoten", strconv.FormatInt(nodes, 10))
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s, %d MB, %d Knoten, %d Wege", r.ID, st.Size()>>20, nodes, ways), nil
}

func inputSig(files []string, extra ...string) string {
	var b strings.Builder
	for _, f := range files {
		et, _ := os.ReadFile(f + ".etag")
		fmt.Fprintf(&b, "%s=%s;", filepath.Base(f), strings.TrimSpace(string(et)))
	}
	for _, e := range extra { // Inhalt statt Änderungszeit: die ändert sich mit jedem neuen Image
		if data, err := os.ReadFile(e); err == nil {
			fmt.Fprintf(&b, "%s@%x;", filepath.Base(e), sha256.Sum256(data))
		}
	}
	return b.String()
}

// osmStats liest Knoten- und Wegzahl aus osmconvert --out-statistics.
func osmStats(ctx context.Context, pbf string) (nodes, ways int64, err error) {
	c := exec.CommandContext(ctx, "nice", "-n", "19", "osmconvert", pbf, "--out-statistics")
	out, err := c.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("osmconvert --out-statistics: %w", err)
	}
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		k, v, _ := strings.Cut(s.Text(), ":")
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch k {
		case "nodes":
			nodes = n
		case "ways":
			ways = n
		}
	}
	return nodes, ways, nil
}

// jobValhalla baut Tiles aus dem aktuellen OSM-Stand und prüft sie mit einem kurzzeitig gestarteten Dienst.
func jobValhalla(ctx context.Context, w *jobs.Watch) (string, error) {
	osm := jobs.Current(*dir, "osm")
	if osm == "" {
		return "", errors.New("kein OSM-Stand, erst Job osm")
	}
	osmID := filepath.Base(osm)
	cur := jobs.Current(*dir, "valhalla")
	if !*force && cur != "" && jobs.ReadStamp(cur, "osm") == osmID {
		return "OSM-Stand " + osmID + " schon gebaut", errSkip
	}
	r, err := jobs.NewRelease(*dir, "valhalla", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	logf := filepath.Join(r.Dir, "bau.log")
	tiles := filepath.Join(r.Dir, "tiles")
	conf := filepath.Join(r.Dir, "valhalla.json")
	out, err := exec.CommandContext(ctx, "valhalla_build_config",
		"--mjolnir-tile-dir", tiles, "--mjolnir-tile-extract", filepath.Join(r.Dir, "tiles.tar"),
		"--mjolnir-admin", filepath.Join(r.Dir, "admin.sqlite"), "--mjolnir-timezone", filepath.Join(r.Dir, "tz.sqlite"),
		"--mjolnir-concurrency", strconv.Itoa(*concurrency), "--mjolnir-max-cache-size", "64000000",
		"--httpd-service-listen", "tcp://127.0.0.1:8012").Output()
	if err != nil {
		return "", fmt.Errorf("valhalla_build_config: %w", err)
	}
	if err := os.WriteFile(conf, out, 0o644); err != nil {
		return "", err
	}
	pbf := filepath.Join(osm, "region.osm.pbf")
	steps := []struct {
		name string
		args []string
	}{
		{"valhalla_build_admins", []string{"-c", conf, pbf}},
		{"valhalla_build_tiles", []string{"-c", conf, pbf}},
		{"valhalla_build_extract", []string{"-c", conf}},
	}
	var times []string
	for _, s := range steps {
		t := time.Now()
		if err := cmd(ctx, logf, s.name, s.args...); err != nil {
			return "", err
		}
		times = append(times, fmt.Sprintf("%s %v", strings.TrimPrefix(s.name, "valhalla_build_"), time.Since(t).Round(time.Second)))
	}
	// Nur tiles.tar wird im Betrieb gebraucht (mmap); Einzel-Tiles und admin.sqlite wieder weg.
	os.RemoveAll(tiles)
	os.Remove(filepath.Join(r.Dir, "admin.sqlite"))
	st, err := os.Stat(filepath.Join(r.Dir, "tiles.tar"))
	if err != nil || st.Size() < 10<<20 {
		return "", errors.New("Prüfung: tiles.tar fehlt oder zu klein")
	}
	if err := checkValhalla(ctx, conf, logf); err != nil {
		return "", fmt.Errorf("Prüfung: %w", err)
	}
	jobs.WriteStamp(r.Dir, "osm", osmID)
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s aus OSM %s, tiles.tar %d MB (%s)", r.ID, osmID, st.Size()>>20, strings.Join(times, ", ")), nil
}

// Testrouten zwischen neutralen Orten: Fußweg in Aachen, Auto bis Köln und nach Maastricht (deckt den Rand ab).
var checks = []struct{ costing, from, to string }{
	{"pedestrian", "50.7678,6.0911", "50.7748,6.0868"}, // Aachen Hbf → Elisenbrunnen
	{"auto", "50.7678,6.0911", "50.9430,6.9589"},       // Aachen Hbf → Köln Hbf
	{"bicycle", "50.7678,6.0911", "50.8497,5.7053"},    // Aachen Hbf → Maastricht
}

// startValhalla startet valhalla_service mit conf (lauscht auf 127.0.0.1:8012); stop beendet ihn wieder.
func startValhalla(ctx context.Context, conf, logf string, threads int) (stop func(), err error) {
	ctx, cancel := context.WithCancel(ctx)
	lf, err := os.OpenFile(logf, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		cancel()
		return nil, err
	}
	c := exec.CommandContext(ctx, "nice", "-n", "19", "valhalla_service", conf, strconv.Itoa(threads))
	c.Stdout, c.Stderr = lf, lf
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	if err := c.Start(); err != nil {
		cancel()
		lf.Close()
		return nil, err
	}
	// erst beenden, dann warten (sonst wartet Wait ewig auf den Dienst); mehrfacher Aufruf schadet nicht
	var once sync.Once
	return func() { once.Do(func() { cancel(); c.Wait(); lf.Close() }) }, nil
}

func checkValhalla(ctx context.Context, conf, logf string) error {
	stop, err := startValhalla(ctx, conf, logf, 1)
	if err != nil {
		return err
	}
	defer stop()
	var last error
	for _, ch := range checks {
		body := fmt.Sprintf(`{"locations":[{"lat":%s,"lon":%s},{"lat":%s,"lon":%s}],"costing":%q}`,
			strings.Split(ch.from, ",")[0], strings.Split(ch.from, ",")[1],
			strings.Split(ch.to, ",")[0], strings.Split(ch.to, ",")[1], ch.costing)
		for try := 0; try < 30; try++ { // Dienst braucht einen Moment zum Starten
			last = routeOK(ctx, body)
			if last == nil || ctx.Err() != nil {
				break
			}
			time.Sleep(time.Second)
		}
		if last != nil {
			return fmt.Errorf("%s: %w", ch.costing, last)
		}
	}
	return nil
}

func routeOK(ctx context.Context, body string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8012/route", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Trip struct {
			Status  int `json:"status"`
			Summary struct {
				Length float64 `json:"length"`
			} `json:"summary"`
		} `json:"trip"`
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.Trip.Status != 0 || r.Trip.Summary.Length <= 0 {
		return fmt.Errorf("keine Route")
	}
	return nil
}

func restartSibling(ctx context.Context, sup *jobs.Supervisor, name string, report *[]string) {
	slug, err := sup.SiblingSlug(ctx, name)
	if err == nil {
		var done bool
		done, err = sup.RestartIfRunning(ctx, slug)
		if done {
			*report = append(*report, name+": neu gestartet")
		}
	}
	if err != nil {
		*report = append(*report, name+": Neustart fehlgeschlagen ("+err.Error()+")")
	}
}

// applyOptions übernimmt Werte aus einer JSON-Datei (HA-Add-on-Optionen) für alle Flags, die nicht auf der
// Kommandozeile stehen (wie in routing-core).
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
		if l, ok := v.([]any); ok { // Listen-Optionen (osm_sources) → Komma
			parts := make([]string, len(l))
			for i, x := range l {
				parts[i] = fmt.Sprint(x)
			}
			v = strings.Join(parts, ",")
		}
		if err := flag.Set(k, fmt.Sprint(v)); err != nil {
			return fmt.Errorf("Option %s: %w", k, err)
		}
	}
	return nil
}

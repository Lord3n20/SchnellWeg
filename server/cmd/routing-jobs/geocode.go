package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// jobGeocode baut die Suchliste für routing-core aus eigenen, ganzen Geofabrik-Ländern (geocode_sources, ohne Box;
// D-26) → geocode/current/geocode.tsv + geocode.idx (Wortindex, routing-core blendet beides per mmap ein).
// Höchstens alle osm_tage und nur bei neuem Stand der Quellen.
func jobGeocode(ctx context.Context, w *jobs.Watch) (string, error) {
	if info, fresh := fresh("geocode"); fresh {
		return info, errSkip
	}
	var files []string
	raw := filepath.Join(*dir, "geocode-quellen")
	os.MkdirAll(raw, 0o755)
	changed := false
	for _, p := range strings.Split(*geoSources, ",") {
		if p = strings.Trim(strings.TrimSpace(p), "/"); p == "" {
			continue
		}
		dst := filepath.Join(raw, strings.ReplaceAll(p, "/", "_")+".osm.pbf")
		u := "https://download.geofabrik.de/" + p + "-latest.osm.pbf"
		ch, err := jobs.Fetch(ctx, u, u+".md5", dst)
		if err != nil {
			return "", fmt.Errorf("Download %s: %w", p, err)
		}
		changed = changed || ch
		files = append(files, dst)
	}
	if len(files) == 0 {
		return "", errors.New("keine geocode_sources")
	}
	sig := "format=2;" + inputSig(files)
	cur := jobs.Current(*dir, "geocode")
	if !*force && !changed && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "Quellen unverändert", errSkip
	}
	// Das Add-on setzt GOMEMLIMIT=100MiB; damit liefe ständig der GC. Lebendig sind ganz DE ~0,5 GB (Knoten,
	// Straßen, PBF-Puffer; der Index liegt in seiner eingeblendeten Datei), mehr Spielraum ließe den Heap nur bis
	// zur Grenze wachsen (Betreiber 28.10.: < 1 GB). Die harte Grenze des Jobs überwacht w (max_mem_mb).
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(min(*maxMem*3/4, 800) << 20))
	r, err := jobs.NewRelease(*dir, "geocode", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	tmp := filepath.Join(r.Dir, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return "", err
	}
	tsv := filepath.Join(r.Dir, "geocode.tsv")
	out, err := os.Create(tsv)
	if err != nil {
		return "", err
	}
	t0 := time.Now()
	kinds, err := geocode.ExtractFiles(ctx, files, tmp, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("OSM lesen: %w", err)
	}
	os.Remove(tmp)
	tExtract := time.Since(t0)
	n := 0
	for _, k := range kinds {
		n += k
	}
	// Prüfung: plausibel viele Adressen (27.10.: 180 000 allein in der StädteRegion), kein Einbruch gegenüber vorher
	if kinds["adresse"] < 50_000 || kinds["strasse"] < 2_000 {
		return "", fmt.Errorf("Prüfung: nur %d Adressen, %d Straßen", kinds["adresse"], kinds["strasse"])
	}
	if prev, _ := strconv.Atoi(jobs.ReadStamp(cur, "eintraege")); prev > 0 && n < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Einträge, vorher %d (−10 %% Grenze)", n, prev)
	}
	debug.FreeOSMemory()
	t0 = time.Now()
	if err := buildIndex(tsv, filepath.Join(r.Dir, "geocode.idx")); err != nil {
		return "", fmt.Errorf("Index: %w", err)
	}
	// Probe: der fertige Index findet einen bekannten Ort
	ix, err := geocode.LoadFiles(tsv)
	if err != nil {
		return "", err
	}
	if hs := ix.Search("aachen", 3); len(hs) == 0 {
		return "", errors.New("Prüfung: Suche nach „aachen“ ohne Treffer")
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "eintraege", strconv.Itoa(n))
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s, %d Adressen, %d Straßen, %d Orte, %d Einrichtungen (Liste %v, Index %v)", r.ID,
		kinds["adresse"], kinds["strasse"], kinds["ort"], kinds["poi"], tExtract.Round(time.Second),
		time.Since(t0).Round(time.Second)), nil
}

func buildIndex(tsv, idx string) error {
	f, err := os.Open(tsv)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return err
	}
	defer syscall.Munmap(data)
	return geocode.BuildIndexFile(data, idx)
}

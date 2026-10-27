package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// Protomaps-Basiskarte: tägliche Builds des ganzen Planeten (ODbL Produced Work, OSM-Namensnennung; Betreiber
// rät vom direkten Verlinken ab und empfiehlt eigene Kopien → Ausschnitt per HTTP-Range, docs/data-sources.md).
const mapBuilds = "https://build.protomaps.com/"

// jobMap schneidet die Region aus dem neuesten Protomaps-Build (pmtiles extract, nur die nötigen Byte-Bereiche)
// → map/current/region.pmtiles. Höchstens alle osm_tage neu, die Karte ändert sich langsam.
func jobMap(ctx context.Context, w *jobs.Watch) (string, error) {
	if info, fresh := fresh("map"); fresh {
		return info, errSkip
	}
	if _, err := jobs.ParseBox(*mapBox); err != nil {
		return "", err
	}
	build, err := latestBuild(ctx)
	if err != nil {
		return "", err
	}
	sig := fmt.Sprintf("build=%s;box=%s;zoom=%d", build, *mapBox, *mapZoom)
	cur := jobs.Current(*dir, "map")
	if !*force && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "Build " + build + " schon ausgeschnitten", errSkip
	}
	r, err := jobs.NewRelease(*dir, "map", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	out := filepath.Join(r.Dir, "region.pmtiles")
	logf := filepath.Join(r.Dir, "extract.log")
	t := time.Now()
	if err := cmd(ctx, logf, "pmtiles", "extract", mapBuilds+build+".pmtiles", out, "--bbox="+*mapBox,
		fmt.Sprintf("--maxzoom=%d", *mapZoom), "--download-threads=2"); err != nil {
		return "", err
	}
	took := time.Since(t).Round(time.Second)
	st, err := os.Stat(out)
	if err != nil || st.Size() < 2<<20 || st.Size() > 2<<30 {
		return "", errors.New("Prüfung: Kartendatei fehlt oder Größe unplausibel")
	}
	if err := cmd(ctx, logf, "pmtiles", "verify", out); err != nil {
		return "", fmt.Errorf("Prüfung: %w", err)
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s aus Build %s, %d MB (Ausschnitt %v)", r.ID, build, st.Size()>>20, took), nil
}

// latestBuild sucht den neuesten verfügbaren Tages-Build (heute bis vor 7 Tagen).
func latestBuild(ctx context.Context) (string, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	for d := 0; d < 8; d++ {
		name := time.Now().UTC().AddDate(0, 0, -d).Format("20060102")
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, mapBuilds+name+".pmtiles", nil)
		req.Header.Set("User-Agent", jobs.UserAgent)
		resp, err := c.Do(req)
		if err != nil {
			return "", fmt.Errorf("Protomaps nicht erreichbar: %w", errors.Unwrap(err))
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return name, nil
		}
	}
	return "", errors.New("kein Protomaps-Build der letzten 7 Tage")
}

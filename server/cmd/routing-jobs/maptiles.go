package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// Protomaps-Basiskarte: tägliche Builds des ganzen Planeten (ODbL Produced Work, OSM-Namensnennung; Betreiber
// rät vom direkten Verlinken ab und empfiehlt eigene Kopien → Ausschnitt per HTTP-Range, docs/data-sources.md).
const mapBuilds = "https://build.protomaps.com/"

// Kartenregionen (D-20): Bundesländer als Rechtecke, kleine Stadtstaaten beim Nachbarn. Überlappungen sind
// gewollt (die App nimmt die erste Datei, die eine Kachel hat). Rechtecke reichen etwas über die Grenze.
var mapRegions = []struct{ ID, Name, Box string }{
	{"nw", "Nordrhein-Westfalen", "5.86,50.32,9.47,52.53"},
	{"bw", "Baden-Württemberg", "7.50,47.53,10.50,49.80"},
	{"by", "Bayern", "8.97,47.27,13.84,50.57"},
	{"bb", "Berlin/Brandenburg", "11.26,51.36,14.77,53.56"},
	{"he", "Hessen", "7.77,49.39,10.24,51.66"},
	{"mv", "Mecklenburg-Vorpommern", "10.59,53.11,14.42,54.69"},
	{"ni", "Niedersachsen/Bremen", "6.65,51.29,11.60,53.89"},
	{"rp", "Rheinland-Pfalz", "6.11,48.97,8.51,50.94"},
	{"sl", "Saarland", "6.36,49.11,7.40,49.64"},
	{"sn", "Sachsen", "11.87,50.17,15.04,51.69"},
	{"st", "Sachsen-Anhalt", "10.56,50.94,13.19,53.04"},
	{"sh", "Schleswig-Holstein/Hamburg", "7.86,53.36,11.32,55.06"},
	{"th", "Thüringen", "9.88,50.20,12.65,51.65"},
}

// mapRegion beschreibt eine Datei in map/current/regionen.json (liest routing-core für /v1/map).
type mapRegion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Box  string `json:"box"`
	Zoom int    `json:"zoom"`
}

// jobMap holt Deutschland einmal aus dem neuesten Protomaps-Build (pmtiles extract, nur die nötigen
// Byte-Bereiche, ~8 GB) und schneidet daraus lokal die Übersicht (bis map_uebersicht_zoom) und je
// Bundesland eine Detaildatei. Die große Zwischendatei wird danach gelöscht. Höchstens alle map_tage neu.
func jobMap(ctx context.Context, w *jobs.Watch) (string, error) {
	// Frist nur, wenn der vorhandene Stand mit denselben Einstellungen gebaut wurde
	cfg := fmt.Sprintf("box=%s;zoom=%d;uebersicht=%d;regionen=%d", *mapBox, *mapZoom, *mapOverview, len(mapRegions))
	cur := jobs.Current(*dir, "map")
	if info, fresh := freshDays("map", *mapDays); fresh && cur != "" && strings.HasSuffix(jobs.ReadStamp(cur, "eingang"), ";"+cfg) {
		return info, errSkip
	}
	if _, err := jobs.ParseBox(*mapBox); err != nil {
		return "", err
	}
	for _, g := range mapRegions {
		if _, err := jobs.ParseBox(g.Box); err != nil {
			return "", fmt.Errorf("Region %s: %w", g.ID, err)
		}
	}
	if free, err := jobs.FreeDisk(*dir); err == nil && free < mapDiskMB {
		return "", fmt.Errorf("zu wenig Platz für die Karte: %d MB frei, %d MB nötig", free, mapDiskMB)
	}
	build, err := latestBuild(ctx)
	if err != nil {
		return "", err
	}
	sig := "build=" + build + ";" + cfg
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
	logf := filepath.Join(r.Dir, "extract.log")
	gesamt := filepath.Join(r.Dir, "gesamt.pmtiles.tmp")
	defer os.Remove(gesamt)
	t := time.Now()
	if err := cmd(ctx, logf, "pmtiles", "extract", mapBuilds+build+".pmtiles", gesamt, "--bbox="+*mapBox,
		fmt.Sprintf("--maxzoom=%d", *mapZoom), "--download-threads=2"); err != nil {
		return "", err
	}
	laden := time.Since(t).Round(time.Second)
	if st, err := os.Stat(gesamt); err != nil || st.Size() < 100<<20 {
		return "", errors.New("Prüfung: Gesamtkarte fehlt oder zu klein")
	}

	t = time.Now()
	index := []mapRegion{{"uebersicht", "Übersicht", *mapBox, *mapOverview}}
	for _, g := range mapRegions {
		index = append(index, mapRegion{g.ID, g.Name, g.Box, *mapZoom})
	}
	var summe int64
	for _, g := range index {
		out := filepath.Join(r.Dir, g.ID+".pmtiles")
		// lokale Quelle: pmtiles liest die Datei per Pfad, kein Netz
		if err := cmd(ctx, logf, "pmtiles", "extract", gesamt, out, "--bbox="+g.Box,
			fmt.Sprintf("--maxzoom=%d", g.Zoom)); err != nil {
			return "", fmt.Errorf("Region %s: %w", g.ID, err)
		}
		st, err := os.Stat(out)
		if err != nil || st.Size() < 1<<20 {
			return "", fmt.Errorf("Prüfung: Region %s fehlt oder zu klein", g.ID)
		}
		if err := cmd(ctx, logf, "pmtiles", "verify", out); err != nil {
			return "", fmt.Errorf("Prüfung %s: %w", g.ID, err)
		}
		summe += st.Size()
	}
	schneiden := time.Since(t).Round(time.Second)
	os.Remove(gesamt)
	b, _ := json.MarshalIndent(index, "", "  ")
	if err := os.WriteFile(filepath.Join(r.Dir, "regionen.json"), b, 0o644); err != nil {
		return "", err
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s aus Build %s: %d Dateien, %d MB (laden %v, schneiden %v)",
		r.ID, build, len(index), summe>>20, laden, schneiden), nil
}

// mapDiskMB: Gesamtdatei (~8 GB) + Regionen (~12 GB) + Rollback-Stand, mit Luft.
const mapDiskMB = 40000

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

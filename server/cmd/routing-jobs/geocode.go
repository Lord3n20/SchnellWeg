package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// jobGeocode baut aus dem aktuellen OSM-Stand die Suchliste für routing-core (Adressen, Straßen, Orte und benannte
// Einrichtungen in gtfs_box) → geocode/current/geocode.tsv. Nur bei neuem OSM-Stand.
func jobGeocode(ctx context.Context, w *jobs.Watch) (string, error) {
	osmDir := jobs.Current(*dir, "osm")
	if osmDir == "" {
		return "", errors.New("erst Job osm")
	}
	b, err := jobs.ParseBox(*gtfsBox)
	if err != nil {
		return "", err
	}
	sig := fmt.Sprintf("osm=%s;box=%s;format=1", filepath.Base(osmDir), *gtfsBox)
	cur := jobs.Current(*dir, "geocode")
	if !*force && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "OSM-Stand " + filepath.Base(osmDir) + " schon verarbeitet", errSkip
	}
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
	f, err := os.Open(filepath.Join(osmDir, "region.osm.pbf"))
	if err != nil {
		return "", err
	}
	es, err := geocode.Extract(ctx, f, geocode.Box(b))
	f.Close()
	if err != nil {
		return "", fmt.Errorf("OSM lesen: %w", err)
	}
	kinds := map[string]int{}
	for _, e := range es {
		kinds[e.Kind]++
	}
	// Prüfung: plausibel viele Adressen (27.10.: 180 000 in der StädteRegion), kein Einbruch gegenüber vorher
	if kinds["adresse"] < 50_000 || kinds["strasse"] < 2_000 {
		return "", fmt.Errorf("Prüfung: nur %d Adressen, %d Straßen", kinds["adresse"], kinds["strasse"])
	}
	if prev, _ := strconv.Atoi(jobs.ReadStamp(cur, "eintraege")); prev > 0 && len(es) < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Einträge, vorher %d (−10 %% Grenze)", len(es), prev)
	}
	out, err := os.Create(filepath.Join(r.Dir, "geocode.tsv"))
	if err != nil {
		return "", err
	}
	if err := geocode.Write(out, es); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "eintraege", strconv.Itoa(len(es)))
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s, %d Adressen, %d Straßen, %d Orte, %d Einrichtungen", r.ID, kinds["adresse"],
		kinds["strasse"], kinds["ort"], kinds["poi"]), nil
}

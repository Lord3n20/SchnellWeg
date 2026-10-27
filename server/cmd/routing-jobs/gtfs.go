package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// gtfsFormat ändert sich, wenn der gtfs-Job anders baut; dann wird trotz gleicher Quellen neu gebaut.
const gtfsFormat = "box1"

// gtfs.de, CC BY 4.0 (docs/data-sources.md). de_nv wird auf die Box geschnitten, de_rv/de_fv bleiben ganz (D-08).
var gtfsFeeds = []struct{ name, url string }{
	{"de_nv.zip", "https://download.gtfs.de/germany/nv_free/latest.zip"},
	{"de_rv.zip", "https://download.gtfs.de/germany/rv_free/latest.zip"},
	{"de_fv.zip", "https://download.gtfs.de/germany/fv_free/latest.zip"},
}

// jobGTFS lädt die Fahrpläne, schneidet den Nahverkehr auf die Box und prüft das Ergebnis mit dem Lader von
// routing-core für heute und in 6 Tagen (Feed darf nicht gleich auslaufen) → gtfs/current/{sr_gtfs,de_rv,de_fv}.zip
func jobGTFS(ctx context.Context, w *jobs.Watch) (string, error) {
	box, err := jobs.ParseBox(*gtfsBox)
	if err != nil {
		return "", err
	}
	raw := filepath.Join(*dir, "raw")
	os.MkdirAll(raw, 0o755)
	changed := false
	var files []string
	for _, f := range gtfsFeeds {
		dst := filepath.Join(raw, f.name)
		ch, err := jobs.Fetch(ctx, f.url, "", dst)
		if err != nil {
			return "", fmt.Errorf("Download %s: %w", f.name, err)
		}
		changed = changed || ch
		files = append(files, dst)
	}
	sig := gtfsFormat + ";" + inputSig(files) + "box=" + *gtfsBox
	cur := jobs.Current(*dir, "gtfs")
	if !*force && !changed && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "Quellen unverändert", errSkip
	}

	r, err := jobs.NewRelease(*dir, "gtfs", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	t := time.Now()
	sr := filepath.Join(r.Dir, "sr_gtfs.zip")
	st, err := jobs.CutGTFS(files[0], sr, box)
	if err != nil {
		return "", fmt.Errorf("Zuschnitt: %w", err)
	}
	if ctx.Err() != nil {
		return "", context.Cause(ctx)
	}
	cut := time.Since(t).Round(time.Second)
	for _, f := range files[1:] {
		if err := linkOrCopy(f, filepath.Join(r.Dir, filepath.Base(f))); err != nil {
			return "", err
		}
	}

	feeds := []string{sr, filepath.Join(r.Dir, "de_rv.zip"), filepath.Join(r.Dir, "de_fv.zip") + ":fern"}
	y, m, d := time.Now().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	var stops, routes int
	for _, day := range []time.Time{today, today.AddDate(0, 0, 6)} {
		tt, err := raptor.Load(feeds, day, 400)
		if err != nil {
			return "", fmt.Errorf("Prüfung: %w", err)
		}
		if len(tt.Routes) < 100 {
			return "", fmt.Errorf("Prüfung: nur %d Muster am %s", len(tt.Routes), day.Format("02.01."))
		}
		stops, routes = len(tt.Stops), max(routes, len(tt.Routes))
	}
	if prev, _ := strconv.Atoi(jobs.ReadStamp(cur, "halte")); prev > 0 && stops < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Halte, vorher %d (−10 %% Grenze)", stops, prev)
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "halte", strconv.Itoa(stops))
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s, Region %d Fahrten/%d Halte (Zuschnitt %v), gesamt %d Halte, %d Muster",
		r.ID, st.Trips, st.Stops, cut, stops, routes), nil
}

// linkOrCopy legt dst als harten Link auf src an (gleiches Dateisystem), sonst als Kopie.
func linkOrCopy(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fresh meldet, ob der aktuelle Stand einer Komponente jünger als osm_tage ist (Release-ID = Bauzeit in UTC).
// Der tägliche Lauf baut OSM, Valhalla und Karte so nur etwa wöchentlich neu (D-14); force baut immer.
func fresh(comp string) (string, bool) {
	cur := jobs.Current(*dir, comp)
	if *force || cur == "" || *osmDays <= 0 {
		return "", false
	}
	built, err := time.Parse("20060102-150405", filepath.Base(cur))
	if err != nil {
		return "", false
	}
	age := time.Since(built)
	// halber Tag Spielraum, damit ein Lauf um 04:30 statt 04:00 die Woche nicht verpasst
	if age >= time.Duration(*osmDays)*24*time.Hour-12*time.Hour {
		return "", false
	}
	return fmt.Sprintf("Stand %s ist %d Tage alt (neu ab %d)", filepath.Base(cur), int(age.Hours()/24), *osmDays), true
}

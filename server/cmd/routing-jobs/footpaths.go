package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Lord3n20/RWaysTH/server/internal/jobs"
	"github.com/Lord3n20/RWaysTH/server/internal/raptor"
)

// D-12: Umstiege vorsichtig mit 4,3 km/h, Kandidaten bis 400 m Luftlinie (routing-core -walk); Wege über 2 × walk
// verwirft erst routing-core beim Einlesen, die CSV enthält alle gefundenen Wege.
const (
	footKMH   = 4.3
	footWalkM = 400
)

// jobFootpaths rechnet die Fußwege zwischen nahen Halten des aktuellen GTFS-Stands mit einem kurz gestarteten
// Valhalla auf dem aktuellen Tile-Stand (wie scripts/measure/spike/footpaths.py) → footpaths/current/footpaths.csv.
// Quellen sind nur Halte in gtfs_box (dort liegen die Tiles sicher, de_rv/de_fv reichen bis in ganz DE).
func jobFootpaths(ctx context.Context, w *jobs.Watch) (string, error) {
	gtfs, vh := jobs.Current(*dir, "gtfs"), jobs.Current(*dir, "valhalla")
	if gtfs == "" || vh == "" {
		return "", errors.New("erst Jobs gtfs und valhalla")
	}
	box, err := jobs.ParseBox(*gtfsBox)
	if err != nil {
		return "", err
	}
	sig := fmt.Sprintf("gtfs=%s;valhalla=%s;kmh=%v;walk=%d;box=%s", filepath.Base(gtfs), filepath.Base(vh), footKMH, footWalkM, *gtfsBox)
	cur := jobs.Current(*dir, "footpaths")
	if !*force && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return "GTFS- und Tile-Stand schon gerechnet", errSkip
	}

	y, m, d := time.Now().Date()
	tt, err := raptor.Load([]string{filepath.Join(gtfs, "sr_gtfs.zip"), filepath.Join(gtfs, "de_rv.zip"),
		filepath.Join(gtfs, "de_fv.zip") + ":fern"}, time.Date(y, m, d, 0, 0, 0, 0, time.Local), footWalkM)
	if err != nil {
		return "", fmt.Errorf("GTFS: %w", err)
	}
	type task struct {
		from int32
		to   []int32
	}
	var tasks []task
	for i, fps := range tt.Footpaths {
		if s := tt.Stops[i]; len(fps) == 0 || !s.InBox(box) {
			continue
		}
		t := task{from: int32(i)}
		for _, fp := range fps {
			t.to = append(t.to, fp.To)
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return "", errors.New("keine Halte mit Nachbarn in der Box")
	}

	r, err := jobs.NewRelease(*dir, "footpaths", time.Now())
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			r.Abort()
		}
	}()
	conf := filepath.Join(r.Dir, "valhalla.json")
	out, err := exec.CommandContext(ctx, "valhalla_build_config",
		"--mjolnir-tile-extract", filepath.Join(vh, "tiles.tar"), "--mjolnir-tile-dir", "/nonexistent",
		"--mjolnir-admin", "/nonexistent/admin.sqlite", "--mjolnir-timezone", "/nonexistent/tz.sqlite",
		"--mjolnir-max-cache-size", "32000000", "--httpd-service-listen", "tcp://127.0.0.1:8012").Output()
	if err != nil {
		return "", fmt.Errorf("valhalla_build_config: %w", err)
	}
	if err := os.WriteFile(conf, out, 0o644); err != nil {
		return "", err
	}
	threads := max(*concurrency/2, 1)
	// Das Dienstprotokoll (~1 MB je Lauf, eine Zeile je Anfrage) wird nicht gebraucht; Fehler meldet matrix().
	stop, err := startValhalla(ctx, conf, os.DevNull, threads)
	if err != nil {
		return "", err
	}
	defer stop()
	// warten, bis der Dienst antwortet (erster Halt als Probe)
	var probe error
	for try := 0; try < 30; try++ {
		if _, probe = matrix(ctx, tt, tasks[0].from, tasks[0].to); probe == nil || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	if probe != nil {
		return "", fmt.Errorf("Valhalla antwortet nicht: %w", probe)
	}

	t0 := time.Now()
	results := make([][]cell, len(tasks))
	next := make(chan int)
	var wg sync.WaitGroup
	for range threads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				// Fehler (Halt nicht anschließbar) → alle Paare dieser Quelle ohne Weg, wie footpaths.py
				results[i], _ = matrix(ctx, tt, tasks[i].from, tasks[i].to)
			}
		}()
	}
	for i := range tasks {
		if ctx.Err() != nil {
			break
		}
		next <- i
	}
	close(next)
	wg.Wait()
	if ctx.Err() != nil {
		return "", context.Cause(ctx)
	}
	stop()

	f, err := os.Create(filepath.Join(r.Dir, "footpaths.csv"))
	if err != nil {
		return "", err
	}
	cw := csv.NewWriter(f)
	cw.Write([]string{"from_id", "to_id", "sec", "m"})
	pairs, found := 0, 0
	for i, t := range tasks {
		for j, to := range t.to {
			pairs++
			sec, m := "", ""
			if j < len(results[i]) && results[i][j].Time != nil && results[i][j].Distance != nil {
				sec, m = strconv.Itoa(*results[i][j].Time), strconv.Itoa(int(*results[i][j].Distance*1000+0.5))
				found++
			}
			cw.Write([]string{tt.Stops[t.from].ID, tt.Stops[to].ID, sec, m})
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// Prüfung: genug Paare, fast alle mit Weg (27.10.: 14 000 Paare) und kein Einbruch gegenüber vorher
	if pairs < 1000 || found < pairs*8/10 {
		return "", fmt.Errorf("Prüfung: %d Paare, nur %d mit Weg", pairs, found)
	}
	if prev, _ := strconv.Atoi(jobs.ReadStamp(cur, "wege")); prev > 0 && found < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Wege, vorher %d (−10 %% Grenze)", found, prev)
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "wege", strconv.Itoa(found))
	if err := r.Commit(); err != nil {
		return "", err
	}
	ok = true
	return fmt.Sprintf("Stand %s, %d Quellen, %d Paare, %d mit Weg (Matrix %v)", r.ID, len(tasks), pairs, found,
		time.Since(t0).Round(time.Second)), nil
}

type cell struct {
	Time     *int     `json:"time"`
	Distance *float64 `json:"distance"` // km
}

// matrix fragt die Gehzeiten von einem Halt zu seinen Nachbarn ab (one-to-many).
func matrix(ctx context.Context, tt *raptor.Timetable, from int32, to []int32) ([]cell, error) {
	type loc struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}
	a := tt.Stops[from]
	body := map[string]any{
		"sources":         []loc{{a.Lat, a.Lon}},
		"costing":         "pedestrian",
		"costing_options": map[string]any{"pedestrian": map[string]any{"walking_speed": footKMH}},
	}
	targets := make([]loc, len(to))
	for i, t := range to {
		targets[i] = loc{tt.Stops[t].Lat, tt.Stops[t].Lon}
	}
	body["targets"] = targets
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8012/sources_to_targets", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r struct {
		Rows [][]cell `json:"sources_to_targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if len(r.Rows) != 1 || len(r.Rows[0]) != len(to) {
		return nil, errors.New("unerwartete Antwort")
	}
	return r.Rows[0], nil
}

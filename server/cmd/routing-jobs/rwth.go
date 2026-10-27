package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// jobRWTH holt die Gebäudeliste der RWTH Maps (inoffiziell, D-18) → rwth/current/gebaeude.tsv, die routing-core
// zusätzlich zur Suchliste lädt. Rückfall für die App, die RWTH Maps sonst live fragt. Klein (~250 KB, 2 s),
// darum bei jedem Lauf; neue Version nur bei geänderter Liste.
func jobRWTH(ctx context.Context, _ *jobs.Watch) (string, error) {
	if *rwthURL == "" {
		return "aus (rwth_url leer)", errSkip
	}
	es, err := geocode.RWTHBuildings(ctx, &http.Client{Timeout: 30 * time.Second}, *rwthURL, jobs.UserAgent)
	if err != nil {
		return "", fmt.Errorf("RWTH Maps: %w", err)
	}
	cur := jobs.Current(*dir, "rwth")
	// Prüfung: 27.10.2026 waren es 359 Gebäude mit Koordinate; kein Einbruch gegenüber vorher
	if len(es) < 200 {
		return "", fmt.Errorf("Prüfung: nur %d Gebäude", len(es))
	}
	if prev, _ := strconv.Atoi(jobs.ReadStamp(cur, "eintraege")); prev > 0 && len(es) < prev*9/10 {
		return "", fmt.Errorf("Prüfung: %d Gebäude, vorher %d (−10 %% Grenze)", len(es), prev)
	}
	var buf bytes.Buffer
	if err := geocode.Write(&buf, es); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	sig := hex.EncodeToString(sum[:])
	if !*force && cur != "" && jobs.ReadStamp(cur, "eingang") == sig {
		return fmt.Sprintf("%d Gebäude, unverändert", len(es)), errSkip
	}
	r, err := jobs.NewRelease(*dir, "rwth", time.Now())
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "gebaeude.tsv"), buf.Bytes(), 0o644); err != nil {
		r.Abort()
		return "", err
	}
	jobs.WriteStamp(r.Dir, "eingang", sig)
	jobs.WriteStamp(r.Dir, "eintraege", strconv.Itoa(len(es)))
	if err := r.Commit(); err != nil {
		r.Abort()
		return "", err
	}
	return fmt.Sprintf("Stand %s, %d Gebäude", r.ID, len(es)), nil
}

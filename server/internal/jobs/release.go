// Package jobs enthält die Bausteine der Datenjobs: Releases mit atomarer Umschaltung, bedingte Downloads,
// Speicherwächter und HA-Benachrichtigung.
//
// Ablage je Komponente (osm, valhalla, …) im Konfigurationsordner des Job-Add-ons:
//
//	<komp>/releases/<zeitstempel>/   fertige Stände
//	<komp>/current  → releases/…     relativer Symlink, per rename umgeschaltet
//	<komp>/previous → releases/…     Rollback
package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Release ist ein Stand im Aufbau; erst Commit macht ihn sichtbar.
type Release struct {
	comp string // <dir>/<komp>
	ID   string
	Dir  string // Arbeitsordner, bis Commit mit Endung .tmp
}

// NewRelease legt <dir>/<komp>/releases/<zeitstempel>.tmp an und räumt liegengebliebene .tmp-Ordner ab.
func NewRelease(dir, comp string, now time.Time) (*Release, error) {
	base := filepath.Join(dir, comp)
	rels := filepath.Join(base, "releases")
	if err := os.MkdirAll(rels, 0o755); err != nil {
		return nil, err
	}
	old, _ := filepath.Glob(filepath.Join(rels, "*.tmp"))
	for _, o := range old {
		os.RemoveAll(o)
	}
	id := now.UTC().Format("20060102-150405")
	r := &Release{comp: base, ID: id, Dir: filepath.Join(rels, id+".tmp")}
	return r, os.Mkdir(r.Dir, 0o755)
}

// Abort verwirft den Stand.
func (r *Release) Abort() { os.RemoveAll(r.Dir) }

// Commit benennt den Stand um, schaltet current atomar darauf und previous auf den alten Stand.
// Ältere Stände außer current und previous werden gelöscht.
func (r *Release) Commit() error {
	final := strings.TrimSuffix(r.Dir, ".tmp")
	if err := os.Rename(r.Dir, final); err != nil {
		return err
	}
	r.Dir = final
	old, _ := os.Readlink(filepath.Join(r.comp, "current"))
	if err := swapLink(r.comp, "current", filepath.Join("releases", r.ID)); err != nil {
		return err
	}
	if old != "" {
		if err := swapLink(r.comp, "previous", old); err != nil {
			return err
		}
	}
	keep := map[string]bool{r.ID: true, filepath.Base(old): true}
	all, _ := os.ReadDir(filepath.Join(r.comp, "releases"))
	for _, e := range all {
		if !keep[e.Name()] {
			os.RemoveAll(filepath.Join(r.comp, "releases", e.Name()))
		}
	}
	return nil
}

func swapLink(dir, name, target string) error {
	tmp := filepath.Join(dir, name+".new")
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// Current liefert den Ordner des aktuellen Stands einer Komponente, "" wenn es keinen gibt.
func Current(dir, comp string) string {
	p := filepath.Join(dir, comp, "current")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	t, err := os.Readlink(p)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, comp, t)
}

// Releases listet die vorhandenen fertigen Stände (sortiert), für Tests und Status.
func Releases(dir, comp string) []string {
	all, _ := os.ReadDir(filepath.Join(dir, comp, "releases"))
	var ids []string
	for _, e := range all {
		if !strings.HasSuffix(e.Name(), ".tmp") {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

// WriteStamp legt in einem Stand eine Textdatei ab (Quelle, Kennzahlen), ReadStamp liest sie.
func WriteStamp(dir, name, text string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(text+"\n"), 0o644)
}

func ReadStamp(dir, name string) string {
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// FreeDisk liefert den freien Platz in MB (für die Prüfung vor dem Start).
func FreeDisk(dir string) (int64, error) {
	var st statfs
	if err := statFS(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs: %w", err)
	}
	return st.free >> 20, nil
}

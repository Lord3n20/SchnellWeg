package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseSwitch(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 28, 4, 0, 0, 0, time.UTC)
	var ids []string
	for i := range 3 {
		r, err := NewRelease(dir, "osm", t0.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if Current(dir, "osm") != "" && i == 0 {
			t.Fatal("current vor dem ersten Commit")
		}
		WriteStamp(r.Dir, "quelle", r.ID)
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
		if got := ReadStamp(Current(dir, "osm"), "quelle"); got != r.ID {
			t.Fatalf("current zeigt auf %q, erwartet %q", got, r.ID)
		}
	}
	prev, _ := os.Readlink(filepath.Join(dir, "osm", "previous"))
	if filepath.Base(prev) != ids[1] {
		t.Fatalf("previous = %s, erwartet %s", prev, ids[1])
	}
	if got := Releases(dir, "osm"); len(got) != 2 || got[0] != ids[1] || got[1] != ids[2] {
		t.Fatalf("Releases = %v, erwartet die letzten zwei", got)
	}
	// Abgebrochener Stand bleibt unsichtbar und wird beim nächsten Start weggeräumt.
	r, _ := NewRelease(dir, "osm", t0.Add(5*time.Hour))
	r.Abort()
	if _, err := os.Stat(r.Dir); !os.IsNotExist(err) {
		t.Fatal("Abort hat den Ordner nicht gelöscht")
	}
	if ReadStamp(Current(dir, "osm"), "quelle") != ids[2] {
		t.Fatal("Abort hat current verändert")
	}
}

func TestPoly(t *testing.T) {
	p, err := PolyFromGeoJSON(strings.NewReader(`{"type":"FeatureCollection","features":[{"geometry":{"type":"Polygon",
		"coordinates":[[[5.45,50.55],[5.45,50.98],[6.2,51.12],[5.45,50.55]]]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "region\n1\n  5.450000  50.550000\n  5.450000  50.980000\n  6.200000  51.120000\n  5.450000  50.550000\nEND\nEND\n"
	if p != want {
		t.Fatalf("poly =\n%s", p)
	}
	if _, err := PolyFromGeoJSON(strings.NewReader(`{"features":[]}`)); err == nil {
		t.Fatal("leere Collection ohne Fehler")
	}
}

func TestRegionFile(t *testing.T) {
	f, err := os.Open("../../../config/region.geojson")
	if err != nil {
		t.Skip("config/region.geojson nicht da")
	}
	defer f.Close()
	if _, err := PolyFromGeoJSON(f); err != nil {
		t.Fatal(err)
	}
}

func TestBudget(t *testing.T) {
	if Budget(2048, 6000) != 2048 || Budget(2048, 2500) != 1476 {
		t.Fatal("Budget falsch")
	}
}

func TestCgroupAnon(t *testing.T) {
	p := filepath.Join(t.TempDir(), "memory.stat")
	os.WriteFile(p, []byte("anon_thp 0\nanon 524288000\nfile 1\n"), 0o644)
	if mb, err := cgroupAnon(p); err != nil || mb != 500 {
		t.Fatalf("anon = %d, %v", mb, err)
	}
}

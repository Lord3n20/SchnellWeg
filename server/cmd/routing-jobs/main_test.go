package main

import (
	"slices"

	"context"
	"github.com/paulmach/osm"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// checkValhalla muss zurückkehren, auch wenn der Dienst von selbst nie endet (Fehler in 0.1.1: Wait vor cancel).
func TestCheckValhallaBeendetDienst(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "valhalla_service"), []byte("#!/bin/sh\nexec sleep 1000\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ln, err := net.Listen("tcp", "127.0.0.1:8012")
	if err != nil {
		t.Skip("Port 8012 belegt")
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"trip":{"status":0,"summary":{"length":1.5}},"height":[170]}`))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	done := make(chan error, 1)
	go func() { done <- checkValhalla(context.Background(), "c.json", filepath.Join(t.TempDir(), "bau.log")) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checkValhalla hängt nach erfolgreicher Prüfung")
	}
}

// Die Signatur hängt am Inhalt der Region, nicht an der Änderungszeit (neues Image ≠ neue Region).
func TestInputSigInhalt(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "region.geojson")
	os.WriteFile(reg, []byte(`{"a":1}`), 0o644)
	a := inputSig(nil, reg)
	os.Chtimes(reg, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	if b := inputSig(nil, reg); a != b {
		t.Fatal("Signatur ändert sich mit der Änderungszeit")
	}
	os.WriteFile(reg, []byte(`{"a":2}`), 0o644)
	if inputSig(nil, reg) == a {
		t.Fatal("Signatur erkennt geänderte Region nicht")
	}
}

func TestHgtKacheln(t *testing.T) {
	got, err := hgtKacheln([]byte(`{"features":[{"geometry":{"coordinates":[[[5.45,50.38],[7.25,50.38],[7.25,51.12],[5.45,50.38]]]}}]}`))
	want := []string{"N50/N50E005", "N50/N50E006", "N50/N50E007", "N51/N51E005", "N51/N51E006", "N51/N51E007"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestBelagKlasse(t *testing.T) {
	tags := func(s, g string) osm.Tags {
		o := osm.Tags{{Key: "highway", Value: "footway"}}
		if s != "" {
			o = append(o, osm.Tag{Key: "surface", Value: s})
		}
		if g != "" {
			o = append(o, osm.Tag{Key: "smoothness", Value: g})
		}
		return o
	}
	for _, c := range []struct {
		s, g string
		k    uint8
	}{{"ground", "", 1}, {"gravel", "", 2}, {"cobblestone", "", 3}, {"sett", "", 4}, {"sett", "bad", 3}, {"paving_stones", "", 5},
		{"asphalt", "", 0}, {"asphalt", "bad", 3}, {"asphalt", "very_bad", 1}, {"", "", 0}, {"sett", "good", 4}} {
		if k := belagKlasse(tags(c.s, c.g)); k != c.k {
			t.Errorf("%s/%s: %d, erwartet %d", c.s, c.g, k, c.k)
		}
	}
	if belagKlasse(osm.Tags{{Key: "surface", Value: "sett"}}) != 0 {
		t.Error("ohne highway kein Weg")
	}
}

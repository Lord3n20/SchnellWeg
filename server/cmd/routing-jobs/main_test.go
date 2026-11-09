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

func TestPflasterWeg(t *testing.T) {
	ja := []osm.Tags{{{Key: "highway", Value: "pedestrian"}, {Key: "surface", Value: "sett"}}, {{Key: "highway", Value: "path"}, {Key: "smoothness", Value: "bad"}}}
	nein := []osm.Tags{{{Key: "highway", Value: "footway"}, {Key: "surface", Value: "paving_stones"}}, {{Key: "surface", Value: "sett"}}, {{Key: "highway", Value: "residential"}, {Key: "surface", Value: "asphalt"}, {Key: "smoothness", Value: "good"}}}
	for _, x := range ja {
		if !pflasterWeg(x) {
			t.Errorf("%v soll Pflaster sein", x)
		}
	}
	for _, x := range nein {
		if pflasterWeg(x) {
			t.Errorf("%v soll kein Pflaster sein", x)
		}
	}
}

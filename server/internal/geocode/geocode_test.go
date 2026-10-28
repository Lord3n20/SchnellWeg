package geocode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testIndex(t *testing.T) *Index {
	es := []Entry{
		{Kind: "strasse", Name: "Pontstraße", Detail: "Aachen", Lat: 50.778, Lon: 6.081},
		{Kind: "adresse", Name: "Pontstraße 5", Detail: "52062 Aachen", Lat: 50.7781, Lon: 6.0812},
		{Kind: "adresse", Name: "Pontstraße 10", Detail: "52062 Aachen", Lat: 50.7782, Lon: 6.0813},
		{Kind: "adresse", Name: "Pontstraße 50", Detail: "52062 Aachen", Lat: 50.7783, Lon: 6.0814},
		{Kind: "poi", Name: "SuperC", Detail: "Templergraben 57, 52062 Aachen", Lat: 50.7784, Lon: 6.0782},
		{Kind: "ort", Name: "Eilendorf", Detail: "suburb", Lat: 50.78, Lon: 6.16},
		{Kind: "strasse", Name: "Eilendorfer Straße", Detail: "Aachen", Lat: 50.77, Lon: 6.12},
	}
	var b bytes.Buffer
	if err := Write(&b, es); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(&b)
	if err != nil || ix.Len() != len(es) {
		t.Fatalf("%v, %d Einträge", err, ix.Len())
	}
	return ix
}

func names(hs []Hit) string {
	var s []string
	for _, h := range hs {
		s = append(s, h.Name)
	}
	return strings.Join(s, "|")
}

func TestSearch(t *testing.T) {
	ix := testIndex(t)
	for q, want := range map[string]string{
		"pontstr":       "Pontstraße|Pontstraße 5|Pontstraße 10",
		"Pont Str. 5":   "Pontstraße 5|Pontstraße 50",
		"pontstraße 10": "Pontstraße 10",
		"superc":        "SuperC",
		"templergraben": "SuperC",
		"eilendorf":     "Eilendorf|Eilendorfer Straße",
		"xyz":           "",
		"":              "",
	} {
		if got := names(ix.Search(q, 3)); got != want {
			t.Errorf("%q: %q, erwartet %q", q, got, want)
		}
	}
}

// Mit echten Daten (ganzes Land, dauert Minuten):
// SCHNELLWEG_OSM=~/.cache/schnellweg-test/germany.osm.pbf go test ./internal/geocode -run Real -v -timeout 60m
func TestReal(t *testing.T) {
	p := os.Getenv("SCHNELLWEG_OSM")
	if p == "" {
		t.Skip("SCHNELLWEG_OSM nicht gesetzt")
	}
	startPeak()
	dir := os.Getenv("SCHNELLWEG_KEEP") // Ordner für TSV + Index; liegen sie dort schon, nur suchen
	if dir == "" {
		dir = t.TempDir()
	}
	tsv := filepath.Join(dir, "geocode.tsv")
	if _, err := os.Stat(filepath.Join(dir, "geocode.idx")); err == nil {
		searchReal(t, tsv)
		return
	}
	out, err := os.Create(tsv)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	kinds, err := ExtractFiles(context.Background(), strings.Split(p, ","), dir, out)
	out.Close()
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(tsv)
	t.Logf("Extract %v: %v, TSV %d MB, Anon-Spitze %d MB", time.Since(t0).Round(time.Second), kinds, st.Size()>>20, peak())
	t0 = time.Now()
	data, err := mmapFile(tsv)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := os.Create(filepath.Join(dir, "geocode.idx"))
	if err != nil {
		t.Fatal(err)
	}
	if err := BuildIndex(data, idx); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	st, _ = os.Stat(idx.Name())
	t.Logf("Index %v: %d MB, Anon-Spitze %d MB", time.Since(t0).Round(time.Second), st.Size()>>20, peak())
	searchReal(t, tsv)
}

func searchReal(t *testing.T, tsv string) {
	runtime.GC()
	ix, err := LoadFiles(tsv)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("geladen: %d Einträge, Heap %d MB", ix.Len(), m.HeapInuse>>20)
	const lat, lon = 50.7753, 6.0839 // Elisenbrunnen
	for _, q := range []string{"pontstr 5", "superc", "audimax", "elisenbrunnen", "uniklinik", "eilendorf", "boxgraben 2",
		"aachen arkaden", "kaiserplatz", "s", "aachen", "52062", "templergraben 57", "bergneustadt", "hauptstraße 5",
		"hauptstr 5 bergneustadt", "berlin hauptbahnhof", "marienplatz münchen", "köln dom", "vaals", "ha", "bahnhofstraße"} {
		t0 := time.Now()
		hs := ix.SearchNear(q, 5, lat, lon)
		d := time.Since(t0)
		var s []string
		for _, h := range hs {
			s = append(s, h.Kind+":"+h.Name+" ("+h.Detail+")")
		}
		t.Logf("%-24q %6.1f ms  %s", q, float64(d.Microseconds())/1000, strings.Join(s, " · "))
	}
}

// peak: höchster anonymer Speicher (ohne eingeblendete Dateien) seit dem letzten Aufruf, alle 100 ms gemessen.
var anonMax atomic.Int64

func startPeak() {
	go func() {
		for {
			b, _ := os.ReadFile("/proc/self/status")
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "RssAnon:") {
					kb, _ := strconv.ParseInt(strings.Fields(l)[1], 10, 64)
					if kb>>10 > anonMax.Load() {
						anonMax.Store(kb >> 10)
					}
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func peak() int64 { return anonMax.Swap(0) }

func TestParseRWTH(t *testing.T) {
	// echte Form der Antwort: ISO-8859-1, lat/lng mal Text, mal Zahl, Extras als Objekte
	raw := []byte("[ { \"gno\": \"1810\", \"bez\": \"Seminargeb\xe4ude [1810]\", \"adr\": \"W\xfcllnerstr. 5b, 52062 Aachen\"," +
		" \"defibrillator\": { \"info\": \"<p>x</p>\" }, \"typ\": \"b\", \"lat\": \"50.77921\", \"lng\": \"6.076617\" }," +
		" { \"gno\": \"1040\", \"bez\": \"SuperC\", \"adr\": \"\", \"lat\": 50.7780, \"lng\": 6.0782 }," +
		" { \"gno\": \"9999\", \"bez\": \"ohne Ort\", \"lat\": \"\", \"lng\": \"\" } ]")
	es, err := parseRWTH(latin1(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Name != "Seminargebäude [1810]" || es[0].Detail != "Wüllnerstr. 5b, 52062 Aachen" ||
		es[1].Name != "SuperC [1040]" || es[1].Lat != 50.778 {
		t.Fatalf("%+v", es)
	}
	var buf strings.Builder
	Write(&buf, es)
	ix, err := Load(strings.NewReader("poi\tSeminargebäude Kirche\tplace_of_worship\t50.7\t6.0\n"), strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]string{"1810": "Seminargebäude [1810]", "superc": "SuperC [1040]", "seminargeb": "Seminargebäude [1810]"} {
		if h := ix.Search(q, 3); len(h) == 0 || h[0].Name != want {
			t.Errorf("%q → %+v, erwartet %q", q, h, want)
		}
	}
}

// Index als Datei per mmap (nur lesend) und Suche mit Standort: Trefferlisten zeigen in die Datei, append darf
// dort nicht schreiben (28.10.: SIGSEGV).
func TestFileNear(t *testing.T) {
	dir := t.TempDir()
	tsv := filepath.Join(dir, "geocode.tsv")
	es := []Entry{
		{Kind: "strasse", Name: "Hauptstraße", Detail: "Bergneustadt", Lat: 51.02, Lon: 7.65},
		{Kind: "strasse", Name: "Hauptstraße", Detail: "Aachen", Lat: 50.77, Lon: 6.09},
		{Kind: "poi", Name: "SuperC", Detail: "Templergraben 57, 52062 Aachen", Lat: 50.7784, Lon: 6.0782},
		{Kind: "strasse", Name: "Hauptstraße", Detail: "Berlin", Lat: 52.5, Lon: 13.4},
	}
	sort.Slice(es, func(i, j int) bool { return cellOf(es[i].Lat, es[i].Lon) < cellOf(es[j].Lat, es[j].Lon) })
	var b bytes.Buffer
	Write(&b, es)
	os.WriteFile(tsv, b.Bytes(), 0o644)
	var idx bytes.Buffer
	if err := BuildIndex(b.Bytes(), &idx); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "geocode.idx"), idx.Bytes(), 0o644)
	ix, err := LoadFiles(tsv)
	if err != nil || ix.Len() != 4 || len(ix.parts[0].cells) != 3 {
		t.Fatalf("%v, %d Einträge", err, ix.Len())
	}
	if h := ix.SearchNear("superc", 3, 50.77, 6.08); len(h) != 1 {
		t.Fatalf("superc: %+v", h)
	}
	for _, c := range []struct{ lat, lon float64 }{{50.77, 6.08}, {51.0, 7.6}, {52.5, 13.4}} {
		want := map[float64]string{50.77: "Aachen", 51.0: "Bergneustadt", 52.5: "Berlin"}[c.lat]
		if h := ix.SearchNear("hauptstr", 3, c.lat, c.lon); len(h) != 3 || h[0].Detail != want {
			t.Errorf("hauptstr bei %v: %+v", c, h)
		}
	}
}

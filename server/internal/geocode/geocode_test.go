package geocode

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
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
	if err != nil || len(ix.Entries) != len(es) {
		t.Fatalf("%v, %d Einträge", err, len(ix.Entries))
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

// Mit echten Daten: SCHNELLWEG_OSM=../data/spike/koeln.osm.pbf go test ./internal/geocode -run Real -v
func TestReal(t *testing.T) {
	p := os.Getenv("SCHNELLWEG_OSM")
	if p == "" {
		t.Skip("SCHNELLWEG_OSM nicht gesetzt")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t0 := time.Now()
	es, err := Extract(context.Background(), f, Box{5.95, 50.48, 6.45, 50.93})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range es {
		kinds[e.Kind]++
	}
	var b bytes.Buffer
	Write(&b, es)
	t.Logf("Extract %v: %d Einträge %v, TSV %d KB", time.Since(t0).Round(time.Millisecond), len(es), kinds, b.Len()>>10)
	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	es = nil
	ix, err := Load(&b)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.Buffer{}
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	t.Logf("Index: Heap %d MB", int64(m1.HeapInuse)>>20)
	for _, q := range []string{"pontstr 5", "superc", "audimax", "elisenbrunnen", "uniklinik", "eilendorf", "boxgraben 2", "aachen arkaden", "kaiserplatz", "s", "aachen", "52062", "super c", "templergraben 57", "studierendensekretariat"} {
		t0 := time.Now()
		hs := ix.Search(q, 5)
		d := time.Since(t0)
		var s []string
		for _, h := range hs {
			s = append(s, h.Kind+":"+h.Name+" ("+h.Detail+")")
		}
		t.Logf("%-15q %6.1f ms  %s", q, float64(d.Microseconds())/1000, strings.Join(s, " · "))
	}
}

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

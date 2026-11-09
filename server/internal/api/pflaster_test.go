package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHangTempo(t *testing.T) {
	h := Hang{Eben: 12, Auf: 6, Ab: 24}
	for _, c := range []struct{ g, v float64 }{{0, 12}, {3, 9}, {6, 6}, {10, 6}, {-3, 18}, {-6, 24}, {-12, 24}} {
		if v := h.kmh(c.g); math.Abs(v-c.v) > 1e-9 {
			t.Errorf("Steigung %v: %v km/h, erwartet %v", c.g, v, c.v)
		}
	}
	f := Hang{Eben: 12, Auf: 4, Ab: 12, Voll: 4}
	if f.kmh(2) != 8 || f.kmh(4) != 4 || f.kmh(9) != 4 || f.kmh(-9) != 12 {
		t.Errorf("zu Fuß ab 4 %%: %v %v %v %v", f.kmh(2), f.kmh(4), f.kmh(9), f.kmh(-9))
	}
	if (&Meiden{HangVoll: 31}).gueltig() {
		t.Error("Grenze hang_voll")
	}
	if (&Meiden{}).hang(12) != nil {
		t.Error("ohne Tempi kein Hang")
	}
	if g := (&Meiden{KmhAb: 20}).hang(12); g.Auf != 12 || g.Ab != 20 {
		t.Errorf("fehlendes Tempo gilt wie eben: %+v", g)
	}
	if (&Meiden{KmhAuf: -1}).gueltig() || (&Meiden{KmhAb: 201}).gueltig() || !(&Meiden{KmhAuf: 5, KmhAb: 30}).gueltig() {
		t.Error("Grenzen der Tempi")
	}
}

func pflasterDatei(t *testing.T, klasse byte, ids ...uint32) string {
	t.Helper()
	b := make([]byte, 5*len(ids))
	for i, x := range ids {
		binary.LittleEndian.PutUint32(b[4*i:], x)
		b[4*len(ids)+i] = klasse
	}
	p := filepath.Join(t.TempDir(), "belag.ids")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Zwei Wege: der kurze hat auf Way 7 Pflaster; sobald er gesperrt ist, antwortet Valhalla mit dem langen.
func pflasterValhalla(t *testing.T, langM float64) (*Valhalla, *int) {
	const kurz, lang = "_ibE_ibE_seK_seK_seK_seK", "_ibE_ibE_seK?_seK?_seK?"
	routen := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			Polyline string         `json:"encoded_polyline"`
			Polygone [][][2]float64 `json:"exclude_polygons"`
		}
		json.Unmarshal(b, &q)
		if r.URL.Path == "/trace_attributes" {
			way, g, km := 8, 6.0, langM
			if q.Polyline == kurz {
				way, g, km = 7, -6, 1.0
			}
			json.NewEncoder(w).Encode(map[string]any{"shape": q.Polyline, "edges": []map[string]any{
				{"length": km, "way_id": way, "weighted_grade": g, "begin_shape_index": 0, "end_shape_index": 2}}})
			return
		}
		routen++
		shape, m := kurz, 1.0
		if len(q.Polygone) > 0 {
			shape, m = lang, langM
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{"legs": []map[string]any{{"shape": shape}},
			"summary": map[string]any{"time": 100, "length": m}}})
	}))
	t.Cleanup(ts.Close)
	return &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client(), Belag: &Belagliste{Muster: pflasterDatei(t, 4, 3, 7, 9)}}, &routen
}

func TestPflasterUmgehen(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	k := Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.8, BelagUmweg: 50, Hang: &Hang{Eben: 12, Auf: 6, Ab: 24}}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 1200 || *routen != 2 {
		t.Fatalf("Umweg erwartet: %+v, %v, %d Routen", w, err, *routen)
	}
	if math.Abs(w.Sec-720) > 1e-6 { // 1,2 km bergauf mit 6 km/h
		t.Errorf("Hang-Zeit %v", w.Sec)
	}
}

func TestPflasterZuWeiterUmweg(t *testing.T) {
	v, _ := pflasterValhalla(t, 2.0)
	k := Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.8, BelagUmweg: 50}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 1000 {
		t.Fatalf("doppelter Weg ist zu viel, Pflaster bleibt: %+v, %v", w, err)
	}
}

func TestBelagUmwegRegler(t *testing.T) {
	v, _ := pflasterValhalla(t, 2.0)
	k := Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.8, BelagUmweg: 150}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 2000 {
		t.Fatalf("150 %% erlaubt den doppelten Weg: %+v, %v", w, err)
	}
	if (&Meiden{BelagUmweg: 301}).gueltig() || (&Meiden{BelagUmweg: -1}).gueltig() {
		t.Error("Grenzen belag_umweg")
	}
}

// Umgeht Klasse 4 nur mit zu langem Weg, Klasse 1 aber im Rahmen: Die Route weicht dann wenigstens Klasse 1 aus.
func TestBelagTeilweise(t *testing.T) {
	const roh, mittel, lang = "_ibE_ibE_seK_seK_seK_seK", "_ibE_ibE_seK?_seK?_seK?", "_ibE_ibE?_seK?_seK?_seK"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			Polyline string         `json:"encoded_polyline"`
			Polygone [][][2]float64 `json:"exclude_polygons"`
		}
		json.Unmarshal(b, &q)
		if r.URL.Path == "/trace_attributes" {
			way := map[string]int{roh: 1, mittel: 4, lang: 9}[q.Polyline]
			json.NewEncoder(w).Encode(map[string]any{"shape": q.Polyline, "edges": []map[string]any{
				{"length": 1.0, "way_id": way, "begin_shape_index": 0, "end_shape_index": 2}}})
			return
		}
		shape, m := roh, 1.0
		switch len(q.Polygone) {
		case 0:
		case 1:
			shape, m = mittel, 1.1
		default:
			shape, m = lang, 5.0
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{"legs": []map[string]any{{"shape": shape}},
			"summary": map[string]any{"time": 100, "length": m}}})
	}))
	defer ts.Close()
	b := make([]byte, 10)
	binary.LittleEndian.PutUint32(b[0:], 1)
	binary.LittleEndian.PutUint32(b[4:], 4)
	b[8], b[9] = 1, 4
	p := filepath.Join(t.TempDir(), "belag.ids")
	os.WriteFile(p, b, 0o644)
	v := &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client(), Belag: &Belagliste{Muster: p}}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.8, BelagUmweg: 20}, time.Second)
	if err != nil || w.Shape != mittel {
		t.Fatalf("erwartet den Weg ohne Klasse 1: %+v, %v", w, err)
	}
}

func TestBelagOhneUmwegWeichtNichtAus(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.8}, time.Second)
	if err != nil || w.M != 1000 || *routen != 1 {
		t.Fatalf("ohne belag_umweg kein Ausweichen: %+v, %v, %d", w, err, *routen)
	}
	if (&Meiden{Belag: 1}).belagUmweg() != 0 || (&Meiden{Pflaster: true}).belagUmweg() != 50 || (&Meiden{Pflaster: true, BelagUmweg: 20}).belagUmweg() != 20 {
		t.Error("Umweg alter Apps")
	}
}

func TestPflasterAus(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, Kosten{Costing: "pedestrian", Faktor: 1}, time.Second)
	if err != nil || w.M != 1000 || *routen != 1 || w.Sec != 100 {
		t.Fatalf("ohne Schalter unverändert: %+v, %v, %d", w, err, *routen)
	}
}

func TestHoehenKachel(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "N50"), 0o755)
	os.WriteFile(filepath.Join(dir, "N50", "N50E006.hgt"), []byte("hoehe"), 0o644)
	s := &Server{HoeheDir: dir}
	for _, c := range []struct {
		k    string
		code int
	}{{"N50E006", 200}, {"N51E006", 404}, {"..%2Fx", 404}} {
		r := httptest.NewRequest("GET", "/v1/hoehe/x", nil)
		r.SetPathValue("kachel", c.k)
		w := httptest.NewRecorder()
		s.hoehe(w, r)
		if w.Code != c.code {
			t.Errorf("%s: %d", c.k, w.Code)
		}
	}
}

func TestBelagStufen(t *testing.T) {
	d := Belagdaten{[]uint32{1, 2, 3, 4, 5}, []uint8{1, 2, 3, 4, 5}}
	for _, c := range []struct {
		stufe float64
		n     int
	}{{0, 0}, {0.2, 1}, {0.5, 2}, {0.6, 3}, {0.8, 4}, {1, 5}} {
		n := 0
		for w := uint64(1); w <= 6; w++ {
			if d.umgehen(w, c.stufe) {
				n++
			}
		}
		if n != c.n {
			t.Errorf("Stufe %v: %d Klassen, erwartet %d", c.stufe, n, c.n)
		}
	}
	if (&Meiden{Pflaster: true, Belag: 0.2}).belagStufe() != 0.8 || (&Meiden{Belag: 1, Pflaster: true}).belagStufe() != 1 {
		t.Error("alte Apps: pflaster gilt als 0,8")
	}
}

func TestBelagKeinSchritt(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, Kosten{Costing: "pedestrian", Faktor: 1, BelagMeiden: 0.6, BelagUmweg: 50}, time.Second)
	if err != nil || w.M != 1000 || *routen != 1 {
		t.Fatalf("Klasse 4 wird bei Stufe 0,6 nicht umgangen: %+v, %v, %d", w, err, *routen)
	}
}

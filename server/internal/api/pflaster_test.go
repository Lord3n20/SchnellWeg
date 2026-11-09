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

func pflasterDatei(t *testing.T, ids ...uint32) string {
	t.Helper()
	b := make([]byte, 4*len(ids))
	for i, x := range ids {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	p := filepath.Join(t.TempDir(), "pflaster.ids")
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
	return &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client(), Pflaster: &Pflasterliste{Muster: pflasterDatei(t, 3, 7, 9)}}, &routen
}

func TestPflasterUmgehen(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	k := Kosten{Costing: "pedestrian", Faktor: 1, Pflaster: true, Hang: &Hang{Eben: 12, Auf: 6, Ab: 24}}
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
	k := Kosten{Costing: "pedestrian", Faktor: 1, Pflaster: true}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 1000 {
		t.Fatalf("doppelter Weg ist zu viel, Pflaster bleibt: %+v, %v", w, err)
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

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
	"slices"
	"strings"
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

func belagRegel(klasse int, kmh float64) (r [belagKlassen + 1]float64) {
	r[klasse] = kmh
	return r
}

func TestBelagTempoWaehltUmweg(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	k := Kosten{Costing: "pedestrian", Faktor: 1, Eben: 12, BelagKmh: belagRegel(4, 3), Hang: &Hang{Eben: 12, Auf: 6, Ab: 24}}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 1200 || *routen != 2 {
		t.Fatalf("Umweg erwartet: %+v, %v, %d Routen", w, err, *routen)
	}
	if math.Abs(w.Sec-720) > 1e-6 { // 1,2 km bergauf mit 6 km/h statt 20 min Pflaster
		t.Errorf("Zeit %v", w.Sec)
	}
}

func TestBelagTempoBleibtDirekt(t *testing.T) {
	v, _ := pflasterValhalla(t, 1.2)
	k := Kosten{Costing: "pedestrian", Faktor: 1, Eben: 12, BelagKmh: belagRegel(4, 11)}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.M != 1000 || math.Abs(w.Sec-1.0*3600/11) > 1e-6 {
		t.Fatalf("der Umweg lohnt sich nicht: %+v, %v", w, err)
	}
}

func TestBelagNichtLangsamerFragtNichtNeu(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	k := Kosten{Costing: "pedestrian", Faktor: 1, Eben: 12, BelagKmh: belagRegel(4, 20)}
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || *routen != 1 || math.Abs(w.Sec-300) > 1e-6 {
		t.Fatalf("%+v, %v, %d Routen", w, err, *routen)
	}
}

func TestBelagOhneRegel(t *testing.T) {
	v, routen := pflasterValhalla(t, 1.2)
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, Kosten{Costing: "pedestrian", Faktor: 1, Eben: 12}, time.Second)
	if err != nil || w.M != 1000 || *routen != 1 || w.Sec != 100 {
		t.Fatalf("ohne Regel unverändert: %+v, %v, %d", w, err, *routen)
	}
}

// Nur Klasse 1 zu umgehen ist am schnellsten; alles zu umgehen wäre zu lang.
func TestBelagTeilweise(t *testing.T) {
	const roh, mittel, lang = "_ibE_ibE_seK_seK_seK_seK", "_ibE_ibE_seK?_seK?_seK?", "_ibE_ibE?_seK?_seK?_seK"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			Polyline string         `json:"encoded_polyline"`
			Polygone [][][2]float64 `json:"exclude_polygons"`
		}
		json.Unmarshal(b, &q)
		km := map[string]float64{roh: 1, mittel: 1.1, lang: 5}
		if r.URL.Path == "/trace_attributes" {
			way := map[string]int{roh: 1, mittel: 4, lang: 9}[q.Polyline]
			json.NewEncoder(w).Encode(map[string]any{"shape": q.Polyline, "edges": []map[string]any{
				{"length": km[q.Polyline], "way_id": way, "begin_shape_index": 0, "end_shape_index": 2}}})
			return
		}
		shape := roh
		switch len(q.Polygone) {
		case 0:
		case 1:
			shape = mittel
		default:
			shape = lang
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{"legs": []map[string]any{{"shape": shape}},
			"summary": map[string]any{"time": 100, "length": km[shape]}}})
	}))
	defer ts.Close()
	b := make([]byte, 10)
	binary.LittleEndian.PutUint32(b[0:], 1)
	binary.LittleEndian.PutUint32(b[4:], 4)
	b[8], b[9] = 1, 4
	p := filepath.Join(t.TempDir(), "belag.ids")
	os.WriteFile(p, b, 0o644)
	v := &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client(), Belag: &Belagliste{Muster: p}}
	k := Kosten{Costing: "pedestrian", Faktor: 1, Eben: 12}
	k.BelagKmh[1], k.BelagKmh[4] = 2, 6
	w, err := v.wegAnpassen(context.Background(), 40, 40, 41, 41, k, time.Second)
	if err != nil || w.Shape != mittel {
		t.Fatalf("erwartet den Weg ohne Klasse 1: %+v, %v", w, err)
	}
}

func TestBelagGueltig(t *testing.T) {
	if (&Meiden{BelagKmh: []float64{1, 2, 3}}).gueltig() || (&Meiden{BelagKmh: []float64{0, 0, 0, 0, 201}}).gueltig() || !(&Meiden{BelagKmh: []float64{0, 3, 0, 4.3, 0}}).gueltig() {
		t.Error("Grenzen belag_kmh")
	}
	k := (&Meiden{BelagKmh: []float64{0, 3, 0, 4.3, 0}}).art().kosten(5, 12, 0.3)[0]
	if k.BelagKmh[2] != 3 || k.BelagKmh[4] != 4.3 || k.Eben != 12 {
		t.Errorf("%+v", k)
	}
}

func TestMessungAuswertung(t *testing.T) {
	var a messAbgleich
	json.Unmarshal([]byte(`{"edges":[{"way_id":7,"weighted_grade":0.2},{"way_id":99,"weighted_grade":-4.6}],
		"matched_points":[{"edge_index":0,"type":"matched"},{"edge_index":0,"type":"matched"},{"edge_index":1,"type":"interpolated"},{"edge_index":4294967295,"type":"unmatched"}]}`), &a)
	d := Belagdaten{[]uint32{7}, []uint8{4}}
	got := messAuswerten([][4]float64{{50, 6, 0, 0}, {50, 6, 20, 5}, {50, 6, 30, 3}, {50, 6, 99, 9}}, a, d)
	want := []messStueck{{G: -5, C: 0, M: 30, S: 3}, {G: 0, C: 4, M: 20, S: 5}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestMessungAnfrage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"edges":[{"way_id":1,"weighted_grade":2}],"matched_points":[{"edge_index":0,"type":"matched"},{"edge_index":0,"type":"matched"}]}`))
	}))
	defer ts.Close()
	s := &Server{Valhalla: &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}}
	for _, c := range []struct {
		body string
		code int
	}{{`{"punkte":[[50,6,0,0],[50,6,10,2]]}`, 200}, {`{"punkte":[[50,6,0,0]]}`, 400}, {`kaputt`, 400}} {
		w := httptest.NewRecorder()
		s.messung(w, httptest.NewRequest("POST", "/v1/messung", strings.NewReader(c.body)))
		if w.Code != c.code {
			t.Errorf("%s: %d", c.body, w.Code)
		}
	}
}

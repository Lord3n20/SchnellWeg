package api

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Eine Linie A→B mit zwei Fahrten; die Halte liegen ~1,1 km auseinander.
func testServer(day time.Time) *Server {
	tt := &raptor.Timetable{Stops: []raptor.Stop{{ID: "A", Name: "A", Lat: 50.77, Lon: 6.08}, {ID: "B", Name: "B", Lat: 50.78, Lon: 6.08}}}
	tt.Routes = []raptor.Route{{Stops: []int32{0, 1}, Arr: []int32{28800, 29400, 30600, 31200},
		Dep: []int32{28800, 29400, 30600, 31200}, NTrips: 2,
		Trips: []raptor.TripInfo{{ID: "t1", Line: "1"}, {ID: "t2", Line: "1"}}}}
	tt.StopRoutes = [][]int32{{0}, {0}}
	tt.StopPos = [][]int32{{0}, {1}}
	tt.Footpaths = [][]raptor.Footpath{nil, nil}
	tt.StopIndex = map[string]int32{"A": 0, "B": 1}
	s := &Server{AccessM: 300, MaxTrips: 3, WaitLimit: time.Second}
	s.Swap(NewPlan(tt, day, 1))
	return s
}

func post(s *Server, body string) (*httptest.ResponseRecorder, routeResp) {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/route", strings.NewReader(body)))
	var r routeResp
	json.Unmarshal(w.Body.Bytes(), &r)
	return w, r
}

func TestDayBaseZeitumstellung(t *testing.T) {
	// 25.10.2026: Umstellung auf Winterzeit, der Tag hat 25 Stunden. 08:00 GTFS = 08:00 Ortszeit (MEZ).
	b := DayBase(time.Date(2026, 10, 25, 0, 0, 0, 0, Berlin))
	if got := b.Add(8 * time.Hour).In(Berlin).Format("15:04"); got != "08:00" {
		t.Fatalf("08:00 GTFS ergab %s", got)
	}
}

func TestRoute(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	w, r := post(s, `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"}`)
	if w.Code != 200 || len(r.Journeys) != 1 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	j := r.Journeys[0]
	if j.Legs[1].TripID != "t2" || j.Legs[1].Dep.In(Berlin).Format("15:04") != "08:30" {
		t.Fatalf("falsche Fahrt: %+v", j.Legs[1])
	}
	// Zugangsweg endet genau zur Abfahrt, statt um 08:05 loszugehen und zu warten.
	if !j.Legs[0].Arr.Equal(j.Legs[1].Dep) || !j.Dep.Equal(j.Legs[0].Dep) {
		t.Fatalf("Zugangsweg nicht verschoben: %+v", j.Legs[0])
	}
}

func TestRouteFehler(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	for body, want := range map[string]int{
		`{"from":{"lat":50.77},"to":{"lat":50.78,"lon":6.08}}`:                                               400,
		`{"from":{"lat":50.77,"lon":6.08},"to":{"lat":50.78,"lon":6.08},"x":1}`:                              400,
		`{"from":{"lat":50.77,"lon":6.08},"to":{"lat":50.78,"lon":6.08},"time":"2026-10-29T08:00:00+01:00"}`: 422,
		`{"from":{"lat":50.77,"lon":6.08},"to":{"lat":51.5,"lon":7.0},"time":"2026-10-28T08:00:00+01:00"}`:   422,
	} {
		if w, _ := post(s, body); w.Code != want || strings.Contains(w.Body.String(), "50.7") {
			t.Errorf("%s: Code %d (erwartet %d), %s", body, w.Code, want, w.Body)
		}
	}
}

func TestFern(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	s.Current().TT.Routes[0].Fern = true
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"%s}`
	if _, r := post(s, fmt.Sprintf(body, "")); len(r.Journeys) != 0 {
		t.Fatalf("Fernverkehr ohne fern:true benutzt: %+v", r.Journeys)
	}
	if _, r := post(s, fmt.Sprintf(body, `,"fern":true`)); len(r.Journeys) != 1 || !r.Journeys[0].Legs[1].Fern {
		t.Fatalf("mit fern:true erwartet 1 Weg mit Kennzeichen: %+v", r.Journeys)
	}
}

func TestGehtempo(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	// Zugang ~11 m → 60 s Mindestwert. Ab 07:58:30 erreicht man die Fahrt um 08:00 nur mit Grundtempo.
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T07:58:30+01:00"%s}`
	for extra, want := range map[string]string{"": "t1", `,"kmh":4.3`: "t1", `,"kmh":2.15`: "t2", `,"kmh":6`: "t1"} {
		w, r := post(s, fmt.Sprintf(body, extra))
		if w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Legs[1].TripID != want {
			t.Errorf("%q: Code %d, erwartet %s: %s", extra, w.Code, want, w.Body)
		}
	}
	_, r := post(s, fmt.Sprintf(body, `,"kmh":2.15`))
	if l := r.Journeys[0].Legs; l[2].Arr.Sub(l[2].Dep) != 2*time.Minute {
		t.Errorf("Abgang bei halbem Tempo: %v", l[2].Arr.Sub(l[2].Dep))
	}
	for _, k := range []string{`,"kmh":0`, `,"kmh":12`, `,"kmh":"x"`} {
		if w, _ := post(s, fmt.Sprintf(body, k)); w.Code != 400 {
			t.Errorf("%s: Code %d, erwartet 400", k, w.Code)
		}
	}
}

func TestFahrradUndEinrad(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T07:55:00+01:00"%s}`
	// Linie 1 ist ein Bus → Fahrrad findet keinen Halt in Reichweite
	if w, _ := post(s, fmt.Sprintf(body, `,"art":"rad","kmh":15`)); w.Code != 422 {
		t.Errorf("Rad mit Bus: Code %d, erwartet 422: %s", w.Code, w.Body)
	}
	s.Current().TT.Routes[0].Bahn = true
	w, r := post(s, fmt.Sprintf(body, `,"art":"rad","kmh":15`))
	if w.Code != 200 || len(r.Journeys) != 1 || !r.Journeys[0].Legs[1].Rad || r.Journeys[0].Legs[0].Rad {
		t.Errorf("Rad mit Bahn: erwartet Fahrt mit Fahrradticket: %s", w.Body)
	}
	for k, code := range map[string]int{
		`,"art":"einrad","kmh":13`: 200, `,"art":"einrad","kmh":25`: 200, `,"art":"einrad","kmh":500`: 200, `,"art":"einrad","kmh":0.5`: 200,
		`,"art":"einrad","kmh":0`: 400, `,"art":"einrad","kmh":-3`: 400, `,"art":"rad","kmh":30`: 200,
		`,"kmh":13`: 400, `,"art":"auto","kmh":5`: 400,
	} {
		if w, _ := post(s, fmt.Sprintf(body, k)); w.Code != code {
			t.Errorf("%s: Code %d, erwartet %d", k, w.Code, code)
		}
	}
	if _, r := post(s, fmt.Sprintf(body, `,"art":"einrad","kmh":13`)); len(r.Journeys) == 1 && r.Journeys[0].Legs[1].Rad {
		t.Error("Einrad braucht kein Fahrradticket")
	}
}

func TestFahrradtypen(t *testing.T) {
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T07:55:00+01:00"%s}`
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin)
	s := testServer(day) // Linie 1 ist ein Bus
	// Klapprad: zusammengeklappt in den Bus, ohne Fahrradticket; Fixie/Rennrad/MTB nur Bahn
	if w, r := post(s, fmt.Sprintf(body, `,"art":"klapprad","kmh":15`)); w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Legs[1].Rad {
		t.Errorf("Klapprad im Bus: %d %s", w.Code, w.Body)
	}
	for _, a := range []string{"fixie", "rennrad", "mtb"} {
		if w, _ := post(s, fmt.Sprintf(body, `,"art":"`+a+`","kmh":15`)); w.Code != 422 {
			t.Errorf("%s im Bus: Code %d, erwartet 422", a, w.Code)
		}
	}
	for k, code := range map[string]int{`,"art":"rennrad","kmh":8`: 400, `,"art":"rennrad","kmh":42`: 422,
		`,"art":"klapprad","kmh":32`: 400, `,"art":"klapprad","kmh":28`: 200, `,"art":"mtb","kmh":30`: 422} {
		if w, _ := post(s, fmt.Sprintf(body, k)); w.Code != code {
			t.Errorf("%s: Code %d, erwartet %d", k, w.Code, code)
		}
	}

	// Räder rechnen mit Valhallas Fahrradkosten im eigenen Tempo: 60 s bleiben 60 s (D-30); das Einrad skaliert
	// Fußzeiten.
	s = testServer(day)
	s.Current().TT.Routes[0].Bahn = true
	s.Valhalla = fakeValhalla(t, 60, 0.3, 200)
	for a, want := range map[string]time.Duration{"rennrad": time.Minute, "fixie": time.Minute, "mtb": time.Minute, "einrad": 13 * time.Second} {
		w, r := post(s, fmt.Sprintf(body, `,"art":"`+a+`","kmh":20`))
		if w.Code != 200 || len(r.Journeys) != 1 {
			t.Fatalf("%s: %d %s", a, w.Code, w.Body)
		}
		if l := r.Journeys[0].Legs[2]; l.Arr.Sub(l.Dep) != want {
			t.Errorf("%s: Abgang %v statt %v", a, l.Arr.Sub(l.Dep), want)
		}
	}
}

// fakeValhalla antwortet auf jede Matrix mit derselben Zeit/Strecke je Zelle; code ≠ 200 simuliert einen Fehler.
func fakeValhalla(t *testing.T, sec, km float64, code int) *Valhalla {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code != 200 {
			w.WriteHeader(code)
			return
		}
		if r.URL.Path == "/route" { // Fußweg mit Form; Zeit/Strecke wie bei der Matrix
			var req vhRouteReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Locations) != 2 || (req.Costing != "pedestrian" && req.Costing != "bicycle") {
				t.Errorf("falsche Routenanfrage: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
				"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": sec, "length": km}}})
			return
		}
		var req matrixReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.URL.Path != "/sources_to_targets" {
			t.Errorf("falsche Anfrage %s: %v", r.URL.Path, err)
		}
		rows := make([][]map[string]float64, len(req.Sources))
		for i := range rows {
			for range req.Targets {
				rows[i] = append(rows[i], map[string]float64{"time": sec, "distance": km})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"sources_to_targets": rows})
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &Valhalla{URL: ts.URL, KMH: 4.3, MaxM: 600, Timeout: time.Second, Client: ts.Client()}
}

func TestValhallaZugang(t *testing.T) {
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"}`
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin)

	// Echter Weg 7 min: Zugang dauert 7 min, Abgang ebenso → Ankunft 08:40 + 7 min.
	s := testServer(day)
	s.Valhalla = fakeValhalla(t, 420, 0.5, 200)
	w, r := post(s, body)
	if w.Code != 200 || len(r.Journeys) != 1 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	if l := r.Journeys[0].Legs[0]; l.Arr.Sub(l.Dep) != 7*time.Minute {
		t.Fatalf("Zugang %v statt 7 min", l.Arr.Sub(l.Dep))
	}
	if got := r.Journeys[0].Arr.In(Berlin).Format("15:04"); got != "08:47" {
		t.Fatalf("Ankunft %s statt 08:47", got)
	}

	// Echter Weg länger als MaxM (Umweg um Gleise) → kein Halt erreichbar.
	s = testServer(day)
	s.Valhalla = fakeValhalla(t, 900, 1.2, 200)
	if w, _ := post(s, body); w.Code != 422 {
		t.Fatalf("zu langer Weg: Code %d, %s", w.Code, w.Body)
	}

	// Valhalla-Fehler (z. B. außerhalb der Tiles) → Luftlinie wie ohne Valhalla.
	s = testServer(day)
	s.Valhalla = fakeValhalla(t, 0, 0, 400)
	if w, r := post(s, body); w.Code != 200 || len(r.Journeys) != 1 {
		t.Fatalf("Rückfall auf Luftlinie: Code %d, %s", w.Code, w.Body)
	}
}

func TestWegformen(t *testing.T) {
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"}`
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	s.Valhalla = fakeValhalla(t, 420, 0.5, 200)
	s.DirectM, s.DirectMax = 3000, time.Hour
	w, r := post(s, body)
	if w.Code != 200 || len(r.Journeys) != 2 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	l := r.Journeys[0].Legs
	if l[0].Shape != "form" || l[2].Shape != "form" || l[1].Shape != "" {
		t.Errorf("Wegformen fehlen: %+v", l)
	}
	if fmt.Sprint(l[1].Stops) != "[A B]" {
		t.Errorf("Halte der Fahrt %v", l[1].Stops)
	}
	// Reiner Fußweg hinten: 7 min ab Anfragezeit, mit Form.
	d := r.Journeys[1]
	if d.Trips != 0 || len(d.Legs) != 1 || d.Legs[0].Shape != "form" || d.Arr.Sub(d.Dep) != 7*time.Minute ||
		d.Dep.In(Berlin).Format("15:04") != "08:05" {
		t.Errorf("Fußweg %+v", d)
	}

	// Zu lang (Höchstdauer) → kein Fußweg; ohne Valhalla Luftlinie ohne Form.
	s.DirectMax = 5 * time.Minute
	if _, r := post(s, body); len(r.Journeys) != 1 {
		t.Errorf("Fußweg trotz Höchstdauer: %d Verbindungen", len(r.Journeys))
	}
	s.Valhalla, s.DirectMax = nil, time.Hour
	if _, r := post(s, body); len(r.Journeys) != 2 || r.Journeys[1].Legs[0].Shape != "" || r.Journeys[0].Legs[0].Shape != "" {
		t.Errorf("ohne Valhalla: %+v", r.Journeys)
	}
}

func TestNurFussweg(t *testing.T) {
	// Kein Halt im Umkreis, Ziel 1 km entfernt → nur der Fußweg statt 422.
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	s.DirectM, s.DirectMax = 3000, time.Hour
	body := `{"from":{"lat":50.80,"lon":6.10},"to":{"lat":50.809,"lon":6.10},"time":"2026-10-28T08:05:00+01:00"}`
	w, r := post(s, body)
	if w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Trips != 0 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	s.DirectM = 0
	if w, _ := post(s, body); w.Code != 422 {
		t.Fatalf("ohne Fußweg: Code %d", w.Code)
	}
}

func TestRadVergleich(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 28, 8, m, 0, 0, Berlin) }
	d := journeyOut{Arr: at(10)}
	js := []journeyOut{{Trips: 0, Arr: at(9)}, {Trips: 1, Arr: at(8)}, {Trips: 1, Arr: at(7)}, {Trips: 1, Arr: at(9)}}
	bus := []bool{false, true, true, false}
	got := radVergleich(js, bus, d, 3*time.Minute)
	// ohne Fahrt: weg; Bus 2 min früher: weg; Bus 3 min früher: bleibt; Bahn 1 min früher: bleibt
	if len(got) != 2 || !got[0].Arr.Equal(at(7)) || !got[1].Arr.Equal(at(9)) {
		t.Errorf("radVergleich: %+v", got)
	}
}

func TestStopsGzip(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	r := httptest.NewRequest(http.MethodGet, "/v1/stops", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.stops(w, r)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("ohne gzip")
	}
	z, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Stops [][4]any }
	if err := json.NewDecoder(z).Decode(&v); err != nil || len(v.Stops) == 0 {
		t.Fatalf("Inhalt: %v %d", err, len(v.Stops))
	}
}

func TestWeitWeg(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	// Start ~2,2 km südlich von A: außerhalb von AccessM (300 m), erst die erweiterte Suche findet A.
	body := `{"from":{"lat":50.75,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T07:00:00+01:00"}`
	if w, _ := post(s, body); w.Code != 422 || !strings.Contains(w.Body.String(), `"seite":"start"`) {
		t.Fatalf("ohne FarM: Code %d, %s", w.Code, w.Body)
	}
	s.FarM = 5000
	w, r := post(s, body)
	if w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Legs[0].From != nil || r.Journeys[0].Legs[1].TripID != "t1" {
		t.Fatalf("mit FarM: Code %d, %s", w.Code, w.Body)
	}
	if d := r.Journeys[0].Legs[0].Arr.Sub(r.Journeys[0].Legs[0].Dep); d < 25*time.Minute {
		t.Errorf("Zugangsweg %v, erwartet ~30 min", d)
	}
	// Ab LangM erst nachfragen: grobe Schätzung statt Route, mit "lang" die volle Rechnung
	s.LangM = 2000
	if w, _ := post(s, body); w.Code != 422 || !strings.Contains(w.Body.String(), `"start":{"km":3,"min":40}`) {
		t.Errorf("LangM: Code %d, %s", w.Code, w.Body)
	}
	if w, _ := post(s, strings.TrimSuffix(body, "}")+`,"lang":true}`); w.Code != 200 {
		t.Errorf("lang: Code %d, %s", w.Code, w.Body)
	}
	s.LangM = 0
	// Ziel weit außerhalb (Gebiet ohne Fahrplan): auch mit FarM kein Halt
	if w, _ := post(s, strings.Replace(body, `"lat":50.7799`, `"lat":48.1`, 1)); w.Code != 422 || !strings.Contains(w.Body.String(), `"seite":"ziel"`) {
		t.Errorf("Ziel außerhalb: Code %d, %s", w.Code, w.Body)
	}
}

func TestNearest(t *testing.T) {
	acc := []raptor.Access{{Stop: 1, Sec: 2000}, {Stop: 2, Sec: 500}, {Stop: 3, Sec: 1300}, {Stop: 4, Sec: 600}}
	if got := nearest(acc); len(got) != 3 || got[0].Stop != 2 || got[2].Stop != 3 {
		t.Errorf("nearest: %+v", got)
	}
}

func TestAnkunft(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	// Fahrten A→B um 08:00 (an 08:10) und 08:30 (an 08:40), Abgang ~1 min
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T%s:00+01:00","ankunft":true}`
	for bis, want := range map[string]string{"08:45": "t2", "08:41": "t2", "08:40": "t1", "08:15": "t1"} {
		w, r := post(s, fmt.Sprintf(body, bis))
		if w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Legs[1].TripID != want {
			t.Errorf("an bis %s: erwartet %s, Code %d: %s", bis, want, w.Code, w.Body)
		}
	}
	if _, r := post(s, fmt.Sprintf(body, "08:05")); len(r.Journeys) != 0 {
		t.Errorf("an bis 08:05: keine Fahrt möglich, bekam %+v", r.Journeys)
	}
}

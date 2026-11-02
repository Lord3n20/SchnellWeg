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
		if w.Code != 200 || len(r.Journeys) == 0 || r.Journeys[0].Legs[1].TripID != want {
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
	if w.Code != 200 || len(r.Journeys) == 0 || !r.Journeys[0].Legs[1].Rad || r.Journeys[0].Legs[0].Rad {
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
	if _, r := post(s, fmt.Sprintf(body, `,"art":"einrad","kmh":13`)); len(r.Journeys) > 0 && r.Journeys[0].Legs[1].Rad {
		t.Error("Einrad braucht kein Fahrradticket")
	}
}

func TestFahrradtypen(t *testing.T) {
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T07:55:00+01:00"%s}`
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin)
	s := testServer(day) // Linie 1 ist ein Bus
	// Klapprad: zusammengeklappt in den Bus, ohne Fahrradticket; Fixie/Rennrad/MTB nur Bahn
	if w, r := post(s, fmt.Sprintf(body, `,"art":"klapprad","kmh":15`)); w.Code != 200 || len(r.Journeys) == 0 || r.Journeys[0].Legs[1].Rad {
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
		if w.Code != 200 || len(r.Journeys) == 0 {
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
	if w.Code != 200 || len(r.Journeys) == 0 || r.Journeys[0].Legs[0].From != nil || r.Journeys[0].Legs[1].TripID != "t1" {
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

func TestOptionenUndKnapp(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	// Fahrten A→B um 08:00 und 08:30, Zugang 60 s (Mindestwert)
	body := `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T%s+01:00"%s}`
	if _, r := post(s, fmt.Sprintf(body, "07:50:00", "")); len(r.Journeys) != 2 || r.Journeys[1].Legs[1].TripID != "t2" {
		t.Fatalf("erwartet beide Fahrten: %+v", r.Journeys)
	}
	// 07:59:30: t1 nur noch mit doppeltem Tempo (4,3 → 8,6, aufgerundet 9 km/h)
	if _, r := post(s, fmt.Sprintf(body, "07:59:30", "")); len(r.Journeys) != 1 || r.Journeys[0].Legs[1].TripID != "t2" {
		t.Errorf("ohne knapp nur t2: %+v", r.Journeys)
	}
	w, r := post(s, fmt.Sprintf(body, "07:59:30", `,"knapp":true`))
	if w.Code != 200 || len(r.Journeys) != 2 || r.Journeys[0].Legs[1].TripID != "t1" || r.Journeys[0].Eil != 9 || r.Journeys[1].Eil != 0 {
		t.Fatalf("knapp: %s", w.Body)
	}
	if j := r.Journeys[0]; j.Dep.In(Berlin).Format("15:04:05") != "07:59:30" || !j.Legs[0].Dep.Equal(j.Dep) {
		t.Errorf("knapp: losgehen um 07:59:30 erwartet, %v", j.Dep)
	}
	// 07:59:45: bräuchte vierfaches Tempo → nicht mehr
	if _, r := post(s, fmt.Sprintf(body, "07:59:45", `,"knapp":true`)); len(r.Journeys) != 1 || r.Journeys[0].Eil != 0 {
		t.Errorf("zu knapp: %+v", r.Journeys)
	}
	// Ankunftsmodus: knapp ändert nichts
	if w, r := post(s, fmt.Sprintf(body, "08:45:00", `,"ankunft":true,"knapp":true`)); w.Code != 200 || len(r.Journeys) != 1 {
		t.Errorf("ankunft+knapp: %s", w.Body)
	}
}

func TestAuto(t *testing.T) {
	var gefragt vhRouteReq
	code := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/route" {
			w.WriteHeader(400)
			return
		}
		gefragt = vhRouteReq{}
		json.NewDecoder(r.Body).Decode(&gefragt)
		if code != 200 {
			w.WriteHeader(code)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": 3000, "length": 78}}})
	}))
	defer ts.Close()
	// Ohne Fahrplan: das Auto braucht keinen.
	s := &Server{Valhalla: &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}}
	body := `{"from":{"lat":50.77,"lon":6.09},"to":{"lat":50.94,"lon":6.96},"time":"2026-10-28T08:00:00+01:00","art":"auto"%s}`

	w, r := post(s, fmt.Sprintf(body, `,"kmh":120`))
	if w.Code != 200 || len(r.Journeys) != 1 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	j := r.Journeys[0]
	if l := j.Legs[0]; j.Trips != 0 || len(j.Legs) != 1 || l.Mode != "drive" || l.Shape != "form" || j.Arr.Sub(j.Dep) != 50*time.Minute {
		t.Errorf("Fahrt falsch: %s", w.Body)
	}
	opts, _ := gefragt.CostingOptions["auto"].(map[string]any)
	if gefragt.Costing != "auto" || opts["top_speed"] != 120.0 || gefragt.Locations[1].SearchCutoff != autoFangM {
		t.Errorf("Valhalla-Anfrage: %+v", gefragt)
	}
	if w, _ := post(s, fmt.Sprintf(body, "")); w.Code != 200 {
		t.Errorf("ohne kmh: Code %d", w.Code)
	} else if opts, _ := gefragt.CostingOptions["auto"].(map[string]any); opts["top_speed"] != nil {
		t.Errorf("ohne kmh kein Höchsttempo: %+v", opts)
	}
	_, r = post(s, fmt.Sprintf(body, `,"kmh":120,"ankunft":true`))
	if got := r.Journeys[0].Dep.In(Berlin).Format("15:04"); got != "07:10" {
		t.Errorf("Ankunftszeit: los %s statt 07:10", got)
	}
	for _, k := range []string{`,"kmh":29`, `,"kmh":501`} {
		if w, _ := post(s, fmt.Sprintf(body, k)); w.Code != 400 {
			t.Errorf("%s: Code %d, erwartet 400", k, w.Code)
		}
	}
	code = 400 // kein Weg oder außerhalb der Tiles
	if w, _ := post(s, fmt.Sprintf(body, "")); w.Code != 422 || !strings.Contains(w.Body.String(), "kein_autoweg") {
		t.Errorf("kein Weg: Code %d, %s", w.Code, w.Body)
	}
	s.Valhalla = nil
	if w, _ := post(s, fmt.Sprintf(body, "")); w.Code != 503 {
		t.Errorf("ohne Valhalla: Code %d", w.Code)
	}
}

func TestMeidenKosten(t *testing.T) {
	m := Meiden{Treppen: 0.5, Belag: 0.8, Hauptstrassen: 1, Steigung: 0.5, Fusswege: true}
	k := m.art().kosten(5, 20, 0.25)
	if len(k) != 2 || k[0].Costing != "bicycle" || k[1].Costing != "pedestrian" {
		t.Fatalf("Kostenarten: %+v", k)
	}
	rad, fuss := k[0].Opts["bicycle"].(map[string]any), k[1].Opts["pedestrian"].(map[string]any)
	if rad["bicycle_type"] != "Road" || rad["avoid_bad_surfaces"] != 0.8 || rad["use_roads"] != 0.0 || rad["use_hills"] != 0.25 {
		t.Errorf("Rad: %+v", rad)
	}
	if fuss["step_penalty"] != 300.0 || fuss["use_hills"] != 0.25 {
		t.Errorf("Fuß: %+v", fuss)
	}
	// Ohne Fußwege meiden nur Fußgänger-Kosten, ohne Stufen Valhallas Standard
	k = (&Meiden{}).art().kosten(5, 15, 0.3)
	if o := k[0].Opts["pedestrian"].(map[string]any); len(k) != 1 || len(o) != 1 || (&Meiden{}).art().Rad {
		t.Errorf("leer: %+v", k)
	}
	if a := (&Meiden{BusMin: 5, NurBahn: true}).art(); !a.Rad || !a.NurBahn || a.BusMalus != 5*time.Minute {
		t.Errorf("Bus: %+v", a)
	}
}

func TestMeidenAnfrage(t *testing.T) {
	var gefragt []vhRouteReq
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q vhRouteReq
		json.NewDecoder(r.Body).Decode(&q)
		gefragt = append(gefragt, q)
		if q.Costing == "bicycle" { // kein Radweg: Rückfall auf Fußwege
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": 600, "length": 2}}})
	}))
	defer ts.Close()
	s := &Server{Valhalla: &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}}
	body := `{"from":{"lat":50.77,"lon":6.09},"to":{"lat":50.94,"lon":6.96},"time":"2026-10-28T08:00:00+01:00",%s}`

	for _, schlecht := range []string{`"art":"fixie","meiden":{}`, `"meiden":{"treppen":2}`, `"meiden":{"bus_min":-1}`, `"meiden":{"umstiege":-1}`, `"meiden":{},"kmh":0`} {
		if w, _ := post(s, fmt.Sprintf(body, schlecht)); w.Code != 400 {
			t.Errorf("%s: Code %d", schlecht, w.Code)
		}
	}
	// ÖV meiden: direkter Weg ohne Fahrplan, jedes Tempo, Rückfall auf die zweite Kostenart
	w, r := post(s, fmt.Sprintf(body, `"kmh":60,"meiden":{"oev":true,"fusswege":true}`))
	if w.Code != 200 || len(r.Journeys) != 1 || r.Journeys[0].Legs[0].Mode != "walk" {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	if len(gefragt) != 2 || gefragt[1].Costing != "pedestrian" || gefragt[0].Locations[0].SearchCutoff != autoFangM {
		t.Errorf("Valhalla-Anfragen: %+v", gefragt)
	}
	// Auto: meiden geht in die Autokosten
	gefragt = nil
	if w, _ := post(s, fmt.Sprintf(body, `"art":"auto","kmh":100,"meiden":{"autobahn":1,"maut":1,"belag":0.5}`)); w.Code != 200 {
		t.Fatalf("Auto: %s", w.Body)
	}
	o := gefragt[0].CostingOptions["auto"].(map[string]any)
	if o["use_highways"] != 0.0 || o["use_tolls"] != 0.0 || o["exclude_unpaved"] != true || o["top_speed"] != 100.0 {
		t.Errorf("Auto: %+v", o)
	}
}

func TestWegOhneFahrtNurEinmal(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	// Start und Ziel am selben Halt: RAPTOR fände je Minute später wieder den Weg ohne Fahrt
	body := `{"from":{"lat":50.7700,"lon":6.08},"to":{"lat":50.7701,"lon":6.08},"time":"2026-10-28T07:50:00+01:00"}`
	zaehle := func(r routeResp) (n int) {
		for _, j := range r.Journeys {
			if j.Trips == 0 {
				n++
			}
		}
		return n
	}
	s.DirectM = 0 // ohne direkten Weg bleibt der von RAPTOR
	if w, r := post(s, body); w.Code != 200 || zaehle(r) != 1 {
		t.Errorf("ohne direkten Weg: %s", w.Body)
	}
	s.DirectM, s.DirectMax = 3000, time.Hour // mit direktem Weg nur dieser
	if w, r := post(s, body); w.Code != 200 || zaehle(r) != 1 {
		t.Errorf("mit direktem Weg: %s", w.Body)
	}
}

func TestRadTempoAusserhalbValhalla(t *testing.T) {
	m := Meiden{Fusswege: true}
	for _, c := range []struct{ kmh, vh, faktor float64 }{{4, 5, 1.25}, {20, 20, 1}, {120, 60, 0.5}} {
		k := m.art().kosten(5, c.kmh, 1)[0]
		if got := k.Opts["bicycle"].(map[string]any)["cycling_speed"]; got != c.vh || k.Faktor != c.faktor {
			t.Errorf("%v km/h: cycling_speed %v, Faktor %v", c.kmh, got, k.Faktor)
		}
	}
}

func TestAutoFreieAutobahn(t *testing.T) {
	traceCode := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trace_attributes" {
			if traceCode != 200 {
				w.WriteHeader(traceCode)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"edges": []map[string]any{
				{"length": 100, "speed": 100, "speed_limit": "unlimited", "road_class": "motorway"},
				{"length": 60, "speed": 120, "speed_limit": 120, "road_class": "motorway"}, // Limit gilt weiter
				{"length": 5, "speed": 89, "road_class": "motorway"},                       // Auffahrt ohne Angabe
				{"length": 5, "speed": 50, "speed_limit": "unlimited", "road_class": "primary"},
			}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": 7200, "length": 170}}})
	}))
	defer ts.Close()
	s := &Server{Valhalla: &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}}
	dauer := func(kmh int) time.Duration {
		w, r := post(s, fmt.Sprintf(`{"from":{"lat":50.77,"lon":6.09},"to":{"lat":50.94,"lon":6.96},"art":"auto","kmh":%d}`, kmh))
		if w.Code != 200 {
			t.Fatalf("kmh %d: Code %d, %s", kmh, w.Code, w.Body)
		}
		return r.Journeys[0].Arr.Sub(r.Journeys[0].Dep)
	}
	if got := dauer(300); got != 80*time.Minute { // 100 km in 20 statt 60 min
		t.Errorf("300 km/h: %v", got)
	}
	if got := dauer(80); got != 2*time.Hour { // langsamer als Valhallas Annahme: top_speed regelt das schon
		t.Errorf("80 km/h: %v", got)
	}
	traceCode = 400
	if got := dauer(300); got != 2*time.Hour {
		t.Errorf("ohne Kantendaten: %v", got)
	}
}

func TestAutoBelagMeidetNurWennEsGeht(t *testing.T) {
	var gefragt []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q vhRouteReq
		json.NewDecoder(r.Body).Decode(&q)
		o := q.CostingOptions["auto"].(map[string]any)
		gefragt = append(gefragt, o)
		if o["exclude_unpaved"] == true { // Ziel nur über Feldweg erreichbar
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": 900, "length": 3}}})
	}))
	defer ts.Close()
	s := &Server{Valhalla: &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}}
	body := `{"from":{"lat":50.77,"lon":6.09},"to":{"lat":50.94,"lon":6.96},"art":"auto","meiden":{"belag":1}}`
	if w, _ := post(s, body); w.Code != 200 || len(gefragt) != 2 {
		t.Errorf("Code %d, Anfragen %+v", w.Code, gefragt)
	}
}

// polyline6 kodiert eine Form wie Valhalla.
func polyline6(p []pt) string {
	var b strings.Builder
	var la, lo int
	put := func(d int) {
		u := d << 1
		if d < 0 {
			u = ^u
		}
		for u >= 0x20 {
			b.WriteByte(byte((u&0x1f)|0x20) + 63)
			u >>= 5
		}
		b.WriteByte(byte(u) + 63)
	}
	for _, x := range p {
		a, o := int(x.lat*1e6+0.5), int(x.lon*1e6+0.5)
		put(a - la)
		put(o - lo)
		la, lo = a, o
	}
	return b.String()
}

func TestAutoVerkehr(t *testing.T) {
	// Route nach Osten, 7 km auf einer Breite
	route := []pt{{50.8, 6.1}, {50.8, 6.15}, {50.8, 6.2}}
	koord := func(p ...pt) []any {
		var c []any
		for _, x := range p {
			c = append(c, []float64{x.lon, x.lat})
		}
		return c
	}
	eintrag := func(titel string, delay string, future bool, p ...pt) map[string]any {
		return map[string]any{"title": titel, "subtitle": " West -> Ost", "delayTimeValue": delay, "averageSpeed": "20", "isBlocked": "false",
			"future": future, "description": []string{"Länge: 3 km | Max. 60 km/h"}, "geometry": map[string]any{"coordinates": koord(p...)}}
	}
	ost := []pt{{50.8, 6.11}, {50.8, 6.13}, {50.8, 6.14}}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teil := strings.Split(r.URL.Path, "/")
		l := []map[string]any{}
		if teil[1] == "A4" {
			switch teil[3] {
			case "warning":
				l = []map[string]any{
					eintrag("A4 | Stau", "7", false, ost...),
					eintrag("A4 | derselbe Stau kürzer gemeldet", "3", false, ost...),
					eintrag("A4 | Gegenrichtung", "30", false, ost[2], ost[1], ost[0]),
					eintrag("A4 | Abseits", "30", false, pt{50.9, 6.11}, pt{50.9, 6.14}),
					eintrag("A4 | Später", "30", true, ost...),
				}
			case "roadworks":
				l = []map[string]any{eintrag("A4 | Baustelle", "", false, pt{50.8, 6.15}, pt{50.8, 6.16}, pt{50.8, 6.17})}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{teil[3]: l})
	}))
	defer api.Close()
	vh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trace_attributes" {
			json.NewEncoder(w).Encode(map[string]any{"shape": polyline6(route), "edges": []map[string]any{
				{"length": 3.5, "speed": 100, "speed_limit": 100, "road_class": "motorway", "begin_shape_index": 0, "end_shape_index": 1},
				{"length": 3.5, "speed": 100, "speed_limit": 100, "road_class": "motorway", "begin_shape_index": 1, "end_shape_index": 2},
			}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": polyline6(route)}}, "summary": map[string]float64{"time": 600, "length": 7}}})
	}))
	defer vh.Close()
	s := &Server{Valhalla: &Valhalla{URL: vh.URL, Timeout: time.Second, Client: vh.Client()},
		Verkehr: &Verkehr{URL: api.URL, Client: api.Client(), UA: "Test"}}
	body := `{"from":{"lat":50.8,"lon":6.1},"to":{"lat":50.8,"lon":6.2},"art":"auto","kmh":120}`
	w, r := post(s, body)
	if w.Code != 200 {
		t.Fatalf("Code %d, %s", w.Code, w.Body)
	}
	j := r.Journeys[0]
	if !j.VerkehrOK || len(j.Verkehr) != 2 || j.Verkehr[0].Titel != "A4 Stau" || j.Verkehr[0].VerzMin != 7 || j.Verkehr[1].Art != "baustelle" {
		t.Fatalf("Meldungen: %s", w.Body)
	}
	// 600 s + 7 min Stau; 3,5 km in der Baustelle mit 60 statt 100 km/h kosten 84 s mehr
	if got, soll := j.Arr.Sub(j.Dep), time.Duration(600+420+84)*time.Second; got != soll {
		t.Errorf("Dauer %v, erwartet %v", got, soll)
	}
}

func TestAutoOhneVerkehrsdaten(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer api.Close()
	s := &Server{Valhalla: fakeRoute(t), Verkehr: &Verkehr{URL: api.URL, Client: api.Client()}}
	w, r := post(s, `{"from":{"lat":50.8,"lon":6.1},"to":{"lat":50.8,"lon":6.2},"art":"auto","kmh":120}`)
	if w.Code != 200 || r.Journeys[0].VerkehrOK || r.Journeys[0].Arr.Sub(r.Journeys[0].Dep) != 10*time.Minute {
		t.Errorf("Code %d, %s", w.Code, w.Body)
	}
}

func fakeRoute(t *testing.T) *Valhalla {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/route" {
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{
			"legs": []map[string]string{{"shape": "form"}}, "summary": map[string]float64{"time": 600, "length": 7}}})
	}))
	t.Cleanup(ts.Close)
	return &Valhalla{URL: ts.URL, Timeout: time.Second, Client: ts.Client()}
}

func TestUmfahrung(t *testing.T) {
	route := []pt{{50.8, 6.1}, {50.8, 6.15}, {50.8, 6.2}}
	umweg := []pt{{50.8, 6.1}, {50.82, 6.15}, {50.8, 6.2}}
	var delay string
	var umwegSec, kleinKm float64
	blockiert := "false"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teil := strings.Split(r.URL.Path, "/")
		l := []map[string]any{}
		if teil[1] == "A4" && teil[3] == "warning" {
			l = append(l, map[string]any{"title": "A4 | Stau", "subtitle": "West -> Ost", "delayTimeValue": delay, "isBlocked": blockiert,
				"geometry": map[string]any{"coordinates": [][]float64{{6.11, 50.8}, {6.13, 50.8}, {6.14, 50.8}}}})
		}
		json.NewEncoder(w).Encode(map[string]any{teil[3]: l})
	}))
	defer api.Close()
	var bereiche int
	vh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			vhRouteReq
			Encoded string `json:"encoded_polyline"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		if r.URL.Path == "/trace_attributes" {
			e := []map[string]any{}
			if q.Encoded == polyline6(umweg) {
				e = append(e, map[string]any{"length": kleinKm, "speed": 0, "road_class": "residential"})
			}
			json.NewEncoder(w).Encode(map[string]any{"edges": e})
			return
		}
		form, sec := route, 600.0
		if len(q.ExcludePolygons) > 0 {
			bereiche = len(q.ExcludePolygons)
			form, sec = umweg, umwegSec
		}
		leg := map[string]any{"shape": polyline6(form)}
		if q.DirectionsType != "none" { // wie Valhalla: „none“ auf oberster Ebene gewinnt
			leg["maneuvers"] = []map[string]any{{"instruction": "Rechts abbiegen.", "length": 1.5, "begin_shape_index": 1}, {"instruction": "", "length": 0, "begin_shape_index": 2}}
		}
		json.NewEncoder(w).Encode(map[string]any{"trip": map[string]any{"summary": map[string]float64{"time": sec, "length": 7}, "legs": []map[string]any{leg}}})
	}))
	defer vh.Close()
	frage := func() (*httptest.ResponseRecorder, routeResp) {
		s := &Server{Valhalla: &Valhalla{URL: vh.URL, Timeout: time.Second, Client: vh.Client()},
			Verkehr: &Verkehr{URL: api.URL, Client: api.Client(), UA: "Test"}}
		return post(s, `{"from":{"lat":50.8,"lon":6.1},"to":{"lat":50.8,"lon":6.2},"art":"auto","kmh":120}`)
	}
	fall := func(name, d string, sec, klein float64, block, soll string) {
		t.Helper()
		delay, umwegSec, kleinKm, blockiert, bereiche = d, sec, klein, block, 0
		w, r := frage()
		got := ""
		if len(r.Journeys) > 1 {
			got = r.Journeys[1].Umfahrung
		}
		if w.Code != 200 || got != soll || r.Journeys[0].Umfahrung != "" {
			t.Errorf("%s: Umfahrung %q, erwartet %q: %s", name, got, soll, w.Body)
		}
	}
	// Normale Route mit Stau: 600 s + 30 min = 2400 s
	fall("spart deutlich", "30", 900, 0, "false", "stau")
	if bereiche != 1 {
		t.Errorf("Bereiche: %d", bereiche)
	}
	fall("spart zu wenig", "30", 2200, 0, "false", "")
	fall("über Nebenstraßen", "30", 900, 3, "false", "")
	fall("Nebenstraßen knapp erlaubt", "30", 900, 1.5, "false", "stau")
	fall("kleiner Stau", "3", 100, 0, "false", "")
	fall("Sperrung: auch langsamer und über Nebenstraßen", "0", 5000, 9, "true", "sperrung")
	_, r := frage()
	if l := r.Journeys[0].Legs[0]; len(l.Schritte) != 1 || l.Schritte[0] != (schrittOut{"Rechts abbiegen.", 1500, 1}) {
		t.Errorf("Schritte: %+v", l.Schritte)
	}
}

func TestSperrungNurAufDerRampeSelbst(t *testing.T) {
	route := []pt{{50.8, 6.1}, {50.8, 6.2}}
	// 40 m neben der Fahrbahn, gleiche Richtung: eine Abfahrt, an der man vorbeifährt
	daneben := []pt{{50.80036, 6.11}, {50.80036, 6.12}}
	auf := []pt{{50.8, 6.11}, {50.8, 6.12}}
	for _, f := range []struct {
		art  string
		form []pt
		soll int
	}{{"sperrung", daneben, 0}, {"sperrung", auf, 1}, {"stau", daneben, 1}} {
		if got := len(aufDerRoute([]Meldung{{Art: f.art, Form: f.form}}, route)); got != f.soll {
			t.Errorf("%s %v: %d", f.art, f.form[0], got)
		}
	}
}

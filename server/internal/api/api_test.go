package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lord3n20/RWaysTH/server/internal/raptor"
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

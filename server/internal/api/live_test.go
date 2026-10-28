package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gtfs "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"

	"github.com/Lord3n20/SchnellWeg/server/internal/geocode"
	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
	"github.com/Lord3n20/SchnellWeg/server/internal/rt"
)

// liveFeed: Fahrt trip (Betriebstag date) ist an stop delay Sekunden verspätet.
func liveFeed(t *testing.T, trip, date, stop string, delay int32, canceled bool) *rt.Live {
	t.Helper()
	tu := &gtfs.TripUpdate{Trip: &gtfs.TripDescriptor{TripId: proto.String(trip), StartDate: proto.String(date)},
		StopTimeUpdate: []*gtfs.TripUpdate_StopTimeUpdate{{StopId: proto.String(stop),
			Arrival: &gtfs.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay)}, Departure: &gtfs.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay)}}}}
	if canceled {
		tu.Trip.ScheduleRelationship = gtfs.TripDescriptor_CANCELED.Enum()
	}
	b, _ := proto.Marshal(&gtfs.FeedMessage{Header: &gtfs.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Timestamp: proto.Uint64(1)},
		Entity: []*gtfs.FeedEntity{{Id: proto.String("1"), TripUpdate: tu}}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	t.Cleanup(ts.Close)
	return &rt.Live{URL: ts.URL, Client: ts.Client(), MinAge: time.Minute, MaxAge: time.Hour, UA: "test"}
}

func liveServer(t *testing.T, l *rt.Live) *Server {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	s.Live, s.LiveWait = l, time.Second
	s.Swap(s.Current()) // setzt die Fahrten für den Filter
	return s
}

func postTo(s *Server, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return w
}

func TestDepartures(t *testing.T) {
	s := liveServer(t, liveFeed(t, "t1", "20261028", "A", 300, false))
	w := postTo(s, "/v1/departures", `{"stops":["A","B","X"],"time":"2026-10-28T07:58:00+01:00"}`)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("Code %d %s", w.Code, body)
	}
	for _, want := range []string{`"rt":true`, `"A":[{"line":"1","dep":"2026-10-28T08:00:00+01:00","trip_id":"t1","dep_rt":"2026-10-28T08:05:00+01:00"}`,
		`"trip_id":"t2"`, `"B":null`, `"X":null`} {
		if !strings.Contains(body, want) {
			t.Errorf("fehlt %s in %s", want, body)
		}
	}
	// 08:03: t1 fährt laut Plan 08:00, laut Prognose 08:05 → noch dabei
	if b := postTo(s, "/v1/departures", `{"stops":["A"],"time":"2026-10-28T08:03:00+01:00"}`).Body.String(); !strings.Contains(b, `"t1"`) {
		t.Errorf("verspätete Fahrt fehlt: %s", b)
	}
	for _, bad := range []string{`{"stops":[]}`, `{"stops":["A"],"n":99}`, `{"stops":["A"],"x":1}`} {
		if c := postTo(s, "/v1/departures", bad).Code; c != 400 {
			t.Errorf("%s: Code %d", bad, c)
		}
	}
}

func TestRouteLive(t *testing.T) {
	s := liveServer(t, liveFeed(t, "t2", "20261028", "A", 120, false))
	b := postTo(s, "/v1/route", `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"}`).Body.String()
	for _, want := range []string{`"rt":true`, `"dep_rt":"2026-10-28T08:32:00+01:00"`, `"arr_rt":"2026-10-28T08:42:00+01:00"`} {
		if !strings.Contains(b, want) {
			t.Errorf("fehlt %s in %s", want, b)
		}
	}
	s = liveServer(t, liveFeed(t, "t2", "20261028", "A", 0, true))
	if b := postTo(s, "/v1/route", `{"from":{"lat":50.7701,"lon":6.08},"to":{"lat":50.7799,"lon":6.08},"time":"2026-10-28T08:05:00+01:00"}`).Body.String(); !strings.Contains(b, `"risk":"ausfall"`) {
		t.Errorf("Ausfall nicht markiert: %s", b)
	}
}

func TestStops(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stops", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `["A","A",50.77,6.08]`) {
		t.Fatalf("Code %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/stops", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("ETag nicht beachtet: %d", w2.Code)
	}
}

// Umstieg B → B2 (60 s Fußweg) auf die Fahrt um 600; kommt die erste Fahrt 250 s später an B an, ist er gefährdet.
func TestAnschlussRisiko(t *testing.T) {
	tt := &raptor.Timetable{Stops: []raptor.Stop{{ID: "A"}, {ID: "B"}, {ID: "B2"}, {ID: "C"}}}
	tt.Routes = []raptor.Route{
		{Stops: []int32{0, 1}, Arr: []int32{100, 400, 200, 500}, Dep: []int32{100, 400, 200, 500}, NTrips: 2,
			Trips: []raptor.TripInfo{{ID: "r0a"}, {ID: "r0b"}}},
		{Stops: []int32{2, 3}, Arr: []int32{450, 900, 600, 1000}, Dep: []int32{450, 900, 600, 1000}, NTrips: 2,
			Trips: []raptor.TripInfo{{ID: "r1a"}, {ID: "r1b"}}},
	}
	tt.StopRoutes = [][]int32{{0}, {0}, {1}, {1}}
	tt.StopPos = [][]int32{{0}, {1}, {0}, {1}}
	tt.Footpaths = [][]raptor.Footpath{nil, {{To: 2, Sec: 60}}, {{To: 1, Sec: 60}}, nil}
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin)
	p := NewPlan(tt, day, 1)
	j := raptor.NewRouter(tt).Query(50, []raptor.Access{{Stop: 0}}, []raptor.Access{{Stop: 3}}, 4).Journeys[0]
	for delay, want := range map[int32]string{0: "", 250: "anschluss"} {
		l := liveFeed(t, "r0a", "20261028", "B", delay, false)
		l.SetTrips(tripIDs(tt))
		ctx, cancel := contextTimeout()
		snap := l.Get(ctx)
		cancel()
		jo := convert(tt, DayBase(day), j)
		p.annotate(snap, j, &jo)
		if jo.Risk != want || !jo.RT {
			t.Errorf("Verspätung %d: Risiko %q, erwartet %q", delay, jo.Risk, want)
		}
	}
}

func contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Second)
}

func TestSearch(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	if c := postTo(s, "/v1/search", `{"q":"pont"}`).Code; c != 503 {
		t.Fatalf("ohne Suchliste: %d", c)
	}
	ix, _ := geocode.Load(strings.NewReader("adresse\tPontstraße 5\t52062 Aachen\t50.778\t6.081\n"))
	s.Geocode = ix
	b := postTo(s, "/v1/search", `{"q":"pontstr 5"}`).Body.String()
	if !strings.Contains(b, `"name":"Pontstraße 5"`) || !strings.Contains(b, `"lat":50.778`) {
		t.Fatalf("%s", b)
	}
	if c := postTo(s, "/v1/search", `{"q":"x","n":50}`).Code; c != 400 {
		t.Fatalf("n=50: %d", c)
	}
	if b := postTo(s, "/v1/search", `{"q":"pontstr 5","near":[50.75,6.1]}`).Body.String(); !strings.Contains(b, "Pontstraße 5") {
		t.Fatalf("near: %s", b)
	}
	if c := postTo(s, "/v1/search", `{"q":"pontstr","near":[50.75]}`).Code; c != 400 {
		t.Fatalf("near mit einem Wert: %d", c)
	}
}

func TestMap(t *testing.T) {
	s := testServer(time.Date(2026, 10, 28, 0, 0, 0, 0, Berlin))
	get := func(pfad string, h map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, pfad, nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if c := get("/v1/map", nil).Code; c != 503 {
		t.Fatalf("ohne Karte: %d", c)
	}
	d := t.TempDir()
	os.WriteFile(d+"/regionen.json", []byte(`[{"id":"uebersicht","name":"Übersicht","box":"5,47,15,55","zoom":9},{"id":"nw","name":"NRW","box":"5.8,50.3,9.5,52.5","zoom":15},{"id":"by","name":"Bayern","box":"9,47,14,51","zoom":15}]`), 0o644)
	os.WriteFile(d+"/uebersicht.pmtiles", []byte("PMTiles-Testinhalt"), 0o644)
	os.WriteFile(d+"/nw.pmtiles", []byte("NRW"), 0o644)
	s.MapDir = d
	w := get("/v1/map", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"nw"`) || strings.Contains(w.Body.String(), `"by"`) ||
		!strings.Contains(w.Body.String(), `"bytes":3`) {
		t.Fatalf("Liste %d %s", w.Code, w.Body.String())
	}
	w = get("/v1/map/uebersicht", nil)
	if w.Code != 200 || w.Body.String() != "PMTiles-Testinhalt" {
		t.Fatalf("Code %d", w.Code)
	}
	if w2 := get("/v1/map/uebersicht", map[string]string{"Range": "bytes=0-6"}); w2.Code != 206 || w2.Body.String() != "PMTiles" {
		t.Fatalf("Range: %d %q", w2.Code, w2.Body.String())
	}
	if w3 := get("/v1/map/uebersicht", map[string]string{"If-Modified-Since": w.Header().Get("Last-Modified")}); w3.Code != 304 {
		t.Fatalf("If-Modified-Since: %d", w3.Code)
	}
	for _, p := range []string{"/v1/map/by", "/v1/map/..%2Fx", "/v1/map/regionen"} {
		if c := get(p, nil).Code; c != 404 && c != 503 {
			t.Fatalf("%s: %d", p, c)
		}
	}
}

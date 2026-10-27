package rt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	gtfs "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func feed(t *testing.T) []byte {
	t.Helper()
	d := func(v int32) *gtfs.TripUpdate_StopTimeEvent {
		return &gtfs.TripUpdate_StopTimeEvent{Delay: proto.Int32(v)}
	}
	m := &gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Timestamp: proto.Uint64(1793134915)},
		Entity: []*gtfs.FeedEntity{
			{Id: proto.String("1"), TripUpdate: &gtfs.TripUpdate{
				Trip: &gtfs.TripDescriptor{TripId: proto.String("a"), StartDate: proto.String("20261027")},
				StopTimeUpdate: []*gtfs.TripUpdate_StopTimeUpdate{
					{StopId: proto.String("s2"), Arrival: d(60), Departure: d(120)},
					{StopId: proto.String("s4"), ScheduleRelationship: gtfs.TripUpdate_StopTimeUpdate_SKIPPED.Enum()},
				}}},
			{Id: proto.String("2"), TripUpdate: &gtfs.TripUpdate{
				Trip: &gtfs.TripDescriptor{TripId: proto.String("fremd"), StartDate: proto.String("20261027")}}},
			{Id: proto.String("3"), TripUpdate: &gtfs.TripUpdate{
				Trip: &gtfs.TripDescriptor{TripId: proto.String("b"), StartDate: proto.String("20261027"),
					ScheduleRelationship: gtfs.TripDescriptor_CANCELED.Enum()}}},
		},
	}
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLive(t *testing.T) {
	body := feed(t)
	var hits, full atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		full.Add(1)
		w.Header().Set("ETag", `"v1"`)
		w.Write(body)
	}))
	defer ts.Close()
	l := &Live{URL: ts.URL, Client: ts.Client(), MinAge: 50 * time.Millisecond, MaxAge: time.Minute, UA: "test"}
	l.SetTrips(map[string]struct{}{"a": {}, "b": {}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := l.Get(ctx)
	if s == nil || len(s.Trips) != 2 || s.Stats.Kept != 2 {
		t.Fatalf("Stand: %+v", s)
	}
	if !s.Trips[TripKey{"b", "20261027"}].Canceled {
		t.Fatal("Fahrt b nicht abgesagt")
	}
	if l.Get(ctx) != s || hits.Load() != 1 {
		t.Fatalf("innerhalb MinAge neu abgerufen (%d)", hits.Load())
	}
	time.Sleep(60 * time.Millisecond)
	if s2 := l.Get(ctx); s2 == nil || s2.Trips[TripKey{"a", "20261027"}] == nil || full.Load() != 1 || hits.Load() != 2 {
		t.Fatalf("304 nicht genutzt: hits %d, voll %d", hits.Load(), full.Load())
	}
	if l.LastErr() != "" {
		t.Fatal(l.LastErr())
	}
}

func TestLiveFehler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer ts.Close()
	l := &Live{URL: ts.URL, Client: ts.Client(), MinAge: time.Minute, MaxAge: time.Minute, UA: "test"}
	l.SetTrips(map[string]struct{}{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s := l.Get(ctx); s != nil || l.LastErr() != "HTTP 503" {
		t.Fatalf("erwartet nur Fahrplan und Fehler, bekam %v / %q", s, l.LastErr())
	}
}

func TestDelays(t *testing.T) {
	tr := &Trip{Upd: []StopUpd{
		{StopID: "s2", ArrDelay: 60, DepDel: 120, HasArr: true, HasDep: true},
		{StopID: "s4", Skipped: true},
	}}
	arr, dep, known, skipped := tr.Delays([]string{"s1", "s2", "s3", "s4", "s5"})
	if !reflect.DeepEqual(arr, []int32{0, 60, 120, 120, 120}) || !reflect.DeepEqual(dep, []int32{0, 120, 120, 120, 120}) {
		t.Fatalf("arr %v dep %v", arr, dep)
	}
	if !reflect.DeepEqual(known, []bool{false, true, true, true, true}) || !reflect.DeepEqual(skipped, []bool{false, false, false, true, false}) {
		t.Fatalf("known %v skipped %v", known, skipped)
	}
}

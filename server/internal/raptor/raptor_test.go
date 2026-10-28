package raptor

import (
	"reflect"
	"strings"
	"testing"
)

// Zwei Linien A→B und B→C, Umstieg an B per Fußweg zu B'.
func testTT() *Timetable {
	tt := &Timetable{Stops: []Stop{{"A", "A", 0, 0}, {"B", "B", 0, 0.01}, {"B2", "B2", 0, 0.0101}, {"C", "C", 0, 0.02}}}
	tt.Routes = []Route{
		{Stops: []int32{0, 1}, Arr: []int32{100, 400, 200, 500}, Dep: []int32{100, 400, 200, 500}, NTrips: 2},
		{Stops: []int32{2, 3}, Arr: []int32{450, 900, 600, 1000}, Dep: []int32{450, 900, 600, 1000}, NTrips: 2},
	}
	tt.StopRoutes = [][]int32{{0}, {0}, {1}, {1}}
	tt.StopPos = [][]int32{{0}, {1}, {0}, {1}}
	tt.Footpaths = [][]Footpath{nil, {{2, 60}}, {{1, 60}}, nil}
	return tt
}

func TestUmstieg(t *testing.T) {
	r := NewRouter(testTT())
	res := r.Query(50, []Access{{0, 0}}, []Access{{3, 30}}, 4)
	if res.ArrByTrips[1] != inf {
		t.Fatalf("mit 1 Fahrt unerreichbar erwartet, bekam %d", res.ArrByTrips[1])
	}
	if res.ArrByTrips[2] != 1030 { // 100 ab A, 400 an B, 460 an B2, 450 verpasst → 600, an 1000, +30 Zugang
		t.Fatalf("Ankunft mit 2 Fahrten: %d", res.ArrByTrips[2])
	}
	if res.ArrByTrips[4] != 1030 {
		t.Fatalf("spätere Runden müssen den Bestwert behalten: %d", res.ArrByTrips[4])
	}
}

func TestWalkScale(t *testing.T) {
	r := NewRouter(testTT())
	r.WalkScale = 4 // Umstieg B→B2 240 s statt 60: an B2 640, Anschluss 600 verpasst
	if res := r.Query(50, []Access{{0, 0}}, []Access{{3, 30}}, 4); res.ArrByTrips[4] != inf {
		t.Fatalf("langsam: Anschluss müsste verpasst sein, Ankunft %d", res.ArrByTrips[4])
	}
	r.WalkScale = 0.5 // schneller, aber der Mindestumstieg von 60 s bleibt
	if res := r.Query(50, []Access{{0, 0}}, []Access{{3, 30}}, 4); res.ArrByTrips[2] != 1030 || res.Journeys[0].Legs[2].Arr != 460 {
		t.Fatalf("schnell: %+v", res)
	}
}

func TestSpaeterStart(t *testing.T) {
	r := NewRouter(testTT())
	res := r.Query(150, []Access{{0, 0}}, []Access{{3, 0}}, 3)
	if res.ArrByTrips[2] != 1000 { // 200 ab A, 500 an B, 560 an B2 → 600, an 1000
		t.Fatalf("Ankunft: %d", res.ArrByTrips[2])
	}
}

func TestWeg(t *testing.T) {
	r := NewRouter(testTT())
	res := r.Query(50, []Access{{0, 20}}, []Access{{3, 30}}, 4)
	if len(res.Journeys) != 1 {
		t.Fatalf("ein Weg erwartet, bekam %d", len(res.Journeys))
	}
	j := res.Journeys[0]
	want := []Leg{
		{Kind: Walk, From: -1, To: 0, Dep: 50, Arr: 70},
		{Kind: Ride, From: 0, To: 1, Dep: 100, Arr: 400, Route: 0, Trip: 0},
		{Kind: Walk, From: 1, To: 2, Dep: 400, Arr: 460},
		{Kind: Ride, From: 2, To: 3, Dep: 600, Arr: 1000, Route: 1, Trip: 1},
		{Kind: Walk, From: 3, To: -1, Dep: 1000, Arr: 1030},
	}
	if j.Trips != 2 || j.Arr != 1030 || len(j.Legs) != len(want) {
		t.Fatalf("Weg: %+v", j)
	}
	for i := range want {
		if j.Legs[i] != want[i] {
			t.Errorf("Abschnitt %d: %+v, erwartet %+v", i, j.Legs[i], want[i])
		}
	}
}

func TestReadFootpaths(t *testing.T) {
	tt := &Timetable{
		Stops:     []Stop{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		StopIndex: map[string]int32{"a": 0, "b": 1, "c": 2},
		Footpaths: [][]Footpath{{{1, 100}, {2, 100}}, {{0, 100}}, {{0, 100}}},
	}
	in := "from_id,to_id,sec,m\na,b,30,40\na,c,,\nb,a,500,900\nx,a,10,10\n"
	src, n, err := tt.ReadFootpaths(strings.NewReader(in), 800)
	if err != nil || src != 2 || n != 1 {
		t.Fatalf("src %d n %d err %v", src, n, err)
	}
	// a: nur b, mit 60 s Mindestumstieg; b: Weg zu lang → keiner; c: unverändert Luftlinie
	if !reflect.DeepEqual(tt.Footpaths, [][]Footpath{{{1, 60}}, {}, {{0, 100}}}) {
		t.Fatalf("Footpaths %v", tt.Footpaths)
	}
}

func TestMergeWalks(t *testing.T) {
	in := []Leg{
		{Kind: Walk, From: -1, To: 0, Dep: 0, Arr: 60},
		{Kind: Walk, From: 0, To: 1, Dep: 60, Arr: 120},
		{Kind: Ride, From: 1, To: 2, Dep: 200, Arr: 500},
		{Kind: Walk, From: 2, To: 3, Dep: 500, Arr: 560},
		{Kind: Walk, From: 3, To: -1, Dep: 560, Arr: 600},
	}
	want := []Leg{
		{Kind: Walk, From: -1, To: 1, Dep: 0, Arr: 120},
		{Kind: Ride, From: 1, To: 2, Dep: 200, Arr: 500},
		{Kind: Walk, From: 2, To: -1, Dep: 500, Arr: 600},
	}
	if got := mergeWalks(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeWalks: %+v", got)
	}
}

func TestIsBahn(t *testing.T) {
	for typ, want := range map[int]bool{0: true, 1: true, 2: true, 3: false, 106: true, 109: true, 400: true, 700: false, 715: false, 900: true, 200: false} {
		if IsBahn(typ) != want {
			t.Errorf("route_type %d: %v", typ, !want)
		}
	}
}

func TestIsFern(t *testing.T) {
	for line, want := range map[string]bool{"ICE 10": true, "EC": true, "ECE": true, "THA": true, "FLX10": true,
		"NJ": true, "RJX": true, "IC17": false, "RE4": false, "RB20": false, "S1": false, "E": false, "EW": false, "33": false} {
		if IsFern(line) != want {
			t.Errorf("%s: erwartet %v", line, want)
		}
	}
}

package raptor

import "sort"

const inf = int32(1 << 30)

// Access ist ein Start- oder Zielhalt mit Gehzeit vom/zum eigentlichen Ort.
type Access struct {
	Stop int32
	Sec  int32
}

// Result: beste Ankunft am Ziel je Anzahl Fahrten (Index = Fahrten, 0 = unerreichbar über ÖPNV)
// und je Pareto-Stufe (weniger Fahrten oder früher) der zugehörige Weg.
type Result struct {
	ArrByTrips []int32
	Journeys   []Journey
}

// LegKind unterscheidet Fußweg und Fahrt.
type LegKind uint8

const (
	Walk LegKind = iota
	Ride
)

// Leg ist ein Wegabschnitt. From/To = -1 steht für Start- bzw. Zielort.
// Bei Fahrten sind Route/Trip Indizes in Timetable.Routes bzw. Route.Trips.
type Leg struct {
	Kind        LegKind
	From, To    int32
	Dep, Arr    int32
	Route, Trip int32
}

// Journey ist ein vollständiger Weg vom Start- zum Zielort.
type Journey struct {
	Trips int
	Arr   int32
	Legs  []Leg
}

// label merkt sich, wie ein Halt in einer Runde verbessert wurde.
type label struct {
	set      bool
	kind     uint8 // 0 Zugang, 1 Fahrt, 2 Fußweg
	arr      int32
	sec      int32 // Gehzeit bei Zugang/Fußweg
	from     int32 // Fußweg: Ausgangshalt
	route    int32
	trip     int32
	boardPos int32
}

// Router hält die Arbeitsfelder, damit pro Anfrage nichts alloziert wird. Nicht nebenläufig nutzbar.
type Router struct {
	tt                *Timetable
	best              []int32
	round, prev       []int32
	marked, routeMark []bool
	routeFrom         []int32
	markedList        []int32
	touched           []int32
	labels            [][]label // [Runde][Halt]
	labelSet          []int32   // gesetzte Einträge (Runde*n+Halt) zum Zurücksetzen
	NoFern            bool      // Fernverkehrs-Routen auslassen (Ticketfilter)
	NurBahn           bool      // nur Schiene (Fahrrad, D-21)
	WalkScale         float64   // Umstiegs-Gehzeiten × Faktor (Gehtempo, D-12); 0 = unverändert, mindestens 60 s bleiben
}

func (r *Router) setLabel(k int, s int32, l label) {
	if !r.labels[k][s].set {
		r.labelSet = append(r.labelSet, int32(k*len(r.best))+s)
	}
	l.set = true
	r.labels[k][s] = l
}

// at liefert das jüngste Label von s in Runde ≤ k (also die Ankunft mit höchstens k Fahrten).
func (r *Router) at(k int, s int32) (int, *label) {
	for ; k >= 0; k-- {
		if l := &r.labels[k][s]; l.set {
			return k, l
		}
	}
	return -1, nil
}

func NewRouter(tt *Timetable) *Router {
	n := len(tt.Stops)
	return &Router{tt: tt, best: make([]int32, n), round: make([]int32, n), prev: make([]int32, n),
		marked: make([]bool, n), routeMark: make([]bool, len(tt.Routes)), routeFrom: make([]int32, len(tt.Routes))}
}

// Query: Multi-Source-RAPTOR (frühste Ankunft) mit maxTrips Fahrten.
func (r *Router) Query(dep int32, from, to []Access, maxTrips int) Result {
	tt := r.tt
	for i := range r.best {
		r.best[i], r.round[i], r.marked[i] = inf, inf, false
	}
	r.markedList = r.markedList[:0]
	n := len(r.best)
	for _, i := range r.labelSet {
		r.labels[int(i)/n][int(i)%n] = label{}
	}
	r.labelSet = r.labelSet[:0]
	for len(r.labels) <= maxTrips {
		r.labels = append(r.labels, make([]label, n))
	}
	mark := func(s int32) {
		if !r.marked[s] {
			r.marked[s] = true
			r.markedList = append(r.markedList, s)
		}
	}
	for _, a := range from {
		t := dep + a.Sec
		if t < r.round[a.Stop] {
			r.round[a.Stop], r.best[a.Stop] = t, t
			r.setLabel(0, a.Stop, label{kind: 0, arr: t, sec: a.Sec})
			mark(a.Stop)
		}
	}
	res := Result{ArrByTrips: make([]int32, maxTrips+1)}
	for i := range res.ArrByTrips {
		res.ArrByTrips[i] = inf
	}
	target := func() int32 {
		b := inf
		for _, a := range to {
			if r.best[a.Stop] < inf && r.best[a.Stop]+a.Sec < b {
				b = r.best[a.Stop] + a.Sec
			}
		}
		return b
	}
	for k := 1; k <= maxTrips && len(r.markedList) > 0; k++ {
		copy(r.prev, r.round)
		// Routen sammeln, die an markierten Halten vorbeikommen (frühste Position je Route)
		var routes []int32
		for _, s := range r.markedList {
			for j, ri := range tt.StopRoutes[s] {
				p := tt.StopPos[s][j]
				if !r.routeMark[ri] {
					r.routeMark[ri] = true
					r.routeFrom[ri] = p
					routes = append(routes, ri)
				} else if p < r.routeFrom[ri] {
					r.routeFrom[ri] = p
				}
			}
			r.marked[s] = false
		}
		r.markedList = r.markedList[:0]
		tb := target()
		for _, ri := range routes {
			r.routeMark[ri] = false
			rt := &tt.Routes[ri]
			if r.NoFern && rt.Fern || r.NurBahn && !rt.Bahn {
				continue
			}
			n := len(rt.Stops)
			trip, board := -1, 0
			for i := int(r.routeFrom[ri]); i < n; i++ {
				s := rt.Stops[i]
				if trip >= 0 {
					a := rt.Arr[trip*n+i]
					if a < r.best[s] && a < tb {
						r.best[s], r.round[s] = a, a
						r.setLabel(k, s, label{kind: 1, arr: a, route: ri, trip: int32(trip), boardPos: int32(board)})
						mark(s)
					}
				}
				// früher einsteigen möglich?
				if t := r.prev[s]; t < inf && (trip < 0 || t <= rt.Dep[trip*n+i]) {
					lo, hi := 0, rt.NTrips
					if trip >= 0 {
						hi = trip
					}
					for lo < hi { // erste Fahrt mit Abfahrt ≥ t (Fahrten sind nach Abfahrt am ersten Halt sortiert; Näherung ohne Überholen)
						m := (lo + hi) / 2
						if rt.Dep[m*n+i] >= t {
							hi = m
						} else {
							lo = m + 1
						}
					}
					if lo < rt.NTrips && (trip < 0 || lo < trip) {
						trip, board = lo, i
					}
				}
			}
		}
		// Fußwege von neu verbesserten Halten
		r.touched = append(r.touched[:0], r.markedList...)
		for _, s := range r.touched {
			for _, f := range tt.Footpaths[s] {
				sec := f.Sec
				if r.WalkScale > 0 && r.WalkScale != 1 {
					sec = max(60, int32(float64(sec)*r.WalkScale+0.5))
				}
				if a := r.round[s] + sec; a < r.best[f.To] && a < r.round[f.To] {
					r.best[f.To], r.round[f.To] = a, a
					r.setLabel(k, f.To, label{kind: 2, arr: a, sec: sec, from: s})
					mark(f.To)
				}
			}
		}
		res.ArrByTrips[k] = target()
	}
	for k := 1; k <= maxTrips; k++ { // Runden nach frühem Abbruch übernehmen den Bestwert
		if res.ArrByTrips[k-1] < res.ArrByTrips[k] {
			res.ArrByTrips[k] = res.ArrByTrips[k-1]
		}
	}
	for k := 1; k <= maxTrips; k++ {
		if v := res.ArrByTrips[k]; v < inf && v < res.ArrByTrips[k-1] {
			if j, ok := r.journey(k, to); ok {
				res.Journeys = append(res.Journeys, j)
			}
		}
	}
	return res
}

// journey baut den Weg mit höchstens k Fahrten rückwärts aus den Labels.
func (r *Router) journey(k int, to []Access) (Journey, bool) {
	best, bestA := inf, Access{}
	for _, a := range to {
		if _, l := r.at(k, a.Stop); l != nil && l.arr+a.Sec < best {
			best, bestA = l.arr+a.Sec, a
		}
	}
	if best == inf {
		return Journey{}, false
	}
	j := Journey{Arr: best}
	_, l := r.at(k, bestA.Stop)
	legs := []Leg{{Kind: Walk, From: bestA.Stop, To: -1, Dep: l.arr, Arr: best}}
	s := bestA.Stop
	for guard := 0; guard < 4*len(r.labels)+8; guard++ {
		kk, l := r.at(k, s)
		if l == nil {
			return Journey{}, false
		}
		switch l.kind {
		case 0:
			legs = append(legs, Leg{Kind: Walk, From: -1, To: s, Dep: l.arr - l.sec, Arr: l.arr})
			for a, b := 0, len(legs)-1; a < b; a, b = a+1, b-1 {
				legs[a], legs[b] = legs[b], legs[a]
			}
			j.Legs = mergeWalks(legs)
			return j, true
		case 1:
			rt := &r.tt.Routes[l.route]
			bs := rt.Stops[l.boardPos]
			legs = append(legs, Leg{Kind: Ride, From: bs, To: s, Dep: rt.Dep[int(l.trip)*len(rt.Stops)+int(l.boardPos)],
				Arr: l.arr, Route: l.route, Trip: l.trip})
			j.Trips++
			s, k = bs, kk-1
		case 2:
			legs = append(legs, Leg{Kind: Walk, From: l.from, To: s, Dep: l.arr - l.sec, Arr: l.arr})
			s, k = l.from, kk
		}
	}
	return Journey{}, false
}

// Near liefert Halte mit Abfahrten im Umkreis maxM um einen Ort (Luftlinie). Die Gehzeit ist eine Schätzung;
// mit Valhalla ersetzt die API sie durch den echten Weg.
func (tt *Timetable) Near(lat, lon, maxM float64) []Access {
	tt.byLatOnce.Do(tt.buildByLat)
	dLat := maxM / 111195
	lo := sort.Search(len(tt.byLat), func(k int) bool { return tt.Stops[tt.byLat[k]].Lat >= lat-dLat })
	var out []Access
	for _, i := range tt.byLat[lo:] {
		s := &tt.Stops[i]
		if s.Lat > lat+dLat {
			break
		}
		if d := Dist(lat, lon, s.Lat, s.Lon); d <= maxM {
			out = append(out, Access{i, WalkSec(d)})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Stop < out[b].Stop }) // Reihenfolge wie bisher
	return out
}

// PerRoute dünnt Zugangshalte aus: je Muster nur der Halt mit der kürzesten Gehzeit. Weiter entfernte Halte
// derselben Muster bringen keine frühere Ankunft und kosten in der Valhalla-Matrix nur Zeit.
func (tt *Timetable) PerRoute(acc []Access) []Access {
	best := make([]int32, len(tt.Routes)) // Muster → Index in acc + 1 (0 = noch keiner); günstiger als eine Map
	for i, a := range acc {
		for _, r := range tt.StopRoutes[a.Stop] {
			if j := best[r]; j == 0 || a.Sec < acc[j-1].Sec {
				best[r] = int32(i + 1)
			}
		}
	}
	keep := make([]bool, len(acc))
	for _, j := range best {
		if j > 0 {
			keep[j-1] = true
		}
	}
	var out []Access
	for i, a := range acc {
		if keep[i] {
			out = append(out, a)
		}
	}
	return out
}

// mergeWalks fasst direkt aufeinanderfolgende Fußwege (Zugang + Umstiegsweg, Umstiegsweg + Weg zum Ziel) zu
// einem zusammen; der Zwischenhalt ist für die Anzeige ohne Bedeutung.
func mergeWalks(legs []Leg) []Leg {
	out := legs[:1]
	for _, l := range legs[1:] {
		if p := &out[len(out)-1]; p.Kind == Walk && l.Kind == Walk {
			p.To, p.Arr = l.To, l.Arr
			continue
		}
		out = append(out, l)
	}
	return out
}

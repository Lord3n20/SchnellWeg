// Package raptor ist ein erster RAPTOR-Prototyp (Phase 0, D-02): Fahrplan eines Betriebstags aus GTFS,
// Fußwege per Luftlinie, in der Region ersetzbar durch vorab berechnete Valhalla-Fußwege (ReadFootpaths, D-12).
package raptor

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stop ist ein Halt (Bahnsteig/Haltepunkt) mit Koordinaten.
type Stop struct {
	ID, Name string
	Lat, Lon float64
}

// Footpath verbindet zwei Halte mit Gehzeit in Sekunden.
type Footpath struct {
	To  int32
	Sec int32
}

// Route ist ein RAPTOR-Muster: alle Fahrten mit derselben Haltefolge, nach Abfahrt sortiert.
type Route struct {
	Stops []int32
	// Times[t*len(Stops)+i] = Ankunft/Abfahrt der Fahrt t am i-ten Halt (Sekunden ab Mitternacht).
	Arr, Dep []int32
	NTrips   int
	Trips    []TripInfo // je Fahrt, parallel zu den Zeilen von Arr/Dep
	Fern     bool       // Fernverkehr, nicht im Deutschland(semester)ticket
}

// TripInfo beschreibt eine Fahrt für die Ausgabe.
type TripInfo struct {
	ID, Line, Headsign string
	PrevDay            bool // Fahrt des Vortags nach Mitternacht (Zeiten um 24 h verschoben)
	Fern               bool
}

// fernPrefix: Zuggattungen, die das Deutschlandticket nicht abdeckt. IC fehlt absichtlich: de_rv führt nur die
// ICs mit Tarifanerkennung (IC17/35/56/87, teils nur abschnittsweise); ICs aus de_fv markiert „:fern“ am Feed.
var fernPrefix = regexp.MustCompile(`^(ICE|ECE|ECM|EC|EN|NJ|RJX?|THA|TGV|EST?|FLX)(\b|[0-9 ])`)

// IsFern meldet, ob eine Linie nach ihrem Namen Fernverkehr ist.
func IsFern(line string) bool { return fernPrefix.MatchString(line) }

// Timetable hält einen Betriebstag kompakt im Speicher.
type Timetable struct {
	Stops      []Stop
	StopIndex  map[string]int32
	Routes     []Route
	StopRoutes [][]int32 // Halt → Routen
	StopPos    [][]int32 // parallel zu StopRoutes: Position des Halts in der Route
	Footpaths  [][]Footpath
	byLat      []int32 // Halte mit Abfahrten, nach Breite sortiert (für Near)
}

type csvFile struct {
	r   *csv.Reader
	col map[string]int
}

func open(z *zip.ReadCloser, name string) (*csvFile, io.Closer, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		r := csv.NewReader(rc)
		r.ReuseRecord = true
		r.FieldsPerRecord = -1
		h, err := r.Read()
		if err != nil {
			rc.Close()
			return nil, nil, err
		}
		col := map[string]int{}
		for i, c := range h {
			col[strings.TrimPrefix(c, "\ufeff")] = i
		}
		return &csvFile{r, col}, rc, nil
	}
	return nil, nil, fmt.Errorf("%s fehlt", name)
}

func each(z *zip.ReadCloser, name string, fn func(get func(string) string)) error {
	f, c, err := open(z, name)
	if err != nil {
		return err
	}
	defer c.Close()
	for {
		rec, err := f.r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		fn(func(k string) string {
			if i, ok := f.col[k]; ok && i < len(rec) {
				return rec[i]
			}
			return ""
		})
	}
}

func hms(s string) int32 {
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return -1
	}
	h, _ := strconv.Atoi(p[0])
	m, _ := strconv.Atoi(p[1])
	x, _ := strconv.Atoi(p[2])
	return int32(h*3600 + m*60 + x)
}

type st struct {
	seq      int
	stop     int32
	arr, dep int32
}

type trip struct {
	info TripInfo
	st   []st
}

// Load liest die Fahrten aus einem oder mehreren GTFS-Feeds, die am Tag day verkehren. Halt- und Fahrt-IDs
// bekommen das Präfix „<Feednummer>:“; Umstiege zwischen Feeds laufen über die Fußwege. maxWalkM begrenzt die
// Luftlinien-Fußwege. Ein Pfad mit Endung „:fern“ (de_fv.zip:fern) gilt komplett als Fernverkehr, sonst
// entscheidet IsFern je Linie.
func Load(paths []string, day time.Time, maxWalkM float64) (*Timetable, error) {
	tt := &Timetable{StopIndex: map[string]int32{}}
	var all []*trip
	for i, p := range paths {
		p, fern := strings.CutSuffix(p, ":fern")
		t, err := tt.loadFeed(p, strconv.Itoa(i)+":", day, fern)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		all = append(all, t...)
	}

	// Muster bilden
	type pat struct{ trips []*trip }
	pats := map[string]*pat{}
	for _, t := range all {
		var b strings.Builder
		if t.info.Fern {
			b.WriteByte('F')
		}
		for _, x := range t.st {
			b.WriteString(strconv.Itoa(int(x.stop)))
			b.WriteByte(',')
		}
		k := b.String()
		if pats[k] == nil {
			pats[k] = &pat{}
		}
		pats[k].trips = append(pats[k].trips, t)
	}
	tt.StopRoutes = make([][]int32, len(tt.Stops))
	tt.StopPos = make([][]int32, len(tt.Stops))
	for _, p := range pats {
		sort.Slice(p.trips, func(i, j int) bool { return p.trips[i].st[0].dep < p.trips[j].st[0].dep })
		n := len(p.trips[0].st)
		r := Route{Stops: make([]int32, n), Arr: make([]int32, 0, n*len(p.trips)), Dep: make([]int32, 0, n*len(p.trips)),
			NTrips: len(p.trips), Trips: make([]TripInfo, 0, len(p.trips)), Fern: p.trips[0].info.Fern}
		for i, x := range p.trips[0].st {
			r.Stops[i] = x.stop
		}
		for _, t := range p.trips {
			r.Trips = append(r.Trips, t.info)
			for _, x := range t.st {
				r.Arr = append(r.Arr, x.arr)
				r.Dep = append(r.Dep, x.dep)
			}
		}
		ri := int32(len(tt.Routes))
		tt.Routes = append(tt.Routes, r)
		for i, s := range r.Stops {
			tt.StopRoutes[s] = append(tt.StopRoutes[s], ri)
			tt.StopPos[s] = append(tt.StopPos[s], int32(i))
		}
	}
	tt.buildFootpaths(maxWalkM)
	tt.buildByLat()
	return tt, nil
}

// loadFeed liest Halte und die Fahrten des Tages (und Vortags-Fahrten nach Mitternacht) eines Feeds.
func (tt *Timetable) loadFeed(path, pre string, day time.Time, fern bool) ([]*trip, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	if err := each(z, "stops.txt", func(g func(string) string) {
		lat, _ := strconv.ParseFloat(g("stop_lat"), 64)
		lon, _ := strconv.ParseFloat(g("stop_lon"), 64)
		id := pre + g("stop_id")
		tt.StopIndex[id] = int32(len(tt.Stops))
		tt.Stops = append(tt.Stops, Stop{id, strings.Clone(g("stop_name")), lat, lon})
	}); err != nil {
		return nil, err
	}
	// Bit 1: verkehrt am Tag, Bit 2: verkehrt am Vortag (nur Teile nach Mitternacht zählen)
	svc := map[string]uint8{}
	for bit, d := range map[uint8]time.Time{1: day, 2: day.AddDate(0, 0, -1)} {
		ymd, wd := d.Format("20060102"), strings.ToLower(d.Weekday().String())
		_ = each(z, "calendar.txt", func(g func(string) string) {
			if g(wd) == "1" && g("start_date") <= ymd && ymd <= g("end_date") {
				svc[strings.Clone(g("service_id"))] |= bit
			}
		})
		_ = each(z, "calendar_dates.txt", func(g func(string) string) {
			if g("date") == ymd {
				id := strings.Clone(g("service_id"))
				if g("exception_type") == "1" {
					svc[id] |= bit
				} else {
					svc[id] &^= bit
				}
			}
		})
	}
	lines := map[string]string{}
	_ = each(z, "routes.txt", func(g func(string) string) {
		n := g("route_short_name")
		if n == "" {
			n = g("route_long_name")
		}
		lines[strings.Clone(g("route_id"))] = strings.Clone(n)
	})
	type tripMeta struct {
		days     uint8
		line, hs string
	}
	tripOK := map[string]tripMeta{}
	if err := each(z, "trips.txt", func(g func(string) string) {
		if d := svc[g("service_id")]; d != 0 {
			tripOK[strings.Clone(g("trip_id"))] = tripMeta{d, lines[g("route_id")], strings.Clone(g("trip_headsign"))}
		}
	}); err != nil {
		return nil, err
	}

	trips := map[string]*trip{}
	if err := each(z, "stop_times.txt", func(g func(string) string) {
		id := g("trip_id")
		if _, ok := tripOK[id]; !ok {
			return
		}
		s, ok := tt.StopIndex[pre+g("stop_id")]
		if !ok {
			return
		}
		seq, _ := strconv.Atoi(g("stop_sequence"))
		a, d := hms(g("arrival_time")), hms(g("departure_time"))
		if a < 0 {
			a = d
		}
		if d < 0 {
			d = a
		}
		t := trips[id]
		if t == nil {
			id = strings.Clone(id)
			m := tripOK[id]
			t = &trip{info: TripInfo{ID: pre + id, Line: m.line, Headsign: m.hs, Fern: fern || IsFern(m.line)}}
			trips[id] = t
		}
		t.st = append(t.st, st{seq, s, a, d})
	}); err != nil {
		return nil, err
	}
	// Fahrten des Tages und Vortags-Fahrten, die nach Mitternacht noch unterwegs sind (Zeiten −24 h)
	var all []*trip
	for id, t := range trips {
		sort.Slice(t.st, func(i, j int) bool { return t.st[i].seq < t.st[j].seq })
		if len(t.st) < 2 {
			continue
		}
		days := tripOK[id].days
		if days&1 != 0 {
			all = append(all, t)
		}
		if days&2 != 0 && t.st[len(t.st)-1].arr >= 86400 {
			y := &trip{info: t.info, st: make([]st, len(t.st))}
			y.info.PrevDay = true
			for i, x := range t.st {
				y.st[i] = st{x.seq, x.stop, x.arr - 86400, x.dep - 86400}
			}
			all = append(all, y)
		}
	}

	return all, nil
}

// Dist liefert die Luftlinie in Metern (äquirektanguläre Näherung, für < 10 km genau genug).
func Dist(aLat, aLon, bLat, bLon float64) float64 {
	x := (bLon - aLon) * math.Cos((aLat+bLat)/2*math.Pi/180)
	y := bLat - aLat
	return math.Sqrt(x*x+y*y) * 111195
}

// WalkSec: Luftlinie × 1,3 Umweg bei 1,2 m/s, mindestens 60 s Umstieg.
func WalkSec(m float64) int32 { return int32(math.Max(60, m*1.3/1.2)) }

func (tt *Timetable) buildFootpaths(maxM float64) {
	cell := maxM / 111195
	grid := map[[2]int][]int32{}
	key := func(s Stop) [2]int { return [2]int{int(s.Lat / cell), int(s.Lon / cell / 0.63)} }
	for i, s := range tt.Stops {
		if len(tt.StopRoutes[i]) > 0 {
			grid[key(s)] = append(grid[key(s)], int32(i))
		}
	}
	tt.Footpaths = make([][]Footpath, len(tt.Stops))
	for i, s := range tt.Stops {
		if len(tt.StopRoutes[i]) == 0 {
			continue
		}
		k := key(s)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, j := range grid[[2]int{k[0] + dx, k[1] + dy}] {
					if j == int32(i) {
						continue
					}
					if d := Dist(s.Lat, s.Lon, tt.Stops[j].Lat, tt.Stops[j].Lon); d <= maxM {
						tt.Footpaths[i] = append(tt.Footpaths[i], Footpath{j, WalkSec(d)})
					}
				}
			}
		}
	}
}

func (tt *Timetable) buildByLat() {
	tt.byLat = tt.byLat[:0]
	for i := range tt.Stops {
		if len(tt.StopRoutes[i]) > 0 {
			tt.byLat = append(tt.byLat, int32(i))
		}
	}
	sort.Slice(tt.byLat, func(a, b int) bool { return tt.Stops[tt.byLat[a]].Lat < tt.Stops[tt.byLat[b]].Lat })
}

// InBox: Halt liegt im Rechteck (Valhalla-Tiles decken nur die Region ab).
func (s Stop) InBox(b [4]float64) bool {
	return s.Lon >= b[0] && s.Lat >= b[1] && s.Lon <= b[2] && s.Lat <= b[3]
}

// WritePairs schreibt die Luftlinien-Fußwege mit Quelle in box als Kandidaten für den Valhalla-Fußweg-Job:
// from_id,from_lat,from_lon,to_id,to_lat,to_lon,luft_m.
func (tt *Timetable) WritePairs(w io.Writer, box [4]float64) (int, error) {
	cw := csv.NewWriter(w)
	cw.Write([]string{"from_id", "from_lat", "from_lon", "to_id", "to_lat", "to_lon", "luft_m"})
	n := 0
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', 6, 64) }
	for i, fps := range tt.Footpaths {
		a := tt.Stops[i]
		if !a.InBox(box) {
			continue
		}
		for _, fp := range fps {
			b := tt.Stops[fp.To]
			cw.Write([]string{a.ID, f(a.Lat), f(a.Lon), b.ID, f(b.Lat), f(b.Lon),
				strconv.Itoa(int(Dist(a.Lat, a.Lon, b.Lat, b.Lon)))})
			n++
		}
	}
	cw.Flush()
	return n, cw.Error()
}

// ReadFootpaths ersetzt die Luftlinien-Fußwege aller Quellen, die in r vorkommen, durch die Valhalla-Gehzeiten
// (from_id,to_id,sec,m; leeres sec = kein Fußweg gefunden). Wege über maxM (Umweg über Gleise, Autobahn …)
// entfallen. Andere Quellen behalten die Luftlinie. Liefert ersetzte Quellen und übernommene Fußwege.
func (tt *Timetable) ReadFootpaths(r io.Reader, maxM int) (src, n int, err error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	if _, err = cr.Read(); err != nil {
		return
	}
	done := map[int32]bool{}
	for {
		rec, e := cr.Read()
		if e == io.EOF {
			return
		}
		if e != nil {
			return src, n, e
		}
		a, okA := tt.StopIndex[rec[0]]
		b, okB := tt.StopIndex[rec[1]]
		if !okA || !okB {
			continue // anderer Fahrplanstand
		}
		if !done[a] {
			done[a] = true
			tt.Footpaths[a] = tt.Footpaths[a][:0]
			src++
		}
		if sec, e := strconv.Atoi(rec[2]); e == nil {
			if m, e := strconv.Atoi(rec[3]); e != nil || m > maxM {
				continue
			}
			tt.Footpaths[a] = append(tt.Footpaths[a], Footpath{b, int32(max(60, sec))})
			n++
		}
	}
}

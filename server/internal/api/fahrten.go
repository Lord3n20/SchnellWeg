package api

import (
	"cmp"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/Lord3n20/SchnellWeg/server/internal/raptor"
)

// Fahrt-Erkennung (D-46): Die App schickt die Spur einer aufgezeichneten Route, der Server sucht darin Fahrten des Fahrplans.
// Am Tempo allein ist ein Bus nicht von einem Longboard zu trennen. Deshalb zählen Halte in Fahrplan-Reihenfolge, ein gleichbleibender
// Versatz zum Fahrplan und ob an Zwischenhalten wirklich gehalten wurde. Die Spur wird nicht gespeichert und nicht protokolliert.
const (
	fahrtMaxPunkte  = 8000
	fahrtMaxBytes   = 1 << 20
	haltRadiusM     = 60.0
	besuchLueckeS   = 30  // solange noch derselbe Besuch eines Halts
	fahrtToleranzS  = 300 // so weit darf die Fahrt vom Fahrplan abweichen
	versatzSicherS  = 60  // so wenig darf der Versatz über die Fahrt schwanken
	versatzUnsicher = 90
	stehtUnterMS    = 1.0
	gehaltenAbS     = 5  // an einem Zwischenhalt
	gewartetAbS     = 20 // am Einstieg
	schnellAbMS     = 14 // kein eigenes Profil ist im Schnitt so schnell (Grenze des Messgangs)
)

type fahrtReq struct {
	Punkte [][4]float64 `json:"punkte"` // Breite, Länge, Unix-Sekunden, m/s (< 0 = unbekannt)
}

type fahrtOut struct {
	Linie  string  `json:"linie"`
	Bus    bool    `json:"bus"`
	Von    stopOut `json:"von"`
	Nach   stopOut `json:"nach"`
	Ab     int64   `json:"ab"` // beobachtet, Unix-Sekunden
	An     int64   `json:"an"`
	M      float64 `json:"m"`       // gefahrene Meter
	WarteS int32   `json:"warte_s"` // am Einstieg gewartet
	Sicher bool    `json:"sicher"`
}

type besuch struct {
	first, last, steht int32
}

type treffer struct {
	pos     int32
	v       *besuch
	versatz int32
}

type kandidat struct {
	r      *raptor.Route
	trip   int
	t      []treffer
	spanne int32 // Halte der Fahrt zwischen erstem und letztem Treffer
	streu  int32 // Schwankung des Versatzes
}

func (s *Server) fahrten(w http.ResponseWriter, r *http.Request) {
	var q fahrtReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, fahrtMaxBytes)).Decode(&q); err != nil || len(q.Punkte) > fahrtMaxPunkte {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	p := s.Current()
	if p == nil {
		fail(w, http.StatusServiceUnavailable, "fahrplan_laedt")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"fahrten": fahrtenSuchen(p.TT, DayBase(p.Day), q.Punkte)})
}

// fahrtenSuchen liefert die erkannten Fahrten in zeitlicher Reihenfolge. Nicht sichere Treffer sind dabei, damit die App dann nichts lernt.
func fahrtenSuchen(tt *raptor.Timetable, base time.Time, punkte [][4]float64) []fahrtOut {
	b0 := base.Unix()
	besuche := map[int32][]*besuch{}
	var vor int32 = -1
	for _, p := range punkte {
		sek := int32(int64(p[2]) - b0)
		dt := int32(0)
		if vor >= 0 && sek-vor <= 10 {
			dt = sek - vor
		}
		vor = sek
		for _, a := range tt.Near(p[0], p[1], haltRadiusM) {
			l := besuche[a.Stop]
			if n := len(l); n > 0 && sek-l[n-1].last <= besuchLueckeS {
				l[n-1].last = sek
				if p[3] >= 0 && p[3] < stehtUnterMS {
					l[n-1].steht += dt
				}
				continue
			}
			besuche[a.Stop] = append(l, &besuch{first: sek, last: sek})
		}
	}
	var alle []kandidat
	for halt, vs := range besuche {
		for k, ri := range tt.StopRoutes[halt] {
			alle = append(alle, kandidatenAb(&tt.Routes[ri], tt.StopPos[halt][k], vs, besuche)...)
		}
	}
	slices.SortFunc(alle, func(a, b kandidat) int {
		return cmp.Or(cmp.Compare(len(b.t), len(a.t)), cmp.Compare(a.streu, b.streu), cmp.Compare(a.t[0].v.last, b.t[0].v.last))
	})
	out := []fahrtOut{}
	var belegt [][2]int32
	for _, c := range alle {
		von, bis := c.t[0].v.last, c.t[len(c.t)-1].v.first
		if slices.ContainsFunc(belegt, func(x [2]int32) bool { return von < x[1] && x[0] < bis }) {
			continue
		}
		f, ok := fahrtBewerten(tt, c, b0, punkte)
		if !ok {
			continue
		}
		belegt = append(belegt, [2]int32{von, bis})
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b fahrtOut) int { return cmp.Compare(a.Ab, b.Ab) })
	return out
}

func kandidatenAb(r *raptor.Route, pos int32, vs []*besuch, besuche map[int32][]*besuch) []kandidat {
	n := int32(len(r.Stops))
	if pos >= n-1 {
		return nil
	}
	var out []kandidat
	for t := 0; t < r.NTrips; t++ {
		ab := r.Dep[int32(t)*n+pos]
		for _, v := range vs {
			if ab < v.first-fahrtToleranzS || ab > v.last+fahrtToleranzS {
				continue
			}
			c := kandidat{r: r, trip: t, t: []treffer{{pos, v, versatz(ab, v)}}}
			fehlt := 0
			for j := pos + 1; j < n && fehlt <= 1; j++ {
				an := r.Arr[int32(t)*n+j]
				i := slices.IndexFunc(besuche[r.Stops[j]], func(x *besuch) bool {
					return x.first >= c.t[len(c.t)-1].v.last && an >= x.first-fahrtToleranzS && an <= x.last+fahrtToleranzS
				})
				if i < 0 {
					fehlt++
					continue
				}
				x := besuche[r.Stops[j]][i]
				c.t = append(c.t, treffer{j, x, versatz(an, x)})
				fehlt = 0
			}
			if len(c.t) < 2 {
				continue
			}
			kl, gr := c.t[0].versatz, c.t[0].versatz
			for _, x := range c.t {
				kl, gr = min(kl, x.versatz), max(gr, x.versatz)
			}
			c.streu, c.spanne = gr-kl, c.t[len(c.t)-1].pos-pos+1
			out = append(out, c)
		}
	}
	return out
}

// versatz: beobachtet minus Fahrplan; liegt die Planzeit im Besuch, ist er 0.
func versatz(plan int32, v *besuch) int32 {
	return min(max(plan, v.first), v.last) - plan
}

func fahrtBewerten(tt *raptor.Timetable, c kandidat, b0 int64, punkte [][4]float64) (fahrtOut, bool) {
	erster, letzter := c.t[0].v, c.t[len(c.t)-1].v
	dauer := letzter.first - erster.last
	if dauer <= 0 {
		return fahrtOut{}, false
	}
	m, _ := spurLaenge(punkte, b0+int64(erster.last), b0+int64(letzter.first))
	innen, gehalten := len(c.t)-2, 0
	for _, x := range c.t[1 : len(c.t)-1] {
		if x.v.steht >= gehaltenAbS {
			gehalten++
		}
	}
	hielt := innen > 0 && gehalten*2 >= innen
	sicher := (len(c.t) >= 3 && len(c.t)*4 >= int(c.spanne)*3 && c.streu <= versatzSicherS && hielt) ||
		(m/float64(dauer) >= schnellAbMS && c.streu <= versatzSicherS*2 && dauer >= 60)
	if !sicher && (c.streu > versatzUnsicher || !(hielt || erster.steht >= gewartetAbS)) {
		return fahrtOut{}, false // nichts spricht für eine Fahrt
	}
	info := c.r.Trips[c.trip]
	von, nach := tt.Stops[c.r.Stops[c.t[0].pos]], tt.Stops[c.r.Stops[c.t[len(c.t)-1].pos]]
	return fahrtOut{Linie: info.Line, Bus: !c.r.Bahn, Von: stopOut{von.ID, von.Name}, Nach: stopOut{nach.ID, nach.Name},
		Ab: b0 + int64(erster.last), An: b0 + int64(letzter.first), M: math.Round(m), WarteS: max(0, erster.last-erster.first), Sicher: sicher}, true
}

// spurLaenge summiert die Meter der Spur zwischen zwei Zeitpunkten.
func spurLaenge(punkte [][4]float64, von, bis int64) (m float64, n int) {
	var a *[4]float64
	for i := range punkte {
		p := &punkte[i]
		if int64(p[2]) < von || int64(p[2]) > bis {
			continue
		}
		if a != nil {
			m += abstandM(a[0], a[1], p[0], p[1])
		}
		a = p
		n++
	}
	return m, n
}

func abstandM(aLat, aLon, bLat, bLon float64) float64 {
	const r, rad = 6371000.0, math.Pi / 180
	dLat, dLon := (bLat-aLat)*rad, (bLon-aLon)*rad
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(aLat*rad)*math.Cos(bLat*rad)*math.Pow(math.Sin(dLon/2), 2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

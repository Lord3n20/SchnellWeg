package geocode

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Normalize: klein, Umlaute/ß gefaltet, Satzzeichen zu Leerzeichen; ein Durchlauf, eine Zuweisung (läuft je
// Kandidat, also oft).
func Normalize(s string) string {
	b := make([]byte, 0, len(s)+2)
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, byte(r))
		case r >= 'A' && r <= 'Z':
			b = append(b, byte(r)+32)
		case r < 128:
			b = append(b, ' ')
		default:
			switch unicode.ToLower(r) {
			case 'ä', 'à', 'á', 'â':
				b = append(b, 'a')
			case 'ö', 'ò', 'ó', 'ô':
				b = append(b, 'o')
			case 'ü', 'ù', 'ú', 'û':
				b = append(b, 'u')
			case 'é', 'è', 'ê', 'ë':
				b = append(b, 'e')
			case 'ß':
				b = append(b, 's', 's')
			default:
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					b = utf8.AppendRune(b, unicode.ToLower(r))
				} else {
					b = append(b, ' ')
				}
			}
		}
	}
	return string(b)
}

func words(s string) []string { return strings.Fields(s) }

const (
	maxUnion = 1 << 21 // größte Trefferliste eines Suchworts, die noch zusammengeführt wird
	maxWords = 20000   // Wörter zu einem Wortanfang, darüber zu allgemein („s“, „ha“)
)

func isNum(w string) bool { return w != "" && w[0] >= '0' && w[0] <= '9' }

// term sind die Wörter des Index mit dem Suchwort als Anfang (beim Tippen: „5“ findet auch 50).
func (p *part) term(w string) (ids []int, total int, ok bool) {
	lo := sort.Search(len(p.wordOff)-1, func(i int) bool { return p.word(i) >= w })
	for i := lo; i < len(p.wordOff)-1; i++ {
		x := p.word(i)
		if !strings.HasPrefix(x, w) {
			break
		}
		if len(ids) == maxWords {
			return nil, 0, false
		}
		ids = append(ids, i)
		total += int(p.postOff[i+1] - p.postOff[i])
	}
	return ids, total, true
}

// spans liefert die Ausschnitte der Trefferlisten der Wörter in den Eintragsbereichen rs (nil = ganze Listen).
// stop > 0: aufhören, sobald so viele zusammen sind.
func (p *part) spans(ids []int, rs [][2]uint32, stop int) (ls [][]uint32, n int) {
	for _, i := range ids {
		if stop > 0 && n >= stop {
			break
		}
		l := p.posts[p.postOff[i]:p.postOff[i+1]]
		if rs == nil {
			if len(l) > 0 {
				ls, n = append(ls, l), n+len(l)
			}
			continue
		}
		for _, r := range rs {
			a := sort.Search(len(l), func(k int) bool { return l[k]>>1 >= r[0] })
			b := sort.Search(len(l), func(k int) bool { return l[k]>>1 >= r[1] })
			if a < b {
				ls, n = append(ls, l[a:b]), n+b-a
			}
		}
	}
	return ls, n
}

// mergeAll führt Ausschnitte zusammen: aufsteigend nach Eintrag, Namensmarke bei Dopplung verodert.
func mergeAll(ls [][]uint32) []uint32 {
	for len(ls) > 1 { // paarweise: N·log(k)
		var next [][]uint32
		for i := 0; i < len(ls); i += 2 {
			if i+1 == len(ls) {
				next = append(next, ls[i])
			} else {
				next = append(next, merge(ls[i], ls[i+1]))
			}
		}
		ls = next
	}
	if len(ls) == 0 {
		return nil
	}
	return slices.Clip(ls[0]) // kann direkt in die eingeblendete Datei zeigen: append darf dort nie hineinschreiben
}

// collect nimmt ohne Zusammenführen bis zu limit Einträge, zuerst die mit dem Wort im Namen (Dopplungen
// verwirft die Wertung).
func collect(ls [][]uint32, limit int) []uint32 {
	out := make([]uint32, 0, limit)
	for _, flag := range []uint32{1, 0} {
		for _, l := range ls {
			for _, v := range l {
				if len(out) == limit {
					return out
				}
				if v&1 == flag {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// contains: steht Eintrag v in einem der Ausschnitte (binäre Suche)?
func contains(ls [][]uint32, v uint32) bool {
	for _, l := range ls {
		k := sort.Search(len(l), func(k int) bool { return l[k]>>1 >= v>>1 })
		if k < len(l) && l[k]>>1 == v>>1 {
			return true
		}
	}
	return false
}

func merge(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch x, y := a[i]>>1, b[j]>>1; {
		case x < y:
			out = append(out, a[i])
			i++
		case x > y:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i]|b[j])
			i, j = i+1, j+1
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}

// intersect behält aus a die Einträge, die auch in b stehen (Namensmarke von a).
func intersect(a, b []uint32) []uint32 {
	out := a[:0:0]
	j := 0
	for _, v := range a {
		for j < len(b) && b[j]>>1 < v>>1 {
			j++
		}
		if j < len(b) && b[j]>>1 == v>>1 {
			out = append(out, v)
		}
	}
	return out
}

// candidates sucht in den Bereichen rs (nil = alle) bis zu limit Einträge, die zu allen Suchwörtern passen
// könnten: Trefferliste des seltensten Worts, geschnitten mit den weiteren, solange es mehr als limit sind (je nach
// Größe nachschlagen oder zusammenführen); sonst zuerst die mit dem Wort im Namen (Orte und Postleitzahlen im
// Detail stehen bei Tausenden Adressen). Wörter mit mehr als maxUnion Treffern („5“ samt 5xxxx, „ha“) werden für
// ganz DE nur geprüft, nicht gesammelt.
func (p *part) candidates(qw []string, rs [][2]uint32, limit int) []uint32 {
	type t struct {
		ids   []int
		total int
		ls    [][]uint32
		n     int
	}
	var ts, big []t
	for _, w := range qw {
		if len(w) < 2 && !isNum(w) {
			continue // ein Buchstabe: nur prüfen
		}
		ids, total, ok := p.term(w)
		if !ok {
			continue
		}
		if total == 0 {
			return nil
		}
		if total > maxUnion {
			big = append(big, t{ids: ids, total: total})
			continue
		}
		ls, n := p.spans(ids, rs, 0)
		if n == 0 {
			return nil
		}
		ts = append(ts, t{ids, total, ls, n})
	}
	if len(ts) == 0 {
		if len(big) == 0 || rs == nil {
			return nil
		}
		// nur sehr allgemeine Wörter („ha“): in der Umgebung die ersten Treffer, früh aufhören
		ls, _ := p.spans(big[0].ids, rs, 4*limit)
		return collect(ls, limit)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].n < ts[j].n })
	if len(ts) == 1 && ts[0].n > limit {
		return collect(ts[0].ls, limit)
	}
	c := mergeAll(ts[0].ls)
	for _, x := range ts[1:] {
		if len(c) <= limit {
			break
		}
		if len(c)*len(x.ls)*16 < x.n { // nachschlagen billiger als zusammenführen
			out := c[:0:0]
			for _, v := range c {
				if contains(x.ls, v) {
					out = append(out, v)
				}
			}
			c = out
		} else {
			c = intersect(c, mergeAll(x.ls))
		}
	}
	if len(c) > limit {
		c = collect([][]uint32{c}, limit)
	}
	return c
}

// near liefert die Eintragsbereiche der Zellen im Abstand ≤ d um (lat, lon), aufsteigend.
func (p *part) near(lat, lon float64, d int32) [][2]uint32 {
	if len(p.cells) == 0 {
		return nil
	}
	c := cellOf(lat, lon)
	var rs [][2]uint32
	for dr := -d; dr <= d; dr++ {
		for dc := -d; dc <= d; dc++ {
			if r, ok := p.cells[c+dr*1000+dc]; ok {
				rs = append(rs, r)
			}
		}
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] < rs[j][0] })
	return rs
}

// Hit ist ein Suchtreffer.
type Hit struct {
	Entry
	score int
	km    float64 // Entfernung zum Nutzer, NaN ohne Standort
}

// Search liefert bis zu n Treffer für q ohne Standort.
func (ix *Index) Search(q string, n int) []Hit { return ix.SearchNear(q, n, math.NaN(), math.NaN()) }

// SearchNear liefert bis zu n Treffer für q; mit Standort (lat, lon; NaN = keiner) zählen nahe Treffer mehr und
// die Umgebung (~100 km) wird zuerst durchsucht. Wertung: alle Wörter müssen passen; genaue Wörter, Treffer am
// Namensanfang und Adressen mit passender Hausnummer zählen mehr, bei Gleichstand gewinnt das Nähere, dann der
// kürzere Name.
func (ix *Index) SearchNear(q string, n int, lat, lon float64) []Hit {
	var qw []string
	for _, w := range words(Normalize(q)) { // „Pont Str 5“ → „pontstr 5“
		if (w == "str" || w == "strasse") && len(qw) > 0 {
			qw[len(qw)-1] += w
			continue
		}
		qw = append(qw, w)
	}
	if len(qw) == 0 || len(qw) > 8 {
		return nil
	}
	long := false
	for _, w := range qw {
		long = long || len(w) >= 2
	}
	if !long {
		return nil // ein einzelnes Zeichen passt auf fast alles
	}
	here := !math.IsNaN(lat) && !math.IsNaN(lon)
	var hits []Hit
	for _, p := range ix.parts {
		// ganz DE, dann die eigene Zelle (~35 × 55 km), dann die 3 × 3 Zellen drumherum
		c := p.candidates(qw, nil, 2000)
		if here {
			for d := int32(0); d <= 1; d++ {
				if rs := p.near(lat, lon, d); rs != nil {
					c = append(c, p.candidates(qw, rs, 3000)...)
				}
			}
		}
		hits = append(hits, p.score(c, qw, lat, lon)...)
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		if here {
			if da, db := distRank(hits[a].km), distRank(hits[b].km); da != db {
				return da < db
			}
		}
		if la, lb := len(hits[a].Name), len(hits[b].Name); la != lb {
			return la < lb
		}
		return natLess(hits[a].Name, hits[b].Name)
	})
	// Gleicher Name und gleiches Detail im Umkreis von ~200 m nur einmal (Grenzgebiete, Gebäude + Knoten)
	out := hits[:0]
	for _, h := range hits {
		dup := false
		for _, o := range out {
			if o.Name == h.Name && o.Detail == h.Detail && math.Abs(o.Lat-h.Lat) < 0.002 && math.Abs(o.Lon-h.Lon) < 0.003 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, h)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// distRank fasst Entfernungen grob zusammen (0–1, 1–3, 3–7 … km), damit „Weg 2“ vor „Weg 10“ bleibt.
func distRank(km float64) int {
	if math.IsNaN(km) {
		return 99
	}
	return int(math.Log2(km + 1))
}

func distKm(lat1, lon1, lat2, lon2 float64) float64 {
	dy := (lat2 - lat1) * 111.2
	dx := (lon2 - lon1) * 111.2 * math.Cos((lat1+lat2)/2*math.Pi/180)
	return math.Hypot(dx, dy)
}

// minor sind Kategorien, die selten Ziel einer Fahrt sind und sonst Namen wie „Uniklinik“ verdrängen.
var minor = map[string]bool{"parking": true, "charging_station": true, "car_sharing": true, "bicycle_rental": true,
	"bicycle_parking": true, "vending_machine": true, "parking_entrance": true, "taxi": true, "atm": true,
	"post_box": true, "recycling": true, "waste_basket": true, "bench": true, "parcel_locker": true}

func hasDigit(ws []string) bool {
	for _, w := range ws {
		if isNum(w) {
			return true
		}
	}
	return false
}

// natLess vergleicht so, dass „Weg 2“ vor „Weg 10“ kommt.
func natLess(a, b string) bool {
	ia, ib := strings.LastIndexByte(a, ' '), strings.LastIndexByte(b, ' ')
	if ia > 0 && ib > 0 && a[:ia] == b[:ib] {
		na, ea := strconv.Atoi(strings.TrimRightFunc(a[ia+1:], unicode.IsLetter))
		nb, eb := strconv.Atoi(strings.TrimRightFunc(b[ib+1:], unicode.IsLetter))
		if ea == nil && eb == nil && na != nb {
			return na < nb
		}
	}
	return a < b
}

// score prüft die Kandidaten (jedes Suchwort muss passen) und bewertet sie.
func (p *part) score(cand []uint32, qw []string, lat, lon float64) []Hit {
	var hits []Hit
	seen := make(map[uint32]bool, len(cand))
	digit := hasDigit(qw)
	here := !math.IsNaN(lat) && !math.IsNaN(lon)
	for _, v := range cand {
		id := v >> 1
		if seen[id] {
			continue
		}
		seen[id] = true
		e := p.entry(id)
		norm := Normalize(e.Name)
		nw := len(words(norm))
		ew := words(norm + " " + Normalize(e.Detail))
		score, ok := 0, true
		for _, w := range qw {
			m := 0
			cmp := ew
			if len(w) < 4 && isNum(w) {
				cmp = ew[:nw] // Hausnummer nur im Namen, nicht in der Postleitzahl suchen
			}
			for k, x := range cmp {
				switch {
				case x == w:
					m = max(m, 3)
					if k == 0 {
						m = 4
					}
				case strings.HasPrefix(x, w):
					m = max(m, 1)
					if k == 0 {
						m = max(m, 2)
					}
				}
			}
			if m == 0 {
				ok = false
				break
			}
			score += m
		}
		if !ok {
			continue
		}
		switch e.Kind {
		case "gebaeude": // Hochschulgebäude: gezielt gesucht (Nummer oder Name), vor gleichnamigen Einrichtungen
			score += 3
		case "ort":
			score += 3
			if e.Detail == "city" || e.Detail == "town" {
				score++ // „Bergneustadt“ (Stadt) vor gleichnamigen Weilern
			}
		case "poi":
			if minor[e.Detail] {
				score -= 2
			} else {
				score++
			}
		case "adresse":
			if !digit {
				score -= 2 // ohne Hausnummer erst die Straße zeigen
			}
		case "strasse":
			if digit {
				score -= 2
			}
		}
		km := math.NaN()
		if here {
			km = distKm(lat, lon, e.Lat, e.Lon)
			switch {
			case km < 3:
				score += 3
			case km < 15:
				score += 2
			case km < 60:
				score++
			}
		}
		hits = append(hits, Hit{Entry: e, score: score, km: km})
	}
	return hits
}

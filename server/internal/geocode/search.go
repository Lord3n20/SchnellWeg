package geocode

import (
	"bufio"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Index durchsucht die Einträge über die Wortanfänge: jedes Wort eines Suchtexts muss Anfang eines Worts im
// Eintrag (Name + Detail) sein. Sortierte Wortliste + Trefferlisten, keine Kopien der Einträge.
type Index struct {
	Entries []Entry
	words   []string  // sortiert, eindeutig
	posts   [][]int32 // je Wort die Einträge
}

// Load liest die TSV aus Write.
func Load(r io.Reader) (*Index, error) {
	ix := &Index{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 5 {
			continue
		}
		lat, e1 := strconv.ParseFloat(f[3], 64)
		lon, e2 := strconv.ParseFloat(f[4], 64)
		if e1 != nil || e2 != nil {
			continue
		}
		ix.Entries = append(ix.Entries, Entry{Kind: f[0], Name: f[1], Detail: f[2], Lat: lat, Lon: lon})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	ix.build()
	return ix, nil
}

func (ix *Index) build() {
	m := map[string][]int32{}
	for i, e := range ix.Entries {
		seen := map[string]bool{}
		for _, w := range words(Normalize(e.Name) + " " + Normalize(e.Detail)) {
			if !seen[w] {
				seen[w] = true
				m[w] = append(m[w], int32(i))
			}
		}
	}
	ix.words = make([]string, 0, len(m))
	for w := range m {
		ix.words = append(ix.words, w)
	}
	sort.Strings(ix.words)
	ix.posts = make([][]int32, len(ix.words))
	for i, w := range ix.words {
		ix.posts[i] = m[w]
	}
}

// Normalize: klein, Umlaute/ß gefaltet, „str.“ → „strasse“, Satzzeichen zu Leerzeichen.
func Normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "é", "e", "è", "e", "ë", "e").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func words(s string) []string { return strings.Fields(s) }

// Hit ist ein Suchtreffer.
type Hit struct {
	Entry
	score int
}

// Search liefert bis zu n Treffer für q. Wertung: alle Wörter müssen passen; genaue Wörter, Treffer am
// Namensanfang und Adressen mit passender Hausnummer zählen mehr, kurze Namen gewinnen bei Gleichstand.
func (ix *Index) Search(q string, n int) []Hit {
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
	// Kandidaten aus dem seltensten Wort (Summe der Trefferlisten aller passenden Wörter)
	var cand []int32
	best := -1
	for _, w := range qw {
		if len(w) < 2 {
			continue // nur prüfen, nicht als Kandidatenquelle
		}
		var c []int32
		lo := sort.SearchStrings(ix.words, w)
		total := 0
		for i := lo; i < len(ix.words) && strings.HasPrefix(ix.words[i], w); i++ {
			total += len(ix.posts[i])
			if total > 200000 {
				break
			}
		}
		if best >= 0 && total >= best {
			continue
		}
		for i := lo; i < len(ix.words) && strings.HasPrefix(ix.words[i], w); i++ {
			c = append(c, ix.posts[i]...)
		}
		cand, best = c, total
	}
	var hits []Hit
	seen := map[int32]bool{}
	for _, id := range cand {
		if seen[id] {
			continue
		}
		seen[id] = true
		e := ix.Entries[id]
		norm := Normalize(e.Name)
		nw := len(words(norm))
		ew := words(norm + " " + Normalize(e.Detail))
		score, ok := 0, true
		for _, w := range qw {
			m := 0
			cmp := ew
			if len(w) < 4 && w[0] >= '0' && w[0] <= '9' {
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
		case "ort":
			score += 3
		case "poi":
			if minor[e.Detail] {
				score -= 2
			} else {
				score++
			}
		case "adresse":
			if !hasDigit(qw) {
				score -= 2 // ohne Hausnummer erst die Straße zeigen
			}
		case "strasse":
			if hasDigit(qw) {
				score -= 2
			}
		}
		hits = append(hits, Hit{Entry: e, score: score})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		if la, lb := len(hits[a].Name), len(hits[b].Name); la != lb {
			return la < lb
		}
		return natLess(hits[a].Name, hits[b].Name)
	})
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// minor sind Kategorien, die selten Ziel einer Fahrt sind und sonst Namen wie „Uniklinik“ verdrängen.
var minor = map[string]bool{"parking": true, "charging_station": true, "car_sharing": true, "bicycle_rental": true,
	"bicycle_parking": true, "vending_machine": true, "parking_entrance": true, "taxi": true, "atm": true,
	"post_box": true, "recycling": true, "waste_basket": true, "bench": true, "parcel_locker": true}

func hasDigit(ws []string) bool {
	for _, w := range ws {
		if w != "" && w[0] >= '0' && w[0] <= '9' {
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

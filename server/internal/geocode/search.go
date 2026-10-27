package geocode

import (
	"bufio"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Index durchsucht die Einträge über die Wortanfänge: jedes Wort eines Suchtexts muss Anfang eines Worts im
// Eintrag (Name + Detail) sein. Sortierte Wortliste + Trefferlisten, keine Kopien der Einträge.
type Index struct {
	Entries []Entry
	words   []string  // sortiert, eindeutig
	posts   [][]int32 // je Wort die Einträge (Wort in Name oder Detail)
	nposts  [][]int32 // je Wort die Einträge, in deren Namen es steht (Kandidaten zuerst hieraus)
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
	m, nm := map[string][]int32{}, map[string][]int32{}
	for i, e := range ix.Entries {
		seen := map[string]bool{}
		name := words(Normalize(e.Name))
		for k, w := range append(name, words(Normalize(e.Detail))...) {
			if !seen[w] {
				seen[w] = true
				m[w] = append(m[w], int32(i))
				if k < len(name) {
					nm[w] = append(nm[w], int32(i))
				}
			}
		}
	}
	ix.words = make([]string, 0, len(m))
	for w := range m {
		ix.words = append(ix.words, w)
	}
	sort.Strings(ix.words)
	ix.posts, ix.nposts = make([][]int32, len(ix.words)), make([][]int32, len(ix.words))
	for i, w := range ix.words {
		ix.posts[i], ix.nposts[i] = m[w], nm[w]
	}
}

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

// maxCand begrenzt die geprüften Einträge je Suche (Zeitbudget 30 ms auf dem Server).
const maxCand = 5000

// candidates liefert die Einträge des Suchworts mit den wenigsten Treffern in posts (höchstens maxCand).
func (ix *Index) candidates(qw []string, posts [][]int32) []int32 {
	var cand []int32
	best := -1
	for _, w := range qw {
		if len(w) < 2 {
			continue // nur prüfen, nicht als Kandidatenquelle
		}
		lo := sort.SearchStrings(ix.words, w)
		total := 0
		for i := lo; i < len(ix.words) && strings.HasPrefix(ix.words[i], w) && total <= maxCand; i++ {
			total += len(posts[i])
		}
		if best >= 0 && total >= best {
			continue
		}
		var c []int32
		for i := lo; i < len(ix.words) && strings.HasPrefix(ix.words[i], w) && len(c) < maxCand; i++ {
			c = append(c, posts[i]...)
		}
		cand, best = c, total
	}
	return cand
}

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
	// Kandidaten aus dem seltensten Wort, zuerst nur Einträge mit dem Wort im Namen: Orte und Postleitzahlen im
	// Detail stehen bei Tausenden Adressen („vaals“) und würden sonst alle geprüft.
	hits := ix.score(ix.candidates(qw, ix.nposts), qw)
	if len(hits) == 0 {
		hits = ix.score(ix.candidates(qw, ix.posts), qw)
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

// score prüft die Kandidaten (jedes Suchwort muss passen) und bewertet sie.
func (ix *Index) score(cand []int32, qw []string) []Hit {
	var hits []Hit
	seen := map[int32]bool{}
	digit := hasDigit(qw)
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
			if !digit {
				score -= 2 // ohne Hausnummer erst die Straße zeigen
			}
		case "strasse":
			if digit {
				score -= 2
			}
		}
		hits = append(hits, Hit{Entry: e, score: score})
	}
	return hits
}

// Package geocode baut aus OSM eine Suchliste (Adressen, Straßen, Orte, benannte Einrichtungen) mit Wortindex und
// durchsucht sie. Die Suche läuft nur auf dem eigenen Server; Suchtexte werden nie protokolliert.
package geocode

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
)

// Entry ist ein Treffer. Kind: adresse | strasse | ort | poi | gebaeude.
type Entry struct {
	Kind, Name, Detail string
	Lat, Lon           float64
}

// Tags mit Namen, die als Ziel taugen (Wert egal), und ihre Kategorie für die Anzeige.
var poiKeys = []string{"amenity", "shop", "tourism", "leisure", "office", "healthcare", "craft", "historic", "building"}

var placeKinds = map[string]bool{"city": true, "town": true, "village": true, "suburb": true, "quarter": true,
	"neighbourhood": true, "hamlet": true, "borough": true}

// Zelle: 0,5° × 0,5° (~35 × 55 km), Zeilen von Süd nach Nord. Die Suchliste ist nach Zellen geordnet, damit die
// Suche Einträge in der Nähe des Nutzers über Bereiche der Eintragsnummern findet.
func cellOf(lat, lon float64) int32 {
	return int32(math.Floor(lat*2)+180)*1000 + int32(math.Floor(lon*2)+360)
}

// ExtractFiles liest OSM-PBF-Dateien (ganze Länder, keine Box) und schreibt die Suchliste als TSV (siehe Write)
// nach Zellen geordnet nach w. Je Datei drei Durchläufe, damit nur die gebrauchten Knoten im Speicher stehen:
// Relationen → gesuchte Wege, Wege → je Weg ein bis zwei Knoten, Knoten → deren Koordinaten. tmp nimmt die
// Zwischendateien auf (danach leer). Liefert die Zahl der Einträge je Art.
func ExtractFiles(ctx context.Context, paths []string, tmp string, w io.Writer) (map[string]int, error) {
	b := &buckets{dir: tmp, files: map[int32]*bucket{}}
	defer b.remove()
	for _, p := range paths {
		if err := extractFile(ctx, p, tmp, b); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
	}
	return b.merge(w)
}

// openPBF öffnet p für einen Durchlauf nur über die gewünschten Objektarten.
func scanPBF(ctx context.Context, p string, nodes, ways, rels bool, fn func(osm.Object) error) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := osmpbf.New(ctx, f, 4)
	defer sc.Close()
	sc.SkipNodes, sc.SkipWays, sc.SkipRelations = !nodes, !ways, !rels
	for sc.Scan() {
		if err := fn(sc.Object()); err != nil {
			return err
		}
	}
	return sc.Err()
}

type relPending struct {
	es   []Entry
	ways []int64
}

// ids ist eine aufsteigende Liste von OSM-IDs. Gesammelt wird in Blöcken (kein Verdoppeln beim Wachsen), nach
// finish stehen je ID nur die unteren 32 Bit plus die Grenzen je oberem Teil (Knoten-IDs liegen über 2^32):
// 4 statt 8 Byte je ID, ganz DE ~20 Mio. Knoten. index rückt bei aufsteigenden Abfragen (PBF ist nach ID
// geordnet) nur vor, sonst binäre Suche.
type ids struct {
	blocks [][]int64 // beim Sammeln
	lo     []uint32
	seg    []int // seg[h]: erster Index mit oberem Teil h; seg[len(seg)-1] = Zahl der IDs
	p, h   int
	last   int64
}

const idBlock = 1 << 20

func (s *ids) add(id int64) {
	if id < 0 {
		return
	}
	if n := len(s.blocks); n == 0 || len(s.blocks[n-1]) == idBlock {
		s.blocks = append(s.blocks, make([]int64, 0, idBlock))
	}
	b := &s.blocks[len(s.blocks)-1]
	*b = append(*b, id)
}

func (s *ids) n() int { return len(s.lo) }

func (s *ids) finish() {
	n := 0
	for _, b := range s.blocks {
		n += len(b)
	}
	all := make([]int64, 0, n)
	for i, b := range s.blocks {
		all = append(all, b...)
		s.blocks[i] = nil
	}
	s.blocks = nil
	slices.Sort(all)
	all = slices.Compact(all)
	s.lo = make([]uint32, len(all))
	for i, x := range all {
		s.lo[i] = uint32(x)
	}
	top := 0
	if len(all) > 0 {
		top = int(all[len(all)-1]>>32) + 1
	}
	s.seg = make([]int, top+1)
	for h := range s.seg {
		s.seg[h] = sort.Search(len(all), func(i int) bool { return int(all[i]>>32) >= h })
	}
}

// lower ist der erste Index mit ID ≥ id.
func (s *ids) lower(id int64) int {
	h := int(id >> 32)
	if h < 0 {
		return 0
	}
	if h >= len(s.seg)-1 {
		return len(s.lo)
	}
	a, b := s.seg[h], s.seg[h+1]
	return a + sort.Search(b-a, func(i int) bool { return s.lo[a+i] >= uint32(id) })
}

// val ist die ID an Index i (nur aufsteigend seit dem letzten Zurücksetzen von s.h).
func (s *ids) val(i int) int64 {
	for i >= s.seg[s.h+1] {
		s.h++
	}
	return int64(s.h)<<32 | int64(s.lo[i])
}

func (s *ids) index(id int64) int {
	if id < s.last {
		s.p, s.h = s.lower(id), 0
	}
	s.last = id
	for s.p < len(s.lo) && s.val(s.p) < id {
		s.p++
	}
	if s.p < len(s.lo) && s.val(s.p) == id {
		return s.p
	}
	return -1
}

func (s *ids) find(id int64) int {
	if i := s.lower(id); id >= 0 && i < len(s.lo) && int(id>>32) < len(s.seg)-1 && s.lo[i] == uint32(id) && i < s.seg[id>>32+1] {
		return i
	}
	return -1
}

func extractFile(ctx context.Context, p, tmp string, b *buckets) error {
	// 1. Relationen (Multipolygone wie große Gebäude): Einträge vormerken, ihre äußeren Wege merken.
	var rels []relPending
	var needWays ids
	err := scanPBF(ctx, p, false, false, true, func(o osm.Object) error {
		r := o.(*osm.Relation)
		if t := r.Tags.Find("type"); t != "multipolygon" && t != "building" {
			return nil
		}
		if r.Tags.Find("name") == "" && r.Tags.Find("addr:housenumber") == "" {
			return nil
		}
		es := entries(r.Tags)
		if len(es) == 0 {
			return nil
		}
		rp := relPending{es: es}
		for _, m := range r.Members {
			if m.Type == osm.TypeWay && m.Role != "inner" {
				rp.ways = append(rp.ways, m.Ref)
				needWays.add(m.Ref)
			}
		}
		if len(rp.ways) > 0 {
			rels = append(rels, rp)
		}
		return nil
	})
	if err != nil {
		return err
	}
	needWays.finish()

	// 2. Wege: Einträge mit ihrem Knoten (siehe ends) in eine Zwischendatei.
	wf, err := os.Create(filepath.Join(tmp, "wege.tmp"))
	if err != nil {
		return err
	}
	defer os.Remove(wf.Name())
	ww := bufio.NewWriterSize(wf, 1<<20)
	var needNodes ids
	wayNodes := make([][2]int64, needWays.n()) // je gesuchtem Weg (Reihenfolge wie needWays) seine Knoten
	err = scanPBF(ctx, p, false, true, false, func(o osm.Object) error {
		wy := o.(*osm.Way)
		if len(wy.Nodes) == 0 {
			return nil
		}
		n1, n2 := ends(wy.Nodes)
		if i := needWays.index(int64(wy.ID)); i >= 0 {
			wayNodes[i] = [2]int64{n1, n2}
			needNodes.add(n1)
		}
		name, hw, hn := wy.Tags.Find("name"), wy.Tags.Find("highway"), wy.Tags.Find("addr:housenumber")
		if hn == "" && name == "" {
			return nil
		}
		if hw != "" && name != "" && hn == "" {
			if hw == "bus_stop" || hw == "platform" {
				return nil
			}
			needNodes.add(n1)
			fmt.Fprintf(ww, "%d\t%d\tstrasse\t%s\t\n", n1, n2, clean.Replace(name))
			return nil
		}
		es := entries(wy.Tags)
		if len(es) > 0 {
			needNodes.add(n1)
		}
		for _, e := range es {
			fmt.Fprintf(ww, "%d\t%d\t%s\t%s\t%s\n", n1, n2, e.Kind, clean.Replace(e.Name), clean.Replace(e.Detail))
		}
		return nil
	})
	if err == nil {
		err = ww.Flush()
	}
	if err != nil {
		wf.Close()
		return err
	}
	needNodes.finish()

	// 3. Knoten: Koordinaten der gemerkten Knoten; Knoten mit Tags direkt als Einträge.
	coords := make([]float32, 2*needNodes.n())
	for i := range coords {
		coords[i] = float32(math.NaN())
	}
	var places []Entry
	err = scanPBF(ctx, p, true, false, false, func(o osm.Object) error {
		n := o.(*osm.Node)
		if i := needNodes.index(int64(n.ID)); i >= 0 {
			coords[2*i], coords[2*i+1] = float32(n.Lat), float32(n.Lon)
		}
		if len(n.Tags) == 0 {
			return nil
		}
		for _, e := range entries(n.Tags) {
			e.Lat, e.Lon = n.Lat, n.Lon
			if e.Kind == "ort" {
				places = append(places, e)
			}
			if err := b.add(e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		wf.Close()
		return err
	}
	at := func(n1, n2 int64) (lat, lon float64, ok bool) {
		k := 0
		for _, n := range [2]int64{n1, n2} {
			if i := needNodes.find(n); i >= 0 && !math.IsNaN(float64(coords[2*i])) {
				lat += float64(coords[2*i])
				lon += float64(coords[2*i+1])
				k++
			}
		}
		if k == 0 {
			return 0, 0, false
		}
		return lat / float64(k), lon / float64(k), true
	}
	pl := newPlaceGrid(places)

	// Wege aus der Zwischendatei auflösen; Straßenstücke gleichen Namens je ~1-km-Zelle zu einem Eintrag mitteln.
	type street struct {
		lat, lon float64
		n        int
	}
	type streetKey struct {
		name   string
		la, lo int32
	}
	streets := map[streetKey]*street{}
	if _, err := wf.Seek(0, io.SeekStart); err != nil {
		wf.Close()
		return err
	}
	sc := bufio.NewScanner(bufio.NewReaderSize(wf, 1<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := bytes.SplitN(sc.Bytes(), []byte{'\t'}, 5)
		if len(f) != 5 {
			continue
		}
		n1, _ := strconv.ParseInt(string(f[0]), 10, 64)
		n2, _ := strconv.ParseInt(string(f[1]), 10, 64)
		lat, lon, ok := at(n1, n2)
		if !ok {
			continue
		}
		if string(f[2]) == "strasse" {
			k := streetKey{unsafe.String(unsafe.SliceData(f[3]), len(f[3])), int32(lat * 90), int32(lon * 60)} // Zelle ~1,2 × 1,2 km
			s := streets[k]
			if s == nil {
				k.name = string(f[3]) // eigene Kopie, f zeigt in den Puffer des Scanners
				s = &street{}
				streets[k] = s
			}
			s.lat, s.lon, s.n = s.lat+lat, s.lon+lon, s.n+1
			continue
		}
		if err := b.add(Entry{Kind: string(f[2]), Name: string(f[3]), Detail: string(f[4]), Lat: lat, Lon: lon}); err != nil {
			wf.Close()
			return err
		}
	}
	wf.Close()
	if err := sc.Err(); err != nil {
		return err
	}
	for k, s := range streets {
		e := Entry{Kind: "strasse", Name: k.name, Lat: s.lat / float64(s.n), Lon: s.lon / float64(s.n)}
		e.Detail = pl.nearest(e.Lat, e.Lon)
		if err := b.add(e); err != nil {
			return err
		}
	}
	streets = nil

	// Relationen: Mittel ihrer äußeren Wege
	for _, r := range rels {
		var lat, lon float64
		n := 0
		for _, wid := range r.ways {
			i := needWays.find(wid)
			if i < 0 {
				continue
			}
			if la, lo, ok := at(wayNodes[i][0], wayNodes[i][1]); ok {
				lat, lon, n = lat+la, lon+lo, n+1
			}
		}
		if n == 0 {
			continue
		}
		for _, e := range r.es {
			e.Lat, e.Lon = lat/float64(n), lon/float64(n)
			if err := b.add(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// ends wählt den Knoten, der den Weg vertritt: der mittlere, bei Gebäuden also eine Ecke (ein zweiter Knoten für
// die Mitte kostete ganz DE ~300 MB mehr, für das Ziel einer Fahrt reichen ~20 m).
func ends(ns osm.WayNodes) (int64, int64) {
	m := int64(ns[len(ns)/2].ID)
	return m, m
}

// entries macht aus Tags die Einträge ohne Koordinaten: Adresse, Ort oder Einrichtung; bei einer Einrichtung mit
// Adresse zusätzlich die Adresse selbst („Templergraben 57“).
func entries(t osm.Tags) []Entry {
	e, ok := entry(t)
	if !ok {
		return nil
	}
	out := []Entry{e}
	if e.Kind == "poi" {
		if hn, st := t.Find("addr:housenumber"), t.Find("addr:street"); hn != "" && st != "" {
			out = append(out, Entry{Kind: "adresse", Name: st + " " + hn,
				Detail: strings.TrimSpace(t.Find("addr:postcode") + " " + t.Find("addr:city"))})
		}
	}
	return out
}

func entry(t osm.Tags) (Entry, bool) {
	name := t.Find("name")
	if hn := t.Find("addr:housenumber"); hn != "" {
		st := t.Find("addr:street")
		if st == "" {
			st = t.Find("addr:place")
		}
		if st != "" {
			detail := strings.TrimSpace(t.Find("addr:postcode") + " " + t.Find("addr:city"))
			e := Entry{Kind: "adresse", Name: st + " " + hn, Detail: detail}
			if name != "" && hasAny(t) { // benannte Einrichtung mit Adresse: als Einrichtung, Adresse im Detail
				e = Entry{Kind: "poi", Name: name, Detail: strings.TrimSpace(st + " " + hn + ", " + detail)}
			}
			return e, true
		}
	}
	if name == "" {
		return Entry{}, false
	}
	if p := t.Find("place"); placeKinds[p] {
		return Entry{Kind: "ort", Name: name, Detail: p}, true
	}
	if hasAny(t) {
		return Entry{Kind: "poi", Name: name, Detail: category(t)}, true
	}
	return Entry{}, false
}

func hasAny(t osm.Tags) bool {
	for _, k := range poiKeys {
		if v := t.Find(k); v != "" && v != "no" {
			return true
		}
	}
	return false
}

func category(t osm.Tags) string {
	for _, k := range poiKeys {
		if v := t.Find(k); v != "" && v != "yes" {
			return v
		}
	}
	return ""
}

// placeGrid findet den nächsten Ort (Stadt, Dorf, Stadtteil) für Straßen über ein Gitter aus 0,25°-Zellen.
type placeGrid map[[2]int][]Entry

func newPlaceGrid(ps []Entry) placeGrid {
	g := placeGrid{}
	for _, p := range ps {
		if p.Detail != "city" && p.Detail != "town" && p.Detail != "village" && p.Detail != "suburb" {
			continue
		}
		k := [2]int{int(math.Floor(p.Lat * 4)), int(math.Floor(p.Lon * 4))}
		g[k] = append(g[k], p)
	}
	return g
}

func (g placeGrid) nearest(lat, lon float64) string {
	best, name := math.Inf(1), ""
	r, c := int(math.Floor(lat*4)), int(math.Floor(lon*4))
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			for _, p := range g[[2]int{r + dr, c + dc}] {
				d := (p.Lat-lat)*(p.Lat-lat) + (p.Lon-lon)*(p.Lon-lon)*0.4 // cos²(50°) ≈ 0,41
				if d < best {
					best, name = d, p.Name
				}
			}
		}
	}
	return name
}

// buckets sammelt die Einträge als TSV-Zeilen in einer Datei je Zelle.
type buckets struct {
	dir   string
	files map[int32]*bucket
}

type bucket struct {
	f *os.File
	w *bufio.Writer
}

func (b *buckets) add(e Entry) error {
	e.Lat, e.Lon = math.Round(e.Lat*1e6)/1e6, math.Round(e.Lon*1e6)/1e6 // so steht es in der TSV; Zelle danach
	c := cellOf(e.Lat, e.Lon)
	k := b.files[c]
	if k == nil {
		f, err := os.Create(filepath.Join(b.dir, fmt.Sprintf("zelle-%d.tsv", c)))
		if err != nil {
			return err
		}
		k = &bucket{f, bufio.NewWriterSize(f, 32<<10)}
		b.files[c] = k
	}
	return writeEntry(k.w, e)
}

func (b *buckets) remove() {
	for _, k := range b.files {
		k.f.Close()
		os.Remove(k.f.Name())
	}
}

// merge sortiert jede Zelle (Art, Name, Detail), lässt gleiche Einträge im Umkreis von ~100 m weg (Gebäude und
// Knoten mit derselben Adresse, Grenzgebiete in zwei Ländern) und hängt die Zellen geordnet an w.
func (b *buckets) merge(w io.Writer) (map[string]int, error) {
	cells := make([]int32, 0, len(b.files))
	for c := range b.files {
		cells = append(cells, c)
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i] < cells[j] })
	kinds := map[string]int{}
	bw := bufio.NewWriterSize(w, 1<<20)
	for _, c := range cells {
		k := b.files[c]
		if err := k.w.Flush(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(k.f.Name())
		if err != nil {
			return nil, err
		}
		// nur Zeilennummern sortieren, die Zeilen bleiben in data (statt Entry mit eigenen Strings je Zeile)
		var offs []uint32
		var cs []float64 // Breite, Länge je Zeile
		eachLine(data, func(off int, _, _ string, lat, lon float64) {
			offs = append(offs, uint32(off))
			cs = append(cs, lat, lon)
		})
		line := func(i uint32) []byte {
			l := data[offs[i]:]
			if n := bytes.IndexByte(l, '\n'); n >= 0 {
				l = l[:n]
			}
			return l
		}
		// Art, Name, Detail feldweise vergleichen, ohne die Zeilen zu zerlegen: ein Tab beendet das Feld und
		// zählt damit als kleiner als jedes Zeichen
		cmpKey := func(i, j uint32) int {
			a, b := data[offs[i]:], data[offs[j]:]
			for k, tabs := 0, 0; ; k++ {
				x, y := a[k], b[k]
				if x == y {
					if x == '\t' {
						if tabs++; tabs == 3 {
							return 0
						}
					}
					continue
				}
				switch {
				case x == '\t':
					return -1
				case y == '\t':
					return 1
				case x < y:
					return -1
				}
				return 1
			}
		}
		order := make([]uint32, len(offs))
		for i := range order {
			order[i] = uint32(i)
		}
		slices.SortFunc(order, cmpKey)
		p, first := uint32(0), true
		for _, i := range order {
			if !first {
				if cmpKey(p, i) == 0 && math.Abs(cs[2*p]-cs[2*i]) < 0.001 && math.Abs(cs[2*p+1]-cs[2*i+1]) < 0.0015 {
					continue
				}
			}
			p, first = i, false
			kind, _, _ := bytes.Cut(line(i), []byte{'\t'})
			kinds[string(kind)]++
			bw.Write(line(i))
			bw.WriteByte('\n')
		}
		k.f.Close()
		os.Remove(k.f.Name())
		delete(b.files, c)
	}
	return kinds, bw.Flush()
}

var clean = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

func writeEntry(w *bufio.Writer, e Entry) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Kind, clean.Replace(e.Name), clean.Replace(e.Detail),
		strconv.FormatFloat(e.Lat, 'f', 6, 64), strconv.FormatFloat(e.Lon, 'f', 6, 64))
	return err
}

// Write speichert die Einträge als TSV (Art, Name, Detail, Breite, Länge).
func Write(w io.Writer, es []Entry) error {
	bw := bufio.NewWriter(w)
	for _, e := range es {
		if err := writeEntry(bw, e); err != nil {
			return err
		}
	}
	return bw.Flush()
}

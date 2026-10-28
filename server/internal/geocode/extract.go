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
	"sort"
	"strconv"
	"strings"

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

// ids ist eine aufsteigende Liste von OSM-IDs; has rückt bei aufsteigenden Abfragen (PBF ist nach ID geordnet)
// nur vor, sonst binäre Suche.
type ids struct {
	v    []int64
	p    int
	last int64
}

func (s *ids) finish() {
	sort.Slice(s.v, func(i, j int) bool { return s.v[i] < s.v[j] })
	k := 0
	for i, x := range s.v {
		if i == 0 || x != s.v[k-1] {
			s.v[k] = x
			k++
		}
	}
	s.v = s.v[:k]
}

func (s *ids) index(id int64) int {
	if id < s.last {
		s.p = sort.Search(len(s.v), func(i int) bool { return s.v[i] >= id })
	}
	s.last = id
	for s.p < len(s.v) && s.v[s.p] < id {
		s.p++
	}
	if s.p < len(s.v) && s.v[s.p] == id {
		return s.p
	}
	return -1
}

func (s *ids) find(id int64) int {
	i := sort.Search(len(s.v), func(i int) bool { return s.v[i] >= id })
	if i < len(s.v) && s.v[i] == id {
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
				needWays.v = append(needWays.v, m.Ref)
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
	wayNodes := make([][2]int64, len(needWays.v)) // je gesuchtem Weg (Reihenfolge wie needWays) seine Knoten
	err = scanPBF(ctx, p, false, true, false, func(o osm.Object) error {
		wy := o.(*osm.Way)
		if len(wy.Nodes) == 0 {
			return nil
		}
		n1, n2 := ends(wy.Nodes)
		if i := needWays.index(int64(wy.ID)); i >= 0 {
			wayNodes[i] = [2]int64{n1, n2}
			needNodes.v = append(needNodes.v, n1)
		}
		name, hw, hn := wy.Tags.Find("name"), wy.Tags.Find("highway"), wy.Tags.Find("addr:housenumber")
		if hn == "" && name == "" {
			return nil
		}
		if hw != "" && name != "" && hn == "" {
			if hw == "bus_stop" || hw == "platform" {
				return nil
			}
			needNodes.v = append(needNodes.v, n1)
			fmt.Fprintf(ww, "%d\t%d\tstrasse\t%s\t\n", n1, n2, clean.Replace(name))
			return nil
		}
		es := entries(wy.Tags)
		if len(es) > 0 {
			needNodes.v = append(needNodes.v, n1)
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
	coords := make([]float32, 2*len(needNodes.v))
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
	streets := map[string]*street{}
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
			k := fmt.Sprintf("%s|%d|%d", f[3], int(lat*90), int(lon*60)) // Zelle ~1,2 × 1,2 km
			s := streets[k]
			if s == nil {
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
		e := Entry{Kind: "strasse", Name: k[:strings.Index(k, "|")], Lat: s.lat / float64(s.n), Lon: s.lon / float64(s.n)}
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
		es := parseTSV(data)
		sort.Slice(es, func(i, j int) bool {
			if es[i].Kind != es[j].Kind {
				return es[i].Kind < es[j].Kind
			}
			if es[i].Name != es[j].Name {
				return es[i].Name < es[j].Name
			}
			return es[i].Detail < es[j].Detail
		})
		var p *Entry
		for i := range es {
			e := &es[i]
			if p != nil && p.Kind == e.Kind && p.Name == e.Name && p.Detail == e.Detail && math.Abs(p.Lat-e.Lat) < 0.001 && math.Abs(p.Lon-e.Lon) < 0.0015 {
				continue
			}
			p = e
			kinds[e.Kind]++
			if err := writeEntry(bw, *e); err != nil {
				return nil, err
			}
		}
		k.f.Close()
		os.Remove(k.f.Name())
		delete(b.files, c)
	}
	return kinds, bw.Flush()
}

func parseTSV(data []byte) []Entry {
	var es []Entry
	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n')
		if n < 0 {
			n = len(data)
		}
		f := strings.Split(string(data[:n]), "\t")
		data = data[min(n+1, len(data)):]
		if len(f) != 5 {
			continue
		}
		lat, e1 := strconv.ParseFloat(f[3], 64)
		lon, e2 := strconv.ParseFloat(f[4], 64)
		if e1 == nil && e2 == nil {
			es = append(es, Entry{f[0], f[1], f[2], lat, lon})
		}
	}
	return es
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

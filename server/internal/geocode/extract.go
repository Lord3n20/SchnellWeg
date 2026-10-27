// Package geocode baut aus OSM eine kleine Suchliste (Adressen, Straßen, Orte, benannte Einrichtungen) und
// durchsucht sie im Speicher. Die Suche läuft nur auf dem eigenen Server; Suchtexte werden nie protokolliert.
package geocode

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
)

// Entry ist ein Treffer. Kind: adresse | strasse | ort | poi.
type Entry struct {
	Kind, Name, Detail string
	Lat, Lon           float64
}

// Box ist minLon, minLat, maxLon, maxLat.
type Box [4]float64

func (b Box) has(lon, lat float64) bool {
	return b[0] <= lon && lon <= b[2] && b[1] <= lat && lat <= b[3]
}

// Tags mit Namen, die als Ziel taugen (Wert egal), und ihre Kategorie für die Anzeige.
var poiKeys = []string{"amenity", "shop", "tourism", "leisure", "office", "healthcare", "craft", "historic", "building"}

var placeKinds = map[string]bool{"city": true, "town": true, "village": true, "suburb": true, "quarter": true,
	"neighbourhood": true, "hamlet": true, "borough": true}

// Extract liest eine OSM-PBF-Datei in einem Durchlauf. Knoten in der Box werden sortiert gespeichert (PBF liefert
// Knoten nach ID geordnet vor den Wegen, Wege vor den Relationen), Wege bekommen den Mittelpunkt ihrer Knoten,
// Relationen (Multipolygone wie große Gebäude) den Mittelpunkt ihrer Wege.
func Extract(ctx context.Context, r io.Reader, box Box) ([]Entry, error) {
	var (
		ids    []int64
		coords []float32 // lat, lon je Knoten (float32 reicht: ~1 m)
		wids   []int64   // Wege in der Box, sortiert
		wmid   []float32 // deren Mittelpunkte
		out    []Entry
		places []Entry
	)
	type street struct {
		lat, lon float64
		n        int
	}
	streets := map[string]*street{} // Name + ~1-km-Zelle → Mittel der Wegmitten
	sc := osmpbf.New(ctx, r, 2)
	defer sc.Close()
	for sc.Scan() {
		switch o := sc.Object().(type) {
		case *osm.Node:
			if !box.has(o.Lon, o.Lat) {
				continue
			}
			ids = append(ids, int64(o.ID))
			coords = append(coords, float32(o.Lat), float32(o.Lon))
			if len(o.Tags) > 0 {
				if e, ok := entry(o.Tags, o.Lat, o.Lon); ok {
					if e.Kind == "ort" {
						places = append(places, e)
					}
					out = add(out, e, o.Tags)
				}
			}
		case *osm.Way:
			// Mittelpunkt für jeden Weg, auch ohne Tags: Gebäude-Multipolygone bestehen aus solchen Wegen.
			var lat, lon float64
			n := 0
			for _, wn := range o.Nodes {
				i := sort.Search(len(ids), func(i int) bool { return ids[i] >= int64(wn.ID) })
				if i < len(ids) && ids[i] == int64(wn.ID) {
					lat += float64(coords[2*i])
					lon += float64(coords[2*i+1])
					n++
				}
			}
			if n == 0 {
				continue // außerhalb der Box
			}
			lat, lon = lat/float64(n), lon/float64(n)
			wids = append(wids, int64(o.ID))
			wmid = append(wmid, float32(lat), float32(lon))
			name, hw := o.Tags.Find("name"), o.Tags.Find("highway")
			if o.Tags.Find("addr:housenumber") == "" && name == "" {
				continue
			}
			if hw != "" && name != "" && o.Tags.Find("addr:housenumber") == "" {
				if hw == "bus_stop" || hw == "platform" {
					continue
				}
				k := fmt.Sprintf("%s|%d|%d", name, int(lat*90), int(lon*60)) // Zelle ~1,2 × 1,2 km
				s := streets[k]
				if s == nil {
					s = &street{}
					streets[k] = s
				}
				s.lat, s.lon, s.n = s.lat+lat, s.lon+lon, s.n+1
				continue
			}
			if e, ok := entry(o.Tags, lat, lon); ok {
				out = add(out, e, o.Tags)
			}
		case *osm.Relation:
			if t := o.Tags.Find("type"); t != "multipolygon" && t != "building" {
				continue
			}
			if o.Tags.Find("name") == "" && o.Tags.Find("addr:housenumber") == "" {
				continue
			}
			var lat, lon float64
			n := 0
			for _, m := range o.Members {
				if m.Type != osm.TypeWay || m.Role == "inner" {
					continue
				}
				i := sort.Search(len(wids), func(i int) bool { return wids[i] >= m.Ref })
				if i < len(wids) && wids[i] == m.Ref {
					lat += float64(wmid[2*i])
					lon += float64(wmid[2*i+1])
					n++
				}
			}
			if n == 0 {
				continue
			}
			if e, ok := entry(o.Tags, lat/float64(n), lon/float64(n)); ok {
				out = add(out, e, o.Tags)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for k, s := range streets {
		name := k[:strings.Index(k, "|")]
		e := Entry{Kind: "strasse", Name: name, Lat: s.lat / float64(s.n), Lon: s.lon / float64(s.n)}
		e.Detail = nearestPlace(places, e.Lat, e.Lon)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name+out[i].Detail < out[j].Name+out[j].Detail
	})
	// Gleiche Einträge (Gebäude und Knoten mit derselben Adresse) im Umkreis von ~100 m nur einmal
	dedup := out[:0]
	for _, e := range out {
		if k := len(dedup) - 1; k >= 0 {
			p := dedup[k]
			if p.Kind == e.Kind && p.Name == e.Name && p.Detail == e.Detail && math.Abs(p.Lat-e.Lat) < 0.001 && math.Abs(p.Lon-e.Lon) < 0.0015 {
				continue
			}
		}
		dedup = append(dedup, e)
	}
	return dedup, nil
}

// add hängt e an; bei einer Einrichtung mit Adresse zusätzlich die Adresse selbst („Templergraben 57“).
func add(out []Entry, e Entry, t osm.Tags) []Entry {
	out = append(out, e)
	if e.Kind == "poi" {
		if hn, st := t.Find("addr:housenumber"), t.Find("addr:street"); hn != "" && st != "" {
			out = append(out, Entry{Kind: "adresse", Name: st + " " + hn,
				Detail: strings.TrimSpace(t.Find("addr:postcode") + " " + t.Find("addr:city")), Lat: e.Lat, Lon: e.Lon})
		}
	}
	return out
}

// entry macht aus den Tags eines Knotens oder Wegs einen Eintrag (Adresse, Ort oder Einrichtung).
func entry(t osm.Tags, lat, lon float64) (Entry, bool) {
	name := t.Find("name")
	if hn := t.Find("addr:housenumber"); hn != "" {
		st := t.Find("addr:street")
		if st == "" {
			st = t.Find("addr:place")
		}
		if st != "" {
			detail := strings.TrimSpace(t.Find("addr:postcode") + " " + t.Find("addr:city"))
			e := Entry{Kind: "adresse", Name: st + " " + hn, Detail: detail, Lat: lat, Lon: lon}
			if name != "" && hasAny(t) { // benannte Einrichtung mit Adresse: als Einrichtung, Adresse im Detail
				e = Entry{Kind: "poi", Name: name, Detail: strings.TrimSpace(st + " " + hn + ", " + detail), Lat: lat, Lon: lon}
			}
			return e, true
		}
	}
	if name == "" {
		return Entry{}, false
	}
	if p := t.Find("place"); placeKinds[p] {
		return Entry{Kind: "ort", Name: name, Detail: p, Lat: lat, Lon: lon}, true
	}
	if hasAny(t) {
		return Entry{Kind: "poi", Name: name, Detail: category(t), Lat: lat, Lon: lon}, true
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

func nearestPlace(places []Entry, lat, lon float64) string {
	best, name := math.Inf(1), ""
	for _, p := range places {
		if p.Detail != "city" && p.Detail != "town" && p.Detail != "village" && p.Detail != "suburb" {
			continue
		}
		d := (p.Lat-lat)*(p.Lat-lat) + (p.Lon-lon)*(p.Lon-lon)*0.4 // cos²(50°) ≈ 0,41
		if d < best {
			best, name = d, p.Name
		}
	}
	return name
}

// Write speichert die Einträge als TSV (Art, Name, Detail, Breite, Länge).
func Write(w io.Writer, es []Entry) error {
	bw := bufio.NewWriter(w)
	clean := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	for _, e := range es {
		fmt.Fprintf(bw, "%s\t%s\t%s\t%s\t%s\n", e.Kind, clean.Replace(e.Name), clean.Replace(e.Detail),
			strconv.FormatFloat(e.Lat, 'f', 6, 64), strconv.FormatFloat(e.Lon, 'f', 6, 64))
	}
	return bw.Flush()
}

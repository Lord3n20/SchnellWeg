package jobs

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Box ist ein Rechteck in Grad (minLon, minLat, maxLon, maxLat).
type Box [4]float64

// ParseBox liest "minlon,minlat,maxlon,maxlat".
func ParseBox(s string) (Box, error) {
	var b Box
	f := strings.Split(s, ",")
	if len(f) != 4 {
		return b, errors.New("Box braucht 4 Zahlen")
	}
	for i, x := range f {
		v, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return b, fmt.Errorf("Box: %w", err)
		}
		b[i] = v
	}
	if b[0] >= b[2] || b[1] >= b[3] {
		return b, errors.New("Box: min ≥ max")
	}
	return b, nil
}

func (b Box) has(lon, lat float64) bool {
	return b[0] <= lon && lon <= b[2] && b[1] <= lat && lat <= b[3]
}

// CutStats beschreibt einen GTFS-Auszug.
type CutStats struct{ Trips, Stops, Routes int }

// CutGTFS schreibt nach dst alle Fahrten aus src, die mindestens einen Halt in der Box haben – vollständig, mit
// allen Halten (wie scripts/measure/gtfs_bbox.py). stop_times.txt wird zweimal gestreamt, nichts entpackt.
func CutGTFS(src, dst string, box Box) (CutStats, error) {
	var st CutStats
	z, err := zip.OpenReader(src)
	if err != nil {
		return st, err
	}
	defer z.Close()

	inbox := map[string]bool{}
	if err := scan(z, "stops.txt", func(c cols, r []string) error {
		lat, e1 := strconv.ParseFloat(c.get(r, "stop_lat"), 64)
		lon, e2 := strconv.ParseFloat(c.get(r, "stop_lon"), 64)
		if e1 == nil && e2 == nil && box.has(lon, lat) {
			inbox[strings.Clone(c.get(r, "stop_id"))] = true
		}
		return nil
	}); err != nil {
		return st, err
	}
	trips := map[string]bool{}
	if err := scan(z, "stop_times.txt", func(c cols, r []string) error {
		if inbox[c.get(r, "stop_id")] {
			trips[strings.Clone(c.get(r, "trip_id"))] = true
		}
		return nil
	}); err != nil {
		return st, err
	}
	if len(trips) == 0 {
		return st, errors.New("keine Fahrt in der Box")
	}

	f, err := os.Create(dst)
	if err != nil {
		return st, err
	}
	out := zip.NewWriter(f)
	fail := func(err error) (CutStats, error) {
		out.Close()
		f.Close()
		os.Remove(dst)
		return st, err
	}

	used := map[string]bool{}
	if _, err := filter(z, out, "stop_times.txt", func(c cols, r []string) bool {
		if !trips[c.get(r, "trip_id")] {
			return false
		}
		used[strings.Clone(c.get(r, "stop_id"))] = true
		return true
	}); err != nil {
		return fail(err)
	}
	routes, services := map[string]bool{}, map[string]bool{}
	if st.Trips, err = filter(z, out, "trips.txt", func(c cols, r []string) bool {
		if !trips[c.get(r, "trip_id")] {
			return false
		}
		routes[strings.Clone(c.get(r, "route_id"))] = true
		services[strings.Clone(c.get(r, "service_id"))] = true
		return true
	}); err != nil {
		return fail(err)
	}
	// Übergeordnete Stationen der benutzten Halte mitnehmen
	if err := scan(z, "stops.txt", func(c cols, r []string) error {
		if p := c.get(r, "parent_station"); p != "" && used[c.get(r, "stop_id")] {
			used[strings.Clone(p)] = true
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	if st.Stops, err = filter(z, out, "stops.txt", func(c cols, r []string) bool { return used[c.get(r, "stop_id")] }); err != nil {
		return fail(err)
	}
	agencies := map[string]bool{}
	if st.Routes, err = filter(z, out, "routes.txt", func(c cols, r []string) bool {
		if !routes[c.get(r, "route_id")] {
			return false
		}
		agencies[strings.Clone(c.get(r, "agency_id"))] = true
		return true
	}); err != nil {
		return fail(err)
	}
	keep := []struct {
		name string
		fn   func(cols, []string) bool
	}{
		{"agency.txt", func(c cols, r []string) bool { _, ok := c["agency_id"]; return !ok || agencies[c.get(r, "agency_id")] }},
		{"calendar.txt", func(c cols, r []string) bool { return services[c.get(r, "service_id")] }},
		{"calendar_dates.txt", func(c cols, r []string) bool { return services[c.get(r, "service_id")] }},
	}
	for _, k := range keep {
		if _, err := filter(z, out, k.name, k.fn); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}
	for _, n := range []string{"feed_info.txt", "attributions.txt"} { // Lizenz/Quelle unverändert
		if err := copyEntry(z, out, n); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}
	if err := out.Close(); err != nil {
		return fail(err)
	}
	return st, f.Close()
}

// cols bildet Spaltennamen auf Indizes ab.
type cols map[string]int

func (c cols) get(r []string, name string) string {
	if i, ok := c[name]; ok && i < len(r) {
		return r[i]
	}
	return ""
}

func openCSV(z *zip.ReadCloser, name string) (*csv.Reader, io.Closer, cols, []string, error) {
	f, err := z.Open(name)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cr := csv.NewReader(f)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	h, err := cr.Read()
	if err != nil {
		f.Close()
		return nil, nil, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	h = append([]string(nil), h...)
	if len(h) > 0 {
		h[0] = strings.TrimPrefix(h[0], "\ufeff")
	}
	c := cols{}
	for i, n := range h {
		c[strings.TrimSpace(n)] = i
	}
	return cr, f, c, h, nil
}

func scan(z *zip.ReadCloser, name string, fn func(cols, []string) error) error {
	cr, f, c, _, err := openCSV(z, name)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		r, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := fn(c, r); err != nil {
			return err
		}
	}
}

// filter schreibt die Zeilen von name, für die keep wahr ist, samt Kopfzeile nach out.
func filter(z *zip.ReadCloser, out *zip.Writer, name string, keep func(cols, []string) bool) (int, error) {
	cr, f, c, h, err := openCSV(z, name)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w, err := out.Create(name)
	if err != nil {
		return 0, err
	}
	cw := csv.NewWriter(w)
	cw.Write(h)
	n := 0
	for {
		r, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("%s: %w", name, err)
		}
		if keep(c, r) {
			cw.Write(r)
			n++
		}
	}
	cw.Flush()
	return n, cw.Error()
}

func copyEntry(z *zip.ReadCloser, out *zip.Writer, name string) error {
	f, err := z.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := out.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

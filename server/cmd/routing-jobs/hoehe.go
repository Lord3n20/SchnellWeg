package main

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"

	"github.com/Lord3n20/SchnellWeg/server/internal/jobs"
)

// Höhendaten für die Tiles (D-44): Tilezen/Skadi auf AWS Open Data, einmalig je 1°-Kachel der Region.
const (
	hoeheURL   = "https://elevation-tiles-prod.s3.amazonaws.com/skadi/"
	hgtGroesse = 3601 * 3601 * 2
)

// spanne sucht in beliebig geschachtelten GeoJSON-Koordinaten [w, s, o, n].
func spanne(v any, b *[4]float64) {
	l, ok := v.([]any)
	if !ok {
		return
	}
	if len(l) >= 2 {
		x, okX := l[0].(float64)
		y, okY := l[1].(float64)
		if okX && okY {
			b[0], b[1], b[2], b[3] = min(b[0], x), min(b[1], y), max(b[2], x), max(b[3], y)
			return
		}
	}
	for _, e := range l {
		spanne(e, b)
	}
}

// hgtKacheln liefert die Skadi-Pfade für das Regionspolygon, z. B. N50/N50E006.
func hgtKacheln(geojson []byte) ([]string, error) {
	var g struct {
		Features []struct {
			Geometry struct {
				Coordinates any `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(geojson, &g); err != nil {
		return nil, err
	}
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, f := range g.Features {
		spanne(f.Geometry.Coordinates, &b)
	}
	if b[0] > b[2] {
		return nil, errors.New("Region ohne Koordinaten")
	}
	var out []string
	for lat := int(math.Floor(b[1])); lat <= int(math.Floor(b[3])); lat++ {
		for lon := int(math.Floor(b[0])); lon <= int(math.Floor(b[2])); lon++ {
			ns, ew := 'N', 'E'
			la, lo := lat, lon
			if la < 0 {
				ns, la = 'S', -la
			}
			if lo < 0 {
				ew, lo = 'W', -lo
			}
			name := fmt.Sprintf("%c%02d", ns, la)
			out = append(out, fmt.Sprintf("%s/%s%c%03d", name, name, ew, lo))
		}
	}
	return out, nil
}

// hoeheLaden holt fehlende Kacheln nach ziel und liefert den Ordner. Die Daten ändern sich nicht, vorhandene bleiben.
func hoeheLaden(ctx context.Context, ziel, region string) (string, error) {
	b, err := os.ReadFile(region)
	if err != nil {
		return "", err
	}
	kacheln, err := hgtKacheln(b)
	if err != nil {
		return "", err
	}
	for _, k := range kacheln {
		dst := filepath.Join(ziel, k+".hgt")
		if st, err := os.Stat(dst); err == nil && st.Size() == hgtGroesse {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := hgtHolen(ctx, hoeheURL+k+".hgt.gz", dst); err != nil {
			return "", fmt.Errorf("%s: %w", k, err)
		}
	}
	return ziel, nil
}

func hgtHolen(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", jobs.UserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		return err
	}
	part := dst + ".part"
	defer os.Remove(part)
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, zr)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != hgtGroesse {
		return fmt.Errorf("%d Bytes statt %d", n, hgtGroesse)
	}
	return os.Rename(part, dst)
}

// hoeheOK prüft am Testdienst, dass Valhalla die Höhendaten liest.
func hoeheOK(ctx context.Context) error {
	body := `{"shape":[{"lat":50.7748,"lon":6.0868}],"range":false}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8012/height", strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Höhe: %w", err)
	}
	defer res.Body.Close()
	var r struct {
		Height []*float64 `json:"height"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return fmt.Errorf("Höhe: %w", err)
	}
	if len(r.Height) != 1 || r.Height[0] == nil || *r.Height[0] < 100 || *r.Height[0] > 300 {
		return errors.New("Höhe: keine plausiblen Höhendaten in Aachen")
	}
	return nil
}

// pflasterWeg: Kopfsteinpflaster jeder Art und alles, was in OSM als holprig eingetragen ist (D-44).
func pflasterWeg(t osm.Tags) bool {
	if t.Find("highway") == "" {
		return false
	}
	switch t.Find("surface") {
	case "sett", "cobblestone", "unhewn_cobblestone":
		return true
	}
	switch t.Find("smoothness") {
	case "bad", "very_bad", "horrible", "very_horrible", "impassable":
		return true
	}
	return false
}

// pflasterSchreiben legt die sortierten Way-IDs als uint32 (little endian) ab und liefert ihre Zahl.
func pflasterSchreiben(ctx context.Context, pbf, ziel string) (int, error) {
	f, err := os.Open(pbf)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := osmpbf.New(ctx, f, 2)
	defer sc.Close()
	sc.SkipNodes, sc.SkipRelations = true, true
	var ids []uint32
	for sc.Scan() {
		if w, ok := sc.Object().(*osm.Way); ok && pflasterWeg(w.Tags) {
			if w.ID > math.MaxUint32 {
				return 0, fmt.Errorf("Way-ID %d zu groß (renumber fehlt?)", w.ID)
			}
			ids = append(ids, uint32(w.ID))
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	if len(ids) < 100 {
		return 0, fmt.Errorf("Prüfung: nur %d Pflaster-Wege", len(ids))
	}
	slices.Sort(ids)
	b := make([]byte, 4*len(ids))
	for i, x := range ids {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	return len(ids), os.WriteFile(ziel, b, 0o644)
}

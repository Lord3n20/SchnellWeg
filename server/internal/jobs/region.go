package jobs

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PolyFromGeoJSON wandelt das erste Polygon einer GeoJSON-FeatureCollection (config/region.geojson) in das
// .poly-Format von osmconvert um (-B=datei.poly).
func PolyFromGeoJSON(r io.Reader) (string, error) {
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates [][][2]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.NewDecoder(r).Decode(&fc); err != nil {
		return "", err
	}
	if len(fc.Features) == 0 || fc.Features[0].Geometry.Type != "Polygon" || len(fc.Features[0].Geometry.Coordinates) == 0 {
		return "", fmt.Errorf("kein Polygon gefunden")
	}
	var b strings.Builder
	b.WriteString("region\n")
	for i, ring := range fc.Features[0].Geometry.Coordinates {
		if len(ring) < 4 {
			return "", fmt.Errorf("Ring %d hat zu wenige Punkte", i)
		}
		if i == 0 {
			b.WriteString("1\n")
		} else {
			fmt.Fprintf(&b, "!%d\n", i+1) // Loch
		}
		for _, p := range ring {
			fmt.Fprintf(&b, "  %.6f  %.6f\n", p[0], p[1])
		}
		b.WriteString("END\n")
	}
	b.WriteString("END\n")
	return b.String(), nil
}

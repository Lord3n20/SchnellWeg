package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RWTHBuildings liest die Gebäudeliste der RWTH Maps (inoffizielle Schnittstelle, D-18): ein JSON-Array in
// ISO-8859-1 mit gno (Gebäudenummer), bez („SuperC, studienfunktionales Zentrum [1040]“), adr, lat, lng.
// lat/lng kommen mal als Zahl, mal als Text. Einträge ohne Koordinate fallen weg.
func RWTHBuildings(ctx context.Context, c *http.Client, url, ua string) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return parseRWTH(latin1(raw))
}

// latin1 wandelt ISO-8859-1 in UTF-8 (jedes Byte ist ein Zeichen U+0000–U+00FF).
func latin1(b []byte) io.Reader {
	out := make([]byte, 0, len(b)+len(b)/16)
	for _, c := range b {
		out = utf8.AppendRune(out, rune(c))
	}
	return strings.NewReader(string(out))
}

type rwthBuilding struct {
	GNo string          `json:"gno"`
	Bez string          `json:"bez"`
	Adr string          `json:"adr"`
	Lat json.RawMessage `json:"lat"`
	Lng json.RawMessage `json:"lng"`
}

func parseRWTH(r io.Reader) ([]Entry, error) {
	var bs []rwthBuilding
	if err := json.NewDecoder(r).Decode(&bs); err != nil {
		return nil, fmt.Errorf("JSON: %w", err)
	}
	out := make([]Entry, 0, len(bs))
	for _, b := range bs {
		lat, ok1 := rawFloat(b.Lat)
		lon, ok2 := rawFloat(b.Lng)
		if !ok1 || !ok2 || b.Bez == "" || lat == 0 || lon == 0 {
			continue
		}
		name := strings.TrimSpace(b.Bez)
		if b.GNo != "" && !strings.Contains(name, "["+b.GNo+"]") {
			name += " [" + b.GNo + "]"
		}
		out = append(out, Entry{Kind: "gebaeude", Name: name, Detail: strings.TrimSpace(b.Adr), Lat: lat, Lon: lon})
	}
	return out, nil
}

func rawFloat(m json.RawMessage) (float64, bool) {
	s := strings.Trim(strings.TrimSpace(string(m)), `"`)
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

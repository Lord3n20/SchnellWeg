package api

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
	"unicode/utf8"
)

// Karte (D-20): MapDir zeigt auf map/current des Kartenjobs (Glob erlaubt, wird je Anfrage aufgelöst, damit
// ein neuer Stand oder die erste Karte ohne Neustart greift). regionen.json listet Übersicht + Bundesländer.

type mapRegion struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Box   string    `json:"box"`
	Zoom  int       `json:"zoom"`
	Bytes int64     `json:"bytes"`
	Stand time.Time `json:"stand"`
}

func (s *Server) mapDir() (string, []mapRegion, bool) {
	if s.MapDir == "" {
		return "", nil, false
	}
	m, _ := filepath.Glob(s.MapDir)
	if len(m) == 0 {
		return "", nil, false
	}
	b, err := os.ReadFile(filepath.Join(m[0], "regionen.json"))
	var regs []mapRegion
	if err != nil || json.Unmarshal(b, &regs) != nil {
		return "", nil, false
	}
	return m[0], regs, true
}

// mapIndex listet die Kartendateien mit Größe und Stand; die App lädt davon, was sie braucht.
func (s *Server) mapIndex(w http.ResponseWriter, r *http.Request) {
	dir, regs, ok := s.mapDir()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "karte_fehlt")
		return
	}
	out := regs[:0]
	for _, g := range regs {
		if st, err := os.Stat(filepath.Join(dir, g.ID+".pmtiles")); err == nil {
			g.Bytes, g.Stand = st.Size(), st.ModTime().UTC().Truncate(time.Second)
			out = append(out, g)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]any{"regions": out})
}

// mapFile liefert eine Kartendatei (PMTiles) mit Range-Anfragen und Last-Modified, damit die App sie nur
// bei Änderung neu lädt und abgebrochene Downloads fortsetzen kann.
func (s *Server) mapFile(w http.ResponseWriter, r *http.Request) {
	dir, regs, ok := s.mapDir()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "karte_fehlt")
		return
	}
	id := r.PathValue("id")
	if !slices.ContainsFunc(regs, func(g mapRegion) bool { return g.ID == id }) {
		fail(w, http.StatusNotFound, "region_unbekannt")
		return
	}
	f, err := os.Open(filepath.Join(dir, id+".pmtiles"))
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "karte_fehlt")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "karte_fehlt")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "no-cache") // immer mit If-Modified-Since nachfragen
	http.ServeContent(w, r, id+".pmtiles", st.ModTime(), f)
}

type searchReq struct {
	Q string `json:"q"`
	N int    `json:"n"` // höchstens 20, Standard 10
	// Near: ungefährer Standort [Breite, Länge] (App rundet auf 0,05°), nahe Treffer zuerst; fehlt bei alten Apps.
	// Wird nur für die Wertung genutzt, nie protokolliert.
	Near []float64 `json:"near,omitempty"`
}

type searchHit struct {
	Kind   string  `json:"kind"` // adresse | strasse | ort | poi
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// search sucht Adressen und Orte. Der Suchtext kommt im Body (nie in der URL) und wird nicht protokolliert;
// sortiert wird ohne Standort, die App ordnet bei Bedarf nach Entfernung.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	ix := s.Geocode
	if ix == nil {
		fail(w, http.StatusServiceUnavailable, "suche_aus")
		return
	}
	var req searchReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || utf8.RuneCountInString(req.Q) > 100 || req.N < 0 || req.N > 20 ||
		len(req.Near) != 0 && (len(req.Near) != 2 || math.Abs(req.Near[0]) > 90 || math.Abs(req.Near[1]) > 180) {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	if req.N == 0 {
		req.N = 10
	}
	t0 := time.Now()
	lat, lon := math.NaN(), math.NaN()
	if len(req.Near) == 2 {
		lat, lon = req.Near[0], req.Near[1]
	}
	hs := ix.SearchNear(req.Q, req.N, lat, lon)
	out := make([]searchHit, len(hs))
	for i, h := range hs {
		out[i] = searchHit{h.Kind, h.Name, h.Detail, h.Lat, h.Lon}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"hits": out, "ms": float64(time.Since(t0).Microseconds()) / 1000})
}

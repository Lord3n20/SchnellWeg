package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
	"unicode/utf8"
)

// mapFile liefert den Kartenausschnitt (PMTiles) mit Range-Anfragen und Last-Modified, damit die App ihn nur
// bei Änderung neu lädt. Der Pfad zeigt über current/ auf den neuesten Stand, ohne Neustart.
func (s *Server) mapFile(w http.ResponseWriter, r *http.Request) {
	if s.MapPath == "" {
		fail(w, http.StatusServiceUnavailable, "karte_aus")
		return
	}
	f, err := os.Open(s.MapPath)
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
	http.ServeContent(w, r, "region.pmtiles", st.ModTime(), f)
}

type searchReq struct {
	Q string `json:"q"`
	N int    `json:"n"` // höchstens 20, Standard 10
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
	if err := dec.Decode(&req); err != nil || utf8.RuneCountInString(req.Q) > 100 || req.N < 0 || req.N > 20 {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	if req.N == 0 {
		req.N = 10
	}
	t0 := time.Now()
	hs := ix.Search(req.Q, req.N)
	out := make([]searchHit, len(hs))
	for i, h := range hs {
		out[i] = searchHit{h.Kind, h.Name, h.Detail, h.Lat, h.Lon}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"hits": out, "ms": float64(time.Since(t0).Microseconds()) / 1000})
}

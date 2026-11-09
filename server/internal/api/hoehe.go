package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var hoeheName = regexp.MustCompile(`^[NS][0-9]{2}[EW][0-9]{3}$`)

// hoehe liefert eine 1°-Höhenkachel an die App (D-44). Die App wertet ihren Messgang damit selbst aus, die GPS-Spur
// verlässt das Handy nicht; deshalb ist die ganze Kachel nötig und nicht nur einzelne Höhen.
func (s *Server) hoehe(w http.ResponseWriter, r *http.Request) {
	k := r.PathValue("kachel")
	treffer, _ := filepath.Glob(s.HoeheDir)
	if !hoeheName.MatchString(k) || s.HoeheDir == "" || len(treffer) == 0 {
		fail(w, http.StatusNotFound, "hoehe_unbekannt")
		return
	}
	f, err := os.Open(filepath.Join(treffer[0], k[:3], k+".hgt"))
	if err != nil {
		fail(w, http.StatusNotFound, "hoehe_unbekannt")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "gzip")
	zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if _, err := io.Copy(zw, f); err == nil {
		zw.Close()
	}
}

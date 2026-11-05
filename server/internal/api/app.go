package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Selbstupdate der App (D-42): AppDir enthält version.json und schnellweg-<code>.apk (scripts/app-release.sh legt
// erst die APK, dann version.json ab). Wie alles hier nur mit Client-Zertifikat erreichbar, nichts nach außen.
type appStand struct {
	Version string `json:"version"`
	Code    int    `json:"code"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
}

func (s *Server) appDatei() (*os.File, appStand, bool) {
	var a appStand
	if s.AppDir == "" {
		return nil, a, false
	}
	b, err := os.ReadFile(filepath.Join(s.AppDir, "version.json"))
	if err != nil || json.Unmarshal(b, &a) != nil || a.Code <= 0 || len(a.SHA256) != 64 {
		return nil, a, false
	}
	f, err := os.Open(filepath.Join(s.AppDir, fmt.Sprintf("schnellweg-%d.apk", a.Code)))
	if err != nil {
		return nil, a, false
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, a, false
	}
	a.Bytes = st.Size()
	return f, a, true
}

// appIndex nennt die aktuelle App-Fassung; die App vergleicht Code mit ihrem eigenen versionCode.
func (s *Server) appIndex(w http.ResponseWriter, r *http.Request) {
	f, a, ok := s.appDatei()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "app_fehlt")
		return
	}
	f.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(a)
}

// appFile liefert die APK mit Range-Anfragen, damit ein abgebrochener Download weiterläuft.
func (s *Server) appFile(w http.ResponseWriter, r *http.Request) {
	f, a, ok := s.appDatei()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "app_fehlt")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "app_fehlt")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, fmt.Sprintf("schnellweg-%d.apk", a.Code), st.ModTime(), f)
}

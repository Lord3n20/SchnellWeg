package jobs

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Geofabrik leitet germany-latest.osm.pbf auf einen Spiegel um und hat selbst keine .md5 (28.10.).
func TestFetchMirrorMD5(t *testing.T) {
	data := []byte("pbf-daten")
	mux := http.NewServeMux()
	mux.HandleFunc("/land-latest.osm.pbf", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/spiegel/land-latest.osm.pbf", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/spiegel/land-latest.osm.pbf", func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	mux.HandleFunc("/spiegel/land-latest.osm.pbf.md5", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%x  land-261027.osm.pbf\n", md5.Sum(data))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "land.osm.pbf")
	u := srv.URL + "/land-latest.osm.pbf"
	if ch, err := Fetch(context.Background(), u, u+".md5", dst); err != nil || !ch {
		t.Fatalf("changed=%v, %v", ch, err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(data) {
		t.Fatalf("Inhalt %q", b)
	}
}

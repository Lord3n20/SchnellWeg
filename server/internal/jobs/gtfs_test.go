package jobs

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for n, s := range files {
		e, _ := w.Create(n)
		e.Write([]byte(s))
	}
	w.Close()
	f.Close()
}

func readEntry(t *testing.T, path, name string) string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	f, err := z.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

func TestCutGTFS(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "in.zip"), filepath.Join(dir, "out.zip")
	// Fahrt a hat einen Halt in der Box (s1) und einen außerhalb (s2): beide bleiben. Fahrt b liegt ganz außerhalb.
	writeZip(t, src, map[string]string{
		"stops.txt":          "\ufeffstop_id,stop_name,stop_lat,stop_lon,parent_station\ns1,In,50.77,6.09,p1\ns2,Weit,51.5,7.5,\ns3,Weg,52,8,\np1,Station,50.77,6.09,\n",
		"stop_times.txt":     "trip_id,arrival_time,departure_time,stop_id,stop_sequence\na,08:00:00,08:00:00,s1,1\na,09:00:00,09:00:00,s2,2\nb,08:00:00,08:00:00,s3,1\n",
		"trips.txt":          "route_id,service_id,trip_id\nr1,w,a\nr2,x,b\n",
		"routes.txt":         "route_id,agency_id,route_short_name,route_type\nr1,ag1,\"3A, Ring\",3\nr2,ag2,9,3\n",
		"agency.txt":         "agency_id,agency_name\nag1,Eins\nag2,Zwei\n",
		"calendar_dates.txt": "service_id,date,exception_type\nw,20261028,1\nx,20261028,1\n",
		"feed_info.txt":      "feed_publisher_name\nTest\n",
	})
	box, err := ParseBox("5.95,50.48,6.45,50.93")
	if err != nil {
		t.Fatal(err)
	}
	st, err := CutGTFS(src, dst, box)
	if err != nil {
		t.Fatal(err)
	}
	if st != (CutStats{Trips: 1, Stops: 3, Routes: 1}) {
		t.Fatalf("Kennzahlen %+v", st)
	}
	if s := readEntry(t, dst, "stops.txt"); strings.Contains(s, "s3") || !strings.Contains(s, "p1") || strings.HasPrefix(s, "\ufeff") {
		t.Fatalf("stops.txt:\n%s", s)
	}
	if s := readEntry(t, dst, "routes.txt"); !strings.Contains(s, `"3A, Ring"`) {
		t.Fatalf("routes.txt ohne Anführungszeichen:\n%s", s)
	}
	if s := readEntry(t, dst, "agency.txt"); strings.Contains(s, "Zwei") {
		t.Fatalf("agency.txt:\n%s", s)
	}
	if s := readEntry(t, dst, "feed_info.txt"); s != "feed_publisher_name\nTest\n" {
		t.Fatalf("feed_info.txt:\n%s", s)
	}
	if _, err := CutGTFS(src, dst, Box{0, 0, 1, 1}); err == nil {
		t.Fatal("leere Box ohne Fehler")
	}
}

// Mit echten Daten (lokal): RWAYSTH_DE_NV=../data/raw/de_nv.zip go test ./internal/jobs -run Real -v
func TestCutGTFSReal(t *testing.T) {
	src := os.Getenv("RWAYSTH_DE_NV")
	if src == "" {
		t.Skip("RWAYSTH_DE_NV nicht gesetzt")
	}
	box, _ := ParseBox("5.95,50.48,6.45,50.93")
	st, err := CutGTFS(src, filepath.Join(t.TempDir(), "sr.zip"), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", st)
}

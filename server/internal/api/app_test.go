package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAppUpdate(t *testing.T) {
	s := &Server{}
	get := func(pfad string, h map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, pfad, nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if c := get("/v1/app", nil).Code; c != 503 {
		t.Fatalf("ohne Ordner: %d", c)
	}
	d := t.TempDir()
	s.AppDir = d
	if c := get("/v1/app", nil).Code; c != 503 {
		t.Fatalf("ohne version.json: %d", c)
	}
	sha := strings.Repeat("a", 64)
	os.WriteFile(d+"/version.json", []byte(`{"version":"0.2.0","code":2,"sha256":"`+sha+`"}`), 0o644)
	if c := get("/v1/app/apk", nil).Code; c != 503 {
		t.Fatalf("ohne APK: %d", c)
	}
	os.WriteFile(d+"/schnellweg-2.apk", []byte("APK-Testinhalt"), 0o644)
	w := get("/v1/app", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":2`) || !strings.Contains(w.Body.String(), `"bytes":14`) {
		t.Fatalf("Stand %d %s", w.Code, w.Body.String())
	}
	if w := get("/v1/app/apk", nil); w.Code != 200 || w.Body.String() != "APK-Testinhalt" {
		t.Fatalf("APK %d", w.Code)
	}
	if w := get("/v1/app/apk", map[string]string{"Range": "bytes=4-"}); w.Code != 206 || w.Body.String() != "Testinhalt" {
		t.Fatalf("Range %d %q", w.Code, w.Body.String())
	}
	for _, kaputt := range []string{`{`, `{"code":0,"sha256":"` + sha + `"}`, `{"code":2,"sha256":"kurz"}`} {
		os.WriteFile(d+"/version.json", []byte(kaputt), 0o644)
		if c := get("/v1/app", nil).Code; c != 503 {
			t.Fatalf("%s: %d", kaputt, c)
		}
	}
}

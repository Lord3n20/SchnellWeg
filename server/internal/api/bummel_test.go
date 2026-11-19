package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBummel(t *testing.T) {
	s := &Server{}
	req := func(m, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(m, "/v1/bummel", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if c := req("GET", "").Code; c != 503 {
		t.Fatalf("aus: %d", c)
	}
	pfad := t.TempDir() + "/b/bummel.json"
	s.Bummel = &Bummel{Pfad: pfad}
	a, b := strings.Repeat("a", 16), strings.Repeat("b", 16)
	for _, x := range []string{
		`{"geraet":"` + a + `","haus":"Corps X","gast":"ADB","rein":true,"presslust":8,"lr":3}`,
		`{"geraet":"` + b + `","haus":"Corps X","gast":"CV","rein":false,"presslust":4}`,
		`{"geraet":"` + a + `","haus":"Corps X","gast":"ADB","rein":true,"presslust":6,"lr":3}`, // ersetzt die erste
	} {
		if w := req("POST", x); w.Code != 200 {
			t.Fatalf("bewerten %d %s", w.Code, w.Body.String())
		}
	}
	for _, x := range []string{
		`{"geraet":"kurz","haus":"Corps X","presslust":5}`,
		`{"geraet":"` + a + `","haus":"Corps X","presslust":11}`,
		`{"geraet":"` + a + `","haus":"","presslust":5}`,
		`{"geraet":"` + a + `","haus":"Corps X","lat":50.7}`,
	} {
		if c := req("POST", x).Code; c != 400 {
			t.Fatalf("ungültig angenommen (%d): %s", c, x)
		}
	}
	// Neu laden aus der Datei
	s.Bummel = &Bummel{Pfad: pfad}
	got := req("GET", "").Body.String()
	for _, want := range []string{`"ADB":[1,1]`, `"CV":[0,1]`, `"presslust":[10,2]`, `"lr":[3,1]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("fehlt %s in %s", want, got)
		}
	}
	if strings.Contains(got, a) {
		t.Fatal("Gerätekennung darf nicht ausgeliefert werden")
	}
	// Leere Stimme löscht
	req("POST", `{"geraet":"`+a+`","haus":"Corps X"}`)
	req("POST", `{"geraet":"`+b+`","haus":"Corps X"}`)
	if got := req("GET", "").Body.String(); strings.Contains(got, "Corps X") {
		t.Fatalf("nicht gelöscht: %s", got)
	}
}

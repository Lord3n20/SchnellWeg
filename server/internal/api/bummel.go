package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

// Bummelrunde (D-51): Bewertungen der Studentenverbindungen, geteilt zwischen allen Geräten. Je Gerät (zufällige
// Kennung der App, kein Bezug zur Person) und Haus zählt eine Stimme, eine neue ersetzt die alte. Gespeichert wird
// nur, was bewertet wurde – kein Standort, keine Zeit des Besuchs. Ausgeliefert werden nur Summen.
type Bummel struct {
	Pfad string // JSON-Datei, "" = aus

	mu      sync.Mutex
	geladen bool
	stimmen map[string]map[string]bummelStimme // Haus → Gerät → Stimme
}

type bummelStimme struct {
	Gast        string `json:"gast"` // Dachverband des Gasts, "" = keiner
	Rein        *bool  `json:"rein,omitempty"`
	Presslust   *int   `json:"presslust,omitempty"`
	Fechten     *int   `json:"fechten,omitempty"`
	LinksRechts *int   `json:"lr,omitempty"`
	LibKons     *int   `json:"lk,omitempty"`
	Tag         string `json:"tag"` // Datum der Stimme (für spätere Alterung), ohne Uhrzeit
}

type bummelReq struct {
	Geraet string `json:"geraet"`
	Haus   string `json:"haus"`
	bummelStimme
}

const (
	bummelMaxHaeuser = 5000
	bummelMaxGeraete = 2000 // je Haus
)

var bummelGeraet = regexp.MustCompile(`^[a-z0-9]{16,64}$`)

// Summe ist ein Paar aus Summe und Anzahl.
type Summe [2]int

type bummelHaus struct {
	Rein        map[string]Summe `json:"rein,omitempty"` // Gast-Dachverband → [reingekommen, Stimmen]
	Presslust   *Summe           `json:"presslust,omitempty"`
	Fechten     *Summe           `json:"fechten,omitempty"`
	LinksRechts *Summe           `json:"lr,omitempty"`
	LibKons     *Summe           `json:"lk,omitempty"`
}

func (b *Bummel) laden() error {
	if b.geladen {
		return nil
	}
	b.stimmen = map[string]map[string]bummelStimme{}
	d, err := os.ReadFile(b.Pfad)
	if err == nil {
		if err := json.Unmarshal(d, &b.stimmen); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	b.geladen = true
	return nil
}

func (b *Bummel) speichern() error {
	d, err := json.Marshal(b.stimmen)
	if err != nil {
		return err
	}
	tmp := b.Pfad + ".neu"
	if err := os.MkdirAll(filepath.Dir(b.Pfad), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, d, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.Pfad)
}

func skala(p *int, min int) bool { return p == nil || (*p >= min && *p <= 10) }

func addiere(s **Summe, p *int) {
	if p == nil {
		return
	}
	if *s == nil {
		*s = &Summe{}
	}
	(*s)[0] += *p
	(*s)[1]++
}

// Summen liefert je Haus die Summen aller Stimmen.
func (b *Bummel) Summen() (map[string]*bummelHaus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.laden(); err != nil {
		return nil, err
	}
	out := make(map[string]*bummelHaus, len(b.stimmen))
	for haus, geraete := range b.stimmen {
		h := &bummelHaus{}
		for _, st := range geraete {
			if st.Rein != nil {
				if h.Rein == nil {
					h.Rein = map[string]Summe{}
				}
				s := h.Rein[st.Gast]
				if *st.Rein {
					s[0]++
				}
				s[1]++
				h.Rein[st.Gast] = s
			}
			addiere(&h.Presslust, st.Presslust)
			addiere(&h.Fechten, st.Fechten)
			addiere(&h.LinksRechts, st.LinksRechts)
			addiere(&h.LibKons, st.LibKons)
		}
		out[haus] = h
	}
	return out, nil
}

// Setzen ersetzt die Stimme des Geräts für das Haus; eine Stimme ohne Werte löscht sie.
func (b *Bummel) Setzen(geraet, haus string, st bummelStimme) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.laden(); err != nil {
		return err
	}
	leer := st.Rein == nil && st.Presslust == nil && st.Fechten == nil && st.LinksRechts == nil && st.LibKons == nil
	g := b.stimmen[haus]
	if leer {
		if g == nil {
			return nil
		}
		delete(g, geraet)
		if len(g) == 0 {
			delete(b.stimmen, haus)
		}
		return b.speichern()
	}
	if g == nil {
		if len(b.stimmen) >= bummelMaxHaeuser {
			return errVoll
		}
		g = map[string]bummelStimme{}
		b.stimmen[haus] = g
	}
	if _, da := g[geraet]; !da && len(g) >= bummelMaxGeraete {
		return errVoll
	}
	st.Tag = time.Now().In(Berlin).Format("2006-01-02")
	g[geraet] = st
	return b.speichern()
}

type fehlerText string

func (f fehlerText) Error() string { return string(f) }

const errVoll = fehlerText("voll")

func (s *Server) bummelLesen(w http.ResponseWriter, r *http.Request) {
	if s.Bummel == nil || s.Bummel.Pfad == "" {
		fail(w, http.StatusServiceUnavailable, "bummel_aus")
		return
	}
	h, err := s.Bummel.Summen()
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "bummel_fehler")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]any{"haeuser": h})
}

func (s *Server) bummelBewerten(w http.ResponseWriter, r *http.Request) {
	if s.Bummel == nil || s.Bummel.Pfad == "" {
		fail(w, http.StatusServiceUnavailable, "bummel_aus")
		return
	}
	var q bummelReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&q); err != nil || !bummelGeraet.MatchString(q.Geraet) || q.Haus == "" ||
		utf8.RuneCountInString(q.Haus) > 120 || utf8.RuneCountInString(q.Gast) > 40 ||
		!skala(q.Presslust, 1) || !skala(q.Fechten, 1) || !skala(q.LinksRechts, 1) || !skala(q.LibKons, 1) {
		fail(w, http.StatusBadRequest, "anfrage_ungueltig")
		return
	}
	if err := s.Bummel.Setzen(q.Geraet, q.Haus, q.bummelStimme); err != nil {
		if err == errVoll {
			fail(w, http.StatusInsufficientStorage, "bummel_voll")
		} else {
			fail(w, http.StatusServiceUnavailable, "bummel_fehler")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Supervisor spricht mit der Supervisor-API des Add-ons (SUPERVISOR_TOKEN). Ohne Token (lokaler Test)
// werden Aufrufe übersprungen.
type Supervisor struct {
	Base  string // http://supervisor
	Token string
}

func NewSupervisor() *Supervisor {
	return &Supervisor{Base: "http://supervisor", Token: os.Getenv("SUPERVISOR_TOKEN")}
}

func (s *Supervisor) call(ctx context.Context, method, path string, body, out any) error {
	if s.Token == "" {
		return nil
	}
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Notify legt eine HA-Benachrichtigung an (gleiche id überschreibt die vorige).
func (s *Supervisor) Notify(ctx context.Context, id, title, msg string) error {
	return s.call(ctx, http.MethodPost, "/core/api/services/persistent_notification/create",
		map[string]string{"notification_id": id, "title": title, "message": msg}, nil)
}

// SiblingSlug liefert den Slug eines Add-ons aus demselben Repository (gleicher Hash-Präfix wie dieses).
func (s *Supervisor) SiblingSlug(ctx context.Context, name string) (string, error) {
	var r struct {
		Data struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := s.call(ctx, http.MethodGet, "/addons/self/info", nil, &r); err != nil {
		return "", err
	}
	for i, c := range r.Data.Slug {
		if c == '_' {
			return r.Data.Slug[:i+1] + name, nil
		}
	}
	return name, nil
}

// RestartIfRunning startet ein anderes Add-on neu, wenn es läuft (damit es den neuen Stand lädt).
func (s *Supervisor) RestartIfRunning(ctx context.Context, slug string) (bool, error) {
	if s.Token == "" {
		return false, nil
	}
	var r struct {
		Data struct {
			State string `json:"state"`
		} `json:"data"`
	}
	if err := s.call(ctx, http.MethodGet, "/addons/"+slug+"/info", nil, &r); err != nil {
		return false, err
	}
	if r.Data.State != "started" {
		return false, nil
	}
	return true, s.call(ctx, http.MethodPost, "/addons/"+slug+"/restart", nil, nil)
}

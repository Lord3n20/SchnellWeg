package jobs

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// UserAgent für alle Downloads (ohne Kontaktangabe, siehe F-05).
const UserAgent = "SchnellWeg-jobs (+https://github.com/Lord3n20/SchnellWeg)"

var client = &http.Client{Timeout: 30 * time.Minute}

// Fetch lädt url nach dst, wenn sich die Datei seit dem letzten Abruf geändert hat (ETag in dst.etag).
// changed=false heißt: dst ist unverändert und vorhanden. Geschrieben wird erst in dst.part, dann umbenannt.
// Ist md5url gesetzt (Geofabrik), wird die Prüfsumme vor dem Umbenennen geprüft.
func Fetch(ctx context.Context, url, md5url, dst string) (changed bool, err error) {
	etagFile := dst + ".etag"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if _, err := os.Stat(dst); err == nil {
		if et, err := os.ReadFile(etagFile); err == nil && len(et) > 0 {
			req.Header.Set("If-None-Match", strings.TrimSpace(string(et)))
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return false, err
	}
	h := md5.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		return false, err
	}
	if md5url != "" {
		want, err := fetchMD5(ctx, md5url)
		if err != nil {
			os.Remove(part)
			return false, fmt.Errorf("Prüfsumme: %w", err)
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != want {
			os.Remove(part)
			return false, fmt.Errorf("Prüfsumme falsch")
		}
	}
	if err := os.Rename(part, dst); err != nil {
		return false, err
	}
	os.WriteFile(etagFile, []byte(resp.Header.Get("ETag")), 0o644)
	return true, nil
}

func fetchMD5(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || len(f[0]) != 32 {
		return "", fmt.Errorf("unerwartetes Format")
	}
	return strings.ToLower(f[0]), nil
}

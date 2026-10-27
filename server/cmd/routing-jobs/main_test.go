package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// checkValhalla muss zurückkehren, auch wenn der Dienst von selbst nie endet (Fehler in 0.1.1: Wait vor cancel).
func TestCheckValhallaBeendetDienst(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "valhalla_service"), []byte("#!/bin/sh\nexec sleep 1000\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ln, err := net.Listen("tcp", "127.0.0.1:8012")
	if err != nil {
		t.Skip("Port 8012 belegt")
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"trip":{"status":0,"summary":{"length":1.5}}}`))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	done := make(chan error, 1)
	go func() { done <- checkValhalla(context.Background(), "c.json", filepath.Join(t.TempDir(), "bau.log")) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checkValhalla hängt nach erfolgreicher Prüfung")
	}
}

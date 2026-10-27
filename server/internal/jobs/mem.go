package jobs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// MemAvailableMB liest MemAvailable aus /proc/meminfo (Host-Sicht, auch im Container).
func MemAvailableMB() (int64, error) {
	return meminfo("/proc/meminfo", "MemAvailable")
}

func meminfo(path, key string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), ":")
		if ok && k == key {
			kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			return kb >> 10, err
		}
	}
	return 0, fmt.Errorf("%s fehlt in %s", key, path)
}

// Budget ist die erlaubte Speicherspitze eines Jobs: höchstens maxMB und höchstens MemAvailable − 1 GB.
func Budget(maxMB, availMB int64) int64 {
	return min(maxMB, availMB-1024)
}

// Watch misst den anonymen Speicher der eigenen cgroup (alle Prozesse im Container, ohne Page Cache)
// und ruft over auf, sobald er limitMB übersteigt. Peak liefert die bisherige Spitze.
type Watch struct {
	peak atomic.Int64
	stat string
}

// NewWatch startet die Messung alle 250 ms bis ctx endet.
func NewWatch(ctx context.Context, limitMB int64, over func(usedMB int64)) *Watch {
	w := &Watch{stat: "/sys/fs/cgroup/memory.stat"}
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		fired := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			mb := w.sample()
			if mb > limitMB && !fired {
				fired = true
				over(mb)
			}
		}
	}()
	return w
}

func (w *Watch) sample() int64 {
	mb, err := cgroupAnon(w.stat)
	if err != nil {
		mb, _ = meminfo("/proc/self/status", "VmRSS") // ohne cgroup v2 wenigstens der eigene Prozess
	}
	for {
		p := w.peak.Load()
		if mb <= p || w.peak.CompareAndSwap(p, mb) {
			return mb
		}
	}
}

// Sample misst sofort einmal (z. B. direkt nach einem Teilschritt).
func (w *Watch) Sample() int64 { return w.sample() }

// PeakMB ist die höchste gemessene anonyme Belegung.
func (w *Watch) PeakMB() int64 { return w.peak.Load() }

func cgroupAnon(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), " ")
		if ok && k == "anon" {
			b, err := strconv.ParseInt(v, 10, 64)
			return b >> 20, err
		}
	}
	return 0, fmt.Errorf("anon fehlt in %s", path)
}

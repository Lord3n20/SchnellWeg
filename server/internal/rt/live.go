package rt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	gtfs "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
)

// TripKey bezeichnet eine Fahrt an einem Betriebstag (trip_id aus dem Feed ohne Präfix, start_date JJJJMMTT).
type TripKey struct{ ID, Date string }

// StopUpd ist die Prognose für einen Halt einer Fahrt. Verspätungen gelten laut GTFS-RT auch für die folgenden
// Halte, bis die nächste Angabe kommt.
type StopUpd struct {
	StopID           string
	ArrDelay, DepDel int32
	HasArr, HasDep   bool
	Skipped          bool
}

// Trip ist der Echtzeitstand einer Fahrt.
type Trip struct {
	Canceled bool
	Upd      []StopUpd
}

// Snapshot ist ein Abruf des Feeds, gefiltert auf die Region. Nie verändert, nachdem er veröffentlicht wurde.
type Snapshot struct {
	Fetched  time.Time
	FeedTime time.Time
	Trips    map[TripKey]*Trip
	Stats    Stats
}

// Live holt den Feed nur, wenn jemand fragt (D-05: keine Dauerlast ohne offene App), höchstens alle MinAge,
// bedingt mit If-None-Match. Ein Abruf dient allen gleichzeitigen Anfragen.
type Live struct {
	URL    string
	Client *http.Client  // Timeout = Zeitbudget der Quelle (6 s)
	MinAge time.Duration // frühestens so oft neu abrufen (30 s)
	MaxAge time.Duration // ältere Stände gelten nicht mehr (Prognose wäre irreführend)
	UA     string

	trips atomic.Pointer[map[string]struct{}]
	snap  atomic.Pointer[Snapshot]
	mu    sync.Mutex
	busy  chan struct{} // offen, solange ein Abruf läuft
	etag  string
	last  time.Time // letzter Abrufversuch (auch fehlgeschlagen), für MinAge
	err   atomic.Value
}

// LastErr liefert den Fehlertyp des letzten Abrufs ("" = in Ordnung), für /v1/status.
func (l *Live) LastErr() string {
	s, _ := l.err.Load().(string)
	return s
}

// SetTrips legt fest, welche Fahrten behalten werden (trip_ids ohne Präfix); bei jedem neuen Fahrplan.
func (l *Live) SetTrips(trips map[string]struct{}) {
	l.trips.Store(&trips)
	l.mu.Lock()
	l.etag = "" // neuer Fahrplan: nächster Abruf muss neu filtern
	l.mu.Unlock()
}

// Peek liefert den letzten Stand, ohne abzurufen (für den Status).
func (l *Live) Peek() *Snapshot { return l.snap.Load() }

// Get liefert einen höchstens MaxAge alten Stand. Ist er älter als MinAge, wird neu abgerufen; gewartet wird
// höchstens bis ctx abläuft – dann gilt der bisherige Stand (oder nil = nur Fahrplan).
func (l *Live) Get(ctx context.Context) *Snapshot {
	s := l.snap.Load()
	l.mu.Lock()
	if time.Since(l.last) >= l.MinAge && l.busy == nil {
		l.busy = make(chan struct{})
		l.last = time.Now()
		go l.fetch(l.busy)
	}
	busy := l.busy
	l.mu.Unlock()
	if busy != nil && (s == nil || time.Since(s.Fetched) >= l.MinAge) {
		select {
		case <-busy:
			s = l.snap.Load()
		case <-ctx.Done():
		}
	}
	if s == nil || time.Since(s.Fetched) > l.MaxAge {
		return nil
	}
	return s
}

func (l *Live) fetch(done chan struct{}) {
	defer func() {
		l.mu.Lock()
		l.busy = nil
		l.mu.Unlock()
		close(done)
	}()
	err := l.fetchOnce()
	if err != nil && !errors.Is(err, errNotModified) {
		// ein Wiederholungsversuch, falls das Zeitbudget es zulässt (Client-Timeout gilt je Versuch)
		err = l.fetchOnce()
	}
	if err != nil && !errors.Is(err, errNotModified) {
		l.err.Store(err.Error())
		return
	}
	l.err.Store("")
}

var errNotModified = errors.New("unverändert")

func (l *Live) fetchOnce() error {
	tp := l.trips.Load()
	if tp == nil {
		return errors.New("keine Fahrten gesetzt")
	}
	req, err := http.NewRequest(http.MethodGet, l.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", l.UA)
	l.mu.Lock()
	if l.etag != "" && l.snap.Load() != nil {
		req.Header.Set("If-None-Match", l.etag)
	}
	l.mu.Unlock()
	resp, err := l.Client.Do(req)
	if err != nil {
		return fmt.Errorf("Abruf: %w", errors.Unwrap(err)) // keine URL-Parameter ins Protokoll
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if s := l.snap.Load(); s != nil { // Stand ist weiter aktuell
			c := *s
			c.Fetched = time.Now()
			l.snap.Store(&c)
		}
		return errNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	trips := map[TripKey]*Trip{}
	st, err := Filter(resp.Body, *tp, func(tu *gtfs.TripUpdate) {
		td := tu.GetTrip()
		t := &Trip{Canceled: td.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED}
		for _, u := range tu.StopTimeUpdate {
			su := StopUpd{StopID: u.GetStopId(), Skipped: u.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_SKIPPED}
			if e := u.GetArrival(); e != nil && e.Delay != nil {
				su.ArrDelay, su.HasArr = e.GetDelay(), true
			}
			if e := u.GetDeparture(); e != nil && e.Delay != nil {
				su.DepDel, su.HasDep = e.GetDelay(), true
			}
			t.Upd = append(t.Upd, su)
		}
		trips[TripKey{td.GetTripId(), td.GetStartDate()}] = t
	})
	if err != nil {
		return fmt.Errorf("Feed: %w", err)
	}
	l.snap.Store(&Snapshot{Fetched: time.Now(), FeedTime: time.Unix(int64(st.Timestamp), 0), Trips: trips, Stats: st})
	l.mu.Lock()
	l.etag = resp.Header.Get("ETag")
	l.mu.Unlock()
	return nil
}

// Delays überträgt die Prognose einer Fahrt auf ihre Halte (stops = stop_ids ohne Präfix in Fahrtreihenfolge):
// arr[i]/dep[i] sind die Verspätungen in Sekunden, known[i] ob es für Halt i eine Angabe gibt (auch fortgeschrieben),
// skipped[i] ob der Halt entfällt. Angaben werden der Reihe nach den Halten zugeordnet; vor der ersten Angabe
// gibt es keine Prognose.
func (t *Trip) Delays(stops []string) (arr, dep []int32, known, skipped []bool) {
	n := len(stops)
	arr, dep, known, skipped = make([]int32, n), make([]int32, n), make([]bool, n), make([]bool, n)
	j := 0
	var cur int32
	have := false
	for i, s := range stops {
		a := cur
		match := false
		for k := j; k < len(t.Upd); k++ { // nächste Angabe zu diesem Halt (Reihenfolge wie im Feed)
			if t.Upd[k].StopID == s {
				u := t.Upd[k]
				j, match = k+1, true
				switch {
				case u.HasArr:
					a = u.ArrDelay
				case u.HasDep:
					a = u.DepDel
				}
				cur = a
				if u.HasDep {
					cur = u.DepDel
				}
				skipped[i] = u.Skipped
				have = have || u.HasArr || u.HasDep
				break
			}
		}
		if !match {
			a = cur
		}
		arr[i], dep[i], known[i] = a, cur, have
	}
	return
}

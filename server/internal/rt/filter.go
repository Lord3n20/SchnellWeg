// Package rt liest den bundesweiten GTFS-RT-Feed streamend und behält nur Fahrten der Region.
package rt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	gtfs "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// Feldnummern aus gtfs-realtime.proto
const (
	feedHeader      = 1
	feedEntity      = 2
	entityTripUpd   = 3
	tripUpdTrip     = 1
	tripDescTripID  = 1
	maxEntityLength = 16 << 20 // Schutz gegen kaputte Längenangaben (Meldungen gehen bis ~2 MB)
)

// Stats beschreibt einen Durchlauf, ohne Inhalte der Fahrten.
type Stats struct {
	Bytes, Entities, TripUpdates, Kept int
	Timestamp                          uint64
}

// Filter liest einen FeedMessage-Strom und ruft keep für jede TripUpdate auf, deren trip_id in trips steht.
// Nicht passende Einträge werden nur bis zur trip_id angesehen und nie vollständig dekodiert.
func Filter(r io.Reader, trips map[string]struct{}, keep func(*gtfs.TripUpdate)) (Stats, error) {
	var st Stats
	br := bufio.NewReaderSize(r, 64<<10)
	buf := make([]byte, 0, 4<<10)
	for {
		tag, err := binary.ReadUvarint(br)
		if errors.Is(err, io.EOF) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		num, typ := protowire.DecodeTag(tag)
		if typ != protowire.BytesType {
			return st, fmt.Errorf("unerwarteter Typ %d in FeedMessage", typ)
		}
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return st, err
		}
		if n > maxEntityLength {
			return st, fmt.Errorf("Eintrag zu lang (%d Byte)", n)
		}
		if cap(buf) < int(n) {
			buf = make([]byte, n)
		}
		buf = buf[:n]
		if _, err := io.ReadFull(br, buf); err != nil {
			return st, err
		}
		st.Bytes += int(n)
		switch num {
		case feedHeader:
			var h gtfs.FeedHeader
			if err := proto.Unmarshal(buf, &h); err != nil {
				return st, err
			}
			st.Timestamp = h.GetTimestamp()
		case feedEntity:
			st.Entities++
			tu := subMessage(buf, entityTripUpd)
			if tu == nil {
				continue
			}
			st.TripUpdates++
			if _, ok := trips[string(subMessage(subMessage(tu, tripUpdTrip), tripDescTripID))]; !ok {
				continue
			}
			var m gtfs.TripUpdate
			if err := proto.Unmarshal(tu, &m); err != nil {
				return st, err
			}
			st.Kept++
			keep(&m)
		}
	}
}

// subMessage liefert die Bytes des ersten längenbegrenzten Felds num in b (oder nil).
func subMessage(b []byte, num protowire.Number) []byte {
	for len(b) > 0 {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return nil
		}
		b = b[l:]
		if n == num && typ == protowire.BytesType {
			v, l := protowire.ConsumeBytes(b)
			if l < 0 {
				return nil
			}
			return v
		}
		l = protowire.ConsumeFieldValue(n, typ, b)
		if l < 0 {
			return nil
		}
		b = b[l:]
	}
	return nil
}

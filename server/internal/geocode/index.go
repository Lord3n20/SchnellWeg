package geocode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Indexdatei zu einer Suchliste (geocode.tsv → geocode.idx), wird per mmap eingeblendet (D-26). Alle Teile
// little-endian und auf 4 Byte ausgerichtet:
//
//	Kopf      magic[8] tsvSize u64 nEnt nWord nPost nCell blobLen u32
//	offs      [nEnt]u32       Zeilenanfang je Eintrag in der TSV
//	coord     [2*nEnt]i32     Breite, Länge in 1e-6 Grad
//	wordOff   [nWord+1]u32    in blob, Wörter sortiert
//	blob      [blobLen]byte
//	postOff   [nWord+1]u32    in posts
//	posts     [nPost]u32      Eintrag<<1 | 1 wenn das Wort im Namen steht, je Wort aufsteigend
//	cells     [2*nCell]u32    Zelle, erster Eintrag (nur wenn die TSV nach Zellen geordnet ist)
const idxMagic = "SWGIDX01"

// part ist eine Suchliste mit ihrem Index (Teile zeigen in die eingeblendeten Dateien).
type part struct {
	tsv     []byte
	offs    []uint32
	coord   []int32
	wordOff []uint32
	blob    []byte
	postOff []uint32
	posts   []uint32
	cells   map[int32][2]uint32 // Zelle → Eintragsbereich [von, bis)
}

// BuildIndex schreibt den Index zur TSV tsv nach w (Tests, kleine Listen).
func BuildIndex(tsv []byte, w io.Writer) error {
	idx, err := buildIndex(tsv, func(n int) ([]byte, error) { return aligned(n), nil })
	if err != nil {
		return err
	}
	_, err = w.Write(idx)
	return err
}

// BuildIndexFile schreibt den Index direkt in die eingeblendete Datei path: Zeilenanfänge, Koordinaten und
// Trefferlisten (ganz DE ~0,7 GB) liegen so im Page Cache statt im Heap. Im Speicher steht nur die Wortzählung.
func BuildIndexFile(tsv []byte, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var m []byte
	_, err = buildIndex(tsv, func(n int) ([]byte, error) {
		if err := f.Truncate(int64(n)); err != nil {
			return nil, err
		}
		var err error
		m, err = syscall.Mmap(int(f.Fd()), 0, n, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		return m, err
	})
	if m != nil {
		if uerr := syscall.Munmap(m); err == nil {
			err = uerr
		}
	}
	if err == nil {
		err = f.Sync()
	}
	return err
}

// aligned liefert n Bytes mit 8-Byte-Ausrichtung für die uint32-Sichten.
func aligned(n int) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(make([]uint64, (n+7)/8)))), n)
}

// eachLine ruft fn für jede gültige Zeile der TSV (5 Felder, lesbare Koordinaten) mit ihrem Anfang auf; name
// und detail zeigen ohne Kopie in tsv.
func eachLine(tsv []byte, fn func(off int, name, detail string, lat, lon float64)) {
	var t [4]int
	for off := 0; off < len(tsv); {
		n := bytes.IndexByte(tsv[off:], '\n')
		if n < 0 {
			n = len(tsv) - off
		}
		line := tsv[off : off+n]
		k := 0
		for i, c := range line {
			if c == '\t' {
				if k == 4 {
					k = 5
					break
				}
				t[k] = i
				k++
			}
		}
		if k == 4 {
			str := func(a, b int) string { return unsafe.String(unsafe.SliceData(line[a:]), b-a) }
			lat, e1 := strconv.ParseFloat(str(t[2]+1, t[3]), 64)
			lon, e2 := strconv.ParseFloat(str(t[3]+1, len(line)), 64)
			if e1 == nil && e2 == nil {
				fn(off, str(t[0]+1, t[1]), str(t[1]+1, t[2]), lat, lon)
			}
		}
		off += n + 1
	}
}

// buildIndex baut den Index in zwei Durchläufen über die TSV: zählen (Einträge, Zellen, Wörter), dann den von
// alloc gelieferten Speicher in endgültiger Größe füllen.
func buildIndex(tsv []byte, alloc func(n int) ([]byte, error)) ([]byte, error) {
	if len(tsv) >= 1<<32 {
		return nil, errors.New("Suchliste über 4 GB")
	}
	var seen []string
	eachWord := func(name, detail string, fn func(w string, inName bool)) {
		seen = seen[:0]
		nw := words(Normalize(name))
		for k, w := range append(nw, words(Normalize(detail))...) {
			if !slices.Contains(seen, w) {
				seen = append(seen, w)
				fn(w, k < len(nw))
			}
		}
	}
	// Durchlauf 1: Einträge, Zellen und Einträge je Wort zählen
	nEnt, nCell := 0, 0
	sorted := true
	last := int32(-1)
	count := map[string]uint32{}
	eachLine(tsv, func(_ int, name, detail string, lat, lon float64) {
		if c := cellOf(lat, lon); c != last {
			sorted = sorted && c > last
			nCell++
			last = c
		}
		nEnt++
		eachWord(name, detail, func(w string, _ bool) { count[w]++ })
	})
	if nEnt >= 1<<31 {
		return nil, errors.New("zu viele Einträge")
	}
	if !sorted {
		nCell = 0
	}
	ws := make([]string, 0, len(count))
	blobLen := 0
	for w := range count {
		ws = append(ws, w)
		blobLen += len(w)
	}
	slices.Sort(ws)
	nPost := 0
	for _, w := range ws {
		nPost += int(count[w])
	}
	if nPost >= 1<<32 {
		return nil, errors.New("zu viele Trefferlisten")
	}
	nWord := len(ws)
	size := 40 + 4*(3*nEnt+nWord+1) + pad4(blobLen) + 4*(nWord+1+nPost+2*nCell)
	idx, err := alloc(size)
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	copy(idx, idxMagic)
	le.PutUint64(idx[8:], uint64(len(tsv)))
	for k, v := range []int{nEnt, nWord, nPost, nCell, pad4(blobLen)} {
		le.PutUint32(idx[16+4*k:], uint32(v))
	}
	off := 40
	u32 := func(n int) []uint32 {
		s := unsafe.Slice((*uint32)(unsafe.Pointer(unsafe.SliceData(idx[off:]))), n)
		off += 4 * n
		return s
	}
	offs, coord := u32(nEnt), u32(2*nEnt)
	wordOff := u32(nWord + 1)
	blob := idx[off : off+blobLen]
	off += pad4(blobLen)
	postOff := u32(nWord + 1)
	posts := u32(nPost)
	cells := u32(2 * nCell)
	b := 0
	for i, w := range ws {
		b += copy(blob[b:], w)
		wordOff[i+1] = uint32(b)
		postOff[i+1] = postOff[i] + count[w]
		count[w] = uint32(i) // ab hier: Wortnummer
	}
	ws = nil
	cur := slices.Clone(postOff[:nWord])
	// Durchlauf 2: Zeilen, Koordinaten, Zellen und Trefferlisten (Einträge aufsteigend → Listen sortiert)
	i, c := 0, 0
	last = -1
	eachLine(tsv, func(o int, name, detail string, lat, lon float64) {
		if cl := cellOf(lat, lon); cl != last && nCell > 0 {
			cells[c], cells[c+1] = uint32(cl), uint32(i)
			c += 2
			last = cl
		}
		offs[i] = uint32(o)
		coord[2*i], coord[2*i+1] = uint32(int32(lat*1e6)), uint32(int32(lon*1e6))
		eachWord(name, detail, func(w string, inName bool) {
			k := count[w]
			v := uint32(i) << 1
			if inName {
				v |= 1
			}
			posts[cur[k]] = v
			cur[k]++
		})
		i++
	})
	return idx, nil
}

func pad4(n int) int { return (n + 3) &^ 3 }

// openPart legt die Sichten auf Index und TSV an (ohne Kopie).
func openPart(tsv, idx []byte) (*part, error) {
	le := binary.LittleEndian
	if len(idx) < 40 || string(idx[:8]) != idxMagic {
		return nil, errors.New("keine Indexdatei")
	}
	if le.Uint64(idx[8:]) != uint64(len(tsv)) {
		return nil, errors.New("Index passt nicht zur Suchliste")
	}
	var h [5]int
	for k := range h {
		h[k] = int(le.Uint32(idx[16+4*k:]))
	}
	nEnt, nWord, nPost, nCell, blobLen := h[0], h[1], h[2], h[3], h[4]
	need := 40 + 4*(nEnt+2*nEnt+nWord+1) + blobLen + 4*(nWord+1+nPost+2*nCell)
	if len(idx) != need {
		return nil, fmt.Errorf("Indexdatei hat %d statt %d Bytes", len(idx), need)
	}
	off := 40
	u32 := func(n int) []uint32 {
		s := unsafe.Slice((*uint32)(unsafe.Pointer(unsafe.SliceData(idx[off:]))), n)
		off += 4 * n
		return s
	}
	p := &part{tsv: tsv}
	p.offs = u32(nEnt)
	c := u32(2 * nEnt)
	p.coord = unsafe.Slice((*int32)(unsafe.Pointer(unsafe.SliceData(c))), len(c))
	p.wordOff = u32(nWord + 1)
	p.blob = idx[off : off+blobLen]
	off += blobLen
	p.postOff = u32(nWord + 1)
	p.posts = u32(nPost)
	cl := u32(2 * nCell)
	p.cells = map[int32][2]uint32{}
	for i := 0; i < len(cl); i += 2 {
		end := uint32(nEnt)
		if i+2 < len(cl) {
			end = cl[i+3]
		}
		p.cells[int32(cl[i])] = [2]uint32{cl[i+1], end}
	}
	return p, nil
}

func memPart(tsv []byte) (*part, error) {
	idx, err := buildIndex(tsv, func(n int) ([]byte, error) { return aligned(n), nil })
	if err != nil {
		return nil, err
	}
	return openPart(tsv, idx)
}

func (p *part) n() int { return len(p.offs) }

func (p *part) word(i int) string {
	a, b := p.wordOff[i], p.wordOff[i+1]
	return unsafe.String(unsafe.SliceData(p.blob[a:]), int(b-a))
}

// entry liest Eintrag i aus seiner Zeile; die Strings zeigen ohne Kopie in die Datei.
func (p *part) entry(i uint32) Entry {
	line := p.tsv[p.offs[i]:]
	var f [3]string
	for k := range f {
		t := bytes.IndexByte(line, '\t')
		f[k] = unsafe.String(unsafe.SliceData(line), t)
		line = line[t+1:]
	}
	e := Entry{Kind: f[0], Name: f[1], Detail: f[2]}
	if p.coord != nil {
		e.Lat, e.Lon = float64(p.coord[2*i])/1e6, float64(p.coord[2*i+1])/1e6
	}
	return e
}

// Index durchsucht eine oder mehrere Suchlisten (OSM ganz DE, RWTH-Gebäude …).
type Index struct {
	parts []*part
}

// Len ist die Zahl der Einträge.
func (ix *Index) Len() int {
	n := 0
	for _, p := range ix.parts {
		n += p.n()
	}
	return n
}

// Load liest TSVs aus Write und baut die Indizes im Speicher (Tests, kleine Listen).
func Load(rs ...io.Reader) (*Index, error) {
	ix := &Index{}
	for _, r := range rs {
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		p, err := memPart(b)
		if err != nil {
			return nil, err
		}
		ix.parts = append(ix.parts, p)
	}
	return ix, nil
}

// LoadFiles blendet die TSVs per mmap ein, mit ihrer Indexdatei (x.tsv → x.idx), falls vorhanden und passend;
// sonst wird der Index im Speicher gebaut (kleine Listen wie die RWTH-Gebäude).
func LoadFiles(paths ...string) (*Index, error) {
	ix := &Index{}
	for _, path := range paths {
		tsv, err := mmapFile(path)
		if err != nil {
			return nil, err
		}
		var p *part
		if idx, err := mmapFile(strings.TrimSuffix(path, ".tsv") + ".idx"); err == nil && len(idx) > 0 {
			p, err = openPart(tsv, idx)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		} else if p, err = memPart(tsv); err != nil {
			return nil, err
		}
		ix.parts = append(ix.parts, p)
	}
	return ix, nil
}

// mmapFile blendet eine Datei nur lesend ein (nie wieder ausgeblendet).
func mmapFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil, err
	}
	return syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
}

package geocode

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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

// BuildIndex schreibt den Index zur TSV tsv nach w. Speicher: Wortzählung + alle Trefferlisten (~4 B je Wort und
// Eintrag; ganz DE ~0,8 GB).
func BuildIndex(tsv []byte, w io.Writer) error {
	if len(tsv) >= 1<<32 {
		return errors.New("Suchliste über 4 GB")
	}
	var offs []uint32
	var coord []int32
	var cellList []uint32
	sorted := true
	last := int32(-1)
	for off := 0; off < len(tsv); {
		n := bytes.IndexByte(tsv[off:], '\n')
		if n < 0 {
			n = len(tsv) - off
		}
		f := bytes.Split(tsv[off:off+n], []byte{'\t'})
		if len(f) == 5 {
			lat, e1 := strconv.ParseFloat(string(f[3]), 64)
			lon, e2 := strconv.ParseFloat(string(f[4]), 64)
			if e1 == nil && e2 == nil {
				if c := cellOf(lat, lon); c != last {
					if c < last {
						sorted = false
					}
					cellList = append(cellList, uint32(c), uint32(len(offs)))
					last = c
				}
				offs = append(offs, uint32(off))
				coord = append(coord, int32(lat*1e6), int32(lon*1e6))
			}
		}
		off += n + 1
	}
	if !sorted {
		cellList = nil
	}
	if len(offs) >= 1<<31 {
		return errors.New("zu viele Einträge")
	}
	p := &part{tsv: tsv, offs: offs, coord: coord}
	// Durchlauf 1: Einträge je Wort zählen
	count := map[string]uint32{}
	var seen []string
	each := func(i int, fn func(w string, inName bool)) {
		e := p.entry(uint32(i))
		seen = seen[:0]
		name := words(Normalize(e.Name))
		for k, w := range append(name, words(Normalize(e.Detail))...) {
			dup := false
			for _, s := range seen {
				dup = dup || s == w
			}
			if !dup {
				seen = append(seen, w)
				fn(w, k < len(name))
			}
		}
	}
	for i := range offs {
		each(i, func(w string, _ bool) { count[w]++ })
	}
	ws := make([]string, 0, len(count))
	for w := range count {
		ws = append(ws, w)
	}
	sort.Strings(ws)
	wordOff := make([]uint32, len(ws)+1)
	postOff := make([]uint32, len(ws)+1)
	blobLen := 0
	for i, w := range ws {
		blobLen += len(w)
		wordOff[i+1] = uint32(blobLen)
		postOff[i+1] = postOff[i] + count[w]
		count[w] = uint32(i) // ab hier: Wortnummer
	}
	// Durchlauf 2: Trefferlisten füllen (Einträge aufsteigend → Listen sortiert)
	posts := make([]uint32, postOff[len(ws)])
	cur := make([]uint32, len(ws))
	copy(cur, postOff)
	for i := range offs {
		each(i, func(w string, inName bool) {
			k := count[w]
			v := uint32(i) << 1
			if inName {
				v |= 1
			}
			posts[cur[k]] = v
			cur[k]++
		})
	}
	count, cur = nil, nil

	bw := bufio.NewWriterSize(w, 1<<20)
	head := make([]byte, 40)
	copy(head, idxMagic)
	le := binary.LittleEndian
	le.PutUint64(head[8:], uint64(len(tsv)))
	for k, v := range []int{len(offs), len(ws), len(posts), len(cellList) / 2, pad4(blobLen)} {
		le.PutUint32(head[16+4*k:], uint32(v))
	}
	bw.Write(head)
	binary.Write(bw, le, offs)
	binary.Write(bw, le, coord)
	binary.Write(bw, le, wordOff)
	for _, w := range ws {
		bw.WriteString(w)
	}
	bw.Write(make([]byte, pad4(blobLen)-blobLen))
	binary.Write(bw, le, postOff)
	binary.Write(bw, le, posts)
	binary.Write(bw, le, cellList)
	return bw.Flush()
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
	var b bytes.Buffer
	if err := BuildIndex(tsv, &b); err != nil {
		return nil, err
	}
	// eigene Kopie mit 8-Byte-Ausrichtung für die uint32-Sichten
	idx := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(make([]uint64, (b.Len()+7)/8)))), b.Len())
	copy(idx, b.Bytes())
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

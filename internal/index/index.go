package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"

	"github.com/ristir/urd/internal/corpus"
)

var magic = []byte("URD2")

var ErrBadIndex = errors.New("index: bad or unsupported index file")

// Fingerprint identifies a source. Size plus mtime is reliable enough and does
// not require reading the whole file.
type Fingerprint struct {
	Path  string
	Size  int64
	ModNS int64
}

func Fingerprints(paths []string) []Fingerprint {
	out := make([]Fingerprint, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, Fingerprint{Path: p, Size: st.Size(), ModNS: st.ModTime().UnixNano()})
	}
	return out
}

func Stale(saved, current []Fingerprint) bool {
	if len(saved) != len(current) {
		return true
	}
	byPath := make(map[string]Fingerprint, len(saved))
	for _, f := range saved {
		byPath[f.Path] = f
	}
	for _, f := range current {
		old, ok := byPath[f.Path]
		if !ok || old.Size != f.Size || old.ModNS != f.ModNS {
			return true
		}
	}
	return false
}

func putString(buf *bytes.Buffer, s string) {
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(s)))
	buf.Write(n[:])
	buf.WriteString(s)
}

func putInt64(buf *bytes.Buffer, v int64) {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(v))
	buf.Write(n[:])
}

func putUint32(buf *bytes.Buffer, v uint32) {
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], v)
	buf.Write(n[:])
}

func Save(path string, c *corpus.Corpus, fps []Fingerprint, filters []string) error {
	var buf bytes.Buffer
	buf.Write(magic)
	putUint32(&buf, uint32(len(filters)))
	for _, f := range filters {
		putString(&buf, f)
	}
	putUint32(&buf, uint32(len(fps)))
	for _, f := range fps {
		putString(&buf, f.Path)
		putInt64(&buf, f.Size)
		putInt64(&buf, f.ModNS)
	}
	putUint32(&buf, uint32(len(c.Items)))
	for _, it := range c.Items {
		putString(&buf, it.Cmd)
		putInt64(&buf, it.TS)
		putUint32(&buf, uint32(it.Count))
		putString(&buf, it.Source)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Write to a temporary file and rename: a crash in the middle of WriteFile would
	// otherwise leave a broken index where a working one used to be.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// The smallest record on disk is a 4-byte string length (a path or a command)
// with a zero-length string, plus the record's fixed fields.
const (
	// minFingerprintSize: 4 (len path) + 0 (path) + 8 (Size) + 8 (ModNS).
	minFingerprintSize = 20
	// minItemSize: 4 (len cmd) + 0 (cmd) + 8 (TS) + 4 (Count) + 4 (len source).
	minItemSize = 20
	// minFilterSize: 4 (len pattern) + 0 (pattern).
	minFilterSize = 4
)

// boundedCount rejects a count that cannot fit in the remaining bytes: make()
// with a foreign number dies of OOM before ErrBadIndex otherwise.
func (r *reader) boundedCount(minRecordSize int) int {
	n := int(r.u32())
	if r.err != nil {
		return 0
	}
	if n < 0 {
		r.err = ErrBadIndex
		return 0
	}
	remaining := len(r.b) - r.pos
	if remaining < 0 || n > remaining/minRecordSize {
		r.err = ErrBadIndex
		return 0
	}
	return n
}

type reader struct {
	b   []byte
	pos int
	err error
}

func (r *reader) u32() uint32 {
	if r.err != nil || r.pos+4 > len(r.b) {
		r.err = ErrBadIndex
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) i64() int64 {
	if r.err != nil || r.pos+8 > len(r.b) {
		r.err = ErrBadIndex
		return 0
	}
	v := int64(binary.LittleEndian.Uint64(r.b[r.pos:]))
	r.pos += 8
	return v
}

func (r *reader) str() string {
	n := int(r.u32())
	if r.err != nil || r.pos+n > len(r.b) {
		r.err = ErrBadIndex
		return ""
	}
	s := string(r.b[r.pos : r.pos+n])
	r.pos += n
	return s
}

func Load(path string) (*corpus.Corpus, []Fingerprint, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(data) < len(magic) || !bytes.Equal(data[:len(magic)], magic) {
		return nil, nil, nil, ErrBadIndex
	}
	r := &reader{b: data, pos: len(magic)}

	nfl := r.boundedCount(minFilterSize)
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	filters := make([]string, 0, nfl)
	for i := 0; i < nfl && r.err == nil; i++ {
		filters = append(filters, r.str())
	}
	if r.err != nil {
		return nil, nil, nil, r.err
	}

	nf := r.boundedCount(minFingerprintSize)
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	fps := make([]Fingerprint, 0, nf)
	for i := 0; i < nf && r.err == nil; i++ {
		f := Fingerprint{}
		f.Path = r.str()
		f.Size = r.i64()
		f.ModNS = r.i64()
		fps = append(fps, f)
	}

	ni := r.boundedCount(minItemSize)
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	items := make([]corpus.Item, 0, ni)
	for i := 0; i < ni && r.err == nil; i++ {
		it := corpus.Item{}
		it.Cmd = r.str()
		it.TS = r.i64()
		it.Count = int(r.u32())
		it.Source = r.str()
		items = append(items, it)
	}
	if r.err != nil {
		return nil, nil, nil, r.err
	}
	return corpus.FromItems(items), fps, filters, nil
}

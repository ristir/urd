package dump

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/histfile"
)

type Record struct {
	Cmd    string `json:"cmd"`
	TS     int64  `json:"ts"`
	Source string `json:"source"`
}

func Write(w io.Writer, c *corpus.Corpus) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, it := range c.Items {
		if err := enc.Encode(Record{Cmd: it.Cmd, TS: it.TS, Source: it.Source}); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteZsh writes oldest to newest, the way a real histfile grows by appending:
// otherwise a file appended to ~/.zsh_history reverses history on the next read.
func WriteZsh(w io.Writer, c *corpus.Corpus) error {
	bw := bufio.NewWriter(w)
	for i := len(c.Items) - 1; i >= 0; i-- {
		it := c.Items[i]
		if _, err := fmt.Fprintf(bw, ": %d:0;%s\n", it.TS, it.Cmd); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteBash: a record without a time gets no "#" line - "#0" would show the year
// 1970 in the output of history.
func WriteBash(w io.Writer, c *corpus.Corpus) error {
	bw := bufio.NewWriter(w)
	for i := len(c.Items) - 1; i >= 0; i-- {
		it := c.Items[i]
		if it.TS != 0 {
			if _, err := fmt.Fprintf(bw, "#%d\n", it.TS); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(bw, "%s\n", it.Cmd); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func Read(data []byte, label string) []histfile.Entry {
	return histfile.Parse(data, label)
}

func ImportName(src string, now time.Time) string {
	base := "stdin"
	if src != "-" && src != "" {
		base = filepath.Base(src)
	}
	base = strings.TrimSuffix(base, ".jsonl")
	return fmt.Sprintf("%s-%s.jsonl", base, now.Format("20060102-150405"))
}

// maxImportCollisions: a thousand imports of one basename within a second means
// something is wrong, and that has to be said rather than spun on silently.
const maxImportCollisions = 1000

// DefaultName: no extension, so that "urd --dmp load" without an argument finds
// it by the same "urd_history_*" pattern.
func DefaultName(now time.Time) string {
	return "urd_history_" + now.Format("20060102-1504")
}

// DefaultPath is the home directory root rather than the data directory: a
// snapshot is meant to be carried away.
func DefaultPath(now time.Time) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultName(now)), nil
}

func NewestDefault() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(home, "urd_history_*"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no urd_history_* dump found in %s", home)
	}
	// The name encodes time as YYYYMMDD-HHMM: lexicographic order of the strings
	// matches chronological order, and mtime is not needed.
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// Create: O_EXCL rather than stat-then-create - parallel imports run in separate
// processes, and a second-level stamp plus a shared archive basename collide easily.
func Create(dir, src string, now time.Time) (*os.File, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	name := ImportName(src, now)
	stem := strings.TrimSuffix(name, ".jsonl")
	for n := 1; n <= maxImportCollisions; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d.jsonl", stem, n)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return f, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("dump: %s already has %d imports named like %s", dir, maxImportCollisions, name)
}

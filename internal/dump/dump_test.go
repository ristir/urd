package dump

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/histfile"
)

func sample() *corpus.Corpus {
	c, _ := corpus.Build([]histfile.Entry{
		{Cmd: "ansible-playbook rate-limit.yml", TS: 200, Source: "live"},
		{Cmd: "grep Навыки notes", TS: 100, Source: "archive"},
	})
	return c
}

func TestWriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n"); n != 1 {
		t.Fatalf("expected 2 lines, got %d newlines: %q", n, buf.String())
	}
	got := Read(buf.Bytes(), "dumpfile")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Cmd != "ansible-playbook rate-limit.yml" || got[0].TS != 200 {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].Cmd != "grep Навыки notes" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestWriteZshWritesOldestFirst(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteZsh(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], ": 100:") {
		t.Fatalf("first line = %q, want the older entry (TS=100) first", lines)
	}

	got := histfile.ParseZsh(buf.Bytes(), "exported")
	if len(got) != 2 {
		t.Fatalf("got %+v, want both entries preserved by the round trip", got)
	}
}

func TestWriteBashRoundTripsThroughOwnParser(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBash(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	got := histfile.ParseBash(buf.Bytes(), "exported")
	if len(got) != 2 {
		t.Fatalf("got %+v, want both entries", got)
	}
	if got[0].Cmd != "grep Навыки notes" || got[0].TS != 100 {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].Cmd != "ansible-playbook rate-limit.yml" || got[1].TS != 200 {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestWriteBashOmitsHashLineForUndatedEntries(t *testing.T) {
	c, _ := corpus.Build([]histfile.Entry{
		{Cmd: "ll", TS: 0, Ord: 1, Source: "archive"},
	})
	var buf bytes.Buffer
	if err := WriteBash(&buf, c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "#0") {
		t.Fatalf("undated entry got a #0 line: %q", buf.String())
	}
	got := histfile.ParseBash(buf.Bytes(), "exported")
	if len(got) != 1 || got[0].TS != 0 || got[0].Cmd != "ll" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadDetectsRawHistfile(t *testing.T) {
	got := Read([]byte(": 100:0;ll\n: 200:0;pwd\n"), "raw")
	if len(got) != 2 || got[1].Cmd != "pwd" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadDetectsBashHistfile(t *testing.T) {
	got := Read([]byte("#1700000000\nll\n"), "raw")
	if len(got) != 1 || got[0].TS != 1700000000 {
		t.Fatalf("got %+v", got)
	}
}

func TestReadSkipsBrokenJSONLines(t *testing.T) {
	data := []byte(`{"cmd":"ll","ts":1,"source":"a"}` + "\n" + `{broken` + "\n" + `{"cmd":"pwd","ts":2,"source":"a"}` + "\n")
	got := Read(data, "d")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
}

func TestDefaultPathIsInHomeRootWithoutExtension(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	at := time.Date(2026, 8, 6, 22, 15, 0, 0, time.UTC)
	got, err := DefaultPath(at)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "urd_history_20260806-2215")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNewestDefaultPicksTheLatestByName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"urd_history_20260101-0000", "urd_history_20260806-2215", "urd_history_20260302-1000"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewestDefault()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "urd_history_20260806-2215")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNewestDefaultErrorsWhenNothingMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := NewestDefault(); err == nil {
		t.Fatal("expected an error when no urd_history_* file exists")
	}
}

func TestImportNameIsStableAndNamespaced(t *testing.T) {
	at := time.Date(2026, 8, 6, 22, 15, 0, 0, time.UTC)
	got := ImportName("/home/user/backups/zsh_history_202505", at)
	if got != "zsh_history_202505-20260806-221500.jsonl" {
		t.Fatalf("got %q", got)
	}
	if s := ImportName("-", at); s != "stdin-20260806-221500.jsonl" {
		t.Fatalf("got %q", s)
	}
}

func TestCreateNeverOverwritesAnEarlierImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "imported")
	at := time.Date(2026, 8, 7, 17, 20, 18, 0, time.UTC)

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		f, path, err := Create(dir, "/archive/host-"+strconv.Itoa(i)+"/root/.bash_history", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("{\"cmd\":\"n" + strconv.Itoa(i) + "\",\"ts\":1,\"source\":\"a\"}\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if seen[path] {
			t.Fatalf("import %d reused the path %s", i, path)
		}
		seen[path] = true
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("3 imports, %d files on disk", len(files))
	}
	for _, f := range files {
		st, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: perm %v, want 0600", f.Name(), st.Mode().Perm())
		}
	}
	if !seen[filepath.Join(dir, ".bash_history-20260807-172018.jsonl")] {
		t.Fatalf("the first import did not get the plain name: %v", seen)
	}
	if !seen[filepath.Join(dir, ".bash_history-20260807-172018-2.jsonl")] {
		t.Fatalf("the suffix is not the documented one: %v", seen)
	}
}

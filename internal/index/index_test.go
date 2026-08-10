package index

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/histfile"
)

func sampleCorpus() *corpus.Corpus {
	c, _ := corpus.Build([]histfile.Entry{
		{Cmd: "ansible-playbook rate-limit.yml", TS: 200, Source: "live"},
		{Cmd: "grep Навыки notes", TS: 100, Source: "archive"},
	})
	return c
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	src := filepath.Join(dir, "hist")
	if err := os.WriteFile(src, []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fps := Fingerprints([]string{src})

	if err := Save(p, sampleCorpus(), fps, nil); err != nil {
		t.Fatal(err)
	}
	got, gotFps, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(got.Items))
	}
	if got.Items[0].Cmd != "ansible-playbook rate-limit.yml" || got.Items[0].TS != 200 {
		t.Fatalf("item 0 = %+v", got.Items[0])
	}
	if got.Items[1].Source != "archive" || got.Items[1].Count != 1 {
		t.Fatalf("item 1 = %+v", got.Items[1])
	}
	if len(gotFps) != 1 || gotFps[0].Path != src {
		t.Fatalf("fingerprints = %+v", gotFps)
	}
}

func TestLoadRebuildsFoldedRunes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	if err := Save(p, sampleCorpus(), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range got.Items {
		if len(it.Runes) == 0 || len(it.Folded) != len(it.Runes) {
			t.Fatalf("item %q has runes=%d folded=%d", it.Cmd, len(it.Runes), len(it.Folded))
		}
	}
	if string(got.Items[1].Folded) != "grep навыки notes" {
		t.Fatalf("folded = %q", string(got.Items[1].Folded))
	}
}

func TestStaleDetectsGrowth(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hist")
	if err := os.WriteFile(src, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := Fingerprints([]string{src})
	if err := os.WriteFile(src, []byte("ll\npwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := Fingerprints([]string{src})
	if !Stale(before, after) {
		t.Fatal("growth not detected")
	}
	if Stale(after, after) {
		t.Fatal("identical fingerprints reported as stale")
	}
}

func TestStaleDetectsNewSource(t *testing.T) {
	if !Stale(nil, []Fingerprint{{Path: "a", Size: 1}}) {
		t.Fatal("added source not detected")
	}
}

func TestLoadRejectsWrongMagic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	if err := os.WriteFile(p, []byte("XXXX garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(p); err == nil {
		t.Fatal("garbage index accepted, want error")
	}
}

func TestLoadRejectsAbsurdFingerprintCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	var buf bytes.Buffer
	buf.Write(magic)
	putUint32(&buf, 0) // filter count
	putUint32(&buf, 0xFFFFFFFF)
	buf.WriteString("short")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := Load(p)
	if err == nil {
		t.Fatal("absurd fingerprint count accepted, want error")
	}
	if c != nil {
		t.Fatalf("corpus = %+v, want nil", c)
	}
}

func TestLoadRejectsAbsurdItemCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	var buf bytes.Buffer
	buf.Write(magic)
	putUint32(&buf, 0) // filter count
	putUint32(&buf, 0) // fingerprint count
	putUint32(&buf, 0xFFFFFFFF)
	buf.WriteString("short")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := Load(p)
	if err == nil {
		t.Fatal("absurd item count accepted, want error")
	}
	if c != nil {
		t.Fatalf("corpus = %+v, want nil", c)
	}
}

func TestLoadRejectsTruncatedRecord(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.bin")
	if err := Save(p, sampleCorpus(), nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 5 {
		t.Fatal("index too small to truncate")
	}
	if err := os.WriteFile(p, data[:len(data)-3], 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := Load(p)
	if err == nil {
		t.Fatal("truncated index accepted, want error")
	}
	if c != nil {
		t.Fatalf("corpus = %+v, want nil", c)
	}
}

func TestSaveLoadCarriesFilters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index.bin")
	if err := Save(p, sampleCorpus(), nil, []string{"history", "secret"}); err != nil {
		t.Fatal(err)
	}
	_, _, filters, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(filters) != 2 || filters[0] != "history" || filters[1] != "secret" {
		t.Fatalf("filters = %v", filters)
	}
}

func TestLoadRejectsOldMagic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index.bin")
	if err := os.WriteFile(p, []byte("URD1garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(p); err == nil {
		t.Fatal("old-format index accepted, want error")
	}
}

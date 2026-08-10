package corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ristir/urd/internal/histfile"
)

func TestBuildSortsNewestFirst(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "old", TS: 100, Source: "a"},
		{Cmd: "new", TS: 300, Source: "a"},
		{Cmd: "mid", TS: 200, Source: "a"},
	}
	c, _ := Build(in)
	want := []string{"new", "mid", "old"}
	for i, w := range want {
		if c.Items[i].Cmd != w {
			t.Fatalf("item %d = %q, want %q", i, c.Items[i].Cmd, w)
		}
	}
}

func TestBuildDedupesKeepingNewestAndCounting(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "ll", TS: 100, Source: "archive"},
		{Cmd: "ll", TS: 300, Source: "live"},
		{Cmd: "pwd", TS: 200, Source: "live"},
	}
	c, st := Build(in)
	if len(c.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(c.Items))
	}
	if c.Items[0].Cmd != "ll" || c.Items[0].TS != 300 || c.Items[0].Source != "live" {
		t.Fatalf("item 0 = %+v", c.Items[0])
	}
	if c.Items[0].Count != 2 {
		t.Fatalf("count = %d, want 2", c.Items[0].Count)
	}
	if st.Raw != 3 || st.Kept != 2 || st.Dropped != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestBuildFlattensAndCountsMultiline(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "ansible-playbook x.yml \\\n  -l host", TS: 100, Source: "a"},
	}
	c, st := Build(in)
	if c.Items[0].Cmd != "ansible-playbook x.yml -l host" {
		t.Fatalf("cmd = %q", c.Items[0].Cmd)
	}
	if st.Multiline != 1 {
		t.Fatalf("multiline = %d, want 1", st.Multiline)
	}
}

func TestBuildSqueezesRepeatedSpacesOutsideQuotes(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "cat  /tmp/urd", TS: 1, Source: "a"},
		{Cmd: `grep 'два  слова' notes`, TS: 2, Source: "a"},
	}
	c, _ := Build(in)
	byCmd := map[string]bool{}
	for _, it := range c.Items {
		byCmd[it.Cmd] = true
	}
	if !byCmd["cat /tmp/urd"] {
		t.Fatalf("typo run was not squeezed: %+v", c.Items)
	}
	if !byCmd[`grep 'два  слова' notes`] {
		t.Fatalf("quoted run was squeezed: %+v", c.Items)
	}
}

func TestBuildUndatedEntriesGoLast(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "undated", TS: 0, Ord: 5, Source: "bash"},
		{Cmd: "dated", TS: 1, Source: "zsh"},
	}
	c, _ := Build(in)
	if c.Items[0].Cmd != "dated" || c.Items[1].Cmd != "undated" {
		t.Fatalf("order = %q, %q", c.Items[0].Cmd, c.Items[1].Cmd)
	}
}

func TestBuildFoldedMatchesRuneCount(t *testing.T) {
	in := []histfile.Entry{{Cmd: "GREP Навыки", TS: 1, Source: "a"}}
	c, _ := Build(in)
	it := c.Items[0]
	if len(it.Folded) != len(it.Runes) {
		t.Fatalf("folded %d runes, runes %d", len(it.Folded), len(it.Runes))
	}
	if string(it.Folded) != "grep навыки" {
		t.Fatalf("folded = %q", string(it.Folded))
	}
}

func TestLoadFilesReadsAllSources(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("#200\npwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	got, unread := LoadFiles([]string{a, b, missing})
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Source != a || got[1].TS != 200 {
		t.Fatalf("got %+v", got)
	}
	if len(unread) != 1 || unread[0].Path != missing || unread[0].Err == nil {
		t.Fatalf("unreadable = %+v, want exactly %s", unread, missing)
	}
}

func TestUnreadableNamesOnlyTheUnreadable(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok")
	if err := os.WriteFile(ok, []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(dir, "denied")
	if err := os.WriteFile(denied, []byte(": 100:0;pwd\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	got := Unreadable([]string{ok, denied})
	if len(got) != 1 || got[0].Path != denied {
		t.Fatalf("unreadable = %+v, want exactly %s", got, denied)
	}
	if got := Unreadable([]string{ok}); len(got) != 0 {
		t.Fatalf("a readable source reported as unreadable: %+v", got)
	}
}

func TestFromItemsRestoresRunes(t *testing.T) {
	c := FromItems([]Item{{Cmd: "GREP Навыки", TS: 1, Source: "s", Count: 1}})
	it := c.Items[0]
	if string(it.Folded) != "grep навыки" || len(it.Runes) != len(it.Folded) {
		t.Fatalf("item = %+v", it)
	}
}

func TestExcludeDropsAnchoredMatches(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "history | grep ans | grep rate", TS: 300, Source: "a"},
		{Cmd: "history", TS: 250, Source: "a"},
		{Cmd: "ansible-playbook rate-limit.yml", TS: 200, Source: "a"},
	}
	got, dropped, bad := Exclude(in, []string{"^history"})
	if dropped != 2 || len(got) != 1 || len(bad) != 0 {
		t.Fatalf("dropped=%d kept=%d bad=%v", dropped, len(got), bad)
	}
	if got[0].Cmd != "ansible-playbook rate-limit.yml" {
		t.Fatalf("kept %q", got[0].Cmd)
	}
}

func TestExcludeAnchorSparesHistoryFilePaths(t *testing.T) {
	in := []histfile.Entry{
		{Cmd: "cat ~/.zsh_history", TS: 2, Source: "a"},
		{Cmd: "urd load ~/backups/sh_history/laptop/bash_history", TS: 1, Source: "a"},
	}
	got, dropped, _ := Exclude(in, []string{"^history"})
	if dropped != 0 || len(got) != 2 {
		t.Fatalf("dropped=%d kept=%d", dropped, len(got))
	}
}

func TestExcludeIsCaseSensitiveUnlessAsked(t *testing.T) {
	in := []histfile.Entry{{Cmd: "HISTORY | grep x", TS: 1, Source: "a"}}
	if _, dropped, _ := Exclude(in, []string{"^history"}); dropped != 0 {
		t.Fatalf("case-sensitive pattern matched uppercase, dropped=%d", dropped)
	}
	if _, dropped, _ := Exclude(in, []string{"(?i)^history"}); dropped != 1 {
		t.Fatalf("(?i) pattern did not match, dropped=%d", dropped)
	}
}

func TestExcludeEmptyPatternListKeepsEverything(t *testing.T) {
	in := []histfile.Entry{{Cmd: "history", TS: 1, Source: "a"}}
	got, dropped, _ := Exclude(in, nil)
	if dropped != 0 || len(got) != 1 {
		t.Fatalf("dropped=%d kept=%d", dropped, len(got))
	}
}

func TestExcludeReportsUncompilablePattern(t *testing.T) {
	in := []histfile.Entry{{Cmd: "ll", TS: 1, Source: "a"}}
	got, dropped, bad := Exclude(in, []string{"[unclosed", "^history"})
	if dropped != 0 || len(got) != 1 {
		t.Fatalf("dropped=%d kept=%d", dropped, len(got))
	}
	if len(bad) != 1 || bad[0] != "[unclosed" {
		t.Fatalf("bad = %v", bad)
	}
}

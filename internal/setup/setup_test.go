package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/engine"
)

func TestRCPathHonoursZDOTDIR(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	t.Setenv("ZDOTDIR", "/cfg/zsh")
	if got := RCPath(Zsh); got != "/cfg/zsh/.zshrc" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("ZDOTDIR", "")
	if got := RCPath(Zsh); got != "/home/x/.zshrc" {
		t.Fatalf("got %q", got)
	}
}

func TestHasHookMatchesAnyMention(t *testing.T) {
	cases := map[string]bool{
		`eval "$(urd hook zsh)"`:                true,
		`eval "$(/usr/local/bin/urd hook zsh)"`: true,
		`  # eval "$(urd hook zsh)"`:            true,
		`alias urd=urd`:                         false,
		``:                                      false,
	}
	for in, want := range cases {
		if got := HasHook(in); got != want {
			t.Errorf("HasHook(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEnsureHookAppendsAtEndWithBackup(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	original := "export ZSH=/omz\nsource $ZSH/oh-my-zsh.sh\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	line := HookLine("urd", Zsh)

	changed, backup, err := EnsureHook(rc, line)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), line+"\n") {
		t.Fatalf("hook is not at the end:\n%s", got)
	}
	if !strings.HasPrefix(string(got), original) {
		t.Fatal("original content changed")
	}
	bak, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(bak) != original {
		t.Fatal("backup does not hold the original content")
	}
}

func TestEnsureHookIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rc, []byte(`eval "$(urd hook zsh)"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, backup, err := EnsureHook(rc, HookLine("urd", Zsh))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("hook added twice")
	}
	if backup != "" {
		t.Fatal("backup created without a change")
	}
}

func TestEnsureHookAbortsWithoutTouchingRCIfBackupFails(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	original := "export ZSH=/omz\nsource $ZSH/oh-my-zsh.sh\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, _, err := EnsureHook(rc, HookLine("urd", Zsh))
	if err == nil {
		t.Fatal("expected an error when the backup could not be written")
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("original .zshrc changed despite the backup failure:\n%s", got)
	}
}

func TestEnsureHookCreatesMissingFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	changed, _, err := EnsureHook(rc, HookLine("urd", Zsh))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(rc); err != nil {
		t.Fatalf("rc not created: %v", err)
	}
}

func TestHasHistLimitsMatchesEitherVariable(t *testing.T) {
	cases := map[string]bool{
		"HISTSIZE=50000\nSAVEHIST=50000\n": true,
		"HISTSIZE=50000\n":                 true,
		"SAVEHIST=50000\n":                 true,
		"  # HISTSIZE=50000\n":             true,
		"export ZSH=/omz\n":                false,
		"":                                 false,
	}
	for in, want := range cases {
		if got := HasHistLimits(in); got != want {
			t.Errorf("HasHistLimits(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNeedsHistLimitsGatesOnBothAbsenceAndSize(t *testing.T) {
	cases := []struct {
		name    string
		content string
		entries int
		want    bool
	}{
		{"small corpus, no vars set", "", 100, false},
		{"large corpus, no vars set", "", HistTrimThreshold + 1, true},
		{"large corpus, exactly at threshold", "", HistTrimThreshold, false},
		{"large corpus, vars already set", "SAVEHIST=5000\n", HistTrimThreshold + 1, false},
	}
	for _, c := range cases {
		if got := NeedsHistLimits(c.content, c.entries); got != c.want {
			t.Errorf("%s: NeedsHistLimits(%q, %d) = %v, want %v", c.name, c.content, c.entries, got, c.want)
		}
	}
}

func TestHistLimitDoublesAndRoundsToATenThousandStep(t *testing.T) {
	cases := map[int]int{
		1:     10000,
		17800: 40000,
		20000: 40000,
		20001: 50000,
	}
	for entries, want := range cases {
		if got := HistLimit(entries); got != want {
			t.Errorf("HistLimit(%d) = %d, want %d", entries, got, want)
		}
	}
}

func TestHistLimitLinesSetBothVariablesEqual(t *testing.T) {
	got := HistLimitLines(40000, Zsh)
	if !strings.Contains(got, "HISTSIZE=40000") || !strings.Contains(got, "SAVEHIST=40000") {
		t.Fatalf("missing a variable: %q", got)
	}
}

func TestEnsureHistLimitsAppendsAtEndWithBackup(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	original := "export ZSH=/omz\nsource $ZSH/oh-my-zsh.sh\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, backup, err := EnsureHistLimits(rc, 40000, Zsh)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), HistLimitLines(40000, Zsh)+"\n") {
		t.Fatalf("limits are not at the end:\n%s", got)
	}
	if !strings.HasPrefix(string(got), original) {
		t.Fatal("original content changed")
	}
	bak, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(bak) != original {
		t.Fatal("backup does not hold the original content")
	}
}

func TestEnsureHistLimitsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rc, []byte("SAVEHIST=99999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, backup, err := EnsureHistLimits(rc, 40000, Zsh)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("limits added twice")
	}
	if backup != "" {
		t.Fatal("backup created without a change")
	}
}

func TestSummaryOnRebuildShowsFullBreakdown(t *testing.T) {
	info := engine.Info{
		Files:   3,
		Stats:   corpus.Stats{Kept: 100, Multiline: 5, Dropped: 10, Filtered: 2},
		Rebuilt: true,
		Elapsed: 12 * time.Millisecond,
	}
	got := Summary(info)
	for _, want := range []string{
		"100 entries", "3 sources", "5 multiline joined",
		"10 duplicates dropped", "2 excluded by filters", "indexed in 12ms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestSummaryOnReadPathOmitsUnmeasuredCounts(t *testing.T) {
	info := engine.Info{
		Files:   3,
		Stats:   corpus.Stats{Kept: 100},
		Rebuilt: false,
		Elapsed: 6 * time.Millisecond,
	}
	got := Summary(info)
	if strings.Contains(got, "multiline") || strings.Contains(got, "duplicate") || strings.Contains(got, "filter") {
		t.Fatalf("read path claims unmeasured stats: %q", got)
	}
	if strings.Contains(got, "indexed") {
		t.Fatalf("read path claims to have indexed: %q", got)
	}
	if !strings.Contains(got, "100 entries") || !strings.Contains(got, "3 sources") {
		t.Fatalf("missing the facts this path did establish: %q", got)
	}
	if !strings.Contains(got, "up to date") {
		t.Fatalf("missing the up-to-date note: %q", got)
	}
}

func TestSummaryStaysQuietWhenEveryFilterCompiles(t *testing.T) {
	info := engine.Info{Files: 2, Stats: corpus.Stats{Kept: 10}, Rebuilt: true, Elapsed: time.Millisecond}
	if got := Summary(info); strings.Contains(got, "ignored") || strings.Contains(got, "urd:") {
		t.Fatalf("reports an ignored pattern although there was none: %q", got)
	}
	if got := Warnings(info); len(got) != 0 {
		t.Fatalf("warnings printed although every pattern compiled: %v", got)
	}
}

func TestSummaryAgreesOnSingleSourceAndSubMillisecond(t *testing.T) {
	one := Summary(engine.Info{Files: 1, Stats: corpus.Stats{Kept: 1}, Rebuilt: true, Elapsed: 700 * time.Microsecond})
	if !strings.Contains(one, "1 entry from 1 source,") {
		t.Errorf("plural disagreement on a single entry and source: %q", one)
	}
	if strings.Contains(one, "in 0s") {
		t.Errorf("sub-millisecond work reported as zero: %q", one)
	}
	if !strings.Contains(one, "700µs") {
		t.Errorf("missing the measured duration: %q", one)
	}
	read := Summary(engine.Info{Files: 1, Stats: corpus.Stats{Kept: 3}, Elapsed: 900 * time.Microsecond})
	if !strings.Contains(read, "from 1 source,") || strings.Contains(read, "in 0s") {
		t.Errorf("read path: %q", read)
	}
	many := Summary(engine.Info{Files: 3, Stats: corpus.Stats{Kept: 3}, Elapsed: time.Millisecond})
	if !strings.Contains(many, "from 3 sources,") {
		t.Errorf("plural lost for several sources: %q", many)
	}
}

func TestSyncDirHintNamesTheImportedDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	got := SyncDirHint()
	if !strings.Contains(got, filepath.Join(".local", "share", "urd", "imported")) {
		t.Fatalf("hint does not name the sync dir: %q", got)
	}
}

func TestRCPathAndHookLineFollowTheShell(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	if got := HookLine("urd", Bash); got != `eval "$(urd hook bash)"` {
		t.Errorf("bash hook line = %q", got)
	}
	if got := HookLine("urd", Zsh); got != `eval "$(urd hook zsh)"` {
		t.Errorf("zsh hook line = %q", got)
	}
	if got := RCPath(Bash); !strings.HasSuffix(got, "/.bashrc") {
		t.Errorf("bash rc = %q", got)
	}
	if got := RCPath(Zsh); !strings.HasSuffix(got, "/.zshrc") {
		t.Errorf("zsh rc = %q", got)
	}
	t.Setenv("ZDOTDIR", "/tmp/zdot")
	if got := RCPath(Zsh); got != "/tmp/zdot/.zshrc" {
		t.Errorf("ZDOTDIR ignored: %q", got)
	}
	if got := RCPath(Bash); got == "/tmp/zdot/.bashrc" {
		t.Errorf("ZDOTDIR moved bash's rc: %q", got)
	}
}

func TestHistLimitsFollowTheShell(t *testing.T) {
	if got := HistLimitLines(40000, Bash); !strings.Contains(got, "HISTFILESIZE=40000") || strings.Contains(got, "SAVEHIST") {
		t.Errorf("bash lines = %q", got)
	}
	if got := HistLimitLines(40000, Zsh); !strings.Contains(got, "SAVEHIST=40000") || strings.Contains(got, "HISTFILESIZE") {
		t.Errorf("zsh lines = %q", got)
	}
	if got := HistLimitNames(Bash); got != "HISTSIZE/HISTFILESIZE" {
		t.Errorf("bash names = %q", got)
	}
}

func TestDetectAlwaysNamesASupportedShell(t *testing.T) {
	for _, sh := range []string{"", "/bin/bash", "/bin/zsh", "/usr/bin/fish", "nonsense"} {
		t.Setenv("SHELL", sh)
		switch got := Detect(); got {
		case Zsh, Bash:
		default:
			t.Errorf("SHELL=%q gave %q", sh, got)
		}
	}
}

func TestDetectPrefersTheParentOverSHELL(t *testing.T) {
	name := parentName()
	if shellOf(name) == "" {
		t.Skipf("parent %q is not a shell here", name)
	}
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := Detect(); got != shellOf(name) {
		t.Errorf("Detect() = %q, parent %q says %q", got, name, shellOf(name))
	}
}

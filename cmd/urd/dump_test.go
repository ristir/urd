package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/engine"
)

func dumpEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestRunDmpWritesToStdoutWithExplicitDash(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"-"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), `"cmd":"ll"`) {
		t.Fatalf("dump = %q", out.String())
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "secret") {
		t.Fatalf("no warning about secrets: %q", errOut.String())
	}
}

func TestRunDmpRejectsUnrecognisedFlag(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"--nonsense"}); code == 0 {
		t.Fatalf("exit 0, want non-zero for a typo'd flag")
	}
	if !strings.Contains(errOut.String(), "--nonsense") {
		t.Fatalf("error does not name the offending argument: %q", errOut.String())
	}
}

func TestRunDmpToExplicitFileIs0600(t *testing.T) {
	home := dumpEnv(t)
	p := filepath.Join(home, "out.jsonl")
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{p}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", st.Mode().Perm())
	}
}

func TestRunDmpWithNoArgsWritesDefaultHomeFile(t *testing.T) {
	home := dumpEnv(t)
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	matches, err := filepath.Glob(filepath.Join(home, "urd_history_*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("home dir dumps = %v, want exactly one", matches)
	}
	st, err := os.Stat(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", st.Mode().Perm())
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cmd":"ll"`) {
		t.Fatalf("default dump = %q", data)
	}
}

func TestRunDmpIgnoresExcludeFilters(t *testing.T) {
	home := dumpEnv(t)
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n: 200:0;history | grep ans\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"-"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "history | grep ans") {
		t.Fatalf("dump dropped a command matching the default exclude filter: %q", out.String())
	}
}

func TestRunDmpLoadCopiesIntoImportedAndBecomesSource(t *testing.T) {
	home := dumpEnv(t)
	src := filepath.Join(home, "other-machine")
	if err := os.WriteFile(src, []byte(": 500:0;echo imported-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"load", src}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	files, err := os.ReadDir(config.ImportedDir())
	if err != nil || len(files) != 1 {
		t.Fatalf("imported dir = %v err = %v", files, err)
	}
	st, err := os.Stat(filepath.Join(config.ImportedDir(), files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", st.Mode().Perm())
	}

	c, _, err := engine.Rebuild(mustConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range c.Items {
		if it.Cmd == "echo imported-marker" {
			found = true
		}
	}
	if !found {
		t.Fatal("imported command did not become part of the corpus")
	}
}

func TestRunDmpLoadWithNoArgsPicksTheNewestDefaultDump(t *testing.T) {
	home := dumpEnv(t)
	older := filepath.Join(home, "urd_history_20260101-0000")
	newer := filepath.Join(home, "urd_history_20260806-2215")
	if err := os.WriteFile(older, []byte(`{"cmd":"echo old-dump","ts":1,"source":"a"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(`{"cmd":"echo new-dump","ts":2,"source":"a"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"load"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), filepath.Base(newer)) {
		t.Fatalf("did not load the newest default dump (%s): %q", newer, out.String())
	}
	c, _, err := engine.Rebuild(mustConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range c.Items {
		if it.Cmd == "echo new-dump" {
			found = true
		}
	}
	if !found {
		t.Fatal("the newer default dump's entry did not become part of the corpus")
	}
}

func TestRunDmpLoadWithNoArgsAndNoDefaultDumpFails(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	code := runDmp(&out, &errOut, []string{"load"})
	if code == 0 {
		t.Fatal("exit 0, want a non-zero code when no default dump exists")
	}
	if errOut.Len() == 0 {
		t.Fatal("silent about the missing default dump")
	}
}

func TestRunDmpLoadPreservesSourceThroughRoundTrip(t *testing.T) {
	home := dumpEnv(t)
	src := filepath.Join(home, "remote-dump.jsonl")
	line := `{"cmd":"echo from-remote","ts":700,"source":"stg-10.adv.nl.lab"}` + "\n"
	if err := os.WriteFile(src, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"load", src}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	files, err := os.ReadDir(config.ImportedDir())
	if err != nil || len(files) != 1 {
		t.Fatalf("imported dir = %v err = %v", files, err)
	}
	imported := filepath.Join(config.ImportedDir(), files[0].Name())

	data, err := os.ReadFile(imported)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "stg-10.adv.nl.lab") {
		t.Fatalf("source did not survive the round trip: %s", data)
	}
}

func TestRunDmpExportZshWritesHistfileFormatToStdout(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"export", "zsh"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), ": 100:0;ll") {
		t.Fatalf("export = %q", out.String())
	}
}

func TestRunDmpExportBashWritesHistfileFormatToFile(t *testing.T) {
	home := dumpEnv(t)
	p := filepath.Join(home, "exported.bash_history")
	var out, errOut bytes.Buffer
	if code := runDmp(&out, &errOut, []string{"export", "bash", p}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ll") {
		t.Fatalf("export file = %q", data)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", st.Mode().Perm())
	}
}

func TestRunDmpExportRejectsUnknownShellName(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	code := runDmp(&out, &errOut, []string{"export", "fish"})
	if code == 0 {
		t.Fatal("exit 0, want non-zero for an unsupported shell")
	}
	if !strings.Contains(errOut.String(), `unsupported shell "fish"`) {
		t.Fatalf("error does not name fish as unsupported: %q", errOut.String())
	}
}

func TestRunDmpExportRequiresAShellArgument(t *testing.T) {
	dumpEnv(t)
	var out, errOut bytes.Buffer
	code := runDmp(&out, &errOut, []string{"export"})
	if code == 0 {
		t.Fatal("exit 0, want non-zero without a shell argument")
	}
	if !strings.Contains(errOut.String(), "expected zsh or bash") {
		t.Fatalf("error = %q", errOut.String())
	}
}

func mustConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunDmpLoadKeepsEveryImportWithTheSameBasename(t *testing.T) {
	home := dumpEnv(t)
	hosts := []string{"stg-10.adv.nl.lab", "stg-11.adv.nl.lab", "stg-12.adv.nl.lab"}

	var srcs []string
	for i, host := range hosts {
		dir := filepath.Join(home, "archive", host, "root")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, ".bash_history")
		body := fmt.Sprintf("#17000000%02d\necho only-on-%s\n", i+1, host)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}

	targets := map[string]bool{}
	for _, p := range srcs {
		var out, errOut bytes.Buffer
		if code := runDmp(&out, &errOut, []string{"load", p}); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		targets[out.String()] = true
	}
	if len(targets) != len(srcs) {
		t.Fatalf("%d imports reported only %d distinct targets: %v", len(srcs), len(targets), targets)
	}

	files, err := os.ReadDir(config.ImportedDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(srcs) {
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Fatalf("imported %d files, %d survived: %v", len(srcs), len(files), names)
	}

	c, _, err := engine.Rebuild(mustConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range hosts {
		want := "echo only-on-" + host
		found := false
		for _, it := range c.Items {
			if it.Cmd == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q is not searchable: an earlier import was overwritten", want)
		}
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCfgShowsDefaultsWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	var out, errOut bytes.Buffer
	if code := runCfg(&out, &errOut, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got := out.String()
	path := filepath.Join(dir, "urd", "config.toml")
	if !strings.Contains(got, path) {
		t.Fatalf("output does not name the config path: %q", got)
	}
	if !strings.Contains(got, "does not exist yet") {
		t.Fatalf("output does not say the file is missing: %q", got)
	}
	if !strings.Contains(got, "[engine]") {
		t.Fatalf("output does not show the defaults: %q", got)
	}
}

func TestRunCfgPrintsExistingFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "urd")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(confDir, "config.toml")
	content := "[engine]\n  mode = \"daemon\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runCfg(&out, &errOut, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	want := path + "\n" + content
	if !strings.HasPrefix(out.String(), want) {
		t.Fatalf("got %q, want it to start with %q", out.String(), want)
	}
	rest := strings.TrimPrefix(out.String(), want)
	for _, line := range []string{"# in effect but not in the file:", `# search.delimiters = "-_/.,;:="`} {
		if !strings.Contains(rest, line) {
			t.Errorf("output lacks %q:\n%s", line, rest)
		}
	}
	if strings.Contains(rest, "engine.mode") {
		t.Errorf("output names engine.mode, which is in the file:\n%s", rest)
	}
}

func TestRunCfgEditCreatesMissingFileWithoutOpeningEditor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	var out, errOut bytes.Buffer
	if code := runCfg(&out, &errOut, []string{"edit"}); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	path := filepath.Join(dir, "urd", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not created by edit: %v", err)
	}
}

func TestRunCfgWarnsAndContinuesOnBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "urd")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(confDir, "config.toml")
	broken := "mode = [unterminated\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = runCfg(&out, &errOut, nil)
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(stderr, path) {
		t.Fatalf("warning does not name the config path: %q", stderr)
	}
	if !strings.Contains(out.String(), broken) {
		t.Fatalf("urd cfg aborted instead of showing the broken file: %q", out.String())
	}
}

func TestRunCfgEditReportsStatErrorsOtherThanMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "urd"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := runCfg(&out, &errOut, []string{"edit"})
	if code == 0 {
		t.Fatalf("expected non-zero exit on unexpected stat error, stderr=%q", errOut.String())
	}
	if errOut.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}

func TestRunCfgNamesAMisspelledKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "urd")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(confDir, "config.toml")
	if err := os.WriteFile(path, []byte("[colors]\nuiltin = \"fg=red\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = runCfg(&out, &errOut, nil)
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(stderr, "colors.uiltin") {
		t.Fatalf("does not name the misspelled key: %q", stderr)
	}
}

func TestRunCfgRejectsUnknownFlagByName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	var out, errOut bytes.Buffer
	code := runCfg(&out, &errOut, []string{"--nonsense"})
	if code == 0 {
		t.Fatalf("exit 0, want non-zero for a typo'd flag")
	}
	if !strings.Contains(errOut.String(), "--nonsense") {
		t.Fatalf("error does not name the offending flag: %q", errOut.String())
	}
}

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunQueryPrintsProtocolResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("HISTFILE", "")
	hist := ": 100:0;ansible-playbook playbooks/rate-limit.yml -e APP_VERSION=ratelimit:7eb83dc3\n"
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(hist), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runQuery(&out, []string{"0", "ans", "ratel"}); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "urd1 1 0") {
		t.Fatalf("head = %q", lines[0])
	}
	if !strings.Contains(lines[1], "rate-limit.yml") {
		t.Fatalf("cmd = %q", lines[1])
	}
	if lines[2] == "" {
		t.Fatal("expected highlight spans")
	}
}

func TestRunQueryNoMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("HISTFILE", "")
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runQuery(&out, []string{"0", "nosuchthing"}); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out.String() != "urd1 0 0\n\n\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestRunQueryNoArgsUsesProtocolEncoder(t *testing.T) {
	var out bytes.Buffer
	if code := runQuery(&out, nil); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out.String() != "urd1 0 0\n\n\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestRunQueryAnswersWhenIndexWriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dataFile := filepath.Join(home, "data-is-a-file")
	if err := os.WriteFile(dataFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataFile)
	t.Setenv("HISTFILE", "")
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runQuery(&out, []string{"0", "ll"}); code != 0 {
		t.Fatalf("exit code %d, want 0: corpus was still usable despite the cache write failure", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "urd1 1 0") {
		t.Fatalf("head = %q", lines[0])
	}
	if !strings.Contains(lines[1], "ll") {
		t.Fatalf("cmd = %q", lines[1])
	}
}

func TestRunQueryStaysSilentOnBrokenConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("HISTFILE", "")
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	confDir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"), []byte("mode = [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	var out bytes.Buffer
	code := runQuery(&out, []string{"0", "ll"})
	w.Close()
	os.Stderr = old
	msg, _ := io.ReadAll(r)

	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if len(msg) != 0 {
		t.Fatalf("hot path must stay silent about a broken config: %q", msg)
	}
}

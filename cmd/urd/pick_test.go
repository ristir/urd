package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pickEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	hist := ": 100:0;echo alpha rate-limit\n: 200:0;echo beta ratelimit\n"
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(hist), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunPickReturnsSelectedCommandOnStdout(t *testing.T) {
	pickEnv(t)
	var out, tty bytes.Buffer
	if code := runPick(&out, strings.NewReader("ratel\n\n"), &tty, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, tty.String())
	}
	if got := strings.TrimSpace(out.String()); got != "echo beta ratelimit" {
		t.Fatalf("stdout = %q", got)
	}
	// "rate-limit" is a deliberate decoy: the dash breaks the substring "ratel", so only one line matches.
	if !strings.Contains(tty.String(), "[1/1]") {
		t.Fatalf("no candidate counter on the tty: %q", tty.String())
	}
}

func TestRunPickEmptyOnNoMatch(t *testing.T) {
	pickEnv(t)
	var out, tty bytes.Buffer
	if code := runPick(&out, strings.NewReader("nosuch\n\n"), &tty, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestRunPickAbortsOnEmptyFirstInput(t *testing.T) {
	pickEnv(t)
	var out, tty bytes.Buffer
	if code := runPick(&out, strings.NewReader("\n"), &tty, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

// Checked on bash 3.2.57 and 5.3.3: while a bind -x callback runs, ICRNL is off and Enter
// arrives as a bare '\r', on which ReadString('\n') would never have returned.
func TestRunPickAcceptsBareCarriageReturn(t *testing.T) {
	pickEnv(t)
	var out, tty bytes.Buffer
	if code := runPick(&out, strings.NewReader("ratel\r\r"), &tty, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, tty.String())
	}
	if got := strings.TrimSpace(out.String()); got != "echo beta ratelimit" {
		t.Fatalf("stdout = %q", got)
	}
}

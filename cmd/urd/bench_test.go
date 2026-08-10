package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBenchReportsBothModes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	var hist strings.Builder
	for i := 0; i < 500; i++ {
		hist.WriteString(": ")
		hist.WriteString(strings.Repeat("1", 1))
		hist.WriteString("00")
		hist.WriteString(":0;ansible-playbook rate-limit.yml -e APP_VERSION=v")
		hist.WriteString(strings.Repeat("x", i%20+1))
		hist.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(hist.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runBench(&out, &errOut, []string{"--query", "ans ratel", "--runs", "3"}); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{"oneshot", "daemon", "median", "p99", "keystrokes"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
}

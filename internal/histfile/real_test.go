package histfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseZshRealFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	data, err := os.ReadFile(filepath.Join(home, ".zsh_history"))
	if err != nil {
		t.Skip("no ~/.zsh_history")
	}
	got := ParseZsh(data, "real")
	if len(got) == 0 {
		t.Fatal("parsed zero entries from a non-empty history")
	}
	multi := 0
	for _, e := range got {
		if strings.Contains(e.Cmd, "\n") {
			multi++
		}
	}
	t.Logf("entries=%d multiline=%d", len(got), multi)
	if multi == 0 {
		t.Skip("this history has no continuation lines, nothing to observe about joining here")
	}
}

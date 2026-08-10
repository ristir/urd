package corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ristir/urd/internal/config"
)

func TestDiscoverFindsAutoSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Discover(config.Sources{Auto: true})
	if len(got) != 1 || filepath.Base(got[0]) != ".zsh_history" {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverSkipsMissingAndDedupes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	live := filepath.Join(home, ".zsh_history")
	if err := os.WriteFile(live, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTFILE", live)
	got := Discover(config.Sources{Auto: true, Extra: []string{live}})
	if len(got) != 1 {
		t.Fatalf("got %v, want deduped single path", got)
	}
}

func TestExpandGlobDoubleStar(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(deep, "zsh_history_202505")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "zsh_history*"))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "hist_one")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob("~/hist_*")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobDoubleStarWithSeparatorInTail(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "bash_history", "host-a", "host-a", "root")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(deep, ".bash_history")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "root", ".bash_history"))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobTwoDoubleStars(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "bash_history", "host-a", "host-a", "root")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(deep, ".bash_history")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "host-a", "**", ".bash_history"))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobDoubleStarMatchesZeroSegments(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "direct.txt")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "direct.txt"))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobDoubleStarNonMatchingTail(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "bash_history", "host-a", "host-a", "root")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, ".bash_history"), []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "root", ".psql_history"))
	if len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestExpandGlobAbsolutePatternWithLiteralPrefix(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(deep, "target.txt")
	if err := os.WriteFile(want, []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandGlob(filepath.Join(root, "**", "target.txt"))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestExpandGlobRelativePatternFallsBackToWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "target.txt"), []byte("ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	got := ExpandGlob(filepath.Join("**", "target.txt"))
	want := filepath.Join("a", "b", "target.txt")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ristir/urd/internal/setup"
)

// Detection falls through to $SHELL under go test, and the suite would follow the machine's login shell.
func TestMain(m *testing.M) {
	detectShell = func() setup.Shell { return setup.Zsh }
	os.Exit(m.Run())
}

func TestBootBinIsAnAbsolutePath(t *testing.T) {
	if !filepath.IsAbs(bootBin) {
		t.Fatalf("bootBin = %q, want an absolute path", bootBin)
	}
}

func TestComposeVersion(t *testing.T) {
	const at = "2026-08-10T09:42:11Z"
	for _, c := range []struct{ ver, rev, built, want string }{
		{"v0.1.0", "a97293f", at, "v0.1.0 (a97293f, " + at + ")"},
		{"v0.1.0-3-ga97293f", "a97293f", at, "v0.1.0-3-ga97293f (" + at + ")"},
		{"a97293f", "a97293f", at, "a97293f (" + at + ")"},
		{"v0.1.0", "a97293f", "", "v0.1.0 (a97293f)"},
		{"v0.1.0", "", "", "v0.1.0"},
		{"", "a97293f", at, "a97293f, " + at},
		{"", "", at, at},
		{"", "", "", "dev"},
	} {
		if got := composeVersion(c.ver, c.rev, c.built); got != c.want {
			t.Errorf("composeVersion(%q, %q, %q) = %q, want %q", c.ver, c.rev, c.built, got, c.want)
		}
	}
}

func TestBuildVersionIsNeverEmpty(t *testing.T) {
	if got := buildVersion(); got == "" {
		t.Fatal("buildVersion() is empty")
	}
}

func TestDetectShellIsPinnedForTests(t *testing.T) {
	if detectShell() != setup.Zsh {
		t.Fatalf("the suite runs with %q pinned, want zsh", detectShell())
	}
}

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ristir/urd/internal/config"
)

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("HISTFILE", "")
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"),
		[]byte(": 100:0;ansible-playbook rate-limit.yml\n: 200:0;kubectl get pods\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestLoadBuildsWhenIndexMissing(t *testing.T) {
	setupHome(t)
	c, info, err := Load(config.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(c.Items))
	}
	if !info.Rebuilt || info.Files != 1 {
		t.Fatalf("info = %+v", info)
	}
	if _, err := os.Stat(config.IndexPath()); err != nil {
		t.Fatalf("index not written: %v", err)
	}
}

func TestLoadUsesIndexOnSecondCall(t *testing.T) {
	setupHome(t)
	if _, _, err := Load(config.Default(), false); err != nil {
		t.Fatal(err)
	}
	_, info, err := Load(config.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rebuilt {
		t.Fatal("second call rebuilt instead of reading the index")
	}
}

func TestLoadDoesNotRebuildStaleIndexWhenForbidden(t *testing.T) {
	home := setupHome(t)
	if _, _, err := Load(config.Default(), false); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(home, ".zsh_history"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(": 300:0;echo fresh\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	c, info, err := Load(config.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rebuilt {
		t.Fatal("stale index rebuilt on the hot path")
	}
	if len(c.Items) != 2 {
		t.Fatalf("got %d items, want the stale 2", len(c.Items))
	}
}

func TestLoadRebuildsStaleIndexWhenAllowed(t *testing.T) {
	home := setupHome(t)
	if _, _, err := Load(config.Default(), false); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(home, ".zsh_history"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(": 300:0;echo fresh\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	c, info, err := Load(config.Default(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Rebuilt || len(c.Items) != 3 {
		t.Fatalf("info = %+v items = %d", info, len(c.Items))
	}
}

func TestLoadCorruptIndexFallsBackToRebuild(t *testing.T) {
	setupHome(t)
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.IndexPath(), []byte("XXXX broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, info, err := Load(config.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Rebuilt || len(c.Items) != 2 {
		t.Fatalf("info = %+v items = %d", info, len(c.Items))
	}
}

func TestLoadRebuildsWhenFiltersChange(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = nil
	if _, _, err := Load(cfg, true); err != nil {
		t.Fatal(err)
	}
	cfg.Search.Exclude = []string{"kubectl"}
	c, info, err := Load(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Rebuilt {
		t.Fatal("changed filters did not invalidate the index")
	}
	for _, it := range c.Items {
		if strings.Contains(it.Cmd, "kubectl") {
			t.Fatalf("filtered command survived: %q", it.Cmd)
		}
	}
}

func TestRefresherRebuildsWhenConfigFiltersChange(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = nil
	if _, _, err := Load(cfg, true); err != nil {
		t.Fatal(err)
	}
	r := NewRefresher()
	if c, err := r.Next(cfg); err != nil || c != nil {
		t.Fatalf("nothing changed, but Next returned %v (err %v)", c, err)
	}

	cfg.Search.Exclude = []string{"kubectl"}
	c, err := r.Next(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("filters changed in the config and Next reported nothing to do")
	}
	for _, it := range c.Items {
		if strings.Contains(it.Cmd, "kubectl") {
			t.Fatalf("filtered command survived: %q", it.Cmd)
		}
	}
}

func TestRefresherPicksUpAnExternalRebuild(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = nil
	if _, _, err := Load(cfg, true); err != nil {
		t.Fatal(err)
	}
	r := NewRefresher()

	cfg.Search.Exclude = []string{"kubectl"}
	if _, _, err := Rebuild(cfg); err != nil {
		t.Fatal(err)
	}
	if StaleNow(cfg) {
		t.Fatal("the fixture is wrong: the index must already agree with the config here")
	}

	c, err := r.Next(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("the index was rewritten by another process and Next reported nothing to do")
	}
	for _, it := range c.Items {
		if strings.Contains(it.Cmd, "kubectl") {
			t.Fatalf("stale corpus kept serving: %q", it.Cmd)
		}
	}
}

func TestRebuildReportsFilteredCount(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = []string{"kubectl"}
	_, info, err := Rebuild(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if info.Stats.Filtered != 1 {
		t.Fatalf("filtered = %d, want 1", info.Stats.Filtered)
	}
}

func TestLoadNamesBadFilterPatternsOnTheReadPath(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = []string{"*bad(", "^history"}

	_, info, err := Load(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Rebuilt {
		t.Fatal("the fixture is wrong: the first call must rebuild")
	}
	if len(info.BadFilters) != 1 || info.BadFilters[0] != "*bad(" {
		t.Fatalf("rebuild path: BadFilters = %v", info.BadFilters)
	}

	_, info, err = Load(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rebuilt {
		t.Fatal("the fixture is wrong: the index must be current on the second call")
	}
	if len(info.BadFilters) != 1 || info.BadFilters[0] != "*bad(" {
		t.Fatalf("read path stayed silent about the pattern: BadFilters = %v", info.BadFilters)
	}
}

func TestUnfilteredIgnoresExcludeAndLeavesTheIndexAlone(t *testing.T) {
	home := setupHome(t)
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"),
		[]byte(": 100:0;ansible-playbook rate-limit.yml\n: 200:0;kubectl get pods\n: 300:0;history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Search.Exclude = []string{"^history"}

	c, _, err := Load(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range c.Items {
		if it.Cmd == "history" {
			t.Fatalf("fixture is wrong: the filtered index already has %q", it.Cmd)
		}
	}

	uc, _ := Unfiltered(cfg)
	found := false
	for _, it := range uc.Items {
		if it.Cmd == "history" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unfiltered build dropped an excluded command: %+v", uc.Items)
	}

	c2, info2, err := Load(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if info2.Rebuilt {
		t.Fatal("Unfiltered rebuilt the search index")
	}
	for _, it := range c2.Items {
		if it.Cmd == "history" {
			t.Fatalf("the search index picked up the excluded command: %+v", c2.Items)
		}
	}
}

func TestLoadStaysQuietWhenEveryFilterCompiles(t *testing.T) {
	setupHome(t)
	cfg := config.Default()
	cfg.Search.Exclude = []string{"^history", "^urd"}
	if _, _, err := Load(cfg, true); err != nil {
		t.Fatal(err)
	}
	_, info, err := Load(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rebuilt {
		t.Fatal("the fixture is wrong: the index must be current here")
	}
	if len(info.BadFilters) != 0 {
		t.Fatalf("named a pattern although all of them compile: %v", info.BadFilters)
	}
}

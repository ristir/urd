package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsMatchSpec(t *testing.T) {
	c := Default()
	if c.Engine.Mode != "daemon" {
		t.Fatalf("mode = %q, want daemon", c.Engine.Mode)
	}
	if !c.Sources.Auto {
		t.Fatal("sources.auto should default to true")
	}
	if c.UI.Indicator != "suffix" || c.UI.Trigger != "urd" {
		t.Fatalf("ui = %+v", c.UI)
	}
	if c.UI.Hotkey != "" || c.UI.StealCtrlR {
		t.Fatalf("hotkey must be unbound by default: %+v", c.UI)
	}
	if c.IdleDuration() != time.Hour {
		t.Fatalf("idle = %v, want 1h", c.IdleDuration())
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c := Default()
	c.UI.Trigger = "hish"
	c.Sources.Extra = []string{"~/arch/**/zsh_history*"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "urd", "config.toml")); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.UI.Trigger != "hish" || len(got.Sources.Extra) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := Load()
	if err != nil {
		t.Fatalf("missing config must not be an error: %v", err)
	}
	if got.UI.Trigger != "urd" {
		t.Fatalf("got %+v", got)
	}
}

func TestIdleDurationFallsBackOnGarbage(t *testing.T) {
	c := Default()
	c.Engine.IdleTimeout = "not-a-duration"
	if c.IdleDuration() != time.Hour {
		t.Fatalf("got %v, want 1h fallback", c.IdleDuration())
	}
}

func TestDefaultFiltersExcludeAnchoredHistoryAndUrd(t *testing.T) {
	c := Default()
	if len(c.Search.Exclude) != 2 || c.Search.Exclude[0] != "^history" || c.Search.Exclude[1] != "^urd" {
		t.Fatalf("filters = %+v, want [^history ^urd]", c.Filters)
	}
}

func TestFiltersSurviveRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := Default()
	c.Search.Exclude = []string{"^history", "secret"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Search.Exclude) != 2 || got.Search.Exclude[0] != "^history" {
		t.Fatalf("got %+v", got.Filters)
	}
}

func TestSaveWithBackupAbortsWithoutTouchingTheConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# hand-written\n[ui]\ntrigger = \"hist\"\n"
	if err := os.WriteFile(Path(), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(Dir(), 0o755) })

	if _, err := SaveWithBackup(Default()); err == nil {
		t.Fatal("expected an error when the backup could not be written")
	}
	if err := os.Chmod(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("the config changed despite the backup failure:\n%s", got)
	}
}

func TestSaveWithBackupKeepsThePreviousContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# hand-written\n[ui]\ntrigger = \"hist\"\n"
	if err := os.WriteFile(Path(), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	backup, err := SaveWithBackup(Default())
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("no backup path returned although the file existed")
	}
	saved, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != original {
		t.Fatalf("backup does not hold the original:\n%s", saved)
	}
}

func TestSaveWithBackupOnAFreshInstall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	backup, err := SaveWithBackup(Default())
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("backup %q created although there was no config", backup)
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Fatalf("config not created: %v", err)
	}
}

func TestDefaultColors(t *testing.T) {
	c := Default()
	want := Colors{Prompt: "fg=cyan,bold", Mark: "fg=white,bold", Builtin: "fg=green", Hint: "fg=8", Query: "underline"}
	if c.Colors != want {
		t.Fatalf("colors = %+v, want %+v", c.Colors, want)
	}
}

func TestColorsSurviveRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := Default()
	c.Colors.Mark = "fg=#8fbf7f"
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Colors.Mark != "fg=#8fbf7f" {
		t.Fatalf("mark = %q", got.Colors.Mark)
	}
}

func writeConfigFile(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMetaNamesAMisspelledKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\nuiltin = \"fg=red\"\n")

	c, unknown, err := LoadMeta()
	if err != nil {
		t.Fatalf("a misspelled key must not be a hard error: %v", err)
	}
	if len(unknown) != 1 || unknown[0] != "colors.uiltin" {
		t.Fatalf("unknown = %v, want [colors.uiltin]", unknown)
	}
	if c.Colors.Builtin != Default().Colors.Builtin {
		t.Fatalf("builtin = %q, the misspelled key must not have reached it", c.Colors.Builtin)
	}
}

func TestLoadMetaStaysQuietWhenEveryKeyIsKnown(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\nbuiltin = \"fg=red\"\n")

	_, unknown, err := LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

func TestLoadMetaKeepsKnownSiblingFieldsOfAMisspelledSection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\nmark = \"fg=magenta\"\nuiltin = \"fg=red\"\n")

	c, unknown, err := LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 1 || unknown[0] != "colors.uiltin" {
		t.Fatalf("unknown = %v", unknown)
	}
	if c.Colors.Mark != "fg=magenta" {
		t.Fatalf("mark = %q, the sibling key should have loaded", c.Colors.Mark)
	}
}

func TestLoadMetaOnBrokenSyntaxReportsNoUnknownKeysAndFallsBackToDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "mode = [unterminated\n")

	c, unknown, err := LoadMeta()
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none on a syntax error", unknown)
	}
	if c.Colors != Default().Colors || c.UI != Default().UI {
		t.Fatalf("c = %+v, want pure defaults on a syntax error", c)
	}
}

func TestLoadIgnoresUnknownKeysAndKeepsWorking(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\nmark = \"fg=magenta\"\nuiltin = \"fg=red\"\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Colors.Mark != "fg=magenta" {
		t.Fatalf("mark = %q", c.Colors.Mark)
	}
}

func TestAbsentNamesKeysMissingFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "[engine]\n  mode = \"daemon\"\n  idle_timeout = \"1h\"\n[filters]\n  exclude = [\"^history\"]\n"
	if err := os.WriteFile(Path(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(Absent(), "\n")
	for _, want := range []string{
		`search.delimiters = "-_/.,;:="`,
		`colors.query = "underline"`,
		`ui.trigger = "urd"`,
		`sources.auto = true`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Absent() lacks %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"engine.mode", "filters.exclude"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("Absent() names %q, which is in the file:\n%s", unwanted, joined)
		}
	}
}

func TestAbsentIsEmptyWhenEveryKeyIsWritten(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Default()); err != nil {
		t.Fatal(err)
	}
	if got := Absent(); len(got) != 0 {
		t.Fatalf("Absent() = %q, want none after a full Save", got)
	}
}

func TestAbsentNamesEveryKeyWithoutAFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	joined := strings.Join(Absent(), "\n")
	for _, want := range []string{`engine.mode = "daemon"`, `search.delimiters = "-_/.,;:="`} {
		if !strings.Contains(joined, want) {
			t.Errorf("Absent() lacks %q:\n%s", want, joined)
		}
	}
}

func TestAbsentSaysNothingAboutABrokenFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("[engine\nmode ="), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Absent(); got != nil {
		t.Fatalf("Absent() = %q on a broken file", got)
	}
}

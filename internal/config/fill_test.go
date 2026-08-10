package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestFillNeverStartsTheFileWithABlankLine(t *testing.T) {
	for name, start := range map[string]string{
		"no file":             "",
		"empty file":          "\n",
		"comment only":        "# mine\n",
		"one section":         "[engine]\n  mode = \"daemon\"\n",
		"legacy first":        "[filters]\n  exclude = [\"^history\"]\n\n[engine]\n  mode = \"daemon\"\n",
		"blank in the middle": "[engine]\n  mode = \"daemon\"\n\n\n[colors]\n  hint = \"fg=8\"\n",
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if start != "" {
			writeConfigFile(t, start)
		} else if err := os.MkdirAll(Dir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := Fill(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := readConfig(t)
		if strings.HasPrefix(got, "\n") {
			t.Errorf("%s: the file starts with a blank line:\n%s", name, got)
		}
	}
}

func TestFillWritesHintsAtTheEndOfTheLine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\n  mark = \"fg=white,bold\"\n")

	if _, _, _, err := Fill(); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	for i, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			t.Errorf("line %d is a comment on its own: %q", i+1, l)
		}
	}
	if !strings.Contains(got, `query = "underline"  # underline | standout`) {
		t.Errorf("the key went in without its hint:\n%s", got)
	}
}

func TestEveryKeyHasAHint(t *testing.T) {
	ct := reflect.TypeOf(Config{})
	for i := 0; i < ct.NumField(); i++ {
		section := ct.Field(i).Tag.Get("toml")
		if _, legacy := legacySections[section]; legacy {
			continue
		}
		st := ct.Field(i).Type
		for j := 0; j < st.NumField(); j++ {
			if key := section + "." + st.Field(j).Tag.Get("toml"); hints[key] == "" {
				t.Errorf("no hint for %s", key)
			}
		}
	}
}

func TestTextRoundTrips(t *testing.T) {
	c := Default()
	c.Search.Exclude = []string{`^ssh .*\d+`, `^echo "hi"`}
	c.Colors.Mark = "fg=#8fbf7f"
	c.Sources.Extra = []string{"~/arch/**/*history*"}

	var got Config
	if _, err := toml.Decode(Text(c), &got); err != nil {
		t.Fatalf("Text does not parse: %v\n%s", err, Text(c))
	}
	if len(got.Search.Exclude) != 2 || got.Search.Exclude[0] != `^ssh .*\d+` {
		t.Errorf("exclude = %q", got.Search.Exclude)
	}
	if got.Search.Exclude[1] != `^echo "hi"` {
		t.Errorf("a quoted value did not survive: %q", got.Search.Exclude[1])
	}
	if got.Colors.Mark != "fg=#8fbf7f" || got.Sources.Extra[0] != "~/arch/**/*history*" {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(Text(c), "# daemon | oneshot") {
		t.Error("Text wrote no hints")
	}
	if strings.Contains(Text(c), "[filters]") {
		t.Errorf("Text wrote a legacy section:\n%s", Text(c))
	}
}

func TestFillInsertsIntoAnExistingSection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\n  mark = \"fg=white,bold\"\n")

	added, _, backup, err := Fill()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("no backup path returned although the file existed")
	}
	got := readConfig(t)
	if n := strings.Count(got, "[colors]"); n != 1 {
		t.Fatalf("[colors] appears %d times:\n%s", n, got)
	}
	if !strings.Contains(got, "query = \"underline\"") {
		t.Fatalf("colors.query was not written:\n%s", got)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("the filled config no longer loads: %v", err)
	}
	if c.Colors.Mark != "fg=white,bold" {
		t.Fatalf("mark = %q, the human's value was lost", c.Colors.Mark)
	}
	if len(added) == 0 {
		t.Fatal("Fill reported no keys although it wrote some")
	}
}

func TestFillAppendsAMissingSection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[engine]\n  mode = \"daemon\"\n")

	if _, _, _, err := Fill(); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	if !strings.Contains(got, "[search]") || !strings.Contains(got, `delimiters = "-_/.,;:="`) {
		t.Fatalf("[search] was not written:\n%s", got)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("the filled config no longer loads: %v", err)
	}
	if c.Search.Delimiters != "-_/.,;:=" || c.Engine.Mode != "daemon" {
		t.Fatalf("got %+v", c)
	}
}

func TestLegacyFiltersSectionStillFilters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[filters]\n  exclude = [\"^secret\", \"^history\"]\n")

	c, _, err := LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Search.Exclude) != 2 || c.Search.Exclude[0] != "^secret" {
		t.Fatalf("exclude = %q, the old section was ignored", c.Search.Exclude)
	}
}

func TestNewExcludeWinsOverTheLegacySection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[filters]\n  exclude = [\"^old\"]\n[search]\n  exclude = [\"^new\"]\n")

	c, _, err := LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Search.Exclude) != 1 || c.Search.Exclude[0] != "^new" {
		t.Fatalf("exclude = %q", c.Search.Exclude)
	}
}

func TestEmptyExcludeTurnsFilteringOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[search]\n  exclude = []\n")

	c, _, err := LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Search.Exclude) != 0 {
		t.Fatalf("exclude = %q, want none", c.Search.Exclude)
	}
}

func TestFillMovesTheLegacySection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[filters]\n  exclude = [\"^history\", \"^urd\"]\n\n[colors]\n  hint = \"fg=8\"\n")

	_, moved, _, err := Fill()
	if err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	if strings.Contains(got, "[filters]") {
		t.Fatalf("the old section is still there:\n%s", got)
	}
	if len(moved) != 1 || !strings.Contains(moved[0], "filters") {
		t.Fatalf("moved = %q, the move was not reported", moved)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("the filled config no longer loads: %v", err)
	}
	if len(c.Search.Exclude) != 2 || c.Search.Exclude[0] != "^history" {
		t.Fatalf("exclude = %q, the value did not survive the move", c.Search.Exclude)
	}
	if c.Colors.Hint != "fg=8" {
		t.Fatalf("hint = %q, a neighbouring section was damaged", c.Colors.Hint)
	}
}

func TestFillKeepsALegacySectionHoldingSomethingUnknown(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[filters]\n  exclude = [\"^history\"]\n  mine = 1\n")

	if _, moved, _, err := Fill(); err != nil {
		t.Fatal(err)
	} else if len(moved) != 0 {
		t.Fatalf("moved = %q, a section with an unknown key must stay", moved)
	}
	got := readConfig(t)
	if !strings.Contains(got, "mine = 1") {
		t.Fatalf("an unknown key was lost:\n%s", got)
	}
}

func TestFillKeepsCommentsAndOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	original := "# my notes\n[ui]\n  trigger = \"urd\"\n  # keep Ctrl-R mine\n  steal_ctrl_r = true\n"
	writeConfigFile(t, original)

	if _, _, _, err := Fill(); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t)
	for _, want := range []string{"# my notes", "# keep Ctrl-R mine", "trigger = \"urd\""} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "trigger") > strings.Index(got, "steal_ctrl_r") {
		t.Errorf("key order changed:\n%s", got)
	}
}

func TestFillIsIdempotent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[engine]\n  mode = \"daemon\"\n")

	if _, _, _, err := Fill(); err != nil {
		t.Fatal(err)
	}
	first := readConfig(t)
	added, _, backup, err := Fill()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || backup != "" {
		t.Fatalf("the second run wrote %v (backup %q)", added, backup)
	}
	if got := readConfig(t); got != first {
		t.Fatalf("the second run changed the file:\n%s", got)
	}
}

func TestFillMatchesTheFileIndentation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfigFile(t, "[colors]\nmark = \"fg=white,bold\"\n")

	if _, _, _, err := Fill(); err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(readConfig(t), "\n") {
		if strings.Contains(l, "query =") && strings.HasPrefix(l, " ") {
			t.Fatalf("indented into a file that does not indent: %q", l)
		}
	}
}

func TestFillRefusesABrokenFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	broken := "mode = [unterminated\n"
	writeConfigFile(t, broken)

	added, _, _, err := Fill()
	if err != nil {
		t.Fatalf("a broken file is not an error to report here: %v", err)
	}
	if len(added) != 0 {
		t.Fatalf("wrote %v into a file that does not parse", added)
	}
	if got := readConfig(t); got != broken {
		t.Fatalf("the broken file was rewritten:\n%s", got)
	}
}

func TestFillOutputAlwaysParses(t *testing.T) {
	for _, start := range []string{
		"",
		"[engine]\nmode=\"daemon\"\n",
		"[colors]\n\n\n",
		"# only a comment\n",
		"[ui]\ntrigger=\"urd\"\n[colors]\nhint=\"fg=8\"\n",
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if start != "" {
			writeConfigFile(t, start)
		} else if err := os.MkdirAll(Dir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := Fill(); err != nil {
			t.Fatalf("start %q: %v", start, err)
		}
		var c Config
		if _, err := toml.Decode(readConfig(t), &c); err != nil {
			t.Fatalf("start %q produced a file that does not parse: %v\n%s", start, err, readConfig(t))
		}
	}
}

func readConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

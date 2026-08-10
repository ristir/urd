package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ristir/urd/internal/shell"
)

func TestUsageOnlyAdvertisesDispatchedCommands(t *testing.T) {
	text := usageText()
	for name := range hiddenCommands {
		if strings.Contains(text, "urd "+name) {
			t.Errorf("usage leaks hidden command %q", name)
		}
	}
}

func TestUsageMentionsEveryDispatchedCommand(t *testing.T) {
	text := usageText()
	for name := range commands {
		if hiddenCommands[name] {
			continue
		}
		if !strings.Contains(text, "urd "+spellings(name)) {
			t.Errorf("usage does not mention dispatched command %q", name)
		}
	}
}

func TestHelpForCoversEveryDispatchedCommand(t *testing.T) {
	for name := range commands {
		if hiddenCommands[name] {
			continue
		}
		lines := helpFor(name)
		if len(lines) == 0 {
			t.Errorf("helpFor(%q) is empty, but the command is dispatched", name)
			continue
		}
		for _, l := range lines {
			if strings.TrimSpace(l.about) == "" {
				t.Errorf("helpTable entry %q has an empty description", l.usage())
			}
		}
	}
}

func TestHintTreeMatchesTheVisibleCommands(t *testing.T) {
	inTree := map[string]bool{}
	for _, c := range shell.Commands {
		inTree[c.Name] = true
		if len(helpFor(c.Name)) == 0 {
			t.Errorf("widget hints offer %q, but the help table says nothing about it", c.Name)
		}
	}
	for _, l := range helpTable {
		if l.name == "" {
			continue
		}
		if !inTree[l.name] {
			t.Errorf("help table advertises %q, but the widget will never hint it", l.name)
		}
	}
}

func TestEverySubcommandPrintsItsOwnHelp(t *testing.T) {
	for name := range commands {
		if hiddenCommands[name] {
			continue
		}
		var out, errOut bytes.Buffer
		if code := dispatch(&out, &errOut, name, []string{"--help"}); code != 0 {
			t.Errorf("%s --help exited %d", name, code)
		}
		if !strings.Contains(out.String(), "urd "+spellings(name)) {
			t.Errorf("%s --help does not describe itself: %q", name, out.String())
		}
	}
}

func TestEverySubcommandRejectsUnknownFlagByName(t *testing.T) {
	for name := range commands {
		if hiddenCommands[name] {
			continue
		}
		var out, errOut bytes.Buffer
		code := dispatch(&out, &errOut, name, []string{"--nonsense"})
		if code == 0 {
			t.Errorf("%s accepted --nonsense", name)
		}
		if !strings.Contains(errOut.String(), `unrecognised flag "--nonsense"`) {
			t.Errorf("%s does not reject the flag with the shared wording: %q", name, errOut.String())
		}
	}
}

func TestHookRejectsUnknownShellNameDifferentlyFromAFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch(&out, &errOut, "hook", []string{"fish"})
	if code == 0 {
		t.Fatal("hook accepted fish")
	}
	if !strings.Contains(errOut.String(), `unsupported shell "fish"`) {
		t.Fatalf("hook does not name fish as an unsupported shell: %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "unrecognised flag") {
		t.Fatalf("hook fish was rejected as a flag: %q", errOut.String())
	}
}

func TestOldBareSubcommandWordsAreQueries(t *testing.T) {
	for _, old := range []string{"cfg", "save", "load", "bak", "bench", "ans", "-"} {
		if !isQuery(old) {
			t.Errorf("%q is not treated as a query", old)
		}
		var out, errOut bytes.Buffer
		if code := dispatch(&out, &errOut, old, nil); code == 0 {
			t.Errorf("dispatch still runs bare %q as a command", old)
		}
	}
}

func TestCommandsAndFlagsAreNotQueries(t *testing.T) {
	for name := range commands {
		if isQuery(name) {
			t.Errorf("%q would be searched for instead of run", name)
		}
	}
	for alias := range shell.Aliases() {
		if isQuery(alias) {
			t.Errorf("alias %q would be searched for instead of run", alias)
		}
	}
	for _, flag := range []string{"--help", "--version", "--setup", "-h", "-v", "-s"} {
		if isQuery(flag) {
			t.Errorf("%q would be searched for instead of run", flag)
		}
	}
}

func TestFormatFlagIsGone(t *testing.T) {
	if strings.Contains(usageText(), "--format") {
		t.Fatal("usage still mentions --format")
	}
}

func TestEveryAliasResolvesToItsCommand(t *testing.T) {
	for alias, want := range shell.Aliases() {
		if got := canonical(alias); got != want {
			t.Errorf("canonical(%q) = %q, want %q", alias, got, want)
		}
	}
}

func TestAliasesDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for _, c := range shell.Commands {
		for _, a := range shell.AliasesOf(c) {
			if other, dup := seen[a]; dup {
				t.Errorf("%q is an alias of both %s and %s", a, other, c.Name)
			}
			seen[a] = c.Name
		}
	}
}

func TestNoAliasShadowsACanonicalName(t *testing.T) {
	aliases := shell.Aliases()
	for _, c := range shell.Commands {
		if to, ok := aliases[c.Name]; ok {
			t.Errorf("%s is also an alias of %s", c.Name, to)
		}
	}
}

func TestUsageSpellsShortAliases(t *testing.T) {
	text := usageText()
	for _, want := range []string{"-c|--cfg", "-d|--dmp", "-s|--setup", "-b|--bench", "-h|--help", "-v|--version"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage lacks %q:\n%s", want, text)
		}
	}
}

func TestHelpReachedThroughAnAlias(t *testing.T) {
	for _, name := range []string{"--cfg", "-c", "-cfg"} {
		var out, errOut bytes.Buffer
		if code := dispatch(&out, &errOut, name, []string{"--help"}); code != 0 {
			t.Fatalf("%s --help: exit %d (%s)", name, code, errOut.String())
		}
		if !strings.Contains(out.String(), "-c|--cfg [edit|fill]") {
			t.Errorf("%s --help printed %q", name, out.String())
		}
	}
}

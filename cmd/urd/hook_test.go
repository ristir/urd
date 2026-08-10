package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHookZshPrintsGlue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := runHook(&out, &errOut, []string{"zsh"}); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	got := out.String()
	for _, want := range []string{"_urd_loaded", "bindkey -N urd", "URD_TRIGGER='urd'"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in glue", want)
		}
	}
	if strings.Contains(got, "@@") {
		t.Error("unsubstituted placeholder in glue")
	}
}

func TestRunHookRejectsMissingShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runHook(&out, &errOut, nil); code != 2 {
		t.Errorf("no shell: exit code %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Errorf("nothing should go to stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "expected zsh or bash") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRunHookRejectsUnknownShellName(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runHook(&out, &errOut, []string{"fish"}); code != 2 {
		t.Errorf("fish: exit code %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Errorf("nothing should go to stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), `unsupported shell "fish"`) {
		t.Errorf("stderr = %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "unrecognised flag") {
		t.Errorf("fish was rejected as a flag, not as an unsupported shell: %q", errOut.String())
	}
}

func TestRunHookRejectsUnknownFlagByName(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runHook(&out, &errOut, []string{"--nonsense"})
	if code == 0 {
		t.Fatalf("exit 0, want non-zero for a typo'd flag")
	}
	if !strings.Contains(errOut.String(), `unrecognised flag "--nonsense"`) {
		t.Fatalf("error does not name the offending flag as a flag: %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "unsupported shell") {
		t.Fatalf("hook still spells the flag rejection as unsupported shell: %q", errOut.String())
	}
}

func TestRunHookBashPrintsGlue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := runHook(&out, &errOut, []string{"bash"}); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	got := out.String()
	for _, want := range []string{"_urd_loaded", "bind -x", "READLINE_LINE"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in glue", want)
		}
	}
	if strings.Contains(got, "@@") {
		t.Error("unsubstituted placeholder in glue")
	}
}

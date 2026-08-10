package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunSearchPrintsOnlyTheCommand(t *testing.T) {
	pickEnv(t)
	var out, errOut bytes.Buffer
	if code := runSearch(&out, &errOut, []string{"ratel"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := out.String(); got != "echo beta ratelimit\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunSearchTakesTheFreshestMatch(t *testing.T) {
	pickEnv(t)
	var out, errOut bytes.Buffer
	if code := runSearch(&out, &errOut, []string{"echo"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "echo beta ratelimit" {
		t.Fatalf("stdout = %q, want the newest entry", got)
	}
	if !strings.Contains(errOut.String(), "freshest of 2 matches") {
		t.Errorf("the count was not reported on stderr: %q", errOut.String())
	}
}

func TestRunSearchReportsNoMatch(t *testing.T) {
	pickEnv(t)
	var out, errOut bytes.Buffer
	if code := runSearch(&out, &errOut, []string{"nosuchthing"}); code == 0 {
		t.Fatalf("exit 0 with nothing found, stdout %q", out.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errOut.String(), "nothing matches") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRunSearchRequiresEveryWord(t *testing.T) {
	pickEnv(t)
	var out, errOut bytes.Buffer
	if code := runSearch(&out, &errOut, []string{"echo", "alpha"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "echo alpha rate-limit" {
		t.Fatalf("stdout = %q", got)
	}
}

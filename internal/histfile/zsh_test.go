package histfile

import "testing"

func TestParseZshExtended(t *testing.T) {
	data := []byte(": 1785848398:0;kubectl get pods\n: 1785848678:2;cat site.yml\n")
	got := ParseZsh(data, "h")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Cmd != "kubectl get pods" || got[0].TS != 1785848398 || got[0].Dur != 0 {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].Dur != 2 || got[1].Source != "h" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestParseZshJoinsContinuation(t *testing.T) {
	data := []byte(": 1785848574:0;ansible-playbook rate-limit.yml \\\\\n    -l cache-02 \\\\\n    -e V=1\n")
	got := ParseZsh(data, "h")
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	want := "ansible-playbook rate-limit.yml \\\n    -l cache-02 \\\n    -e V=1"
	if got[0].Cmd != want {
		t.Fatalf("got %q, want %q", got[0].Cmd, want)
	}
}

func TestParseZshDoesNotJoinWhenNextLineIsNewEntry(t *testing.T) {
	data := []byte(": 1000000000:0;net use \\\\\\\\srv\\\\share \\\n: 1000000001:0;echo next\n")
	got := ParseZsh(data, "h")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[1].Cmd != "echo next" {
		t.Fatalf("entry 1 = %q", got[1].Cmd)
	}
}

func TestParseZshPlainFormat(t *testing.T) {
	data := []byte("ll\npwd\n")
	got := ParseZsh(data, "h")
	if len(got) != 2 || got[0].Cmd != "ll" || got[0].TS != 0 || got[0].Ord != 0 || got[1].Ord != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseZshUnmetafiesCommand(t *testing.T) {
	data := []byte(": 1000000000:0;grep \xD0\x83\xBD\xD0\xB0\xD0\xB2\xD1\x83\xAB\xD0\xBA\xD0\xB8\n")
	got := ParseZsh(data, "h")
	if len(got) != 1 || got[0].Cmd != "grep Навыки" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseZshCorruptionGuard(t *testing.T) {
	data := []byte(": 1000000000:0;start \\\\\n")
	for i := 0; i < 200; i++ {
		data = append(data, []byte("more \\\\\n")...)
	}
	got := ParseZsh(data, "h")
	if len(got) == 0 {
		t.Fatal("expected at least one entry")
	}
	if n := len(splitLines(got[0].Cmd)); n > 101 {
		t.Fatalf("joined %d lines, guard should cap at 101", n)
	}
}

func TestParseZshSkipsEmptyCommands(t *testing.T) {
	data := []byte(": 1000000000:0;\n: 1000000001:0;echo ok\n")
	got := ParseZsh(data, "h")
	if len(got) != 1 || got[0].Cmd != "echo ok" {
		t.Fatalf("got %+v", got)
	}
}

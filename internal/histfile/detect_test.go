package histfile

import "testing"

func TestParsePicksZshForExtendedFormat(t *testing.T) {
	got := Parse([]byte(": 1700000000:0;ll\n"), "s")
	if len(got) != 1 || got[0].TS != 1700000000 {
		t.Fatalf("got %+v", got)
	}
}

func TestParsePicksBashForHashTimestamps(t *testing.T) {
	got := Parse([]byte("#1700000000\nll\n"), "s")
	if len(got) != 1 || got[0].TS != 1700000000 || got[0].Cmd != "ll" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFallsBackToPlainLines(t *testing.T) {
	got := Parse([]byte("ll\npwd\n"), "s")
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

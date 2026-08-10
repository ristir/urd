package histfile

import "testing"

func TestParsePicksJSONLAndKeepsSource(t *testing.T) {
	data := []byte(`{"cmd":"echo one","ts":100,"source":"stg-10.adv.nl.lab"}` + "\n" +
		`{"cmd":"echo two","ts":200,"source":"live"}` + "\n")
	got := Parse(data, "label")
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Cmd != "echo one" || got[0].TS != 100 || got[0].Source != "stg-10.adv.nl.lab" {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].Cmd != "echo two" || got[1].TS != 200 || got[1].Source != "live" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestParseJSONLSkipsBrokenLinesAndFallsBackToLabel(t *testing.T) {
	data := []byte(`{"cmd":"ll","ts":1}` + "\n" + `{broken` + "\n" + `{"cmd":"pwd","ts":2,"source":"a"}` + "\n")
	got := ParseJSONL(data, "label")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].Source != "label" {
		t.Fatalf("entry without source = %+v, want fallback to label", got[0])
	}
	if got[1].Source != "a" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestParseDoesNotMistakeBraceGroupForJSONL(t *testing.T) {
	data := []byte("{ echo grouped; }\nls -la\npwd\n")
	got := Parse(data, "s")
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	if got[0].Cmd != "{ echo grouped; }" || got[1].Cmd != "ls -la" || got[2].Cmd != "pwd" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFallsBackWhenFirstLineIsJSONButNotARecord(t *testing.T) {
	data := []byte(`{"foo":"bar"}` + "\npwd\n")
	got := Parse(data, "s")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[1].Cmd != "pwd" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseDetectsJSONLAfterLeadingBlankLines(t *testing.T) {
	data := []byte("\n\n" + `{"cmd":"echo one","ts":100,"source":"live"}` + "\n")
	got := Parse(data, "s")
	if len(got) != 1 || got[0].Cmd != "echo one" || got[0].Source != "live" {
		t.Fatalf("got %+v", got)
	}
}

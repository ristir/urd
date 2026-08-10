package protocol

import (
	"bufio"
	"strings"
	"testing"

	"github.com/ristir/urd/internal/query"
)

func TestParseRequest(t *testing.T) {
	got, err := ParseRequest("Q 2 ans rate-li")
	if err != nil {
		t.Fatal(err)
	}
	if got.Nav != 2 || got.Query != "ans rate-li" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseRequestEmptyQuery(t *testing.T) {
	got, err := ParseRequest("Q 0 ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Query != "" {
		t.Fatalf("query = %q, want empty", got.Query)
	}
}

func TestParseRequestRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "X 0 ans", "Q x ans", "Q"} {
		if _, err := ParseRequest(in); err == nil {
			t.Fatalf("input %q accepted, want error", in)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := query.Result{
		Total: 17,
		Index: 0,
		Match: &query.Match{
			Cmd:   "ansible-playbook rate-limit.yml",
			Spans: []query.Span{{Start: 0, End: 16}, {Start: 17, End: 32}},
		},
	}
	var buf strings.Builder
	if err := EncodeResponse(&buf, in); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Fatalf("encoded %d lines, want 3: %q", n, buf.String())
	}
	got, err := DecodeResponse(bufio.NewReader(strings.NewReader(buf.String())))
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 17 || got.Index != 0 || got.Match.Cmd != in.Match.Cmd {
		t.Fatalf("got %+v", got)
	}
	if len(got.Match.Spans) != 2 || got.Match.Spans[1] != in.Match.Spans[1] {
		t.Fatalf("spans = %+v", got.Match.Spans)
	}
}

func TestEncodeNoMatch(t *testing.T) {
	var buf strings.Builder
	if err := EncodeResponse(&buf, query.Result{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "urd1 0 0\n\n\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestEncodeRejectsNewlineInCommand(t *testing.T) {
	in := query.Result{Total: 1, Match: &query.Match{Cmd: "a\nb"}}
	if err := EncodeResponse(&strings.Builder{}, in); err == nil {
		t.Fatal("newline in command accepted, want error")
	}
}

func TestDecodeRejectsWrongVersion(t *testing.T) {
	_, err := DecodeResponse(bufio.NewReader(strings.NewReader("urd2 1 0\ncmd\n\n")))
	if err == nil {
		t.Fatal("wrong version accepted, want error")
	}
}

package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/histfile"
)

func build(cmds ...string) *corpus.Corpus {
	entries := make([]histfile.Entry, 0, len(cmds))
	for i, c := range cmds {
		entries = append(entries, histfile.Entry{Cmd: c, TS: int64(1000 + i), Source: "t"})
	}
	c, _ := corpus.Build(entries)
	return c
}

func spanTexts(m *Match) []string {
	rs := []rune(m.Cmd)
	out := make([]string, 0, len(m.Spans))
	for _, s := range m.Spans {
		out = append(out, string(rs[s.Start:s.End]))
	}
	return out
}

func TestSearchGoldenRatel(t *testing.T) {
	c := build(
		"ansible-playbook playbooks/apps/billing/rate-limit.yml -l cache-01.zone01.de.lab -e APP_VERSION=billing-rate-limit-1a6dc963",
		"ansible-playbook playbooks/apps/billing/rate-limit.yml -l cache-02.zone01.us.lab -e APP_VERSION=ratelimit:7eb83dc3",
	)
	got := Search(c, "ans ratel", 0, DefaultDelims)
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	want := []string{"ansible", "ratelimit"}
	if diff := spanTexts(got.Match); !equal(diff, want) {
		t.Fatalf("spans = %q, want %q", diff, want)
	}
}

func TestSearchGoldenRateDashLi(t *testing.T) {
	c := build(
		"ansible-playbook playbooks/apps/billing/rate-limit.yml -l cache-01.zone01.de.lab -e APP_VERSION=billing-rate-limit-1a6dc963",
		"ansible-playbook playbooks/apps/billing/rate-limit.yml -l cache-02.zone01.us.lab -e APP_VERSION=ratelimit:7eb83dc3",
	)
	got := Search(c, "ans rate-li", 0, DefaultDelims)
	if got.Total != 2 {
		t.Fatalf("total = %d, want 2", got.Total)
	}
	want := []string{"ansible", "rate", "limit"}
	if diff := spanTexts(got.Match); !equal(diff, want) {
		t.Fatalf("spans = %q, want %q", diff, want)
	}
}

func TestSearchReturnsNewestFirst(t *testing.T) {
	c := build("ansible old", "ansible new")
	got := Search(c, "ansible", 0, DefaultDelims)
	if got.Match.Cmd != "ansible new" {
		t.Fatalf("cmd = %q, want newest", got.Match.Cmd)
	}
}

func TestSearchNavStepsIntoPast(t *testing.T) {
	c := build("ansible old", "ansible new")
	got := Search(c, "ansible", 1, DefaultDelims)
	if got.Match.Cmd != "ansible old" || got.Index != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchNavClampsToLast(t *testing.T) {
	c := build("ansible old", "ansible new")
	got := Search(c, "ansible", 99, DefaultDelims)
	if got.Match.Cmd != "ansible old" || got.Index != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchNoMatch(t *testing.T) {
	c := build("kubectl get pods")
	got := Search(c, "ansible", 0, DefaultDelims)
	if got.Total != 0 || got.Match != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchWordOrderIrrelevant(t *testing.T) {
	c := build("ansible-playbook rate-limit.yml")
	a := Search(c, "rate ans", 0, DefaultDelims)
	b := Search(c, "ans rate", 0, DefaultDelims)
	if a.Total != 1 || b.Total != 1 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestSearchCaseInsensitiveWithCyrillic(t *testing.T) {
	c := build("grep -Ri Навыки notes")
	got := Search(c, "навыки", 0, DefaultDelims)
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	if s := spanTexts(got.Match); !equal(s, []string{"Навыки"}) {
		t.Fatalf("spans = %q", s)
	}
}

func TestSearchQueryCaseIgnored(t *testing.T) {
	c := build("ansible-playbook rate-limit.yml -e APP_VERSION=abc123")
	lower := Search(c, "ans rate ver", 0, DefaultDelims)
	upper := Search(c, "ans rate VER", 0, DefaultDelims)
	mixed := Search(c, "ANS Rate vEr", 0, DefaultDelims)
	if lower.Total != 1 || upper.Total != 1 || mixed.Total != 1 {
		t.Fatalf("lower=%d upper=%d mixed=%d, want 1 each", lower.Total, upper.Total, mixed.Total)
	}
	want := []string{"ansible", "rate", "VERSION"}
	for name, res := range map[string]Result{"lower": lower, "upper": upper, "mixed": mixed} {
		if s := spanTexts(res.Match); !equal(s, want) {
			t.Fatalf("%s spans = %q, want %q", name, s, want)
		}
	}
}

func TestSearchHighlightsAllMatchingTokens(t *testing.T) {
	c := build("cp rate.yml rate.bak")
	got := Search(c, "rate", 0, DefaultDelims)
	if s := spanTexts(got.Match); !equal(s, []string{"rate", "rate"}) {
		t.Fatalf("spans = %q", s)
	}
}

func segTexts(words []Word) []string {
	var out []string
	for _, w := range words {
		for _, s := range w.segs {
			out = append(out, string(s.runes))
		}
	}
	return out
}

func TestSplitDropsEmptyWords(t *testing.T) {
	if got := segTexts(Split("  ans   rate  ", DefaultDelims)); !equal(got, []string{"ans", "rate"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSplitCutsWordOnDelimiters(t *testing.T) {
	got := segTexts(Split("ans-pl pl/us/", DefaultDelims))
	want := []string{"ans", "-", "pl", "pl", "/", "us", "/"}
	if !equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitKeepsRunOfDelimitersAsOneLiteral(t *testing.T) {
	if got := segTexts(Split("a//b", DefaultDelims)); !equal(got, []string{"a", "//", "b"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSplitQuotedWordKeepsDelimitersInside(t *testing.T) {
	got := segTexts(Split("'cache-01.zone01' ans", DefaultDelims))
	if !equal(got, []string{"cache-01.zone01", "ans"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSplitUnclosedQuoteIsLiteralToEnd(t *testing.T) {
	if got := segTexts(Split("'rate-li", DefaultDelims)); !equal(got, []string{"rate-li"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSplitQuoteInsideWordIsOrdinaryCharacter(t *testing.T) {
	if got := segTexts(Split("don't", DefaultDelims)); !equal(got, []string{"don't"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSearchDelimiterMatchesAcrossTheWord(t *testing.T) {
	c := build("ansible-playbook site.yml")
	if got := Search(c, "ans-pl", 0, DefaultDelims); got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	if got := Search(c, "pl-ans", 0, DefaultDelims); got.Total != 0 {
		t.Fatalf("reversed total = %d, want 0", got.Total)
	}
}

func TestSearchDelimiterMustOccurLiterally(t *testing.T) {
	c := build("ansible playbook site.yml")
	if got := Search(c, "ans-pl", 0, DefaultDelims); got.Total != 0 {
		t.Fatalf("total = %d, want 0", got.Total)
	}
}

func TestSearchLeadingDelimiterStaysWithTheWord(t *testing.T) {
	c := build("echo alpha -e beta", "echo gamma-newest ratelimit")
	got := Search(c, "-e", 0, DefaultDelims)
	if got.Total != 1 || got.Match.Cmd != "echo alpha -e beta" {
		t.Fatalf("got %+v", got)
	}
	if s := spanTexts(got.Match); !equal(s, []string{"-e"}) {
		t.Fatalf("spans = %q", s)
	}
}

func TestSearchTrailingDelimiterIsRequired(t *testing.T) {
	c := build("ls playbooks", "ls playbooks/users")
	got := Search(c, "pl/", 0, DefaultDelims)
	if got.Total != 1 || got.Match.Cmd != "ls playbooks/users" {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchMarksOnlyTheTightestPlace(t *testing.T) {
	c := build("ansible-playbook playbooks/users/admins.yml -l 'cache-01.zone02.eu.lab' -bD")
	got := Search(c, "ans-pl", 0, DefaultDelims)
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	if s := spanTexts(got.Match); !equal(s, []string{"ansible", "playbook"}) {
		t.Fatalf("spans = %q, want [ansible playbook]", s)
	}
}

func TestSearchStillMarksEveryGenuineOccurrence(t *testing.T) {
	c := build("cp rate.yml rate.bak")
	got := Search(c, "rate", 0, DefaultDelims)
	if s := spanTexts(got.Match); !equal(s, []string{"rate", "rate"}) {
		t.Fatalf("spans = %q, want two marks", s)
	}
}

func TestSearchMarksFragmentsNotWholeToken(t *testing.T) {
	c := build("ansible-playbook playbooks/users/admins.yml -bD")
	got := Search(c, "ans-pl pl/us/", 0, DefaultDelims)
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	want := []string{"ansible", "playbook", "playbooks", "users"}
	if s := spanTexts(got.Match); !equal(s, want) {
		t.Fatalf("spans = %q, want %q", s, want)
	}
}

func TestSearchQuotedWordIsOneLiteral(t *testing.T) {
	c := build("ansible-playbook site.yml", "grep ans-pl notes")
	got := Search(c, "'ans-pl'", 0, DefaultDelims)
	if got.Total != 1 || got.Match.Cmd != "grep ans-pl notes" {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchQuotedMatchIsMarkedWhole(t *testing.T) {
	c := build("ansible -l 'cache-01.zone01.de.lab' -bD")
	got := Search(c, "'cache-01'", 0, DefaultDelims)
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1", got.Total)
	}
	if s := spanTexts(got.Match); !equal(s, []string{"cache-01"}) {
		t.Fatalf("spans = %q", s)
	}
}

func TestSpaceSeparatesWordsWhateverTheDelimiters(t *testing.T) {
	c := build("ansible-playbook site.yml", "ansible nothing else")
	for name, d := range map[string]Delims{"default": DefaultDelims, "empty": NewDelims(""), "space": NewDelims(" ")} {
		got := Search(c, "ans site", 0, d)
		if got.Total != 1 || got.Match.Cmd != "ansible-playbook site.yml" {
			t.Errorf("%s: got %+v, both words must still be required", name, got)
		}
	}
}

func TestSearchEmptyDelimitersMatchWholeWord(t *testing.T) {
	none := NewDelims("")
	c := build("ansible-playbook site.yml")
	if got := Search(c, "ans-pl", 0, none); got.Total != 0 {
		t.Fatalf("total = %d, want 0", got.Total)
	}
	got := Search(c, "ans", 0, none)
	if s := spanTexts(got.Match); !equal(s, []string{"ansible-playbook"}) {
		t.Fatalf("spans = %q, want whole token", s)
	}
}

func BenchmarkSearchRealHistory(b *testing.B) {
	home, err := os.UserHomeDir()
	if err != nil {
		b.Skip("no home dir")
	}
	data, err := os.ReadFile(filepath.Join(home, ".zsh_history"))
	if err != nil {
		b.Skip("no ~/.zsh_history")
	}
	c, _ := corpus.Build(histfile.Parse(data, "real"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Search(c, "ans ratel", 0, DefaultDelims)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

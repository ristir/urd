package query

import (
	"sort"
	"unicode"

	"github.com/ristir/urd/internal/corpus"
)

// Span is a half-open interval in runes. region_highlight in zsh counts
// characters, so byte offsets are not allowed here.
type Span struct {
	Start int
	End   int
}

type Match struct {
	Cmd   string
	Spans []Span
}

type Result struct {
	Total int
	Index int
	Match *Match
}

// Delims cut a query word into fragments and stop a highlight; an empty set means the
// whole word is one literal.
type Delims struct{ m map[rune]bool }

func NewDelims(s string) Delims {
	d := Delims{m: make(map[rune]bool, len(s))}
	for _, r := range s {
		d.m[r] = true
	}
	return d
}

func (d Delims) is(r rune) bool { return d.m[r] }

// DefaultDelims duplicates config.Default().Search.Delimiters so that packages
// with no config at hand (tests, urd pick) still match the way the shell does.
var DefaultDelims = NewDelims("-_/.,;:=")

type segment struct {
	runes []rune
	delim bool
}

// Word is an ordered chain of literals: "ans-pl" is "ans", "-", "pl", exactly
// *ans*-*pl* - not fuzzy, where it would also reach "ansible playbook".
type Word struct{ segs []segment }

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// A quote at the start of a word turns splitting off; inside a word it is ordinary.
func isQuote(r rune) bool { return r == '\'' || r == '"' }

func fold(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = unicode.ToLower(r)
	}
	return out
}

// Split cuts the query into words on spaces and every word into literals on delimiters.
// A word with no literal at all (bare quotes) is dropped: it would match everything.
func Split(q string, d Delims) []Word {
	rs := []rune(q)
	var out []Word
	for i := 0; i < len(rs); {
		for i < len(rs) && isSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		var w Word
		start := i
		for i < len(rs) && !isSpace(rs[i]) {
			switch {
			case isQuote(rs[i]) && i == start:
				mark := rs[i]
				i++
				lit := i
				for i < len(rs) && rs[i] != mark {
					i++
				}
				if lit < i {
					w.segs = append(w.segs, segment{runes: fold(rs[lit:i])})
				}
				// An unclosed quote is typing in progress, not an error.
				if i < len(rs) {
					i++
				}
			case d.is(rs[i]) && len(w.segs) == 0:
				// A leading delimiter joins the piece after it: "-e" has to find exactly
				// "-e", not a dash and an e somewhere further along.
				run := i
				for i < len(rs) && !isSpace(rs[i]) && d.is(rs[i]) {
					i++
				}
				for i < len(rs) && !isSpace(rs[i]) && !d.is(rs[i]) {
					i++
				}
				w.segs = append(w.segs, segment{runes: fold(rs[run:i])})
			case d.is(rs[i]):
				// A run of delimiters is one literal: in "a//b" it is "//" that has to be found.
				run := i
				for i < len(rs) && !isSpace(rs[i]) && d.is(rs[i]) {
					i++
				}
				w.segs = append(w.segs, segment{runes: rs[run:i], delim: true})
			default:
				lit := i
				for i < len(rs) && !isSpace(rs[i]) && !d.is(rs[i]) {
					i++
				}
				w.segs = append(w.segs, segment{runes: fold(rs[lit:i])})
			}
		}
		if len(w.segs) > 0 {
			out = append(out, w)
		}
	}
	return out
}

func indexRunes(hay, needle []rune, from int) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := from; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// The leftmost match leaves the rest the most room, so no backtracking is needed.
func (w Word) match(folded []rune) bool {
	pos := 0
	for _, s := range w.segs {
		at := indexRunes(folded, s.runes, pos)
		if at < 0 {
			return false
		}
		pos = at + len(s.runes)
	}
	return true
}

// A quote bounds a mark although it is not a delimiter: otherwise a mark on 'cache-01'
// would swallow the quote itself.
func (d Delims) isBound(r rune) bool { return isSpace(r) || isQuote(r) || d.is(r) }

// Grown outwards from the match, never inwards: a quoted literal contains a delimiter
// itself, and growing from the inside would cut the mark at it.
func (d Delims) spanAround(rs []rune, at, n int) Span {
	start, end := at, at+n
	for start > 0 && !d.isBound(rs[start-1]) {
		start--
	}
	for end < len(rs) && !d.isBound(rs[end]) {
		end++
	}
	return Span{Start: start, End: end}
}

// Failure from p means failure from any later start too - strictly less room - which is
// what lets the caller stop looking rather than keep scanning.
func (w Word) chainFrom(folded []rune, p int) (at []int, end int, ok bool) {
	at = make([]int, len(w.segs))
	at[0] = p
	pos := p + len(w.segs[0].runes)
	for i := 1; i < len(w.segs); i++ {
		j := indexRunes(folded, w.segs[i].runes, pos)
		if j < 0 {
			return nil, 0, false
		}
		at[i] = j
		pos = j + len(w.segs[i].runes)
	}
	return at, pos, true
}

// The tightest chain wins - "ans-pl" could take "pl" from a later word, and marking both
// was the bug - then the search continues, so "cp rate.yml rate.bak" marks both.
func (w Word) chains(folded []rune) [][]int {
	var out [][]int
	from := 0
	for {
		var best []int
		bestEnd, bestSpan := -1, 0
		for p := indexRunes(folded, w.segs[0].runes, from); p >= 0; p = indexRunes(folded, w.segs[0].runes, p+1) {
			at, end, ok := w.chainFrom(folded, p)
			if !ok {
				break
			}
			if bestEnd < 0 || end-p < bestSpan {
				best, bestEnd, bestSpan = at, end, end-p
			}
		}
		if bestEnd < 0 {
			return out
		}
		out = append(out, best)
		from = bestEnd
	}
}

func matches(it corpus.Item, words []Word) bool {
	for _, w := range words {
		if !w.match(it.Folded) {
			return false
		}
	}
	return true
}

// spansFor is called for the one candidate on screen, not for every match: with 574
// hits the other 573 would be marked for nobody.
func spansFor(it corpus.Item, words []Word, d Delims) []Span {
	var spans []Span
	for _, w := range words {
		for _, at := range w.chains(it.Folded) {
			for i, s := range w.segs {
				// A typed delimiter carries no mark: a highlighted "-" is only noise.
				if s.delim {
					continue
				}
				spans = append(spans, d.spanAround(it.Runes, at[i], len(s.runes)))
			}
		}
	}
	return dedupeSpans(spans)
}

func dedupeSpans(spans []Span) []Span {
	if len(spans) < 2 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End < spans[j].End
	})
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.Start <= last.End {
			if s.End > last.End {
				last.End = s.End
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

func Search(c *corpus.Corpus, q string, nav int, d Delims) Result {
	words := Split(q, d)
	if len(words) == 0 {
		return Result{}
	}
	var ids []int
	for i := range c.Items {
		if matches(c.Items[i], words) {
			ids = append(ids, i)
		}
	}
	return pick(c, ids, words, nav, d)
}

func pick(c *corpus.Corpus, ids []int, words []Word, nav int, d Delims) Result {
	if len(ids) == 0 {
		return Result{}
	}
	if nav < 0 {
		nav = 0
	}
	if nav >= len(ids) {
		nav = len(ids) - 1
	}
	it := c.Items[ids[nav]]
	return Result{Total: len(ids), Index: nav, Match: &Match{Cmd: it.Cmd, Spans: spansFor(it, words, d)}}
}

package corpus

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ristir/urd/internal/histfile"
)

// Item is a corpus record. Folded is folded rune by rune, so its rune indices
// line up with Runes and highlight ranges need no recalculation.
type Item struct {
	Cmd    string
	TS     int64
	Source string
	Count  int
	Runes  []rune
	Folded []rune
}

// Corpus is sorted from freshest to oldest.
type Corpus struct {
	Items []Item
}

type Stats struct {
	Files     int
	Raw       int
	Kept      int
	Dropped   int
	Multiline int
	Filtered  int
}

// fold folds case rune by rune, one to one. Full Unicode case folding is not
// used: it changes length (İ -> i̇) and breaks the offset arithmetic.
func fold(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = unicode.ToLower(r)
	}
	return out
}

func Build(entries []histfile.Entry) (*Corpus, Stats) {
	st := Stats{Raw: len(entries)}
	byCmd := make(map[string]*Item, len(entries))
	order := make([]string, 0, len(entries))
	undatedOrd := make(map[string]int, len(entries))

	for _, e := range entries {
		if strings.Contains(e.Cmd, "\n") {
			st.Multiline++
		}
		cmd := histfile.SqueezeSpaces(histfile.Flatten(e.Cmd))
		if cmd == "" {
			continue
		}
		it, ok := byCmd[cmd]
		if !ok {
			rs := []rune(cmd)
			byCmd[cmd] = &Item{Cmd: cmd, TS: e.TS, Source: e.Source, Count: 1, Runes: rs, Folded: fold(rs)}
			undatedOrd[cmd] = e.Ord
			order = append(order, cmd)
			continue
		}
		it.Count++
		if e.TS >= it.TS {
			it.TS = e.TS
			it.Source = e.Source
			undatedOrd[cmd] = e.Ord
		}
	}

	items := make([]Item, 0, len(order))
	for _, cmd := range order {
		items = append(items, *byCmd[cmd])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TS != items[j].TS {
			return items[i].TS > items[j].TS
		}
		return undatedOrd[items[i].Cmd] > undatedOrd[items[j].Cmd]
	})

	st.Kept = len(items)
	st.Dropped = st.Raw - st.Kept
	return &Corpus{Items: items}, st
}

// FromItems rebuilds Runes and Folded: the index does not store them, to avoid
// doubling the size of the file.
func FromItems(items []Item) *Corpus {
	out := make([]Item, len(items))
	for i, it := range items {
		it.Runes = []rune(it.Cmd)
		it.Folded = fold(it.Runes)
		out[i] = it
	}
	return &Corpus{Items: out}
}

// compile sets aside expressions that do not compile: a broken one neither fails
// the corpus nor applies, so there has to be a way to tell about it.
func compile(patterns []string) ([]*regexp.Regexp, []string) {
	res := make([]*regexp.Regexp, 0, len(patterns))
	var bad []string
	for _, p := range patterns {
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			bad = append(bad, p)
			continue
		}
		res = append(res, re)
	}
	return res, bad
}

// BadPatterns is for the index read path: filters are not applied again there,
// but the condition still holds, and silence about it looks like working fine.
func BadPatterns(patterns []string) []string {
	_, bad := compile(patterns)
	return bad
}

func Exclude(entries []histfile.Entry, patterns []string) ([]histfile.Entry, int, []string) {
	res, bad := compile(patterns)
	if len(res) == 0 {
		return entries, 0, bad
	}
	out := make([]histfile.Entry, 0, len(entries))
	dropped := 0
	for _, e := range entries {
		hit := false
		for _, re := range res {
			if re.MatchString(e.Cmd) {
				hit = true
				break
			}
		}
		if hit {
			dropped++
			continue
		}
		out = append(out, e)
	}
	return out, dropped, bad
}

type SourceError struct {
	Path string
	Err  error
}

// LoadFiles: one broken archive neither fails the corpus nor passes silently -
// it comes back separately, so it does not count as a source that was read.
func LoadFiles(paths []string) ([]histfile.Entry, []SourceError) {
	var out []histfile.Entry
	var bad []SourceError
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			bad = append(bad, SourceError{Path: p, Err: err})
			continue
		}
		out = append(out, histfile.Parse(data, p)...)
	}
	return out, bad
}

// Unreadable checks access without reading contents: the index read path has to
// report the same numbers as a rebuild, or the count jumps between runs.
func Unreadable(paths []string) []SourceError {
	var bad []SourceError
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			bad = append(bad, SourceError{Path: p, Err: err})
			continue
		}
		f.Close()
	}
	return bad
}

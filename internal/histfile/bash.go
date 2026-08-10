package histfile

import (
	"strconv"
	"strings"
)

// bashTimestamp parses the "#<epoch>" line bash writes when HISTTIMEFORMAT is
// set. An ordinary comment like "#cmd" does not count as a timestamp.
func bashTimestamp(line string) (int64, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, false
	}
	ts, err := strconv.ParseInt(line[1:], 10, 64)
	if err != nil || ts <= 0 {
		return 0, false
	}
	return ts, true
}

// ParseBash: this format has no multiline records, bash writes one line per command.
func ParseBash(data []byte, source string) []Entry {
	lines := splitLines(string(Unmetafy(data)))
	out := make([]Entry, 0, len(lines))
	ord := 0
	var pending int64

	for _, line := range lines {
		if line == "" {
			continue
		}
		if ts, ok := bashTimestamp(line); ok {
			pending = ts
			continue
		}
		out = append(out, Entry{Cmd: line, TS: pending, Ord: ord, Source: source})
		pending = 0
		ord++
	}
	return out
}

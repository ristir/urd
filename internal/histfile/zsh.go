package histfile

import (
	"strconv"
	"strings"
)

// maxJoin is a fuse: at most this many lines are joined into one record, so a
// corrupted file cannot swallow the whole corpus.
const maxJoin = 100

func splitLines(s string) []string { return strings.Split(s, "\n") }

// zshHeader parses the extended zsh format prefix: ": <epoch>:<dur>;".
func zshHeader(line string) (ts int64, dur int, rest string, ok bool) {
	if !strings.HasPrefix(line, ": ") {
		return 0, 0, "", false
	}
	sep := strings.IndexByte(line, ';')
	if sep < 0 {
		return 0, 0, "", false
	}
	head := line[2:sep]
	colon := strings.IndexByte(head, ':')
	if colon < 0 {
		return 0, 0, "", false
	}
	ts, err := strconv.ParseInt(head[:colon], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	dur, err = strconv.Atoi(head[colon+1:])
	if err != nil {
		return 0, 0, "", false
	}
	return ts, dur, line[sep+1:], true
}

// ParseZsh reads a zsh histfile: extended and plain formats are told apart by
// the contents of the lines, not by shell config.
func ParseZsh(data []byte, source string) []Entry {
	lines := splitLines(string(Unmetafy(data)))
	out := make([]Entry, 0, len(lines))
	ord := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		ts, dur, rest, ok := zshHeader(line)
		if !ok {
			ts, dur, rest = 0, 0, line
		}

		joined := []string{rest}
		for len(joined) <= maxJoin && strings.HasSuffix(joined[len(joined)-1], `\`) && i+1 < len(lines) {
			if _, _, _, isNew := zshHeader(lines[i+1]); isNew {
				break
			}
			i++
			last := len(joined) - 1
			joined[last] = strings.TrimSuffix(joined[last], `\`)
			joined = append(joined, lines[i])
		}

		cmd := strings.Join(joined, "\n")
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		out = append(out, Entry{Cmd: cmd, TS: ts, Dur: dur, Ord: ord, Source: source})
		ord++
	}
	return out
}

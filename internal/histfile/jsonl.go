package histfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// jsonlRecord is the only format in this package that stores the source in the
// record itself rather than deriving it from the file (an archive from elsewhere).
type jsonlRecord struct {
	Cmd    string `json:"cmd"`
	TS     int64  `json:"ts"`
	Source string `json:"source"`
}

// jsonlHeader rejects a match on the first byte alone: the shell group
// "{ echo grouped; }" also starts with '{' but does not parse as JSON.
func jsonlHeader(line string) (jsonlRecord, bool) {
	var r jsonlRecord
	if err := json.Unmarshal([]byte(line), &r); err != nil || r.Cmd == "" {
		return jsonlRecord{}, false
	}
	return r, true
}

// ParseJSONL reads line-delimited JSON. A broken line is skipped rather than
// failing the file, the same way an exhausted maxJoin does not fail ParseZsh.
func ParseJSONL(data []byte, label string) []Entry {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	ord := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r jsonlRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.Cmd == "" {
			continue
		}
		src := r.Source
		if src == "" {
			src = label
		}
		out = append(out, Entry{Cmd: r.Cmd, TS: r.TS, Ord: ord, Source: src})
		ord++
	}
	return out
}

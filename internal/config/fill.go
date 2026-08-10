package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Every key needs a hint, which TestEveryKeyHasAHint checks.
var hints = map[string]string{
	"engine.mode":         `daemon | oneshot`,
	"engine.idle_timeout": `"30m", "1h", "24h"`,
	"sources.auto":        `true | false`,
	"sources.extra":       `e.g. ["~/backups/**/*history*"]`,
	"ui.indicator":        `suffix | below | off`,
	"ui.trigger":          `any word: "urd", "hist", "h"`,
	"ui.hotkey":           `"^R", "^Xu"; "" = unbound`,
	"ui.steal_ctrl_r":     `true | false; native search stays on ^Xr`,
	"search.exclude":      `RE2, anchor it: ["^history", "^sudo rm"]`,
	"search.delimiters":   `any characters; "" = whole words only`,
	"colors.prompt":       `fg=cyan,bold | fg=8 | fg=#8fbf7f | "" = none`,
	"colors.mark":         `fg=white,bold | standout | underline`,
	"colors.builtin":      `fg=green | bold | "" = none`,
	"colors.hint":         `fg=8 | fg=blue | "" = none`,
	"colors.query":        `underline | standout | fg=white`,
}

// Keys that moved: still read so an old file keeps working, but Fill takes the section
// out rather than filling it in.
var legacySections = map[string]string{"filters": "search"}

// Key is one config key with the value in effect for it.
type Key struct {
	Section string
	Name    string
	Value   string
}

func (k Key) String() string { return k.Section + "." + k.Name + " = " + k.Value }

// AbsentKeys lists the keys in effect but not written down. A zero value is skipped:
// the encoder writes no empty list at all, so sources.extra would never settle.
func AbsentKeys() []Key {
	data, err := os.ReadFile(Path())
	// An unparsable file is not understood, so nothing is claimed about what it lacks.
	if err != nil && !os.IsNotExist(err) {
		return nil
	}
	eff := Default()
	meta, err := toml.Decode(string(data), &eff)
	if err != nil {
		return nil
	}
	adoptLegacy(&eff, meta)
	var out []Key
	cv := reflect.ValueOf(eff)
	ct := cv.Type()
	for i := 0; i < ct.NumField(); i++ {
		section := ct.Field(i).Tag.Get("toml")
		sv := cv.Field(i)
		st := sv.Type()
		// Offering a legacy section back would keep it alive forever.
		if _, legacy := legacySections[section]; legacy {
			continue
		}
		for j := 0; j < st.NumField(); j++ {
			name := st.Field(j).Tag.Get("toml")
			if meta.IsDefined(section, name) || sv.Field(j).IsZero() {
				continue
			}
			out = append(out, Key{Section: section, Name: name, Value: tomlValue(sv.Field(j))})
		}
	}
	return out
}

// Absent keeps the printable shape that urd --cfg uses.
func Absent() []string {
	keys := AbsentKeys()
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.String())
	}
	return out
}

func sectionOf(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if len(s) < 3 || s[0] != '[' || s[len(s)-1] != ']' {
		return "", false
	}
	return s[1 : len(s)-1], true
}

// indentOf copies the file's own indentation: the encoder writes two spaces,
// hand-written configs often write none.
func indentOf(lines []string, from int) string {
	for _, l := range lines[from:] {
		if _, ok := sectionOf(l); ok {
			break
		}
		if t := strings.TrimLeft(l, " \t"); t != "" && !strings.HasPrefix(t, "#") {
			return l[:len(l)-len(t)]
		}
	}
	return "  "
}

// Fill writes the keys the file lacks into the file itself and takes out sections whose
// keys have moved. Line-based, because Save re-encodes and would drop comments and order.
func Fill() (added []Key, moved []string, backup string, err error) {
	absent := AbsentKeys()
	if len(absent) == 0 {
		return nil, nil, "", nil
	}
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, "", err
	}
	// Checked on the trimmed text, not on len(data): Split("", "\n") is one empty
	// element, not none, and a file holding a single newline counted as having a line.
	lines := []string{}
	if trimmed := strings.TrimRight(string(data), "\n"); trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}

	// Removed before anything is inserted, so insertion points land on the final layout.
	moved = nil
	for old, into := range legacySections {
		if trimmed, ok := dropSection(lines, old); ok {
			lines = trimmed
			moved = append(moved, "["+old+"] -> ["+into+"]")
		}
	}

	bySection := map[string][]Key{}
	order := []string{}
	for _, k := range absent {
		if _, seen := bySection[k.Section]; !seen {
			order = append(order, k.Section)
		}
		bySection[k.Section] = append(bySection[k.Section], k)
	}

	type insert struct {
		at    int
		lines []string
	}
	var inserts []insert
	var tail []string
	for _, section := range order {
		start := -1
		for i, l := range lines {
			if s, ok := sectionOf(l); ok && s == section {
				start = i
				break
			}
		}
		if start < 0 {
			// A section the file never had goes to the end: a header in the middle
			// would capture the keys below it.
			if len(lines) > 0 || len(tail) > 0 {
				tail = append(tail, "")
			}
			tail = append(tail, "["+section+"]")
			for _, k := range bySection[section] {
				tail = append(tail, render("  ", k)...)
			}
			continue
		}
		indent := indentOf(lines, start+1)
		// A key appended after a blank line still belongs to the section but reads
		// as if it did not, so the section ends at its last non-blank line.
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if _, ok := sectionOf(lines[i]); ok {
				end = i
				break
			}
		}
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		var block []string
		for _, k := range bySection[section] {
			block = append(block, render(indent, k)...)
		}
		inserts = append(inserts, insert{at: end, lines: block})
	}

	for i := len(inserts) - 1; i >= 0; i-- {
		in := inserts[i]
		lines = append(lines[:in.at], append(append([]string{}, in.lines...), lines[in.at:]...)...)
	}
	lines = append(lines, tail...)
	// Dropped whether this run made it or an earlier one did.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	out := strings.Join(lines, "\n") + "\n"

	// Parsed before anything is written: a file that no longer loads would take the
	// user's filters and sources with it.
	var probe Config
	if _, err := toml.Decode(out, &probe); err != nil {
		return nil, nil, "", fmt.Errorf("the filled config would not parse (%v), nothing written", err)
	}

	if len(data) > 0 {
		backup = Path() + ".urd-bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return nil, nil, "", err
		}
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return nil, nil, "", err
	}
	if err := os.WriteFile(Path(), []byte(out), 0o644); err != nil {
		return nil, nil, backup, err
	}
	return absent, moved, backup, nil
}

// dropSection removes a section only when every key in it is one this version moved.
// Losing a line a human wrote is worse than leaving a stale header.
func dropSection(lines []string, section string) ([]string, bool) {
	start := -1
	for i, l := range lines {
		if s, ok := sectionOf(l); ok && s == section {
			start = i
			break
		}
	}
	if start < 0 {
		return lines, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if _, ok := sectionOf(lines[i]); ok {
			end = i
			break
		}
	}
	known := map[string]bool{}
	st := reflect.TypeOf(Config{})
	for i := 0; i < st.NumField(); i++ {
		if st.Field(i).Tag.Get("toml") != section {
			continue
		}
		ft := st.Field(i).Type
		for j := 0; j < ft.NumField(); j++ {
			known[ft.Field(j).Tag.Get("toml")] = true
		}
	}
	for _, l := range lines[start+1 : end] {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(t, "=", 2)[0])
		if !known[name] {
			return lines, false
		}
	}
	// A blank line left behind would stack up with the one before the next section.
	for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
		end++
	}
	return append(lines[:start:start], lines[end:]...), true
}

// render trails the hint on the key's own line, never above it: a line of its own is
// one more line to delete by hand.
func render(indent string, k Key) []string {
	line := indent + k.Name + " = " + k.Value
	if h := hints[k.Section+"."+k.Name]; h != "" {
		line += "  # " + h
	}
	return []string{line}
}

// Text renders a whole config with its hints. Not toml.NewEncoder, which cannot emit a
// comment at all.
func Text(c Config) string {
	var b strings.Builder
	cv := reflect.ValueOf(c)
	ct := cv.Type()
	for i := 0; i < ct.NumField(); i++ {
		section := ct.Field(i).Tag.Get("toml")
		if _, legacy := legacySections[section]; legacy {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[" + section + "]\n")
		sv := cv.Field(i)
		st := sv.Type()
		for j := 0; j < st.NumField(); j++ {
			k := Key{Section: section, Name: st.Field(j).Tag.Get("toml"), Value: tomlValue(sv.Field(j))}
			b.WriteString(render("  ", k)[0] + "\n")
		}
	}
	return b.String()
}

package histfile

import "strings"

// Entry is one history record. TS == 0 means the source carries no time
// (bash without HISTTIMEFORMAT); then Ord gives the order.
type Entry struct {
	Cmd    string // newlines kept as they are
	TS     int64  // unix seconds
	Ord    int    // positional number inside the source
	Dur    int    // duration from zsh, seconds
	Source string // path of the source file
}

func Flatten(cmd string) string {
	if !strings.ContainsAny(cmd, "\n\\") {
		return cmd
	}
	parts := strings.Split(cmd, "\n")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i > 0 {
			p = strings.TrimLeft(p, " \t")
		}
		p = strings.TrimRight(p, " \t")
		p = strings.TrimSuffix(p, `\`)
		p = strings.TrimRight(p, " \t")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// SqueezeSpaces touches spaces outside quotes only: inside, a run of them often
// is the content (SQL, a pipeline). Unbalanced quotes leave cmd as it is.
func SqueezeSpaces(cmd string) string {
	var out strings.Builder
	out.Grow(len(cmd))
	var quote byte
	run := 0
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote == c:
			quote = 0
		}
		if quote == 0 && c == ' ' {
			run++
			if run > 1 {
				continue
			}
		} else {
			run = 0
		}
		out.WriteByte(c)
	}
	if quote != 0 {
		return cmd
	}
	return out.String()
}

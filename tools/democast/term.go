package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// A grid is needed because ZLE redraws differentially and wraps every changed character
// in its own escape code, tearing a word apart in the byte stream. Colour is not tracked.
type termCell struct{ ch rune }

type term struct {
	w, h   int
	grid   [][]termCell
	cx, cy int
}

func newTerm(w, h int) *term {
	t := &term{w: w, h: h}
	t.grid = make([][]termCell, h)
	for i := range t.grid {
		t.grid[i] = blankRow(w)
	}
	return t
}

func blankRow(w int) []termCell {
	row := make([]termCell, w)
	for i := range row {
		row[i] = termCell{ch: ' '}
	}
	return row
}

func (t *term) lf() {
	t.cy++
	if t.cy >= t.h {
		t.grid = append(t.grid[1:], blankRow(t.w))
		t.cy = t.h - 1
	}
}

func (t *term) put(r rune) {
	if t.cx >= t.w {
		t.cx = 0
		t.lf()
	}
	t.grid[t.cy][t.cx] = termCell{ch: r}
	t.cx++
}

func (t *term) eraseLine(from, to int) {
	for x := from; x < to && x < t.w; x++ {
		t.grid[t.cy][x] = termCell{ch: ' '}
	}
}

// ZLE edits the line by inserting and deleting characters when terminfo promises
// them (ich1/dch1 on xterm): without those a "shorter than before" redraw reads as garbage.
func (t *term) insertChars(n int) {
	row := t.grid[t.cy]
	for x := t.w - 1; x >= t.cx+n; x-- {
		row[x] = row[x-n]
	}
	for x := t.cx; x < t.cx+n && x < t.w; x++ {
		row[x] = termCell{ch: ' '}
	}
}

func (t *term) deleteChars(n int) {
	row := t.grid[t.cy]
	for x := t.cx; x < t.w; x++ {
		if x+n < t.w {
			row[x] = row[x+n]
		} else {
			row[x] = termCell{ch: ' '}
		}
	}
}

func (t *term) insertLines(n int) {
	for i := 0; i < n; i++ {
		t.grid = append(t.grid[:t.cy], append([][]termCell{blankRow(t.w)}, t.grid[t.cy:t.h-1]...)...)
	}
}

func (t *term) deleteLines(n int) {
	for i := 0; i < n; i++ {
		t.grid = append(t.grid[:t.cy], append(t.grid[t.cy+1:], blankRow(t.w))...)
	}
}

func (t *term) write(p []byte) {
	for i := 0; i < len(p); {
		switch b := p[i]; {
		case b == 0x1b:
			i += t.escape(p[i:])
		case b == '\r':
			t.cx = 0
			i++
		case b == '\n':
			t.lf()
			i++
		case b == '\b':
			if t.cx > 0 {
				t.cx--
			}
			i++
		case b == 0x07:
			i++
		default:
			r, n := utf8.DecodeRune(p[i:])
			t.put(r)
			i += n
		}
	}
}

// escape returns the number of bytes parsed, ESC included.
func (t *term) escape(p []byte) int {
	if len(p) < 2 {
		return len(p)
	}
	switch p[1] {
	case '[':
		for j := 2; j < len(p); j++ {
			if p[j] >= 0x40 && p[j] <= 0x7e {
				t.csi(string(p[2:j]), p[j])
				return j + 1
			}
		}
		return len(p)
	case ']':
		for j := 2; j < len(p); j++ {
			if p[j] == 0x07 {
				return j + 1
			}
		}
		return len(p)
	default:
		return 2
	}
}

func (t *term) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") {
		return
	}
	args := strings.Split(params, ";")
	num := func(i, def int) int {
		if i >= len(args) || args[i] == "" {
			return def
		}
		n, err := strconv.Atoi(args[i])
		if err != nil {
			return def
		}
		return n
	}
	switch final {
	// 'm' (SGR) is deliberately not parsed: this copy needs no cell colours.
	case 'C':
		t.cx += num(0, 1)
		if t.cx > t.w-1 {
			t.cx = t.w - 1
		}
	case 'D':
		t.cx -= num(0, 1)
		if t.cx < 0 {
			t.cx = 0
		}
	case 'A':
		t.cy -= num(0, 1)
		if t.cy < 0 {
			t.cy = 0
		}
	case 'B':
		t.cy += num(0, 1)
		if t.cy > t.h-1 {
			t.cy = t.h - 1
		}
	case 'G':
		t.cx = num(0, 1) - 1
	case 'H', 'f':
		t.cy = num(0, 1) - 1
		t.cx = num(1, 1) - 1
	case 'K':
		switch num(0, 0) {
		case 1:
			t.eraseLine(0, t.cx+1)
		case 2:
			t.eraseLine(0, t.w)
		default:
			t.eraseLine(t.cx, t.w)
		}
	case '@':
		t.insertChars(num(0, 1))
	case 'P':
		t.deleteChars(num(0, 1))
	case 'L':
		t.insertLines(num(0, 1))
	case 'M':
		t.deleteLines(num(0, 1))
	case 'J':
		if num(0, 0) == 2 {
			for y := 0; y < t.h; y++ {
				t.grid[y] = blankRow(t.w)
			}
			return
		}
		t.eraseLine(t.cx, t.w)
		for y := t.cy + 1; y < t.h; y++ {
			t.grid[y] = blankRow(t.w)
		}
	}
}

func (t *term) rows() []string {
	out := make([]string, 0, t.h)
	for _, row := range t.grid {
		var b strings.Builder
		for _, c := range row {
			b.WriteRune(c.ch)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func (t *term) text() string { return strings.Join(t.rows(), "\n") }

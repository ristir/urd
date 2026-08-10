package shell

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

type cell struct {
	ch        rune
	fg        string
	bold      bool
	underline bool
}

type term struct {
	w, h      int
	grid      [][]cell
	cx, cy    int
	fg        string
	bold      bool
	underline bool
}

func newTerm(w, h int) *term {
	t := &term{w: w, h: h}
	t.grid = make([][]cell, h)
	for i := range t.grid {
		t.grid[i] = blankRow(w)
	}
	return t
}

func blankRow(w int) []cell {
	row := make([]cell, w)
	for i := range row {
		row[i] = cell{ch: ' '}
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
	t.grid[t.cy][t.cx] = cell{ch: r, fg: t.fg, bold: t.bold, underline: t.underline}
	t.cx++
}

func (t *term) eraseLine(from, to int) {
	for x := from; x < to && x < t.w; x++ {
		t.grid[t.cy][x] = cell{ch: ' '}
	}
}

func (t *term) insertChars(n int) {
	row := t.grid[t.cy]
	for x := t.w - 1; x >= t.cx+n; x-- {
		row[x] = row[x-n]
	}
	for x := t.cx; x < t.cx+n && x < t.w; x++ {
		row[x] = cell{ch: ' '}
	}
}

func (t *term) deleteChars(n int) {
	row := t.grid[t.cy]
	for x := t.cx; x < t.w; x++ {
		if x+n < t.w {
			row[x] = row[x+n]
		} else {
			row[x] = cell{ch: ' '}
		}
	}
}

func (t *term) insertLines(n int) {
	for i := 0; i < n; i++ {
		t.grid = append(t.grid[:t.cy], append([][]cell{blankRow(t.w)}, t.grid[t.cy:t.h-1]...)...)
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
	case 'm':
		t.sgr(args)
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

func (t *term) sgr(args []string) {
	for _, a := range args {
		switch {
		case a == "" || a == "0":
			t.fg = ""
			t.bold = false
			t.underline = false
		case a == "1":
			t.bold = true
		case a == "4":
			t.underline = true
		case a == "22":
			t.bold = false
		case a == "24":
			t.underline = false
		case a == "39":
			t.fg = ""
		case len(a) == 2 && a[0] == '3', len(a) == 2 && a[0] == '9':
			t.fg = a
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

func (t *term) styleOf(sub string) (fg string, bold bool, found bool) {
	want := []rune(sub)
	for i := len(t.grid) - 1; i >= 0; i-- {
		row := t.grid[i]
		for x := 0; x+len(want) <= len(row); x++ {
			match := true
			for k, r := range want {
				if row[x+k].ch != r {
					match = false
					break
				}
			}
			if match {
				return row[x].fg, row[x].bold, true
			}
		}
	}
	return "", false, false
}

func (t *term) cellsOf(sub string) (cells []cell, endRow, endCol int, found bool) {
	want := []rune(sub)
	for i := len(t.grid) - 1; i >= 0; i-- {
		row := t.grid[i]
		for x := 0; x+len(want) <= len(row); x++ {
			match := true
			for k, r := range want {
				if row[x+k].ch != r {
					match = false
					break
				}
			}
			if match {
				return row[x : x+len(want)], i, x + len(want), true
			}
		}
	}
	return nil, 0, 0, false
}

func TestTermTracksUnderline(t *testing.T) {
	e := newTerm(20, 2)
	e.write([]byte("a\x1b[4mbc\x1b[24md"))
	cells, _, _, found := e.cellsOf("abcd")
	if !found {
		t.Fatalf("not on canvas: %q", e.text())
	}
	for i, want := range []bool{false, true, true, false} {
		if cells[i].underline != want {
			t.Fatalf("cell %d underline = %v, want %v", i, cells[i].underline, want)
		}
	}
}

func TestTermWrapsAtWidth(t *testing.T) {
	e := newTerm(5, 3)
	e.write([]byte("abcdefg"))
	if got := e.rows(); got[0] != "abcde" || got[1] != "fg" {
		t.Fatalf("rows = %q", got)
	}
}

func TestTermCarriageReturnAndBackspaceOverwrite(t *testing.T) {
	e := newTerm(10, 2)
	e.write([]byte("abc\rX"))
	if got := e.rows()[0]; got != "Xbc" {
		t.Fatalf("after CR: %q", got)
	}
	e.write([]byte("\b\bZ"))
	if got := e.rows()[0]; got != "Zbc" {
		t.Fatalf("after backspace: %q", got)
	}
}

func TestTermEraseToEndOfLine(t *testing.T) {
	e := newTerm(10, 2)
	e.write([]byte("abcdef\r\x1b[3C\x1b[K"))
	if got := e.rows()[0]; got != "abc" {
		t.Fatalf("rows = %q", got)
	}
}

func TestTermTracksForegroundAndBold(t *testing.T) {
	e := newTerm(20, 2)
	e.write([]byte("a\x1b[1m\x1b[36mhi\x1b[0m\x1b[39mb"))
	fg, bold, found := e.styleOf("hi")
	if !found || fg != "36" || !bold {
		t.Fatalf("hi: fg=%q bold=%v found=%v", fg, bold, found)
	}
	fg, bold, _ = e.styleOf("b")
	if fg != "" || bold {
		t.Fatalf("b should be plain: fg=%q bold=%v", fg, bold)
	}
}

func TestTermInsertsAndDeletesChars(t *testing.T) {
	e := newTerm(8, 2)
	e.write([]byte("abcdef\r\x1b[3C\x1b[2P"))
	if got := e.rows()[0]; got != "abcf" {
		t.Fatalf("after DCH: %q", got)
	}
	e.write([]byte("\r\x1b[1C\x1b[2@XY"))
	if got := e.rows()[0]; got != "aXYbcf" {
		t.Fatalf("after ICH: %q", got)
	}
}

func TestTermCursorUpKeepsColumn(t *testing.T) {
	e := newTerm(10, 3)
	e.write([]byte("ab\r\ncd\x1b[A\r\x1b[1CX"))
	if got := e.rows(); got[0] != "aX" || got[1] != "cd" {
		t.Fatalf("rows = %q", got)
	}
}

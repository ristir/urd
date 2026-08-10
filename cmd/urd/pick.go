package main

import (
	"bufio"
	"fmt"
	"io"

	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/query"
)

// readLine reads byte by byte: bind -x turns ICRNL off, Enter arrives as a bare
// '\r', and bufio.ReadString('\n') would never have returned on it.
func readLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return string(line), err
		}
		if b == '\n' || b == '\r' {
			return string(line), nil
		}
		line = append(line, b)
	}
}

// runPick is the dialog for shells without ZLE: the interface goes to the tty and
// only the chosen command reaches stdout - the shell puts it in via READLINE_LINE.
func runPick(out io.Writer, in io.Reader, tty io.Writer, args []string) int {
	cfg, _ := loadConfigOrWarn()
	c, _, _ := engine.Load(cfg, false)
	if c == nil {
		fmt.Fprintln(tty, "urd: no corpus available")
		return 1
	}

	reader := bufio.NewReader(in)
	var current *query.Match
	nav := 0

	for {
		fmt.Fprint(tty, "urd> ")
		q, rerr := readLine(reader)

		if q == "" {
			if current != nil {
				fmt.Fprintln(out, current.Cmd)
			}
			return 0
		}

		res := query.Search(c, q, nav, query.NewDelims(cfg.Search.Delimiters))
		if res.Match == nil {
			fmt.Fprintf(tty, "no match for %q\n", q)
			current = nil
		} else {
			current = res.Match
			fmt.Fprintf(tty, "[%d/%d] %s\n", res.Index+1, res.Total, res.Match.Cmd)
		}
		if rerr != nil {
			if current != nil {
				fmt.Fprintln(out, current.Cmd)
			}
			return 0
		}
	}
}

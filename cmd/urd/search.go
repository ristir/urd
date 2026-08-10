package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/query"
	"github.com/ristir/urd/internal/setup"
)

// Prints the match and nothing else, so $(urd ans rate) is the command. In zsh the
// widget takes these keystrokes and this is reached only from bash or a script.
func runSearch(out, errOut io.Writer, args []string) int {
	cfg, _ := loadConfigOrWarn()
	c, info, err := engine.Load(cfg, true)
	if err != nil {
		fmt.Fprintf(errOut, "urd: %v\n", err)
	}
	if c == nil {
		fmt.Fprintln(errOut, "urd: no history to search")
		return 1
	}
	for _, w := range setup.Warnings(info) {
		fmt.Fprintf(errOut, "urd: %s\n", w)
	}

	q := strings.Join(args, " ")
	res := query.Search(c, q, 0, query.NewDelims(cfg.Search.Delimiters))
	if res.Match == nil {
		fmt.Fprintf(errOut, "urd: nothing matches %q\n", q)
		return 1
	}
	// On stderr, so stdout stays exactly the command.
	if res.Total > 1 {
		fmt.Fprintf(errOut, "urd: freshest of %d matches\n", res.Total)
	}
	fmt.Fprintln(out, res.Match.Cmd)
	return 0
}

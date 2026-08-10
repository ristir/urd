package main

import (
	"io"
	"strconv"
	"strings"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/protocol"
	"github.com/ristir/urd/internal/query"
)

// The hidden oneshot transport: the same three lines the daemon sends. args[0] is nav.
func runQuery(out io.Writer, args []string) int {
	if len(args) == 0 {
		protocol.EncodeResponse(out, query.Result{})
		return 0
	}
	nav, err := strconv.Atoi(args[0])
	if err != nil {
		nav = 0
	}
	cfg, _ := config.Load()
	c, _, _ := engine.Load(cfg, false)
	// An error from Rebuild only means the index write failed, and c is not nil then.
	if c == nil {
		protocol.EncodeResponse(out, query.Result{})
		return 1
	}
	// The words are glued back into a string: quotes in a query act on a whole word,
	// and the glue calls us with a list already cut on spaces.
	res := query.Search(c, strings.Join(args[1:], " "), nav, query.NewDelims(cfg.Search.Delimiters))
	if err := protocol.EncodeResponse(out, res); err != nil {
		return 1
	}
	return 0
}

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/daemon"
	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/query"
)

// A variable, not a constant: the end-to-end test waits one interval.
var refreshInterval = 5 * time.Second

func runServe() int {
	cfg, _ := config.Load()
	ln, err := daemon.Listen(config.SocketPath())
	if err == daemon.ErrAlreadyRunning {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "urd serve: %v\n", err)
		return 1
	}

	// Written after winning the socket, so the pid file names the process that listens.
	if err := writePidFile(config.PidPath(), os.Getpid(), selfPath()); err != nil {
		fmt.Fprintf(os.Stderr, "urd serve: could not write %s: %v\n", config.PidPath(), err)
	}
	defer os.Remove(config.PidPath())

	c, _, err := engine.Load(cfg, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "urd serve: %v\n", err)
		return 1
	}
	answer := daemon.NewSwappable(query.NewCache(c, 128, query.NewDelims(cfg.Search.Delimiters)))

	refresh := time.NewTicker(refreshInterval)
	defer refresh.Stop()
	stopRefresh := make(chan struct{})
	// A copy: idle_timeout is read outside, and sharing it under a reload would race.
	go func(cfg config.Config, cur *corpus.Corpus) {
		refresher := engine.NewRefresher()
		delims := cfg.Search.Delimiters
		for {
			select {
			case <-stopRefresh:
				return
			case <-refresh.C:
				// A broken config keeps the previous one: config.Load returns defaults on it,
				// and rebuilding by those would wipe the user's filters.
				if fresh, err := config.Load(); err == nil {
					cfg = fresh
				}
				fresh, err := refresher.Next(cfg)
				if err == nil && fresh != nil {
					cur = fresh
				}
				// Delimiters live in the config, not in history: without this check a new
				// set would wait for the next recorded command.
				if fresh == nil && cfg.Search.Delimiters == delims {
					continue
				}
				delims = cfg.Search.Delimiters
				answer.Set(query.NewCache(cur, 128, query.NewDelims(delims)))
			}
		}
	}(cfg, c)

	serveErr := daemon.Serve(ln, answer, cfg.IdleDuration())
	close(stopRefresh)
	if serveErr != nil {
		return 1
	}
	return 0
}

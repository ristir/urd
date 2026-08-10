package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/query"
)

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func report(out io.Writer, mode string, samples []time.Duration) {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	fmt.Fprintf(out, "%-8s keystrokes=%d median=%v p95=%v p99=%v\n",
		mode, len(samples),
		percentile(samples, 0.50).Round(time.Microsecond),
		percentile(samples, 0.95).Round(time.Microsecond),
		percentile(samples, 0.99).Round(time.Microsecond),
	)
}

func prefixes(q string) []string {
	rs := []rune(q)
	out := make([]string, 0, len(rs))
	for i := 1; i <= len(rs); i++ {
		out = append(out, string(rs[:i]))
	}
	return out
}

func runBench(out, errOut io.Writer, args []string) int {
	q := "ans rate"
	runs := 20
	// The bound is i<len(args), not len(args)-1: with the old bound a flag in last
	// position fell out of the loop and passed silently.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query":
			i++
			if i < len(args) {
				q = args[i]
			}
		case "--runs":
			i++
			if i < len(args) {
				if n, err := strconv.Atoi(args[i]); err == nil && n > 0 {
					runs = n
				}
			}
		default:
			if isDashFlag(args[i]) {
				return rejectFlag(errOut, "bench", args[i])
			}
		}
	}

	cfg, _ := loadConfigOrWarn()
	c, info, err := engine.Load(cfg, true)
	if err != nil {
		fmt.Fprintf(errOut, "urd bench: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "corpus: %d entries from %d sources\n", len(c.Items), info.Files)

	steps := prefixes(q)

	var oneshot []time.Duration
	for r := 0; r < runs; r++ {
		for _, s := range steps {
			start := time.Now()
			query.Search(c, s, 0, query.NewDelims(cfg.Search.Delimiters))
			oneshot = append(oneshot, time.Since(start))
		}
	}
	report(out, "oneshot", oneshot)

	var daemonSamples []time.Duration
	for r := 0; r < runs; r++ {
		cache := query.NewCache(c, 128, query.NewDelims(cfg.Search.Delimiters))
		for _, s := range steps {
			start := time.Now()
			cache.Search(s, 0)
			daemonSamples = append(daemonSamples, time.Since(start))
		}
	}
	report(out, "daemon", daemonSamples)

	// The measurement excludes fork+exec and the index read that oneshot pays on
	// every keystroke live: those are measured separately, outside, with hyperfine.
	fmt.Fprintln(out, "note: oneshot numbers exclude process start and index read")
	return 0
}

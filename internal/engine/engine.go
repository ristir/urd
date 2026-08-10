package engine

import (
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/index"
)

type Info struct {
	Files      int
	Stats      corpus.Stats
	Rebuilt    bool
	Elapsed    time.Duration
	BadFilters []string
	BadSources []corpus.SourceError
}

// Rebuild reads every source again and overwrites the index.
func Rebuild(cfg config.Config) (*corpus.Corpus, Info, error) {
	start := time.Now()
	paths := corpus.Discover(cfg.Sources)
	entries, unread := corpus.LoadFiles(paths)
	entries, filtered, bad := corpus.Exclude(entries, cfg.Search.Exclude)
	c, st := corpus.Build(entries)
	// Read, not found: an unreadable archive would otherwise keep the summary healthy
	// while its commands are already gone.
	read := len(paths) - len(unread)
	st.Files = read
	st.Filtered = filtered
	info := Info{Files: read, Stats: st, Rebuilt: true, Elapsed: time.Since(start), BadFilters: bad, BadSources: unread}
	if err := index.Save(config.IndexPath(), c, index.Fingerprints(paths), cfg.Search.Exclude); err != nil {
		return c, info, err
	}
	return c, info, nil
}

// Unfiltered writes no index and is unfit for the hot path - it always reads the
// sources again, so an archive does not depend on the filters at dump time.
func Unfiltered(cfg config.Config) (*corpus.Corpus, Info) {
	start := time.Now()
	paths := corpus.Discover(cfg.Sources)
	entries, unread := corpus.LoadFiles(paths)
	c, st := corpus.Build(entries)
	read := len(paths) - len(unread)
	st.Files = read
	return c, Info{Files: read, Stats: st, Rebuilt: true, Elapsed: time.Since(start), BadSources: unread}
}

// allowRebuild=false is the hot path: it reads the index as it is, even a stale one.
func Load(cfg config.Config, allowRebuild bool) (*corpus.Corpus, Info, error) {
	start := time.Now()
	c, savedFps, savedFilters, err := index.Load(config.IndexPath())
	if err != nil {
		return Rebuild(cfg)
	}
	files := len(savedFps)
	var unread []corpus.SourceError
	if allowRebuild {
		paths := corpus.Discover(cfg.Sources)
		if index.Stale(savedFps, index.Fingerprints(paths)) || !sameFilters(savedFilters, cfg.Search.Exclude) {
			return Rebuild(cfg)
		}
		// Only here: opening every source per keystroke is out of the question.
		unread = corpus.Unreadable(paths)
		files = len(paths) - len(unread)
	}
	// Broken expressions are named on the read path too: compiling is cheaper than the
	// index read that already happened above.
	return c, Info{
		Files:      files,
		Stats:      corpus.Stats{Files: files, Kept: len(c.Items)},
		Elapsed:    time.Since(start),
		BadFilters: corpus.BadPatterns(cfg.Search.Exclude),
		BadSources: unread,
	}, nil
}

// Refresher keeps a fingerprint of the index file, because StaleNow does not see
// a rebuild from another process (bare urd): the index already agrees with the config.
type Refresher struct {
	indexFp []index.Fingerprint
}

func NewRefresher() *Refresher {
	return &Refresher{indexFp: index.Fingerprints([]string{config.IndexPath()})}
}

// The config comes in on every call: the daemon lives for hours and has to notice an edit.
func (r *Refresher) Next(cfg config.Config) (*corpus.Corpus, error) {
	if StaleNow(cfg) {
		c, _, err := Rebuild(cfg)
		if err != nil {
			return nil, err
		}
		r.indexFp = index.Fingerprints([]string{config.IndexPath()})
		return c, nil
	}
	cur := index.Fingerprints([]string{config.IndexPath()})
	if !index.Stale(r.indexFp, cur) {
		return nil, nil
	}
	c, _, err := Load(cfg, false)
	if err != nil {
		return nil, err
	}
	r.indexFp = cur
	return c, nil
}

func StaleNow(cfg config.Config) bool {
	_, savedFps, savedFilters, err := index.Load(config.IndexPath())
	if err != nil {
		return true
	}
	if !sameFilters(savedFilters, cfg.Search.Exclude) {
		return true
	}
	return index.Stale(savedFps, index.Fingerprints(corpus.Discover(cfg.Sources)))
}

// Unsorted on purpose: the order of filters tells one config from another.
func sameFilters(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

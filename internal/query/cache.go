package query

import (
	"strings"
	"sync"

	"github.com/ristir/urd/internal/corpus"
)

// Cache holds the indices of matching records per query string: adding a
// character only narrows, so a new query filters a known prefix.
type Cache struct {
	mu    sync.Mutex
	c     *corpus.Corpus
	d     Delims
	max   int
	sets  map[string][]int
	order []string
	hits  int
}

func NewCache(c *corpus.Corpus, max int, d Delims) *Cache {
	if max <= 0 {
		max = 64
	}
	return &Cache{c: c, d: d, max: max, sets: make(map[string][]int, max)}
}

func (k *Cache) Hits() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.hits
}

func (k *Cache) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.sets)
}

func (k *Cache) put(key string, ids []int) {
	if _, seen := k.sets[key]; !seen {
		k.order = append(k.order, key)
	}
	k.sets[key] = ids
	for len(k.order) > k.max {
		oldest := k.order[0]
		k.order = k.order[1:]
		delete(k.sets, oldest)
	}
}

func (k *Cache) base(q string) ([]int, bool) {
	best, bestLen := []int(nil), -1
	for key, ids := range k.sets {
		if len(key) > len(q) || !strings.HasPrefix(q, key) {
			continue
		}
		if len(key) > bestLen {
			best, bestLen = ids, len(key)
		}
	}
	if bestLen < 0 {
		return nil, false
	}
	return best, true
}

func (k *Cache) Search(q string, nav int) Result {
	words := Split(q, k.d)
	if len(words) == 0 {
		return Result{}
	}

	k.mu.Lock()
	candidates, ok := k.base(q)
	if ok {
		k.hits++
	} else {
		candidates = make([]int, len(k.c.Items))
		for i := range k.c.Items {
			candidates[i] = i
		}
	}
	k.mu.Unlock()

	ids := make([]int, 0, len(candidates))
	for _, id := range candidates {
		if matches(k.c.Items[id], words) {
			ids = append(ids, id)
		}
	}

	k.mu.Lock()
	k.put(q, ids)
	k.mu.Unlock()

	return pick(k.c, ids, words, nav, k.d)
}

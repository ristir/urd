package query

import "testing"

func TestCacheSameResultAsDirectSearch(t *testing.T) {
	c := build("ansible rate one", "ansible rate two", "kubectl get pods")
	cache := NewCache(c, 64, DefaultDelims)
	for _, q := range []string{"ans", "ans rate", "ans rate two", "nope"} {
		want := Search(c, q, 0, DefaultDelims)
		got := cache.Search(q, 0)
		if got.Total != want.Total {
			t.Fatalf("q=%q total got %d want %d", q, got.Total, want.Total)
		}
		if (got.Match == nil) != (want.Match == nil) {
			t.Fatalf("q=%q match presence differs", q)
		}
		if got.Match != nil && got.Match.Cmd != want.Match.Cmd {
			t.Fatalf("q=%q cmd got %q want %q", q, got.Match.Cmd, want.Match.Cmd)
		}
	}
}

func TestCacheReusesPrefix(t *testing.T) {
	c := build("ansible rate one", "ansible rate two")
	cache := NewCache(c, 64, DefaultDelims)
	cache.Search("ans", 0)
	before := cache.Hits()
	cache.Search("ans rate", 0)
	if cache.Hits() <= before {
		t.Fatalf("prefix was not reused: hits %d -> %d", before, cache.Hits())
	}
}

func TestCacheBackspaceIsFree(t *testing.T) {
	c := build("ansible rate one")
	cache := NewCache(c, 64, DefaultDelims)
	cache.Search("ans", 0)
	cache.Search("ans rate", 0)
	before := cache.Hits()
	cache.Search("ans", 0)
	if cache.Hits() <= before {
		t.Fatalf("backspace did not hit cache: hits %d -> %d", before, cache.Hits())
	}
}

func TestCacheEvictsBeyondMax(t *testing.T) {
	c := build("ansible rate one")
	cache := NewCache(c, 2, DefaultDelims)
	cache.Search("a", 0)
	cache.Search("an", 0)
	cache.Search("ans", 0)
	cache.Search("ansi", 0)
	if n := cache.Len(); n > 2 {
		t.Fatalf("cache holds %d entries, max is 2", n)
	}
}

func TestCacheNarrowsOnEveryKeystroke(t *testing.T) {
	c := build(
		"ansible-playbook playbooks/users/admins.yml -l 'cache-01.zone01.de.lab' -bD",
		"ansible-playbook playbooks/apps/rate-limit.yml -l cache-02.zone01.us.lab",
		"grep ans-pl notes",
		"kubectl get pods",
	)
	for _, full := range []string{"ans-pl pl/us/", "'cache-01'", "ans rate-li"} {
		cache := NewCache(c, 64, DefaultDelims)
		rs := []rune(full)
		for i := 1; i <= len(rs); i++ {
			q := string(rs[:i])
			want := Search(c, q, 0, DefaultDelims)
			got := cache.Search(q, 0)
			if got.Total != want.Total {
				t.Fatalf("q=%q total got %d want %d", q, got.Total, want.Total)
			}
			if (got.Match == nil) != (want.Match == nil) {
				t.Fatalf("q=%q match presence differs", q)
			}
			if got.Match != nil && got.Match.Cmd != want.Match.Cmd {
				t.Fatalf("q=%q cmd got %q want %q", q, got.Match.Cmd, want.Match.Cmd)
			}
		}
	}
}

func TestCacheEmptyQueryReturnsNothing(t *testing.T) {
	c := build("ansible")
	cache := NewCache(c, 8, DefaultDelims)
	if got := cache.Search("   ", 0); got.Total != 0 || got.Match != nil {
		t.Fatalf("got %+v", got)
	}
}

package ichorgo

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseConfigParsesOncePerText(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))
	before := configParses.Load()

	first, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A copy, as gomobile hands each call: same content, another string.
	second, err := ParseConfig(strings.Clone(cfg))
	if err != nil {
		t.Fatal(err)
	}

	_, _, _ = resolveContext(cfg, "lab")
	_ = contextFingerprints(cfg)
	_ = isDemoContext(cfg, "lab")

	if got := configParses.Load() - before; got != 1 {
		t.Fatalf("parsed %d times, want 1", got)
	}

	if first != second {
		t.Fatal("cached summary differs")
	}
}

func TestConfigTextsDifferingByOneByteParseTwice(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))
	other := strings.Replace(cfg, "10.1.0.1", "10.1.0.2", 1)
	before := configParses.Load()

	if _, err := ParseConfig(cfg); err != nil {
		t.Fatal(err)
	}

	out, err := ParseConfig(other)
	if err != nil {
		t.Fatal(err)
	}

	if got := configParses.Load() - before; got != 2 {
		t.Fatalf("parsed %d times, want 2", got)
	}

	if !strings.Contains(out, "10.1.0.2") {
		t.Fatalf("summary of the second text is stale: %s", out)
	}
}

func TestConfigCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := &configCache{}
	texts := []string{"a: 1", "a: 2", "a: 3", "a: 4", "a: 5"}

	first := cache.get(texts[0])
	for _, text := range texts[1:configCacheSize] {
		cache.get(text)
	}

	// Used again: the second text is now the oldest.
	if cache.get(texts[0]) != first {
		t.Fatal("cached entry not returned")
	}

	cache.get(texts[configCacheSize])

	if len(cache.entries) != configCacheSize {
		t.Fatalf("%d entries, want %d", len(cache.entries), configCacheSize)
	}

	if cache.peek(texts[1]) != nil {
		t.Fatal("least recently used entry kept")
	}

	if cache.peek(texts[0]) != first {
		t.Fatal("recently used entry evicted")
	}
}

func TestConfigCacheKeysTheInputText(t *testing.T) {
	// A context named twice is deduplicated before parsing: the input text keeps hitting.
	cfg := testConfig(t, time.Now().Add(time.Hour))
	other := cfg[strings.Index(cfg, "  other:\n"):]
	doubled := cfg + strings.Replace(other, "10.1.0.1", "10.1.0.9", 1)

	if dedupeContexts(doubled) == doubled {
		t.Fatal("test text has no duplicate context")
	}

	before := configParses.Load()

	for range 3 {
		parsed, err := loadConfig(doubled)
		if err != nil {
			t.Fatal(err)
		}

		if len(parsed.Contexts) != 3 {
			t.Fatalf("contexts %v, want lab, other and other-1", sortedContextNames(parsed))
		}
	}

	if got := configParses.Load() - before; got != 1 {
		t.Fatalf("parsed %d times, want 1", got)
	}
}

func TestResolveContextReturnsACopy(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	_, ctx, err := resolveContext(cfg, "lab")
	if err != nil {
		t.Fatal(err)
	}

	ctx.Endpoints[0] = "changed"
	ctx.Nodes = append(ctx.Nodes, "changed")

	_, again, _ := resolveContext(cfg, "lab")
	if again.Endpoints[0] != "10.0.0.1" || len(again.Nodes) != 2 {
		t.Fatalf("a caller's change reached the cache: %v %v", again.Endpoints, again.Nodes)
	}
}

func TestCacheKeyStable(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	if cacheKey(cfg, "lab") != cacheKey(strings.Clone(cfg), "lab") {
		t.Fatal("same text, different keys")
	}

	if cacheKey(cfg, "lab") == cacheKey(cfg, "other") || cacheKey(cfg, "lab") == cacheKey(cfg+" ", "lab") {
		t.Fatal("different inputs share a key")
	}
}

func TestConfigCacheConcurrentAccess(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))
	before := configParses.Load()

	var wg sync.WaitGroup

	for range 16 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			text := strings.Clone(cfg)
			if _, err := ParseConfig(text); err != nil {
				t.Error(err)
			}

			if _, _, err := resolveContext(text, ""); err != nil {
				t.Error(err)
			}

			_ = cacheKey(text, "lab")
			_ = contextFingerprints(text)
		}()
	}

	wg.Wait()

	if got := configParses.Load() - before; got != 1 {
		t.Fatalf("parsed %d times, want 1", got)
	}
}

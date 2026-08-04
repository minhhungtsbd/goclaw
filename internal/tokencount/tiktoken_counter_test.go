package tokencount

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func newSeededTiktokenCounter(t *testing.T) *tiktokenCounter {
	t.Helper()
	encoder, err := buildBudgetEncoder(budgetEncodingData)
	if err != nil {
		t.Fatalf("build test encoder: %v", err)
	}
	loader := newEncodingLoader(func(string) (*tiktoken.Tiktoken, error) {
		return encoder, nil
	})
	// The bundled cl100k encoding is sufficient for deterministic counter tests.
	// Cache scoping is independent from the vocabulary used by this test fixture.
	loader.encoders[TokenizerCL100K] = encoder
	loader.encoders[TokenizerO200K] = encoder
	return &tiktokenCounter{
		msgCache: make(map[uint64]int),
		fallback: NewFallbackCounter(),
		loader:   loader,
	}
}

func TestCount_CL100K(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	count := c.Count("claude-sonnet-4-5-20250929", "Hello, world!")
	if count < 3 || count > 6 {
		t.Errorf("cl100k count = %d, expected ~4", count)
	}
}

func TestCount_O200K(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	count := c.Count("gpt-4o-mini", "Hello, world!")
	if count < 3 || count > 6 {
		t.Errorf("o200k count = %d, expected ~4", count)
	}
}

func TestCount_UnknownModel(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	count := c.Count("unknown-model-xyz", "Hello, world!")
	expected := NewFallbackCounter().Count("unknown-model-xyz", "Hello, world!")
	if count != expected {
		t.Errorf("fallback count = %d, want %d", count, expected)
	}
}

func TestCountMessages_Cache(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	msgs := []providers.Message{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "The answer is 4."},
	}

	first := c.CountMessages("claude-sonnet-4-5-20250929", msgs)
	second := c.CountMessages("claude-sonnet-4-5-20250929", msgs)
	if first != second {
		t.Errorf("cached count %d != first count %d", second, first)
	}

	c.mu.RLock()
	cacheLen := len(c.msgCache)
	c.mu.RUnlock()
	if cacheLen != 2 {
		t.Errorf("cache has %d entries, want 2", cacheLen)
	}
}

func TestCountMessages_CacheIsTokenizerScoped(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	msgs := []providers.Message{{Role: "user", Content: "internationalization tokenization pseudopseudohypoparathyroidism"}}

	if count := c.CountMessages("claude-sonnet-4-5-20250929", msgs); count <= 0 {
		t.Fatalf("cl100k count = %d, want positive", count)
	}
	if count := c.CountMessages("gpt-4o", msgs); count <= 0 {
		t.Fatalf("o200k count = %d, want positive", count)
	}

	c.mu.RLock()
	cacheLen := len(c.msgCache)
	c.mu.RUnlock()
	if cacheLen != 2 {
		t.Errorf("cache has %d entries, want 2 (one per tokenizer)", cacheLen)
	}
}

func TestCountMessages_Overhead(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	msgs := []providers.Message{{Role: "user", Content: "Hi"}}
	count := c.CountMessages("claude-sonnet-4-5-20250929", msgs)
	rawCount := c.Count("claude-sonnet-4-5-20250929", "Hi")
	if count <= rawCount {
		t.Errorf("messages count %d should exceed raw count %d", count, rawCount)
	}
}

func TestModelContextWindow(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	tests := []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-5-20250929", 200_000},
		{"gpt-4o-mini", 128_000},
		{"gpt-5.5", 1_050_000},
		{"gpt-5.4", 1_000_000},
		{"unknown-model", 200_000},
	}
	for _, tt := range tests {
		if got := c.ModelContextWindow(tt.model); got != tt.want {
			t.Errorf("ModelContextWindow(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestResetCache(t *testing.T) {
	c := newSeededTiktokenCounter(t)
	c.CountMessages("claude-sonnet-4-5-20250929", []providers.Message{{Role: "user", Content: "test"}})

	c.mu.RLock()
	before := len(c.msgCache)
	c.mu.RUnlock()
	if before == 0 {
		t.Fatal("cache should have entries before reset")
	}
	c.ResetCache()

	c.mu.RLock()
	after := len(c.msgCache)
	c.mu.RUnlock()
	if after != 0 {
		t.Errorf("cache has %d entries after reset, want 0", after)
	}
}

func TestNewTokenCounter_Factory(t *testing.T) {
	if fallback := NewTokenCounter(false); fallback == nil {
		t.Fatal("NewTokenCounter(false) returned nil")
	}
	if _, ok := NewTokenCounter(true).(*tiktokenCounter); !ok {
		t.Errorf("NewTokenCounter(true) did not return a tiktoken counter")
	}
}

func TestEncoderLoadDoesNotBlockRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	loader := newEncodingLoader(func(string) (*tiktoken.Tiktoken, error) {
		close(started)
		<-release
		return nil, errors.New("BPE endpoint unavailable")
	})
	c := &tiktokenCounter{msgCache: make(map[uint64]int), fallback: NewFallbackCounter(), loader: loader}

	done := make(chan int, 1)
	go func() { done <- c.Count("gpt-5.6-terra", "image support request") }()
	select {
	case count := <-done:
		if count <= 0 {
			t.Fatalf("fallback count = %d, want positive", count)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("token count waited for encoder download")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background encoder load did not start")
	}

	done = make(chan int, 1)
	go func() { done <- c.Count("gpt-5.6-terra", "second request") }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second token count waited for in-flight encoder download")
	}
	close(release)
}

func TestEncoderFailureIsCachedForProcessLifetime(t *testing.T) {
	var attempts atomic.Int32
	finished := make(chan struct{})
	loader := newEncodingLoader(func(string) (*tiktoken.Tiktoken, error) {
		attempts.Add(1)
		close(finished)
		return nil, errors.New("network unavailable")
	})
	c := &tiktokenCounter{msgCache: make(map[uint64]int), fallback: NewFallbackCounter(), loader: loader}

	c.Count("gpt-5.6-terra", "first")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("background encoder load did not finish")
	}
	for range 20 {
		loader.mu.RLock()
		failed := loader.unavailable[TokenizerO200K]
		loader.mu.RUnlock()
		if failed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for range 3 {
		c.Count("gpt-5.6-terra", "later request")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("encoder load attempts = %d, want 1", got)
	}
}

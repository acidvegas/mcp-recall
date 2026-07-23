// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// cmd/bench/tokenizer_test.go

package main

import "testing"

func TestHeuristicTokenizer(t *testing.T) {
	h := heuristicTokenizer{}
	if h.Count("") != 0 {
		t.Errorf("empty = %d", h.Count(""))
	}
	if got := h.Count("12345678"); got != 2 { // 8 bytes / 4
		t.Errorf("8 bytes = %d, want 2", got)
	}
}

func TestTiktokenLoadsOffline(t *testing.T) {
	tok, ok := newTiktoken()
	if !ok {
		t.Skip("tiktoken o200k_base unavailable in this environment")
	}
	// Known-ish counts: a short English sentence tokenizes to a handful of tokens,
	// always fewer than its byte length and more than one.
	s := "The quick brown fox jumps over the lazy dog."
	n := tok.Count(s)
	if n <= 1 || n >= len(s) {
		t.Errorf("token count %d out of sane range for %d bytes", n, len(s))
	}
	// Monotonic: more text → more tokens.
	if tok.Count(s+s) <= n {
		t.Error("doubling text should increase token count")
	}
	if tok.Count("") != 0 {
		t.Error("empty string should be 0 tokens")
	}
}

// newTokenizer must always return a usable tokenizer (tiktoken or heuristic).
func TestNewTokenizerNonNil(t *testing.T) {
	tok := newTokenizer()
	if tok == nil || tok.Name() == "" {
		t.Fatal("newTokenizer returned an unusable tokenizer")
	}
	if tok.Count("hello world") <= 0 {
		t.Error("tokenizer counted zero tokens for non-empty input")
	}
}

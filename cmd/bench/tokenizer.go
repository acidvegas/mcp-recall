// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// cmd/bench/tokenizer.go
//
// Tokenizer abstracts token counting so the engine can report token savings.
// Byte metrics are always exact; token metrics are an estimate. The default
// tiktoken tokenizer (o200k_base, GPT-4o-class) is a proxy for Claude's
// unpublished tokenizer — typically within ~10% on English/code, and reliable
// for relative comparison. A dependency-free heuristic is available as a
// fallback.

package main

// Tokenizer counts tokens in a string.
type Tokenizer interface {
	Count(s string) int
	Name() string
}

// heuristicTokenizer approximates tokens as bytes/4 — the rule-of-thumb ratio
// for English text. Zero dependencies, fully deterministic, clearly an estimate.
type heuristicTokenizer struct{}

func (heuristicTokenizer) Count(s string) int { return (len(s) + 3) / 4 }
func (heuristicTokenizer) Name() string       { return "heuristic (~bytes/4)" }

// newTokenizer returns the tiktoken o200k_base tokenizer, falling back to the
// dependency-free heuristic if the BPE ranks can't be loaded.
func newTokenizer() Tokenizer {
	if tok, ok := newTiktoken(); ok {
		return tok
	}
	return heuristicTokenizer{}
}

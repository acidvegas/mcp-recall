// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// cmd/bench/tokenizer_tiktoken.go
//
// tiktoken o200k_base tokenizer — a GPT-4o-class BPE used as an offline proxy
// for Claude's unpublished tokenizer. The BPE ranks are supplied by the offline
// loader (embedded), so no network access is needed at runtime.

package main

import (
	"github.com/pkoukk/tiktoken-go"
	tokenloader "github.com/pkoukk/tiktoken-go-loader"
)

type tiktokenTokenizer struct {
	enc *tiktoken.Tiktoken
}

func (t tiktokenTokenizer) Count(s string) int {
	return len(t.enc.Encode(s, nil, nil))
}

func (t tiktokenTokenizer) Name() string { return "tiktoken o200k_base (proxy, ~±10%)" }

// newTiktoken builds an o200k_base tokenizer using embedded offline BPE ranks.
// Returns ok=false if the encoding can't be constructed, so the caller can fall
// back to the heuristic.
func newTiktoken() (Tokenizer, bool) {
	tiktoken.SetBpeLoader(tokenloader.NewOfflineLoader())
	enc, err := tiktoken.GetEncoding("o200k_base")
	if err != nil || enc == nil {
		return nil, false
	}
	return tiktokenTokenizer{enc: enc}, true
}

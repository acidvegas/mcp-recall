// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/chunking_test.go

package db

import (
	"strings"
	"testing"
)

func TestChunkTextShort(t *testing.T) {
	if got := ChunkText(""); len(got) != 0 {
		t.Errorf("empty → %v", got)
	}
	short := "small text"
	got := ChunkText(short)
	if len(got) != 1 || got[0] != short {
		t.Errorf("short → %v", got)
	}
	exact := strings.Repeat("a", ChunkSize)
	if got := ChunkText(exact); len(got) != 1 {
		t.Errorf("exact ChunkSize should be single chunk, got %d", len(got))
	}
}

func TestChunkTextOverlap(t *testing.T) {
	text := strings.Repeat("a", ChunkSize+ChunkSize) // 2x
	got := ChunkText(text)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	// each chunk (except maybe last) is ChunkSize long
	if len([]rune(got[0])) != ChunkSize {
		t.Errorf("first chunk len = %d, want %d", len([]rune(got[0])), ChunkSize)
	}
	// step is ChunkSize-ChunkOverlap
	if got := stepBetween(); got != ChunkSize-ChunkOverlap {
		t.Errorf("step = %d", got)
	}
}

func stepBetween() int { return ChunkSize - ChunkOverlap }

func TestSanitizeFtsQuery(t *testing.T) {
	cases := map[string]string{
		"":             `""`,
		"proxmox":      `"proxmox"`,
		"nginx config": `"nginx" "config"`,
		`say "hi"`:     `"say" """hi"""`,
		"AND OR NOT":   `"AND" "OR" "NOT"`,
	}
	for in, want := range cases {
		if got := SanitizeFtsQuery(in); got != want {
			t.Errorf("SanitizeFtsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

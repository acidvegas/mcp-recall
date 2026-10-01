// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/content_blocks_test.go
// Port of the upstream #270 vectors: incompressible image content blocks are
// stripped so a screenshot is replaced rather than passed through.

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"mcprecall/internal/jsonx"
)

const (
	chromeCaptureText = "Successfully captured screenshot (1419x840, jpeg) - ID: ss_135241jnk"
	chromeTabContext  = "\n\nTab Context:\n- https://example.com/dashboard\n- Title: Dashboard"
)

// jpegShapedBase64 is JPEG SOI + APP2/ICC-shaped bytes; base64 of an
// already-compressed JPEG is incompressible.
func jpegShapedBase64(n int) string {
	raw := make([]byte, n)
	raw[0], raw[1], raw[2], raw[3] = 0xff, 0xd8, 0xff, 0xe2
	copy(raw[4:], "ICC_PROFILE")
	for i := 16; i < n; i++ {
		raw[i] = byte((i*17 + 31) & 0xff)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// chromeScreenshotJSON is the mcp__claude-in-chrome__computer shape: a
// top-level content-block array with a 32 KB image.
func chromeScreenshotJSON() (payload, imageData string) {
	imageData = jpegShapedBase64(32 * 1024)
	b, _ := json.Marshal([]map[string]any{
		{"type": "text", "text": chromeCaptureText},
		{"type": "text", "text": chromeTabContext},
		{"type": "image", "mimeType": "image/jpeg", "data": imageData},
	})
	return string(b), imageData
}

const chromeTextOnlyJSON = `[{"type":"text","text":"Scrolled down 400 pixels"},` +
	`{"type":"text","text":"\n\nTab Context:\n- https://example.com/page\n- Title: Page"}]`

func parse(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonx.ParseString(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func assertScreenshotSummary(t *testing.T, summary, imageData string) {
	t.Helper()
	for _, want := range []string{"ss_135241jnk", "1419x840", "jpeg", "Tab Context"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
	if strings.Contains(summary, imageData) {
		t.Error("summary contains the image bytes")
	}
}

func TestExtractTextDropsImageBlocks(t *testing.T) {
	raw, imageData := chromeScreenshotJSON()
	for _, payload := range []any{parse(t, raw), raw} { // parsed and as a JSON string
		assertScreenshotSummary(t, ExtractText(payload), imageData)
	}
}

// A text-only top-level array stays serialized so jsonHandler routing is unchanged.
func TestExtractTextTextOnlyArrayUnchanged(t *testing.T) {
	payload := parse(t, chromeTextOnlyJSON)
	if got := ExtractText(payload); got != jsonx.Compact(payload) {
		t.Errorf("ExtractText = %q, want serialized array", got)
	}
}

func TestContentBlockHandlerCompressesScreenshot(t *testing.T) {
	raw, imageData := chromeScreenshotJSON()
	payload := parse(t, raw)
	const tool = "mcp__claude-in-chrome__computer"
	h := GetHandler(tool, payload, nil)
	if !samePtr(h, contentBlockHandler) {
		t.Fatalf("routed to %s, want contentBlockHandler", HandlerName(h))
	}
	res := h(tool, payload)
	if res.OriginalSize <= 30_000 {
		t.Errorf("OriginalSize = %d, want > 30000", res.OriginalSize)
	}
	if res.OriginalSize != len(jsonx.Compact(payload)) {
		t.Errorf("OriginalSize = %d, want pre-strip %d", res.OriginalSize, len(jsonx.Compact(payload)))
	}
	if r := 1 - float64(len(res.Summary))/float64(res.OriginalSize); r <= 0.9 {
		t.Errorf("reduction %.3f, want > 0.9", r)
	}
	assertScreenshotSummary(t, res.Summary, imageData)
	if !strings.Contains(res.Summary, "[stripped 1 image content block]") {
		t.Errorf("missing strip note: %q", res.Summary)
	}
}

func TestTextOnlyBlocksRouteToJSON(t *testing.T) {
	payload := parse(t, chromeTextOnlyJSON)
	const tool = "mcp__claude-in-chrome__computer"
	h := GetHandler(tool, payload, nil)
	if !samePtr(h, jsonHandler) {
		t.Errorf("text-only payload routed to %s, want jsonHandler", HandlerName(h))
	}
	res := h(tool, payload)
	if !strings.Contains(res.Summary, "Scrolled down 400 pixels") || !strings.Contains(res.Summary, "Tab Context") {
		t.Errorf("text lost: %q", res.Summary)
	}
}

func TestWrappedContentBlocksStripped(t *testing.T) {
	raw, imageData := chromeScreenshotJSON()
	payload := parse(t, `{"content":`+raw+`}`)
	const tool = "mcp__unknown__tool"
	res := GetHandler(tool, payload, nil)(tool, payload)
	if res.OriginalSize != len(jsonx.Compact(payload)) {
		t.Errorf("OriginalSize = %d, want pre-strip %d", res.OriginalSize, len(jsonx.Compact(payload)))
	}
	if r := 1 - float64(len(res.Summary))/float64(res.OriginalSize); r <= 0.9 {
		t.Errorf("reduction %.3f, want > 0.9", r)
	}
	if !strings.Contains(res.Summary, "ss_135241jnk") || strings.Contains(res.Summary, imageData) {
		t.Errorf("bad summary")
	}
}

// Unrelated arrays (no block-shaped elements) are not content blocks.
func TestUnrelatedArrayNotContentBlocks(t *testing.T) {
	payload := parse(t, `[{"number":1,"title":"x"},{"number":2,"title":"y"}]`)
	if asContentBlocks(payload) != nil || hasNonTextContentBlocks(payload) {
		t.Error("issue list treated as content blocks")
	}
	mixed := parse(t, `[{"type":"text","text":"a"},"junk"]`)
	if asContentBlocks(mixed) != nil {
		t.Error("array with a non-object element treated as content blocks")
	}
}

func TestStripLabelMixedTypes(t *testing.T) {
	payload := parse(t, `[{"type":"image","data":"x"},{"type":"audio","data":"y"}]`)
	if got := contentBlockHandler("t", payload).Summary; got != "[stripped 2 non-text content blocks]" {
		t.Errorf("summary = %q", got)
	}
}

// The cheap shape prefilter must never change what the full parse decides.
func TestPrefilterMatchesFullParse(t *testing.T) {
	screenshot, _ := chromeScreenshotJSON()
	for _, s := range []string{
		screenshot,
		`{"content":` + screenshot + `}`,
		chromeTextOnlyJSON,
		"  \n" + chromeTextOnlyJSON,
		`[{"number":1,"type":"issue"},{"number":2}]`,
		`[{"type":"text","text":"a"},"junk"]`,
		`[{"type":1}]`,
		`[{"type":"widget"}]`,
		`[{"type":"image","data":"x"}]`,
		`{"content":[{"type":"text","text":"a"}],"isError":false}`,
		`{"content":"not an array"}`,
		`{"data":[{"type":"text","text":"a"}]}`,
		`[]`, `{}`, `"str"`, `42`, `not json`, ``, `[{"type":"text","text":"a"}] trailing`,
	} {
		want := false
		if v, err := jsonx.ParseString(s); err == nil {
			want = blocksFromValue(v) != nil
		}
		if got := asContentBlocks(s) != nil; got != want {
			t.Errorf("%q: prefiltered=%v, full parse=%v", s, got, want)
		}
	}
}

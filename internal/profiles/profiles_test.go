// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/profiles_test.go

package profiles

import (
	"os"
	"strings"
	"testing"

	"mcprecall/internal/handlers"
	"mcprecall/internal/jsonx"
)

func isolate(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	os.Setenv("RECALL_USER_PROFILES_PATH", empty)
	os.Setenv("RECALL_COMMUNITY_PROFILES_PATH", empty)
	t.Cleanup(func() {
		os.Unsetenv("RECALL_USER_PROFILES_PATH")
		os.Unsetenv("RECALL_COMMUNITY_PROFILES_PATH")
	})
}

func TestJSONExtractStrategy(t *testing.T) {
	s := Strategy{
		Type:      "json_extract",
		ItemsPath: []string{"issues"},
		Fields:    []string{"key", "fields.summary", "fields.status.name"},
		Labels:    map[string]string{"key": "Key", "fields.summary": "Summary", "fields.status.name": "Status"},
	}
	input := `{"issues":[{"key":"PROJ-1","fields":{"summary":"Login broken","status":{"name":"Open"}}},{"key":"PROJ-2","fields":{"summary":"Upgrade","status":{"name":"Done"}}}]}`
	got := applyJSONExtract(s, input).Summary
	want := "2 items:\n1. Key: PROJ-1 · Summary: Login broken · Status: Open\n2. Key: PROJ-2 · Summary: Upgrade · Status: Done"
	if got != want {
		t.Errorf("json_extract:\n got %q\nwant %q", got, want)
	}
}

func TestJSONExtractLabelFallbackAndCap(t *testing.T) {
	// no labels → use last path segment; max_items caps + overflow
	max := 1
	s := Strategy{Type: "json_extract", Fields: []string{"name"}, MaxItems: &max}
	input := `[{"name":"a"},{"name":"b"},{"name":"c"}]`
	got := applyJSONExtract(s, input).Summary
	if !strings.Contains(got, "1. name: a") || !strings.Contains(got, "…and 2 more") {
		t.Errorf("cap/label: %q", got)
	}
}

func TestJSONTruncateStrategy(t *testing.T) {
	s := Strategy{Type: "json_truncate"} // defaults depth 3, array 3
	input := `{"a":{"b":{"c":{"d":1}}}}`
	got := applyJSONTruncate(s, input).Summary
	pv, _ := jsonx.ParseString(got)
	a, _ := pv.(*jsonx.Obj).Get("a")
	b, _ := a.(*jsonx.Obj).Get("b")
	c, _ := b.(*jsonx.Obj).Get("c")
	d, _ := c.(*jsonx.Obj).Get("d")
	if d != "…" {
		t.Errorf("json_truncate depth: d=%v", d)
	}
}

func TestTextTruncateStrategy(t *testing.T) {
	mc := 10
	s := Strategy{Type: "text_truncate", MaxChars: &mc}
	got := applyTextTruncate(s, "0123456789abcdef").Summary
	if got != "0123456789\n…" {
		t.Errorf("text_truncate: %q", got)
	}
}

func TestResolvePriority(t *testing.T) {
	profs := []Loaded{
		{Spec: Spec{Profile: Meta{ID: "wild"}}, Tier: TierBundled, Patterns: []string{"mcp__x__*"}},
		{Spec: Spec{Profile: Meta{ID: "exact"}}, Tier: TierBundled, Patterns: []string{"mcp__x__query"}},
		{Spec: Spec{Profile: Meta{ID: "user"}}, Tier: TierUser, Patterns: []string{"mcp__x__*"}},
	}
	// user tier beats bundled even when bundled is more specific
	m := resolveProfile("mcp__x__query", profs, tierOrder)
	if m == nil || m.Spec.Profile.ID != "user" {
		t.Errorf("user tier should win: %+v", m)
	}
	// within bundled, exact beats wildcard
	m2 := resolveProfile("mcp__x__query", profs[:2], tierOrder)
	if m2 == nil || m2.Spec.Profile.ID != "exact" {
		t.Errorf("exact should win over wildcard: %+v", m2)
	}
}

func TestBundledJiraLoadsAndResolves(t *testing.T) {
	isolate(t)
	all := Load()
	found := false
	for _, p := range all {
		if p.Spec.Profile.ID == "mcp__jira" {
			found = true
		}
	}
	if !found {
		t.Fatal("bundled jira profile not loaded from embed")
	}
	h := LookupHandler("mcp__jira__search_issues", []string{"bundled"})
	if h == nil {
		t.Fatal("jira profile did not resolve a handler")
	}
	// exercise it — the bundled jira profile is json_extract
	out := h("mcp__jira__search_issues", `{"issues":[{"key":"K-1","fields":{"issuetype":{"name":"Bug"},"summary":"S","status":{"name":"Open"},"assignee":{"displayName":"A"},"priority":{"name":"High"}}}]}`)
	if !strings.Contains(out.Summary, "Key: K-1") || !strings.Contains(out.Summary, "Type: Bug") {
		t.Errorf("bundled jira output wrong: %q", out.Summary)
	}
	_ = handlers.Result{}
}

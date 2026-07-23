// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/retrain_test.go

package learn

import (
	"regexp"
	"strings"
	"testing"

	"mcprecall/internal/db"
	"mcprecall/internal/jsonx"
	"mcprecall/internal/profiles"
)

func parseVal(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonx.ParseString(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}
func parseArr(t *testing.T, s string) []any {
	a, ok := parseVal(t, s).([]any)
	if !ok {
		t.Fatalf("not an array: %s", s)
	}
	return a
}

func makeProfile() profiles.Loaded {
	return profiles.Loaded{
		Spec: profiles.Spec{
			Profile:  profiles.Meta{ID: "mcp__test", Version: "1.0.0", Description: "test profile", MCPPattern: profiles.StringOrSlice{"mcp__test__*"}},
			Strategy: profiles.Strategy{Type: "json_extract", ItemsPath: []string{"items"}, Fields: []string{"id", "name"}},
		},
		Tier: profiles.TierUser, Patterns: []string{"mcp__test__*"}, FilePath: "/tmp/test_profile.toml",
	}
}

const jiraItem = `{"key":"PROJ-1","fields":{"summary":"Fix login bug","status":{"name":"In Progress"},"assignee":{"displayName":"Jane Doe"},"created":"2026-01-01"}}`

// ── detectItemsPath ───────────────────────────────────────────────────────────

func TestDetectItemsPath(t *testing.T) {
	p, items, ok := detectItemsPath(parseVal(t, `[{"id":1},{"id":2}]`))
	if !ok || p != "" || len(items) != 2 {
		t.Errorf("root array: %q %d %v", p, len(items), ok)
	}
	p, items, _ = detectItemsPath(parseVal(t, `{"issues":[{"id":1},{"id":2},{"id":3}]}`))
	if p != "issues" || len(items) != 3 {
		t.Errorf("depth-0: %q %d", p, len(items))
	}
	p, items, _ = detectItemsPath(parseVal(t, `{"data":{"nodes":[{"id":1},{"id":2}]}}`))
	if p != "data.nodes" || len(items) != 2 {
		t.Errorf("depth-1: %q %d", p, len(items))
	}
	p, items, _ = detectItemsPath(parseVal(t, `{"small":[1],"large":[1,2,3,4,5]}`))
	if p != "large" || len(items) != 5 {
		t.Errorf("largest: %q %d", p, len(items))
	}
	if _, _, ok := detectItemsPath(parseVal(t, `{"key":"value","nested":{"x":1}}`)); ok {
		t.Error("no array → false")
	}
	if _, _, ok := detectItemsPath(parseVal(t, `"string"`)); ok {
		t.Error("string → false")
	}
	if _, _, ok := detectItemsPath(parseVal(t, `null`)); ok {
		t.Error("null → false")
	}
}

// ── collectFieldPaths ─────────────────────────────────────────────────────────

func TestCollectFieldPaths(t *testing.T) {
	_, c := collectFieldPaths(parseArr(t, `[{"id":"1","name":"foo"},{"id":"2","name":"bar"}]`), 3)
	if c["id"] != 2 || c["name"] != 2 {
		t.Errorf("top-level counts: %v", c)
	}

	_, c = collectFieldPaths(parseArr(t, `[{"a":{"b":{"c":"deep","d":{"e":"tooDeep"}}}}]`), 3)
	if _, ok := c["a.b.c"]; !ok {
		t.Error("a.b.c should be collected at depth 3")
	}
	if _, ok := c["a.b.d.e"]; ok {
		t.Error("a.b.d.e should NOT be collected at depth 3")
	}

	_, c = collectFieldPaths(parseArr(t, `[{"top":{"mid":{"leaf":"val"}}}]`), 2)
	if len(c) != 0 {
		t.Errorf("maxDepth 2 should collect nothing: %v", c)
	}

	_, c = collectFieldPaths(parseArr(t, `[{"id":"1","empty":"","nothing":null}]`), 3)
	if _, ok := c["id"]; !ok {
		t.Error("id should be collected")
	}
	if _, ok := c["empty"]; ok {
		t.Error("empty string should be skipped")
	}
	if _, ok := c["nothing"]; ok {
		t.Error("null should be skipped")
	}

	_, c = collectFieldPaths(parseArr(t, `[{"tags":["a","b","c"],"name":"foo"}]`), 3)
	if _, ok := c["name"]; !ok {
		t.Error("name should be collected")
	}
	if _, ok := c["tags"]; ok {
		t.Error("array value should be skipped")
	}
	if _, ok := c["tags.0"]; ok {
		t.Error("array index should not be collected")
	}
}

// ── scoreFields ───────────────────────────────────────────────────────────────

func TestScoreFields(t *testing.T) {
	has := func(xs []scored, p string) bool {
		for _, x := range xs {
			if x.path == p {
				return true
			}
		}
		return false
	}
	r := scoreFields([]string{"common", "rare"}, map[string]int{"common": 9, "rare": 1}, 10)
	if !has(r, "common") || has(r, "rare") {
		t.Errorf("filter <50%%: %v", r)
	}
	r = scoreFields([]string{"exactly_half"}, map[string]int{"exactly_half": 5}, 10)
	if len(r) != 1 || r[0].pct != 0.5 {
		t.Errorf("exactly 50%%: %v", r)
	}
	r = scoreFields([]string{"mid", "top", "low"}, map[string]int{"mid": 7, "top": 10, "low": 6}, 10)
	if len(r) != 3 || r[0].path != "top" || r[1].path != "mid" || r[2].path != "low" {
		t.Errorf("sort desc: %v", r)
	}
	if got := scoreFields([]string{"x"}, map[string]int{"x": 3}, 0); len(got) != 0 {
		t.Errorf("total 0 → empty: %v", got)
	}
}

// ── applyRetrainToToml ────────────────────────────────────────────────────────

const baseToml = `[profile]
id          = "mcp__test"
version     = "1.0.0"
description = "test"
mcp_pattern = "mcp__test__*"

[strategy]
type       = "json_extract"
items_path = ["items"]
fields     = [
  "id",
  "name",
]
`

func TestApplyRetrainToToml(t *testing.T) {
	r := applyRetrainToToml(baseToml, []string{"status", "created_at"}, "2026-03-04")
	for _, f := range []string{`"status",`, `"created_at",`, `"id",`, `"name",`} {
		if !strings.Contains(r, f) {
			t.Errorf("missing %q", f)
		}
	}
	if !strings.Contains(r, `"1.0.1"`) || strings.Contains(r, `"1.0.0"`) {
		t.Errorf("version not bumped:\n%s", r)
	}
	if !strings.Contains(r, "# Retrained: 2026-03-04") {
		t.Errorf("missing retrain comment")
	}

	// no-op on fields when empty (but version still bumps)
	empty := applyRetrainToToml(baseToml, nil, "2026-03-04")
	fieldLineRe := regexp.MustCompile(`(?m)^\s*"[^"]+",`)
	if n := len(fieldLineRe.FindAllString(empty, -1)); n != 2 {
		t.Errorf("field count = %d, want 2", n)
	}
}

// ── retrainProfile ────────────────────────────────────────────────────────────

func sample(tool, content string) db.StoredOutput {
	return db.StoredOutput{ToolName: tool, FullContent: content}
}

func TestRetrainProfile(t *testing.T) {
	// empty samples
	r := retrainProfile(nil, makeProfile(), 3)
	if len(r.Fields) != 0 || len(r.NewFields) != 0 || r.SampleCount != 0 {
		t.Errorf("empty: %+v", r)
	}

	// non-json_extract strategy
	p := makeProfile()
	p.Spec.Strategy = profiles.Strategy{Type: "json_truncate"}
	r = retrainProfile([]db.StoredOutput{sample("mcp__test__op", `{"issues":[`+jiraItem+`]}`)}, p, 3)
	if r.StrategyType != "json_truncate" || len(r.Fields) != 0 {
		t.Errorf("non-extract: %+v", r)
	}

	// detects items_path
	r = retrainProfile([]db.StoredOutput{sample("mcp__jira__search", `{"issues":[`+jiraItem+`,`+jiraItem+`,`+jiraItem+`]}`)}, makeProfile(), 3)
	if r.DetectedItemsPath == nil || *r.DetectedItemsPath != "issues" {
		t.Errorf("detected items_path: %v", r.DetectedItemsPath)
	}

	// new fields
	r = retrainProfile([]db.StoredOutput{sample("mcp__test__list", `{"items":[`+jiraItem+`,`+jiraItem+`,`+jiraItem+`]}`)}, makeProfile(), 3)
	if !contains(r.NewFields, "key") || !contains(r.NewFields, "fields.summary") {
		t.Errorf("new fields: %v", r.NewFields)
	}

	// inProfile marking
	itm := `{"id":"1","name":"foo","extra":"bar"}`
	r = retrainProfile([]db.StoredOutput{sample("mcp__test__list", `{"items":[`+itm+`,`+itm+`]}`)}, makeProfile(), 3)
	inProfile := map[string]bool{}
	for _, f := range r.Fields {
		inProfile[f.Path] = f.InProfile
	}
	if !inProfile["id"] || !inProfile["name"] || inProfile["extra"] {
		t.Errorf("inProfile flags: %v", inProfile)
	}

	// maxDepth
	deep := `{"a":{"b":{"c":{"d":"too deep"},"shallow":"ok"}}}`
	r = retrainProfile([]db.StoredOutput{sample("mcp__test__list", `{"items":[`+deep+`,`+deep+`]}`)}, makeProfile(), 3)
	hasField := func(p string) bool {
		for _, f := range r.Fields {
			if f.Path == p {
				return true
			}
		}
		return false
	}
	if !hasField("a.b.shallow") {
		t.Error("a.b.shallow (depth 3) should be found")
	}
	if hasField("a.b.c.d") {
		t.Error("a.b.c.d (depth 4) should NOT be found at maxDepth 3")
	}
}

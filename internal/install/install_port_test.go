// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/install/install_port_test.go
//
// Port of tests/install.test.ts (upstream v1.9.0) granular assertions.
// The Go port exposes these as same-package helpers (sessionStartEntry,
// isOurSessionStart, injectClaudeMd, …) rather than exported symbols.

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcprecall/internal/jsonx"
)

const fakeCLI = "/fake/mcp-recall/dist/cli.js"

// ── isOurSessionStart ─────────────────────────────────────────────────────────

func TestIsOurSessionStart(t *testing.T) {
	if !isOurSessionStart(sessionStartEntry(fakeCLI)) {
		t.Error("should recognise our session-start entry")
	}
	other := jsonx.NewObj()
	h := jsonx.NewObj()
	h.Set("type", "command")
	h.Set("command", "bun /other/tool/cli.js session-start")
	other.Set("hooks", []any{h})
	if isOurSessionStart(other) {
		t.Error("should reject a different tool's session-start hook")
	}
	if isOurSessionStart(nil) || isOurSessionStart("string") {
		t.Error("should reject null / non-object")
	}
}

// ── isOurPostToolUse ──────────────────────────────────────────────────────────

func TestIsOurPostToolUse(t *testing.T) {
	if !isOurPostToolUse(postToolUseEntry(fakeCLI)) {
		t.Error("should recognise our post-tool-use entry")
	}
	// different matcher
	e1 := jsonx.NewObj()
	e1.Set("matcher", "mcp__other__*")
	h1 := jsonx.NewObj()
	h1.Set("type", "command")
	h1.Set("command", "bun "+fakeCLI+" post-tool-use")
	e1.Set("hooks", []any{h1})
	if isOurPostToolUse(e1) {
		t.Error("should reject a hook with a different matcher")
	}
	// our matcher, different command
	e2 := jsonx.NewObj()
	e2.Set("matcher", PostToolUseMatcher)
	h2 := jsonx.NewObj()
	h2.Set("type", "command")
	h2.Set("command", "bun /other/tool/cli.js some-hook")
	e2.Set("hooks", []any{h2})
	if isOurPostToolUse(e2) {
		t.Error("should reject our matcher with a different command")
	}
}

// ── writeObj / loadObj (order-preserving JSON IO) ─────────────────────────────

func TestJSONFileRoundTrip(t *testing.T) {
	dir := t.TempDir()

	p := filepath.Join(dir, "test.json")
	o := jsonx.NewObj()
	o.Set("foo", "bar")
	nested := jsonx.NewObj()
	nested.Set("x", float64(1))
	o.Set("nested", nested)
	if err := writeObj(p, o); err != nil {
		t.Fatal(err)
	}
	got, err := loadObj(p)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Str("foo"); v != "bar" {
		t.Errorf("foo = %q", v)
	}
	if n := getObj(got, "nested"); n == nil {
		t.Error("nested object lost")
	}

	// missing file → empty object
	empty, err := loadObj(filepath.Join(dir, "nonexistent.json"))
	if err != nil || empty == nil || len(empty.Keys()) != 0 {
		t.Errorf("missing file should yield empty obj: %v %v", empty, err)
	}

	// creates parent directories
	deep := filepath.Join(dir, "a", "b", "c.json")
	ok := jsonx.NewObj()
	ok.Set("ok", true)
	if err := writeObj(deep, ok); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Error("parent directories not created")
	}
}

// ── JSON merge — adds our entries non-destructively ───────────────────────────

func TestInstallPreservesPreExistingHooks(t *testing.T) {
	p := tempPaths(t)
	os.MkdirAll(filepath.Dir(p.Settings), 0o755)
	os.WriteFile(p.Settings, []byte(`{"hooks":{"PostToolUse":[{"matcher":"TaskCreate|TaskUpdate","hooks":[{"type":"command","command":"/usr/local/bin/vikunja-sync"}]}]}}`), 0o644)

	if _, err := Install(p); err != nil {
		t.Fatal(err)
	}
	settings, _ := loadObj(p.Settings)
	ptu := getArr(getObj(settings, "hooks"), "PostToolUse")
	if len(ptu) != 2 {
		t.Fatalf("expected 2 PostToolUse hooks, got %d", len(ptu))
	}
	if m, _ := ptu[0].(*jsonx.Obj).Str("matcher"); m != "TaskCreate|TaskUpdate" {
		t.Errorf("pre-existing hook not first: %q", m)
	}
	if m, _ := ptu[1].(*jsonx.Obj).Str("matcher"); m != PostToolUseMatcher {
		t.Errorf("our hook not appended: %q", m)
	}
}

func TestInstallNoDuplicateSessionStart(t *testing.T) {
	p := tempPaths(t)
	Install(p)
	Install(p) // second run
	settings, _ := loadObj(p.Settings)
	ss := getArr(getObj(settings, "hooks"), "SessionStart")
	if len(ss) != 1 {
		t.Fatalf("expected 1 SessionStart hook, got %d", len(ss))
	}
}

func TestInstallUpdatesStalePath(t *testing.T) {
	p := tempPaths(t)
	// Seed a stale session-start hook pointing at an old binary path.
	os.MkdirAll(filepath.Dir(p.Settings), 0o755)
	os.WriteFile(p.Settings, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/old/mcprecall session-start","timeout":10}]}]}}`), 0o644)

	if _, err := Install(p); err != nil {
		t.Fatal(err)
	}
	settings, _ := loadObj(p.Settings)
	ss := getArr(getObj(settings, "hooks"), "SessionStart")
	if len(ss) != 1 {
		t.Fatalf("expected 1 SessionStart hook, got %d", len(ss))
	}
	if got := entryCommand(ss[0]); got != p.Binary+" session-start" {
		t.Errorf("stale path not updated: %q", got)
	}
}

// ── uninstall removes only our entries ────────────────────────────────────────

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	p := tempPaths(t)
	// Seed an unrelated PostToolUse hook, then install ours after it.
	os.MkdirAll(filepath.Dir(p.Settings), 0o755)
	os.WriteFile(p.Settings, []byte(`{"hooks":{"PostToolUse":[{"matcher":"TaskCreate|TaskUpdate","hooks":[{"type":"command","command":"/usr/local/bin/vikunja-sync"}]}]}}`), 0o644)
	Install(p)

	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	claude, _ := loadObj(p.ClaudeJSON)
	if servers := getObj(claude, "mcpServers"); servers != nil {
		if _, ok := servers.Get("recall"); ok {
			t.Error("recall server should be removed")
		}
	}
	settings, _ := loadObj(p.Settings)
	hooks := getObj(settings, "hooks")
	if ss := getArr(hooks, "SessionStart"); len(ss) != 0 {
		t.Errorf("SessionStart should be empty, got %d", len(ss))
	}
	ptu := getArr(hooks, "PostToolUse")
	if len(ptu) != 1 {
		t.Fatalf("PostToolUse should have 1 (the other), got %d", len(ptu))
	}
	if m, _ := ptu[0].(*jsonx.Obj).Str("matcher"); m != "TaskCreate|TaskUpdate" {
		t.Errorf("wrong surviving hook: %q", m)
	}
}

func TestUninstallNoOpWhenNothingInstalled(t *testing.T) {
	p := tempPaths(t)
	if _, err := Uninstall(p); err != nil {
		t.Errorf("uninstall on clean state should not error: %v", err)
	}
}

// ── mcpServers merge ──────────────────────────────────────────────────────────

func TestInstallPreservesOtherMcpServers(t *testing.T) {
	p := tempPaths(t)
	os.WriteFile(p.ClaudeJSON, []byte(`{"mcpServers":{"github":{"type":"stdio","command":"bun","args":["/other/github.js"]}}}`), 0o644)
	Install(p)
	claude, _ := loadObj(p.ClaudeJSON)
	servers := getObj(claude, "mcpServers")
	if servers == nil {
		t.Fatal("mcpServers missing")
	}
	if _, ok := servers.Get("github"); !ok {
		t.Error("github server should be preserved")
	}
	if _, ok := servers.Get("recall"); !ok {
		t.Error("recall server should be added")
	}
}

// ── CLAUDE.md helpers ─────────────────────────────────────────────────────────

func claudeMdInjected(content string) bool { return strings.Contains(content, claudeMdStart) }

func TestClaudeMdInjectedDetection(t *testing.T) {
	if !claudeMdInjected("# My notes\n\n" + claudeMdBlock() + "\n") {
		t.Error("should detect marker present")
	}
	if claudeMdInjected("# My notes\n\nsome content\n") {
		t.Error("should be false when marker absent")
	}
	if claudeMdInjected("") {
		t.Error("should be false for empty string")
	}
}

func TestInjectClaudeMd(t *testing.T) {
	t.Run("creates file and adds block", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		res, err := injectClaudeMd(p)
		if err != nil || res != "added" {
			t.Fatalf("res=%q err=%v", res, err)
		}
		data, _ := os.ReadFile(p)
		if !strings.Contains(string(data), claudeMdStart) || !strings.Contains(string(data), claudeMdEnd) {
			t.Error("markers missing")
		}
	})
	t.Run("appends to existing content", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		os.WriteFile(p, []byte("# Existing notes\n\nsome content\n"), 0o644)
		injectClaudeMd(p)
		data, _ := os.ReadFile(p)
		if !strings.Contains(string(data), "# Existing notes") || !strings.Contains(string(data), claudeMdStart) {
			t.Error("should preserve existing and add block")
		}
	})
	t.Run("returns present when already correct", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		injectClaudeMd(p)
		res, _ := injectClaudeMd(p)
		if res != "present" {
			t.Errorf("res = %q", res)
		}
	})
	t.Run("updates stale block", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		stale := claudeMdStart + "\nold content\n" + claudeMdEnd
		os.WriteFile(p, []byte("# Notes\n\n"+stale+"\n"), 0o644)
		res, _ := injectClaudeMd(p)
		if res != "updated" {
			t.Fatalf("res = %q", res)
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "old content") || !strings.Contains(string(data), claudeMdBlock()) {
			t.Error("stale block not replaced")
		}
	})
}

func TestRemoveClaudeMd(t *testing.T) {
	t.Run("removes block returns true", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		os.WriteFile(p, []byte("# Notes\n\n"+claudeMdBlock()+"\n"), 0o644)
		removed, _ := removeClaudeMd(p)
		if !removed {
			t.Fatal("should return true")
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), claudeMdStart) || !strings.Contains(string(data), "# Notes") {
			t.Error("block not removed / notes lost")
		}
	})
	t.Run("leaves surrounding content intact", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		os.WriteFile(p, []byte("# Before\n\n"+claudeMdBlock()+"\n\n# After\n"), 0o644)
		removeClaudeMd(p)
		data, _ := os.ReadFile(p)
		s := string(data)
		if !strings.Contains(s, "# Before") || !strings.Contains(s, "# After") || strings.Contains(s, claudeMdStart) {
			t.Errorf("surrounding content damaged:\n%s", s)
		}
	})
	t.Run("false when marker absent", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "CLAUDE.md")
		os.WriteFile(p, []byte("# Notes\n\nno recall block here\n"), 0o644)
		if removed, _ := removeClaudeMd(p); removed {
			t.Error("should be false when marker absent")
		}
	})
	t.Run("false when file missing", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "nonexistent-CLAUDE.md")
		if removed, _ := removeClaudeMd(p); removed {
			t.Error("should be false when file missing")
		}
	})
}

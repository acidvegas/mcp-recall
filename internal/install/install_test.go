// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/install/install_test.go

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempPaths(t *testing.T) Paths {
	d := t.TempDir()
	return Paths{
		ClaudeJSON: filepath.Join(d, ".claude.json"),
		Settings:   filepath.Join(d, ".claude", "settings.json"),
		ClaudeMD:   filepath.Join(d, ".claude", "CLAUDE.md"),
		Binary:     "/opt/mcprecall/mcprecall",
	}
}

func TestInstallIsNonDestructiveAndOrderPreserving(t *testing.T) {
	p := tempPaths(t)
	// Seed with unrelated settings around mcpServers to verify preservation + order.
	os.WriteFile(p.ClaudeJSON, []byte(`{"numStartups":5,"mcpServers":{"other":{"type":"stdio","command":"foo"}},"theme":"dark"}`), 0o644)

	changes, err := Install(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("expected changes on first install")
	}

	got, _ := os.ReadFile(p.ClaudeJSON)
	s := string(got)
	// order preserved: numStartups before mcpServers before theme
	if !(strings.Index(s, "numStartups") < strings.Index(s, "mcpServers") && strings.Index(s, "mcpServers") < strings.Index(s, "theme")) {
		t.Errorf("key order not preserved:\n%s", s)
	}
	// unrelated server + our server both present
	if !strings.Contains(s, `"other"`) || !strings.Contains(s, `"recall"`) {
		t.Errorf("expected both other + recall servers:\n%s", s)
	}
	if !strings.Contains(s, `/opt/mcprecall/mcprecall`) {
		t.Errorf("binary path not written:\n%s", s)
	}

	set, _ := os.ReadFile(p.Settings)
	ss := string(set)
	if !strings.Contains(ss, "session-start") || !strings.Contains(ss, "post-tool-use") {
		t.Errorf("hooks not written:\n%s", ss)
	}
	if !strings.Contains(ss, PostToolUseMatcher) {
		t.Errorf("matcher not written:\n%s", ss)
	}

	md, _ := os.ReadFile(p.ClaudeMD)
	if !strings.Contains(string(md), claudeMdStart) || !strings.Contains(string(md), "recall__context") {
		t.Errorf("CLAUDE.md block missing")
	}
}

func TestInstallIdempotent(t *testing.T) {
	p := tempPaths(t)
	if _, err := Install(p); err != nil {
		t.Fatal(err)
	}
	changes, err := Install(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("second install should be a no-op, got: %v", changes)
	}
	st := Status(p)
	if !st.ServerRegistered || !st.SessionStartHook || !st.PostToolUseHook || !st.ClaudeMD {
		t.Errorf("status incomplete after install: %+v", st)
	}
}

func TestUninstallLeavesOtherSettings(t *testing.T) {
	p := tempPaths(t)
	os.WriteFile(p.ClaudeJSON, []byte(`{"mcpServers":{"other":{"command":"foo"}},"theme":"dark"}`), 0o644)
	Install(p)
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p.ClaudeJSON)
	s := string(got)
	if strings.Contains(s, `"recall"`) {
		t.Errorf("recall server should be removed:\n%s", s)
	}
	if !strings.Contains(s, `"other"`) || !strings.Contains(s, `"theme"`) {
		t.Errorf("unrelated settings must survive uninstall:\n%s", s)
	}
	st := Status(p)
	if st.ServerRegistered || st.SessionStartHook || st.PostToolUseHook || st.ClaudeMD {
		t.Errorf("status should be clean after uninstall: %+v", st)
	}
}

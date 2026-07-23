// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/config/config_test.go

package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mcprecall/internal/logx"
)

func withFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if content != "" {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		p = filepath.Join(t.TempDir(), "missing.toml")
	}
	os.Setenv("RECALL_CONFIG_PATH", p)
	Reset()
	t.Cleanup(func() { os.Unsetenv("RECALL_CONFIG_PATH"); Reset() })
	return p
}

func TestDefaults(t *testing.T) {
	withFile(t, "")
	c := Load()
	if c.Store.ExpireAfterSessionDays != 30 || c.Store.Key != "git_root" || c.Store.MaxSizeMB != 500 ||
		c.Store.PinRecommendationThreshold != 5 || c.Store.StaleItemDays != 3 || c.Store.EvictionHalfLifeDays != 7 {
		t.Fatalf("store defaults: %+v", c.Store)
	}
	if c.Retrieve.DefaultMaxBytes != 8192 || len(c.Denylist.Additional) != 0 || len(c.Denylist.OverrideDefaults) != 0 {
		t.Fatalf("other defaults: %+v %+v", c.Retrieve, c.Denylist)
	}
	if c.Debug.Enabled {
		t.Error("debug.enabled should default false")
	}
}

func TestCachingAndReset(t *testing.T) {
	p := withFile(t, "[store]\nmax_size_mb = 100\n")
	if Load().Store.MaxSizeMB != 100 {
		t.Fatal("initial load")
	}
	os.WriteFile(p, []byte("[store]\nmax_size_mb = 200\n"), 0o644)
	if Load().Store.MaxSizeMB != 100 {
		t.Error("second Load should be cached (100)")
	}
	Reset()
	if Load().Store.MaxSizeMB != 200 {
		t.Error("after Reset should re-read (200)")
	}
}

func TestMergePartial(t *testing.T) {
	withFile(t, "[store]\nmax_size_mb = 1024\n")
	c := Load()
	if c.Store.MaxSizeMB != 1024 || c.Store.ExpireAfterSessionDays != 30 || c.Store.Key != "git_root" || c.Retrieve.DefaultMaxBytes != 8192 {
		t.Errorf("merge: %+v", c.Store)
	}
}

func TestInvalidAndMalformed(t *testing.T) {
	withFile(t, "[store]\nkey = \"invalid_value\"\n")
	if Load().Store.Key != "git_root" {
		t.Error("invalid value should fall back to defaults")
	}
	withFile(t, "this is not @@## valid toml")
	if Load().Store.ExpireAfterSessionDays != 30 {
		t.Error("malformed TOML should fall back to defaults")
	}
}

func TestStripsUnknownKeys(t *testing.T) {
	withFile(t, "[store]\nmax_size_mb = 256\nunknown_key = true\n")
	if Load().Store.MaxSizeMB != 256 {
		t.Error("known key should apply, unknown ignored")
	}
}

func TestDebugFromTOML(t *testing.T) {
	withFile(t, "")
	if Load().Debug.Enabled {
		t.Error("default false")
	}
	withFile(t, "[debug]\nenabled = true\n")
	if !Load().Debug.Enabled {
		t.Error("should read true from TOML")
	}
}

// config-driven debug output (Load() syncs debug into logx).
func TestConfigDrivenDebugOutput(t *testing.T) {
	capture := func(fn func()) string {
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		fn()
		w.Close()
		os.Stderr = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		return buf.String()
	}
	os.Unsetenv("RECALL_DEBUG")

	withFile(t, "[debug]\nenabled = false\n")
	Load()
	if got := capture(func() { logx.Debug("nope") }); got != "" {
		t.Errorf("disabled config should be silent: %q", got)
	}

	withFile(t, "[debug]\nenabled = true\n")
	Load()
	if got := capture(func() { logx.Debug("config-based debug") }); got != "[mcp-recall] debug: config-based debug\n" {
		t.Errorf("enabled config debug = %q", got)
	}

	// RECALL_DEBUG=1 overrides even with disabled config
	withFile(t, "[debug]\nenabled = false\n")
	Load()
	os.Setenv("RECALL_DEBUG", "1")
	if got := capture(func() { logx.Debug("env override") }); got != "[mcp-recall] debug: env override\n" {
		t.Errorf("env override = %q", got)
	}
	os.Unsetenv("RECALL_DEBUG")
	logx.SetDebugEnabled(false)
}

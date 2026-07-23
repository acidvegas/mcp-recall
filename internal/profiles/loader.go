// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/loader.go

package profiles

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"mcprecall/internal/logx"
)

//go:embed all:bundled
var bundledFS embed.FS

// ── directory resolution ──────────────────────────────────────────────────────

func userDir() string {
	if p := os.Getenv("RECALL_USER_PROFILES_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mcp-recall", "profiles")
}

func communityDir() string {
	if p := os.Getenv("RECALL_COMMUNITY_PROFILES_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "mcp-recall", "profiles", "community")
}

// ── validation ────────────────────────────────────────────────────────────────

var validTypes = map[string]bool{"json_extract": true, "json_truncate": true, "text_truncate": true}

func validateSpec(spec Spec, filePath string) bool {
	if spec.Profile.ID == "" || spec.Profile.Version == "" || spec.Profile.Description == "" || len(spec.Profile.MCPPattern) == 0 {
		logx.Debug("profile skip · missing required fields · " + filePath)
		return false
	}
	if !validTypes[spec.Strategy.Type] {
		logx.Debug("profile skip · unknown strategy.type \"" + spec.Strategy.Type + "\" · " + filePath)
		return false
	}
	if spec.Strategy.Type == "json_extract" && len(spec.Strategy.Fields) == 0 {
		logx.Debug("profile skip · json_extract missing fields · " + filePath)
		return false
	}
	ceilings := []struct {
		name    string
		val     *int
		ceiling int
	}{
		{"max_depth", spec.Strategy.MaxDepth, 20},
		{"max_items", spec.Strategy.MaxItems, 1000},
		{"max_array_items", spec.Strategy.MaxArrayItems, 1000},
		{"max_chars", spec.Strategy.MaxChars, 1000000},
		{"max_chars_per_field", spec.Strategy.MaxCharsPerField, 100000},
		{"fallback_chars", spec.Strategy.FallbackChars, 100000},
	}
	for _, c := range ceilings {
		if c.val != nil && *c.val > c.ceiling {
			logx.Debug("profile skip · " + c.name + " exceeds maximum · " + filePath)
			return false
		}
	}
	return true
}

func parseSpec(data []byte, filePath string) (Spec, bool) {
	var spec Spec
	if _, err := toml.Decode(string(data), &spec); err != nil {
		logx.Debug("profile parse error · " + filePath + ": " + err.Error())
		return Spec{}, false
	}
	if !validateSpec(spec, filePath) {
		return Spec{}, false
	}
	return spec, true
}

// ── scanning ──────────────────────────────────────────────────────────────────

func scanFS(fsys fs.FS, displayPrefix string, tier Tier) []Loaded {
	var out []Loaded
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".toml") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		display := p
		if displayPrefix != "" {
			display = filepath.Join(displayPrefix, p)
		}
		spec, ok := parseSpec(data, display)
		if !ok {
			return nil
		}
		out = append(out, Loaded{Spec: spec, Tier: tier, Patterns: []string(spec.Profile.MCPPattern), FilePath: display})
		return nil
	})
	return out
}

func scanDir(dir string, tier Tier) []Loaded {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return scanFS(os.DirFS(dir), dir, tier)
}

func scanBundled() []Loaded {
	if override := os.Getenv("RECALL_BUNDLED_PROFILES_PATH"); override != "" {
		return scanDir(override, TierBundled)
	}
	sub, err := fs.Sub(bundledFS, "bundled")
	if err != nil {
		return nil
	}
	return scanFS(sub, "bundled", TierBundled)
}

// Load returns all profiles across the three tiers (user first, then community,
// then bundled).
func Load() []Loaded {
	var all []Loaded
	all = append(all, scanDir(userDir(), TierUser)...)
	all = append(all, scanDir(communityDir(), TierCommunity)...)
	all = append(all, scanBundled()...)
	return all
}

// installedCommunityMap returns id → version for installed community profiles.
func installedCommunityMap() map[string]string {
	out := map[string]string{}
	dir := communityDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(path.Join(dir, e.Name(), "default.toml"))
		if err != nil {
			continue
		}
		var spec Spec
		if _, err := toml.Decode(string(data), &spec); err != nil {
			continue
		}
		v := spec.Profile.Version
		if v == "" {
			v = "0.0.0"
		}
		out[e.Name()] = v
	}
	return out
}

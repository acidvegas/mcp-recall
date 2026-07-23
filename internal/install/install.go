// Package install registers/unregisters the mcp-recall Go binary with Claude
// Code: the MCP server entry in ~/.claude.json, the SessionStart/PostToolUse
// hooks in ~/.claude/settings.json, and an instruction block in ~/.claude/CLAUDE.md.
// Ports src/install/index.ts. Config JSON is read and rewritten order-preserving
// so unrelated user settings keep their position.
package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mcprecall/internal/jsonx"
)

// PostToolUseMatcher excludes recall's own tools and matches all other MCP
// tools plus the native Bash tool. Evaluated by Claude Code (JS regex), so the
// negative lookahead is fine there.
const PostToolUseMatcher = "(mcp__(?!recall__).*|Bash)"

const (
	claudeMdStart = "<!-- BEGIN mcp-recall -->"
	claudeMdEnd   = "<!-- END mcp-recall -->"
)

const claudeMdSnippet = `## mcp-recall

Session context from previous sessions is automatically injected at startup (pinned items, notes, recent activity). If it was truncated, call ` + "`recall__context()`" + ` for the full view.

When a tool output was compressed by mcp-recall (you'll see a summary with a recall ID like ` + "`recall_abc123`" + `), call ` + "`recall__retrieve(\"recall_abc123\")`" + ` when you need the full content.

Proactively:
- ` + "`recall__note(\"…\")`" + ` — save important decisions or context worth keeping across sessions
- ` + "`recall__pin(\"recall_abc123\")`" + ` — protect frequently-needed items from expiry and eviction
- ` + "`recall__search(\"query\")`" + ` — find stored outputs by content when you don't have an ID`

func claudeMdBlock() string { return claudeMdStart + "\n" + claudeMdSnippet + "\n" + claudeMdEnd }

// Paths bundles the three config file locations (overridable for tests).
type Paths struct {
	ClaudeJSON string
	Settings   string
	ClaudeMD   string
	Binary     string // path to the mcprecall binary
}

// DefaultPaths returns the standard locations plus the running binary path.
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	bin, _ := os.Executable()
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	return Paths{
		ClaudeJSON: filepath.Join(home, ".claude.json"),
		Settings:   filepath.Join(home, ".claude", "settings.json"),
		ClaudeMD:   filepath.Join(home, ".claude", "CLAUDE.md"),
		Binary:     bin,
	}
}

// ── order-preserving JSON file IO ─────────────────────────────────────────────

func loadObj(path string) (*jsonx.Obj, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return jsonx.NewObj(), nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return jsonx.NewObj(), nil
	}
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}
	o, ok := v.(*jsonx.Obj)
	if !ok {
		return jsonx.NewObj(), nil
	}
	return o, nil
}

func writeObj(path string, o *jsonx.Obj) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := jsonx.Indent(o) + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ── hook entry builders / detectors ───────────────────────────────────────────

func sessionStartEntry(bin string) *jsonx.Obj {
	hook := jsonx.NewObj()
	hook.Set("type", "command")
	hook.Set("command", bin+" session-start")
	hook.Set("timeout", float64(10))
	e := jsonx.NewObj()
	e.Set("hooks", []any{hook})
	return e
}

func postToolUseEntry(bin string) *jsonx.Obj {
	hook := jsonx.NewObj()
	hook.Set("type", "command")
	hook.Set("command", bin+" post-tool-use")
	hook.Set("timeout", float64(10))
	e := jsonx.NewObj()
	e.Set("matcher", PostToolUseMatcher)
	e.Set("hooks", []any{hook})
	return e
}

func entryCommand(entry any) string {
	o, ok := entry.(*jsonx.Obj)
	if !ok {
		return ""
	}
	hooks, ok := o.Get("hooks")
	if !ok {
		return ""
	}
	arr, ok := hooks.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	h0, ok := arr[0].(*jsonx.Obj)
	if !ok {
		return ""
	}
	cmd, _ := h0.Str("command")
	return cmd
}

func isOurSessionStart(entry any) bool {
	cmd := entryCommand(entry)
	return strings.Contains(cmd, "recall") && strings.Contains(cmd, "session-start")
}

func isOurPostToolUse(entry any) bool {
	o, ok := entry.(*jsonx.Obj)
	if !ok {
		return false
	}
	if m, _ := o.Str("matcher"); m != PostToolUseMatcher {
		return false
	}
	cmd := entryCommand(entry)
	return strings.Contains(cmd, "recall") && strings.Contains(cmd, "post-tool-use")
}

// getArr returns the array at key, or an empty slice.
func getArr(o *jsonx.Obj, key string) []any {
	if v, ok := o.Get(key); ok {
		if a, ok := v.([]any); ok {
			return a
		}
	}
	return nil
}

func getObj(o *jsonx.Obj, key string) *jsonx.Obj {
	if v, ok := o.Get(key); ok {
		if x, ok := v.(*jsonx.Obj); ok {
			return x
		}
	}
	return nil
}

// ── install ───────────────────────────────────────────────────────────────────

// Install registers the MCP server, hooks, and CLAUDE.md block. Idempotent.
// Returns a list of human-readable change lines.
func Install(p Paths) ([]string, error) {
	var changes []string

	// ~/.claude.json — mcpServers.recall
	root, err := loadObj(p.ClaudeJSON)
	if err != nil {
		return nil, err
	}
	servers := getObj(root, "mcpServers")
	if servers == nil {
		servers = jsonx.NewObj()
		root.Set("mcpServers", servers)
	}
	newServer := jsonx.NewObj()
	newServer.Set("type", "stdio")
	newServer.Set("command", p.Binary)
	newServer.Set("args", []any{"server"})

	existing := getObj(servers, "recall")
	curCmd := ""
	if existing != nil {
		curCmd, _ = existing.Str("command")
	}
	if existing == nil {
		servers.Set("recall", newServer)
		if err := writeObj(p.ClaudeJSON, root); err != nil {
			return nil, err
		}
		changes = append(changes, "MCP server registered")
	} else if curCmd != p.Binary {
		servers.Set("recall", newServer)
		if err := writeObj(p.ClaudeJSON, root); err != nil {
			return nil, err
		}
		changes = append(changes, "MCP server path updated")
	}

	// ~/.claude/settings.json — hooks
	settings, err := loadObj(p.Settings)
	if err != nil {
		return nil, err
	}
	hooks := getObj(settings, "hooks")
	if hooks == nil {
		hooks = jsonx.NewObj()
		settings.Set("hooks", hooks)
	}
	settingsChanged := false

	ss := getArr(hooks, "SessionStart")
	ssIdx := -1
	for i, e := range ss {
		if isOurSessionStart(e) {
			ssIdx = i
			break
		}
	}
	newSS := sessionStartEntry(p.Binary)
	if ssIdx == -1 {
		hooks.Set("SessionStart", append(ss, newSS))
		settingsChanged = true
		changes = append(changes, "SessionStart hook added")
	} else if entryCommand(ss[ssIdx]) != entryCommand(newSS) {
		ss[ssIdx] = newSS
		hooks.Set("SessionStart", ss)
		settingsChanged = true
		changes = append(changes, "SessionStart hook updated")
	}

	ptu := getArr(hooks, "PostToolUse")
	ptuIdx := -1
	for i, e := range ptu {
		if isOurPostToolUse(e) {
			ptuIdx = i
			break
		}
	}
	newPTU := postToolUseEntry(p.Binary)
	if ptuIdx == -1 {
		hooks.Set("PostToolUse", append(ptu, newPTU))
		settingsChanged = true
		changes = append(changes, "PostToolUse hook added")
	} else if entryCommand(ptu[ptuIdx]) != entryCommand(newPTU) {
		ptu[ptuIdx] = newPTU
		hooks.Set("PostToolUse", ptu)
		settingsChanged = true
		changes = append(changes, "PostToolUse hook updated")
	}

	if settingsChanged {
		if err := writeObj(p.Settings, settings); err != nil {
			return nil, err
		}
	}

	// ~/.claude/CLAUDE.md
	res, err := injectClaudeMd(p.ClaudeMD)
	if err != nil {
		return nil, err
	}
	if res != "present" {
		changes = append(changes, "CLAUDE.md instructions "+res)
	}

	return changes, nil
}

func injectClaudeMd(path string) (string, error) {
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	block := claudeMdBlock()
	start := strings.Index(existing, claudeMdStart)
	end := strings.Index(existing, claudeMdEnd)
	if start != -1 && end != -1 {
		cur := existing[start : end+len(claudeMdEnd)]
		if cur == block {
			return "present", nil
		}
		updated := existing[:start] + block + existing[end+len(claudeMdEnd):]
		return "updated", writeText(path, updated)
	}
	var content string
	if existing != "" {
		content = strings.TrimRight(existing, "\n\t ") + "\n\n" + block + "\n"
	} else {
		content = block + "\n"
	}
	return "added", writeText(path, content)
}

// ── uninstall ─────────────────────────────────────────────────────────────────

// Uninstall removes the MCP server, hooks, and CLAUDE.md block.
func Uninstall(p Paths) ([]string, error) {
	var changes []string

	if root, err := loadObj(p.ClaudeJSON); err == nil {
		if servers := getObj(root, "mcpServers"); servers != nil {
			if _, ok := servers.Get("recall"); ok {
				servers.Delete("recall")
				if err := writeObj(p.ClaudeJSON, root); err != nil {
					return nil, err
				}
				changes = append(changes, "Removed mcpServers.recall")
			}
		}
	}

	if settings, err := loadObj(p.Settings); err == nil {
		if hooks := getObj(settings, "hooks"); hooks != nil {
			changed := false
			if ss := getArr(hooks, "SessionStart"); ss != nil {
				var kept []any
				for _, e := range ss {
					if !isOurSessionStart(e) {
						kept = append(kept, e)
					}
				}
				if len(kept) != len(ss) {
					hooks.Set("SessionStart", kept)
					changed = true
					changes = append(changes, "Removed SessionStart hook")
				}
			}
			if ptu := getArr(hooks, "PostToolUse"); ptu != nil {
				var kept []any
				for _, e := range ptu {
					if !isOurPostToolUse(e) {
						kept = append(kept, e)
					}
				}
				if len(kept) != len(ptu) {
					hooks.Set("PostToolUse", kept)
					changed = true
					changes = append(changes, "Removed PostToolUse hook")
				}
			}
			if changed {
				if err := writeObj(p.Settings, settings); err != nil {
					return nil, err
				}
			}
		}
	}

	if removed, err := removeClaudeMd(p.ClaudeMD); err == nil && removed {
		changes = append(changes, "Removed CLAUDE.md instructions")
	}
	return changes, nil
}

func removeClaudeMd(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	existing := string(data)
	start := strings.Index(existing, claudeMdStart)
	end := strings.Index(existing, claudeMdEnd)
	if start == -1 || end == -1 {
		return false, nil
	}
	before := strings.TrimRight(existing[:start], "\n\t ")
	after := strings.TrimLeft(existing[end+len(claudeMdEnd):], "\n")
	result := strings.TrimRight(before+"\n"+after, "\n\t ")
	if result != "" {
		result += "\n"
	}
	return true, writeText(path, result)
}

// ── status ────────────────────────────────────────────────────────────────────

// Status reports whether each piece is registered.
type StatusReport struct {
	ServerRegistered bool
	SessionStartHook bool
	PostToolUseHook  bool
	ClaudeMD         bool
	BinaryExists     bool
}

// Status inspects the config files and returns a StatusReport.
func Status(p Paths) StatusReport {
	var r StatusReport
	if root, err := loadObj(p.ClaudeJSON); err == nil {
		if servers := getObj(root, "mcpServers"); servers != nil {
			_, r.ServerRegistered = servers.Get("recall")
		}
	}
	if settings, err := loadObj(p.Settings); err == nil {
		if hooks := getObj(settings, "hooks"); hooks != nil {
			for _, e := range getArr(hooks, "SessionStart") {
				if isOurSessionStart(e) {
					r.SessionStartHook = true
				}
			}
			for _, e := range getArr(hooks, "PostToolUse") {
				if isOurPostToolUse(e) {
					r.PostToolUseHook = true
				}
			}
		}
	}
	if data, err := os.ReadFile(p.ClaudeMD); err == nil {
		r.ClaudeMD = strings.Contains(string(data), claudeMdStart)
	}
	if _, err := os.Stat(p.Binary); err == nil {
		r.BinaryExists = true
	}
	return r
}

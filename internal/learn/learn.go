// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/learn.go

package learn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcprecall/internal/jsonx"
)

type serverConfig struct {
	command string
	args    []string
	env     map[string]string
	url     string
}

type serverEntry struct {
	key string
	cfg serverConfig
}

func userProfilesDir() string {
	if p := os.Getenv("RECALL_USER_PROFILES_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mcp-recall", "profiles")
}

func readClaudeServers() ([]serverEntry, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return nil, err
	}
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	root, ok := v.(*jsonx.Obj)
	if !ok {
		return nil, fmt.Errorf("bad shape")
	}
	sv, ok := root.Get("mcpServers")
	if !ok {
		return nil, nil
	}
	servers, ok := sv.(*jsonx.Obj)
	if !ok {
		return nil, nil
	}
	var out []serverEntry
	for _, key := range servers.Keys() {
		cv, _ := servers.Get(key)
		co, ok := cv.(*jsonx.Obj)
		if !ok {
			continue
		}
		cfg := serverConfig{}
		cfg.command, _ = co.Str("command")
		cfg.url, _ = co.Str("url")
		if av, ok := co.Get("args"); ok {
			if arr, ok := av.([]any); ok {
				for _, e := range arr {
					if s, ok := e.(string); ok {
						cfg.args = append(cfg.args, s)
					}
				}
			}
		}
		if ev, ok := co.Get("env"); ok {
			if eo, ok := ev.(*jsonx.Obj); ok {
				cfg.env = map[string]string{}
				for _, k := range eo.Keys() {
					if s, ok := eo.Str(k); ok {
						cfg.env[k] = s
					}
				}
			}
		}
		out = append(out, serverEntry{key: key, cfg: cfg})
	}
	return out, nil
}

// HandleLearn implements `mcp-recall learn`.
func HandleLearn(args []string) {
	dryRun := false
	var targets []string
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		} else if !strings.HasPrefix(a, "--") {
			targets = append(targets, a)
		}
	}

	servers, err := readClaudeServers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not read ~/.claude.json")
		os.Exit(1)
	}

	var candidates []serverEntry
	for _, s := range servers {
		if s.key == "recall" {
			continue
		}
		if len(targets) > 0 && !contains(targets, s.key) {
			continue
		}
		if s.cfg.command == "" && s.cfg.url == "" {
			fmt.Printf("  %s: skipped (no command or url in config)\n", s.key)
			continue
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 0 {
		fmt.Println("No MCP servers found to learn from.")
		return
	}

	fmt.Printf("\nLearning from %d MCP server(s)…\n\n", len(candidates))
	outputDir := userProfilesDir()
	written, skipped := 0, 0

	for _, s := range candidates {
		fmt.Printf("  %s: connecting… ", s.key)
		var tools []McpTool
		if s.cfg.url != "" {
			res, err := listMcpToolsHTTP(s.cfg.url, 10*time.Second)
			if err != nil {
				fmt.Printf("failed — %v\n", err)
				skipped++
				continue
			}
			if res.streamableError != "" {
				fmt.Printf("\n    streamable HTTP failed (%s), used legacy SSE — ", res.streamableError)
			}
			tools = res.tools
			fmt.Printf("%d tool(s) found (%s)\n", len(tools), res.transport)
		} else {
			t, err := listMcpToolsStdio(s.cfg.command, s.cfg.args, s.cfg.env, 10*time.Second)
			if err != nil {
				fmt.Printf("failed — %v\n", err)
				skipped++
				continue
			}
			tools = t
			fmt.Printf("%d tool(s) found\n", len(tools))
		}

		toml := generateProfile(s.key, tools)
		if dryRun {
			fmt.Printf("\n─── %s ───────────────────────────────\n", s.key)
			fmt.Println(toml)
			continue
		}

		profileDir := filepath.Join(outputDir, "mcp__"+strings.ReplaceAll(s.key, "-", "_"))
		os.MkdirAll(profileDir, 0o755)
		fp := filepath.Join(profileDir, "default.toml")
		os.WriteFile(fp, []byte(toml), 0o644)
		fmt.Printf("     → %s\n", fp)
		written++
	}

	if !dryRun {
		fmt.Printf("\n%d profile(s) written, %d skipped.\n", written, skipped)
		if written > 0 {
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Run a tool from each MCP to see real output")
			fmt.Println("  2. Refine items_path and fields in the generated profiles")
			fmt.Println("  3. Run: mcp-recall profiles check")
			fmt.Println("  4. Share good profiles: mcp-recall profiles feed <path>")
		}
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

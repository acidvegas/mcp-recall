// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/commands.go

package profiles

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
	"mcprecall/internal/denylist"
	"mcprecall/internal/format"
	"mcprecall/internal/handlers"
	"mcprecall/internal/jsonx"
	"mcprecall/internal/projectkey"
)

// RetrainFunc is set by the learn package (to avoid an import cycle) so that
// `profiles retrain` can invoke the corpus-based field suggester.
var RetrainFunc func([]string)

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func firstPositional(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// ── pattern overlap ───────────────────────────────────────────────────────────

func patternsOverlap(a, b string) bool {
	aExact := !strings.HasSuffix(a, "*")
	bExact := !strings.HasSuffix(b, "*")
	if aExact && bExact {
		return a == b
	}
	if aExact {
		return strings.HasPrefix(a, b[:len(b)-1])
	}
	if bExact {
		return strings.HasPrefix(b, a[:len(a)-1])
	}
	ap, bp := a[:len(a)-1], b[:len(b)-1]
	return strings.HasPrefix(ap, bp) || strings.HasPrefix(bp, ap)
}

// ── list ──────────────────────────────────────────────────────────────────────

func cmdList(args []string) {
	profiles := Load()
	if hasFlag(args, "--machine-readable") {
		for _, p := range profiles {
			fmt.Println(sanitize(ShortName(p.Spec.Profile)))
		}
		return
	}
	if len(profiles) == 0 {
		fmt.Println("No profiles installed.")
		fmt.Println("Run: mcp-recall profiles seed")
		return
	}
	header := padEnd("Name", 20) + "  " + padEnd("Tier", 10) + "  " + padEnd("Pattern", 26) + "  Description"
	fmt.Printf("\n%s\n", header)
	fmt.Println(strings.Repeat("─", min(runeLen(header), 100)))
	counts := map[Tier]int{}
	for _, p := range profiles {
		name := padEnd(firstChars(sanitize(ShortName(p.Spec.Profile)), 19), 20)
		tier := padEnd(string(p.Tier), 10)
		pat := padEnd(firstChars(firstOf(p.Patterns), 25), 26)
		desc := firstChars(sanitize(p.Spec.Profile.Description), 55)
		fmt.Printf("%s  %s  %s  %s\n", name, tier, pat, desc)
		counts[p.Tier]++
	}
	var parts []string
	for _, tier := range tierOrder {
		if counts[tier] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[tier], tier))
		}
	}
	fmt.Printf("\n%d total (%s)\n\n", len(profiles), strings.Join(parts, ", "))
}

// ── remove ────────────────────────────────────────────────────────────────────

func cmdRemove(args []string) {
	nameOrID := firstPositional(args)
	if nameOrID == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcp-recall profiles remove <name>")
		os.Exit(1)
	}
	target := findLocal(nameOrID)
	if target == nil {
		fmt.Fprintf(os.Stderr, "%q is not installed.\n", nameOrID)
		os.Exit(1)
	}
	if target.Tier != TierCommunity {
		fmt.Fprintf(os.Stderr, "%q is a %s profile and cannot be removed via this command.\n", nameOrID, target.Tier)
		os.Exit(1)
	}
	id := target.Spec.Profile.ID
	if err := assertSafeID(id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.RemoveAll(filepath.Join(communityDir(), id))
	fmt.Printf("✓ Removed %s\n", id)
}

func findLocal(nameOrID string) *Loaded {
	all := Load()
	for i := range all {
		if all[i].Spec.Profile.ID == nameOrID {
			return &all[i]
		}
	}
	for i := range all {
		if ShortName(all[i].Spec.Profile) == nameOrID {
			return &all[i]
		}
	}
	return nil
}

// ── check ─────────────────────────────────────────────────────────────────────

func cmdCheck() {
	profiles := Load()
	if len(profiles) == 0 {
		fmt.Println("No profiles installed.")
		return
	}
	type conflict struct {
		a, b       Loaded
		patA, patB string
	}
	var conflicts []conflict
	for i := 0; i < len(profiles); i++ {
		for j := i + 1; j < len(profiles); j++ {
			a, b := profiles[i], profiles[j]
			if a.Tier != b.Tier {
				continue
			}
			for _, pa := range a.Patterns {
				for _, pb := range b.Patterns {
					if patternsOverlap(pa, pb) {
						conflicts = append(conflicts, conflict{a, b, pa, pb})
					}
				}
			}
		}
	}
	if len(conflicts) == 0 {
		fmt.Printf("✓ No conflicts across %d profile(s).\n", len(profiles))
		return
	}
	fmt.Printf("\n%d conflict(s):\n\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Printf("  [%s] %s (%s)\n", c.a.Tier, c.a.Spec.Profile.ID, c.patA)
		fmt.Printf("  [%s] %s (%s)\n", c.b.Tier, c.b.Spec.Profile.ID, c.patB)
		fmt.Print("  → resolved by specificity (exact > wildcard, longer prefix > shorter)\n\n")
	}
}

// ── feed ──────────────────────────────────────────────────────────────────────

func cmdFeed(args []string) {
	profilePath := firstPositional(args)
	if profilePath == "" {
		fmt.Println("Usage: mcp-recall profiles feed <path-to-profile.toml>")
		if entries, err := os.ReadDir(userDir()); err == nil {
			var files []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".toml") {
					files = append(files, filepath.Join(userDir(), e.Name()))
				}
			}
			if len(files) > 0 {
				fmt.Println("\nYour local profiles:")
				for _, f := range files {
					fmt.Println("  " + f)
				}
			}
		}
		return
	}
	content, err := os.ReadFile(profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read: %s\n", profilePath)
		os.Exit(1)
	}
	var spec Spec
	if _, err := toml.Decode(string(content), &spec); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid TOML: %v\n", err)
		os.Exit(1)
	}
	if spec.Profile.ID == "" || spec.Profile.Version == "" || len(spec.Profile.MCPPattern) == 0 {
		fmt.Fprintln(os.Stderr, "Profile missing required fields (id, version, mcp_pattern).")
		os.Exit(1)
	}
	fmt.Printf("\nProfile: %s (v%s)\n", spec.Profile.ID, spec.Profile.Version)
	fmt.Printf("Pattern: %s\n", strings.Join([]string(spec.Profile.MCPPattern), ", "))
	fmt.Println("\nTo submit to the community repo:")
	fmt.Printf("  1. Fork https://github.com/%s\n", communityRepo)
	fmt.Printf("  2. Add your file as: profiles/%s/default.toml\n", spec.Profile.ID)
	fmt.Printf("  3. gh pr create --repo %s --title \"feat: %s profile\" --body \"...\"\n", communityRepo, spec.Profile.ID)

	clips := [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"pbcopy"}}
	for _, c := range clips {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(string(content))
		if cmd.Run() == nil {
			fmt.Println("\n✓ Profile content copied to clipboard.")
			return
		}
	}
	fmt.Printf("\nProfile content (copy manually):\n\n%s\n", sanitize(string(content)))
}

// ── install / update / seed (network) ─────────────────────────────────────────

func cmdInstall(args []string) {
	skipVerify := hasFlag(args, "--skip-verify")
	nameOrID := firstPositional(args)
	if nameOrID == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcp-recall profiles install <name> [--skip-verify]")
		os.Exit(1)
	}
	fmt.Print("Fetching manifest… ")
	entries, err := fetchManifest(skipVerify)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
	fmt.Println("done")
	entry, ok := resolveManifestEntry(nameOrID, entries)
	if !ok {
		os.Exit(1)
	}
	if err := installEntry(entry); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func installEntry(entry ManifestEntry) error {
	if err := assertSafeID(entry.ID); err != nil {
		return err
	}
	if err := assertSafeFile(entry.File); err != nil {
		return err
	}
	fmt.Printf("Installing %s v%s… ", sanitize(entry.ID), sanitize(entry.Version))
	content, err := fetchProfileContent(entry.File)
	if err != nil {
		return err
	}
	if err := verifyHash(content, entry.SHA256, entry.ID); err != nil {
		return err
	}
	fp, err := saveToCommunityDir(entry.ID, content)
	if err != nil {
		return err
	}
	fmt.Printf("done\n✓ %s\n", fp)
	return nil
}

func cmdUpdate(args []string) {
	skipVerify := hasFlag(args, "--skip-verify")
	installed := installedCommunityMap()
	if len(installed) == 0 {
		fmt.Println("No community profiles installed.")
		return
	}
	fmt.Print("Fetching manifest… ")
	entries, err := fetchManifest(skipVerify)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
	fmt.Print("done\n\n")
	byID := map[string]ManifestEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	updated := 0
	for _, id := range sortedKeys(installed) {
		cur := installed[id]
		entry, ok := byID[id]
		if !ok {
			fmt.Printf("  %s: not in registry (skipped)\n", id)
			continue
		}
		if entry.Version == cur {
			fmt.Printf("  %s: up to date (%s)\n", id, cur)
			continue
		}
		if assertSafeID(entry.ID) != nil || assertSafeFile(entry.File) != nil {
			continue
		}
		content, err := fetchProfileContent(entry.File)
		if err != nil || verifyHash(content, entry.SHA256, entry.ID) != nil {
			continue
		}
		saveToCommunityDir(id, content)
		fmt.Printf("  ✓ %s: %s → %s\n", id, cur, entry.Version)
		updated++
	}
	fmt.Printf("\n%d profile(s) updated.\n", updated)
}

func cmdSeed(args []string) {
	all := hasFlag(args, "--all")
	skipVerify := hasFlag(args, "--skip-verify")
	fmt.Print("Fetching manifest… ")
	entries, err := fetchManifest(skipVerify)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
	fmt.Print("done\n\n")
	installed := installedCommunityMap()
	installCount, alreadyCount := 0, 0

	if all {
		for _, entry := range entries {
			if _, ok := installed[entry.ID]; ok {
				fmt.Printf("    %s: already installed\n", entry.ID)
				alreadyCount++
				continue
			}
			if assertSafeID(entry.ID) != nil || assertSafeFile(entry.File) != nil {
				continue
			}
			content, err := fetchProfileContent(entry.File)
			if err != nil || verifyHash(content, entry.SHA256, entry.ID) != nil {
				continue
			}
			saveToCommunityDir(entry.ID, content)
			fmt.Printf("  ✓ %s installed\n", entry.ID)
			installCount++
		}
		fmt.Printf("\n%d profile(s) installed (%d already installed, %d total available)\n", installCount, alreadyCount, len(entries))
		return
	}

	serverKeys, err := detectMCPServers()
	if err != nil {
		fmt.Println("Could not read ~/.claude.json — no MCPs detected.")
		return
	}
	if len(serverKeys) == 0 {
		fmt.Println("No MCP servers found in ~/.claude.json (other than recall).")
		return
	}
	fmt.Printf("Detected MCPs: %s\n", strings.Join(serverKeys, ", "))
	for _, key := range serverKeys {
		prefix := "mcp__" + strings.ReplaceAll(key, "-", "_") + "__"
		var matches []ManifestEntry
		for _, e := range entries {
			for _, pat := range e.patterns() {
				stripped := strings.TrimSuffix(pat, "*")
				if stripped == prefix || strings.HasPrefix(prefix, stripped) {
					matches = append(matches, e)
					break
				}
			}
		}
		if len(matches) == 0 {
			fmt.Printf("  %s: no community profile available\n", key)
			continue
		}
		for _, entry := range matches {
			if _, ok := installed[entry.ID]; ok {
				fmt.Printf("  %s: already installed\n", entry.ID)
				alreadyCount++
				continue
			}
			if assertSafeID(entry.ID) != nil || assertSafeFile(entry.File) != nil {
				continue
			}
			content, err := fetchProfileContent(entry.File)
			if err != nil || verifyHash(content, entry.SHA256, entry.ID) != nil {
				continue
			}
			saveToCommunityDir(entry.ID, content)
			fmt.Printf("  ✓ %s installed (matched %s)\n", entry.ID, key)
			installCount++
		}
	}
	fmt.Printf("\n%d profile(s) installed.\n", installCount)
}

func detectMCPServers() ([]string, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return nil, err
	}
	return parseClaudeServers(data)
}

// parseClaudeServers returns the mcpServers keys (in file order) except "recall".
func parseClaudeServers(data []byte) ([]string, error) {
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	root, ok := v.(*jsonx.Obj)
	if !ok {
		return nil, fmt.Errorf("unexpected .claude.json shape")
	}
	sv, ok := root.Get("mcpServers")
	if !ok {
		return nil, nil
	}
	servers, ok := sv.(*jsonx.Obj)
	if !ok {
		return nil, nil
	}
	var keys []string
	for _, k := range servers.Keys() {
		if k != "recall" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// ── info / available ──────────────────────────────────────────────────────────

func cmdInfo(args []string) {
	nameOrID := firstPositional(args)
	if nameOrID == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcp-recall profiles info <name>")
		os.Exit(1)
	}
	local := findLocal(nameOrID)
	var mEntry *ManifestEntry
	fmt.Print("Fetching manifest… ")
	entries, err := fetchManifest(false)
	if err != nil {
		// A verification failure must not be masked as "offline": in `error`
		// mode it means the manifest could not be trusted, so hard-fail like
		// the write commands do (#234). A genuine network/fetch error still
		// degrades to the local-only view.
		var verr *ManifestVerificationError
		if errors.As(err, &verr) {
			fmt.Println()
			fmt.Fprintln(os.Stderr, verr.Error())
			os.Exit(1)
		}
		fmt.Println("(offline — showing local data only)")
	} else {
		fmt.Println("done")
		lookupID := nameOrID
		if local != nil {
			lookupID = local.Spec.Profile.ID
		}
		for i := range entries {
			if entries[i].ID == lookupID || manifestShortName(entries[i]) == nameOrID {
				mEntry = &entries[i]
				break
			}
		}
	}
	if local == nil && mEntry == nil {
		fmt.Fprintf(os.Stderr, "Profile %q not found.\n", nameOrID)
		os.Exit(1)
	}
	id, short, version, desc, author, url := infoFields(local, mEntry)
	var patterns []string
	tier := "community (not installed)"
	if local != nil {
		patterns = local.Patterns
		tier = string(local.Tier)
	} else {
		patterns = mEntry.patterns()
	}
	fmt.Printf("\n%s (%s v%s)\n", short, id, version)
	fmt.Printf("  Description: %s\n", desc)
	fmt.Printf("  Pattern:     %s\n", strings.Join(patterns, ", "))
	fmt.Printf("  Author:      %s\n", author)
	fmt.Printf("  MCP:         %s\n", url)
	if local != nil {
		fmt.Printf("  Strategy:    %s\n", local.Spec.Strategy.Type)
	}
	fmt.Printf("  Tier:        %s\n", tier)
	if local != nil {
		fmt.Printf("  Installed:   %s\n\n", local.FilePath)
	} else {
		fmt.Printf("  Installed:   not installed\n\n")
	}
}

func infoFields(local *Loaded, m *ManifestEntry) (id, short, version, desc, author, url string) {
	pick := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	if local != nil {
		id = local.Spec.Profile.ID
		short = ShortName(local.Spec.Profile)
		version = local.Spec.Profile.Version
		desc = sanitize(local.Spec.Profile.Description)
		author = sanitize(pick(local.Spec.Profile.Author, mOr(m, func(e ManifestEntry) string { return e.Author })))
		url = sanitize(pick(local.Spec.Profile.MCPURL, mOr(m, func(e ManifestEntry) string { return e.MCPURL })))
	} else {
		id = m.ID
		short = manifestShortName(*m)
		version = m.Version
		desc = sanitize(m.Description)
		author = sanitize(m.Author)
		url = sanitize(m.MCPURL)
	}
	if desc == "" {
		desc = "—"
	}
	if author == "" {
		author = "—"
	}
	if url == "" {
		url = "—"
	}
	return
}

func mOr(m *ManifestEntry, f func(ManifestEntry) string) string {
	if m == nil {
		return ""
	}
	return f(*m)
}

func cmdAvailable(args []string) {
	verbose := hasFlag(args, "--verbose")
	fmt.Print("Fetching manifest… ")
	entries, err := fetchManifest(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
	fmt.Print("done\n\n")
	installed := installedCommunityMap()
	header := padEnd("Name", 20) + "  " + padEnd("Description", 46) + "  Status"
	if verbose {
		header += "  MCP URL"
	}
	fmt.Println(header)
	sepLen := runeLen(header)
	if verbose {
		sepLen += 50
	}
	fmt.Println(strings.Repeat("─", min(sepLen, 120)))
	installedCount := 0
	for _, e := range entries {
		name := padEnd(firstChars(sanitize(manifestShortName(e)), 19), 20)
		desc := padEnd(firstChars(sanitize(e.Description), 45), 46)
		status := "         "
		if _, ok := installed[e.ID]; ok {
			status = "installed"
			installedCount++
		}
		urlPart := ""
		if verbose {
			u := sanitize(e.MCPURL)
			if u == "" {
				u = "—"
			}
			urlPart = "  " + u
		}
		fmt.Printf("%s  %s  %s%s\n", name, desc, status, urlPart)
	}
	fmt.Printf("\n%d available, %d installed\n\n", len(entries), installedCount)
}

// ── test ──────────────────────────────────────────────────────────────────────

// TestResult mirrors the original's TestResult.
type TestResult struct {
	ToolName       string
	MatchedProfile *Loaded
	HandlerName    string
	InputBytes     int
	OutputBytes    int
	ReductionPct   int
	Summary        string
}

func handlerName(h handlers.Handler) string {
	name := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// TestProfile is the core of `profiles test`, exported for tests.
func TestProfile(toolName, content string) TestResult {
	matched := resolveProfile(toolName, Load(), tierOrder)
	handler := handlers.GetHandler(toolName, content, nil)
	res := handler(toolName, content)
	out := len(res.Summary)
	pct := 0
	if res.OriginalSize > 0 {
		pct = int(math.Round((1 - float64(out)/float64(res.OriginalSize)) * 100))
	}
	return TestResult{
		ToolName: toolName, MatchedProfile: matched, HandlerName: handlerName(handler),
		InputBytes: res.OriginalSize, OutputBytes: out, ReductionPct: pct, Summary: res.Summary,
	}
}

func cmdTest(args []string) {
	var toolName, storedID, inputFile string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--stored" && i+1 < len(args):
			i++
			storedID = args[i]
		case args[i] == "--input" && i+1 < len(args):
			i++
			inputFile = args[i]
		case !strings.HasPrefix(args[i], "-"):
			toolName = args[i]
		}
	}
	if toolName == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcp-recall profiles test <tool_name> [--stored <id>] [--input <file>]")
		os.Exit(1)
	}
	if storedID == "" && inputFile == "" {
		fmt.Fprintln(os.Stderr, "Provide --stored <recall_id> or --input <file>")
		os.Exit(1)
	}
	var content, source string
	if storedID != "" {
		cwd, _ := os.Getwd()
		database, err := db.Open(db.DefaultDBPath(projectkey.Key(cwd)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer database.Close()
		item, _ := db.RetrieveOutput(database, storedID)
		if item == nil {
			fmt.Fprintf(os.Stderr, "No stored item found: %s\n", storedID)
			os.Exit(1)
		}
		content = item.FullContent
		source = "stored:" + storedID
	} else {
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot read: %s\n", inputFile)
			os.Exit(1)
		}
		content = string(data)
		source = inputFile
	}
	if denylist.IsDenied(toolName, config.Load()) {
		fmt.Printf("Tool %q is on the denylist — output will not be processed or stored.\n", toolName)
		return
	}
	result := TestProfile(toolName, content)
	if result.MatchedProfile != nil {
		p := result.MatchedProfile
		fmt.Printf("\nProfile:  %s (%s) — %s\n", p.Spec.Profile.ID, p.Tier, strings.Join(p.Patterns, ", "))
		fmt.Printf("File:     %s\n", p.FilePath)
		fmt.Printf("Strategy: %s\n", p.Spec.Strategy.Type)
	} else {
		fmt.Printf("\nNo profile match for %s\n", toolName)
		fmt.Printf("Handler:  %s (built-in fallback)\n", result.HandlerName)
		fmt.Println("\nTo add a profile:\n  mcp-recall learn")
	}
	fmt.Printf("\nInput:  %s  (%s)\n", format.Bytes(result.InputBytes), source)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(result.Summary)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Output: %s  (%d%% reduction)\n\n", format.Bytes(result.OutputBytes), result.ReductionPct)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HandleProfilesCommand dispatches `mcp-recall profiles <cmd>`.
func HandleProfilesCommand(args []string) {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	switch cmd {
	case "list":
		cmdList(rest)
	case "install":
		cmdInstall(rest)
	case "update":
		cmdUpdate(rest)
	case "remove":
		cmdRemove(rest)
	case "seed":
		cmdSeed(rest)
	case "feed":
		cmdFeed(rest)
	case "check":
		cmdCheck()
	case "info":
		cmdInfo(rest)
	case "available":
		cmdAvailable(rest)
	case "test":
		cmdTest(rest)
	case "retrain":
		if RetrainFunc != nil {
			RetrainFunc(rest)
		} else {
			fmt.Fprintln(os.Stderr, "retrain unavailable")
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", orNone(cmd))
		fmt.Fprintln(os.Stderr, "Usage: mcp-recall profiles <command>")
		fmt.Fprintln(os.Stderr, "  list | available | info | install | update | remove | seed | feed | check | retrain | test")
		os.Exit(1)
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

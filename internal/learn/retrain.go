// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/retrain.go

package learn

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcprecall/internal/db"
	"mcprecall/internal/jsonx"
	"mcprecall/internal/profiles"
	"mcprecall/internal/projectkey"
)

const (
	minSamples   = 3
	maxSamples   = 5
	defaultDepth = 3
	minFieldPct  = 0.5
)

// FieldSuggestion is a discovered field path with its frequency.
type FieldSuggestion struct {
	Path      string
	Pct       float64
	InProfile bool
}

// RetrainResult mirrors the original's RetrainResult.
type RetrainResult struct {
	ToolName          string
	ProfileID         string
	ProfileTier       string
	ProfileFilePath   string
	StrategyType      string
	SampleCount       int
	ItemCount         int
	DetectedItemsPath *string
	CurrentItemsPath  []string
	Fields            []FieldSuggestion
	NewFields         []string
	Error             string
}

// detectItemsPath finds the largest array at depth 0 or 1.
func detectItemsPath(parsed any) (string, []any, bool) {
	if arr, ok := parsed.([]any); ok {
		return "", arr, true
	}
	obj, ok := parsed.(*jsonx.Obj)
	if !ok {
		return "", nil, false
	}
	bestPath := ""
	var bestItems []any
	bestScore := -1
	for _, key := range obj.Keys() {
		val, _ := obj.Get(key)
		if arr, ok := val.([]any); ok && len(arr) > bestScore {
			bestPath, bestItems, bestScore = key, arr, len(arr)
		}
		if inner, ok := val.(*jsonx.Obj); ok {
			for _, key2 := range inner.Keys() {
				val2, _ := inner.Get(key2)
				if arr, ok := val2.([]any); ok && len(arr) > bestScore {
					bestPath, bestItems, bestScore = key+"."+key2, arr, len(arr)
				}
			}
		}
	}
	if bestScore < 0 {
		return "", nil, false
	}
	return bestPath, bestItems, true
}

func traverseObject(obj *jsonx.Obj, prefix string, depth, maxDepth int, order *[]string, seen map[string]bool) {
	if depth >= maxDepth {
		return
	}
	for _, key := range obj.Keys() {
		val, _ := obj.Get(key)
		if val == nil {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch v := val.(type) {
		case string:
			if v != "" {
				addPath(order, seen, path)
			}
		case float64, bool:
			addPath(order, seen, path)
		case *jsonx.Obj:
			traverseObject(v, path, depth+1, maxDepth, order, seen)
		}
	}
}

func addPath(order *[]string, seen map[string]bool, p string) {
	if !seen[p] {
		seen[p] = true
		*order = append(*order, p)
	}
}

// collectFieldPaths returns paths in first-seen order and their per-item counts.
func collectFieldPaths(items []any, maxDepth int) ([]string, map[string]int) {
	var order []string
	orderSeen := map[string]bool{}
	counts := map[string]int{}
	for _, item := range items {
		obj, ok := item.(*jsonx.Obj)
		if !ok {
			continue
		}
		var perItem []string
		seen := map[string]bool{}
		traverseObject(obj, "", 0, maxDepth, &perItem, seen)
		for _, p := range perItem {
			addPath(&order, orderSeen, p)
			counts[p]++
		}
	}
	return order, counts
}

type scored struct {
	path string
	pct  float64
}

func scoreFields(order []string, counts map[string]int, total int) []scored {
	if total == 0 {
		return nil
	}
	var out []scored
	for _, p := range order {
		pct := float64(counts[p]) / float64(total)
		if pct >= minFieldPct {
			out = append(out, scored{p, pct})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pct > out[j].pct })
	return out
}

// ── TOML manipulation ─────────────────────────────────────────────────────────

func bumpPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return version
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version
		}
		nums[i] = n
	}
	return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2]+1)
}

var (
	firstContentRe = regexp.MustCompile(`(?m)^[^\s#]`)
	fieldsOpenRe   = regexp.MustCompile(`(?m)^(\s*fields\s*=\s*\[)`)
	blockIndentRe  = regexp.MustCompile(`(?m)^(\s+)`)
	versionRe      = regexp.MustCompile(`(?m)^(\s*version\s*=\s*")([^"]+)(")`)
)

func applyRetrainToToml(tomlContent string, newFields []string, date string) string {
	result := tomlContent
	retrainLine := "# Retrained: " + date + "\n"
	if loc := firstContentRe.FindStringIndex(result); loc == nil || loc[0] <= 0 {
		result = retrainLine + result
	} else {
		result = result[:loc[0]] + retrainLine + result[loc[0]:]
	}

	if len(newFields) > 0 {
		if m := fieldsOpenRe.FindStringSubmatchIndex(result); m != nil {
			afterOpen := m[1]
			closeIdx := strings.Index(result[afterOpen:], "]")
			if closeIdx != -1 {
				closeIdx += afterOpen
				block := result[afterOpen:closeIdx]
				indent := "  "
				if im := blockIndentRe.FindStringSubmatch(block); im != nil {
					indent = im[1]
				}
				var lines []string
				for _, f := range newFields {
					lines = append(lines, indent+`"`+f+`",`)
				}
				result = result[:closeIdx] + strings.Join(lines, "\n") + "\n" + result[closeIdx:]
			}
		}
	}

	result = versionRe.ReplaceAllStringFunc(result, func(match string) string {
		sub := versionRe.FindStringSubmatch(match)
		return sub[1] + bumpPatch(sub[2]) + sub[3]
	})
	return result
}

func retrainProfile(samples []db.StoredOutput, profile profiles.Loaded, maxDepth int) RetrainResult {
	res := RetrainResult{
		ProfileID:        profile.Spec.Profile.ID,
		ProfileTier:      string(profile.Tier),
		ProfileFilePath:  profile.FilePath,
		StrategyType:     profile.Spec.Strategy.Type,
		SampleCount:      len(samples),
		CurrentItemsPath: profile.Spec.Strategy.ItemsPath,
	}
	if len(samples) > 0 {
		res.ToolName = samples[0].ToolName
	}
	if profile.Spec.Strategy.Type != "json_extract" {
		return res
	}

	currentFields := map[string]bool{}
	for _, f := range profile.Spec.Strategy.Fields {
		currentFields[f] = true
	}

	var allItems []any
	var detectedPath *string
	detectedPathCount := 0
	for _, sample := range samples {
		parsed, err := jsonx.ParseString(sample.FullContent)
		if err != nil {
			continue
		}
		p, items, ok := detectItemsPath(parsed)
		if ok {
			allItems = append(allItems, items...)
			if detectedPath == nil || *detectedPath == p {
				detectedPathCount++
				pp := p
				detectedPath = &pp
			}
		}
	}

	if len(allItems) == 0 {
		res.DetectedItemsPath = detectedPath
		res.Error = "no parseable JSON items found in samples"
		return res
	}

	order, counts := collectFieldPaths(allItems, maxDepth)
	scoredFields := scoreFields(order, counts, len(allItems))
	for _, s := range scoredFields {
		res.Fields = append(res.Fields, FieldSuggestion{Path: s.path, Pct: s.pct, InProfile: currentFields[s.path]})
		if !currentFields[s.path] {
			res.NewFields = append(res.NewFields, s.path)
		}
	}
	res.ItemCount = len(allItems)
	if detectedPathCount >= int(math.Ceil(float64(len(samples))/2)) {
		res.DetectedItemsPath = detectedPath
	}
	return res
}

func printRetrainResult(r RetrainResult, apply bool) {
	samplePlural, itemPlural := "s", "s"
	if r.SampleCount == 1 {
		samplePlural = ""
	}
	if r.ItemCount == 1 {
		itemPlural = ""
	}
	fmt.Printf("\n%s (%d sample%s · %d item%s):\n", r.ToolName, r.SampleCount, samplePlural, r.ItemCount, itemPlural)
	if r.Error != "" {
		fmt.Printf("  ⚠ %s\n", r.Error)
		return
	}
	if r.StrategyType != "json_extract" {
		fmt.Printf("  Strategy is %s — field extraction not applicable.\n", r.StrategyType)
		fmt.Println("  Tip: if this tool returns structured lists, consider switching to json_extract.")
		return
	}
	if r.DetectedItemsPath != nil {
		status := "⚠ not in current profile items_path"
		for _, p := range r.CurrentItemsPath {
			if p == *r.DetectedItemsPath {
				status = "✓ matches profile"
			}
		}
		fmt.Printf("  items_path: %q  %s\n", *r.DetectedItemsPath, status)
	}
	if len(r.Fields) == 0 {
		fmt.Println("  No fields found at ≥50% frequency.")
		return
	}
	fmt.Println("  Fields (≥50% frequency):")
	colW := 0
	for _, f := range r.Fields {
		if l := len(f.Path); l > colW {
			colW = l
		}
	}
	colW += 2
	if colW > 45 {
		colW = 45
	}
	for _, f := range r.Fields {
		pctStr := padStartStr(fmt.Sprintf("%d%%", int(math.Round(f.Pct*100))), 4)
		tag := "NEW"
		if f.InProfile {
			tag = "in profile"
		}
		fmt.Printf("    %s  %s  %s\n", padEndStr(`"`+f.Path+`"`, colW), pctStr, tag)
	}
	if len(r.NewFields) == 0 {
		fmt.Println("  ✓ Profile is up to date.")
	} else if !apply {
		fmt.Printf("  %d new field(s) found. Run with --apply to update.\n", len(r.NewFields))
	}
}

func applyRetrainResult(r RetrainResult, date string) {
	if len(r.NewFields) == 0 {
		return
	}
	data, err := os.ReadFile(r.ProfileFilePath)
	if err != nil {
		fmt.Printf("  ✗ Could not read %s: %v\n", r.ProfileFilePath, err)
		return
	}
	old := extractVersion(string(data))
	updated := applyRetrainToToml(string(data), r.NewFields, date)
	os.WriteFile(r.ProfileFilePath, []byte(updated), 0o644)
	fmt.Printf("  ✓ Updated: %s (%s → %s)\n", r.ProfileFilePath, old, extractVersion(updated))
}

var extractVersionRe = regexp.MustCompile(`version\s*=\s*"([^"]+)"`)

func extractVersion(toml string) string {
	if m := extractVersionRe.FindStringSubmatch(toml); m != nil {
		return m[1]
	}
	return "?"
}

// HandleRetrain implements `mcp-recall profiles retrain`.
func HandleRetrain(args []string) {
	apply := false
	cliDepth := -1
	var targets []string
	numRe := regexp.MustCompile(`^\d+$`)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--apply":
			apply = true
		case a == "--depth" && i+1 < len(args):
			cliDepth, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--depth="):
			cliDepth, _ = strconv.Atoi(strings.TrimPrefix(a, "--depth="))
		case !strings.HasPrefix(a, "--") && !numRe.MatchString(a):
			targets = append(targets, a)
		}
	}

	cwd, _ := os.Getwd()
	projectKey := projectkey.Key(cwd)
	database, err := db.Open(db.DefaultDBPath(projectKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	var breakdown []db.ToolBreakdownRow
	for _, r := range db.GetToolBreakdown(database, projectKey) {
		if r.Items >= minSamples {
			breakdown = append(breakdown, r)
		}
	}
	if len(breakdown) == 0 {
		fmt.Printf("No tools with ≥%d stored samples. Run some MCP tools first.\n", minSamples)
		return
	}

	loaded := profiles.Load()
	var qualifying []db.ToolBreakdownRow
	for _, r := range breakdown {
		if len(targets) > 0 && !anyContains(r.ToolName, targets) {
			continue
		}
		if profiles.ResolveAllTiers(r.ToolName, loaded) != nil {
			qualifying = append(qualifying, r)
		}
	}
	if len(qualifying) == 0 {
		fmt.Println("No profiled tools with enough data found.")
		if len(targets) > 0 {
			fmt.Printf("(filter: %s)\n", strings.Join(targets, ", "))
		}
		return
	}

	fmt.Println("\nRetraining from stored corpus…")
	date := time.Now().UTC().Format("2006-01-02")
	analyzed, totalNew, applied := 0, 0, 0

	for _, row := range qualifying {
		profile := profiles.ResolveAllTiers(row.ToolName, loaded)
		maxDepth := defaultDepth
		if profile.Spec.Retrain.MaxDepth != nil {
			maxDepth = *profile.Spec.Retrain.MaxDepth
		}
		if cliDepth > 0 {
			maxDepth = cliDepth
		}
		samples := db.SampleOutputs(database, projectKey, row.ToolName, maxSamples)
		result := retrainProfile(samples, *profile, maxDepth)
		printRetrainResult(result, apply)
		if apply && len(result.NewFields) > 0 && result.Error == "" {
			applyRetrainResult(result, date)
			applied++
		}
		analyzed++
		totalNew += len(result.NewFields)
	}

	fmt.Printf("\n%s\n", strings.Repeat("─", 54))
	if apply {
		fmt.Printf("%d profile(s) analyzed · %d new field(s) · %d profile(s) updated.\n", analyzed, totalNew, applied)
	} else {
		fmt.Printf("%d profile(s) analyzed · %d new field(s) found.\n", analyzed, totalNew)
		if totalNew > 0 {
			fmt.Println("Run with --apply to update profiles.")
		}
	}
}

func anyContains(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func padEndStr(s string, n int) string {
	if l := len([]rune(s)); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}
func padStartStr(s string, n int) string {
	if l := len([]rune(s)); l < n {
		return strings.Repeat(" ", n-l) + s
	}
	return s
}

// Register wires retrain into the profiles command dispatcher (avoids a cycle).
func Register() {
	profiles.RetrainFunc = HandleRetrain
}

// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/commands_test.go

package profiles

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcprecall/internal/config"
)

// ── capture / isolation helpers ───────────────────────────────────────────────

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func captureStderr(fn func()) string {
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

// isolateProfiles points all three tiers at empty/temp dirs; returns the user dir.
func isolateProfiles(t *testing.T) string {
	t.Helper()
	userDir := t.TempDir()
	os.Setenv("RECALL_USER_PROFILES_PATH", userDir)
	os.Setenv("RECALL_COMMUNITY_PROFILES_PATH", filepath.Join(t.TempDir(), "none-c"))
	os.Setenv("RECALL_BUNDLED_PROFILES_PATH", filepath.Join(t.TempDir(), "none-b"))
	t.Cleanup(func() {
		os.Unsetenv("RECALL_USER_PROFILES_PATH")
		os.Unsetenv("RECALL_COMMUNITY_PROFILES_PATH")
		os.Unsetenv("RECALL_BUNDLED_PROFILES_PATH")
	})
	return userDir
}

func writeProfile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ── patternsOverlap ───────────────────────────────────────────────────────────

func TestPatternsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"mcp__jira__search", "mcp__jira__search", true},
		{"mcp__jira__search", "mcp__jira__create", false},
		{"mcp__jira__search", "mcp__jira__*", true},
		{"mcp__notion__search", "mcp__jira__*", false},
		{"mcp__jira__*", "mcp__jira__*", true},
		{"mcp__jira__*", "mcp__jira__search*", true},
		{"mcp__jira__*", "mcp__notion__*", false},
	}
	for _, c := range cases {
		if got := patternsOverlap(c.a, c.b); got != c.want {
			t.Errorf("patternsOverlap(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// ── ShortName ─────────────────────────────────────────────────────────────────

func TestShortName(t *testing.T) {
	if got := ShortName(Meta{ID: "mcp__grafana"}); got != "grafana" {
		t.Errorf("strip prefix: %q", got)
	}
	if got := ShortName(Meta{ID: "mcp__grafana", ShortName: "graf"}); got != "graf" {
		t.Errorf("explicit: %q", got)
	}
	if got := ShortName(Meta{ID: "custom_profile"}); got != "custom_profile" {
		t.Errorf("no prefix: %q", got)
	}
}

// ── TestProfile ───────────────────────────────────────────────────────────────

const jiraProfileTOML = `[profile]
id = "mcp__jira"
version = "1.0.0"
description = "Jira"
mcp_pattern = "mcp__jira__*"
[strategy]
type = "json_extract"
fields = ["key", "summary"]`

func TestTestProfile(t *testing.T) {
	dir := isolateProfiles(t)
	writeProfile(t, dir, "jira.toml", jiraProfileTOML)

	content := `{"key":"PROJ-123","summary":"Fix login bug","description":"Long description dropped by extractor"}`
	r := TestProfile("mcp__jira__search_issues", content)
	if r.ToolName != "mcp__jira__search_issues" {
		t.Errorf("toolName = %q", r.ToolName)
	}
	if r.MatchedProfile == nil || r.MatchedProfile.Spec.Profile.ID != "mcp__jira" {
		t.Errorf("matched profile wrong: %+v", r.MatchedProfile)
	}
	if r.InputBytes <= 0 || r.OutputBytes <= 0 || len(r.Summary) == 0 {
		t.Errorf("sizes/summary: %+v", r)
	}

	// no match → nil profile, non-empty handler name
	nm := TestProfile("mcp__unknown__no_match_here", `{"foo":"bar"}`)
	if nm.MatchedProfile != nil || len(nm.HandlerName) == 0 || len(nm.Summary) == 0 {
		t.Errorf("no-match: %+v", nm)
	}

	// reductionPct 0 when originalSize 0
	if TestProfile("mcp__unknown__tool", "").ReductionPct != 0 {
		t.Error("reduction should be 0 for empty input")
	}
	// non-negative sizes
	nb := TestProfile("mcp__unknown__tool", `{"message":"hello world","count":42}`)
	if nb.InputBytes < 0 || nb.OutputBytes < 0 {
		t.Error("sizes must be non-negative")
	}
}

// ── conflict detection (Load + patternsOverlap) ───────────────────────────────

func TestConflictDetection(t *testing.T) {
	dir := isolateProfiles(t)
	writeProfile(t, dir, "jira.toml", "[profile]\nid=\"mcp__jira\"\nversion=\"1.0.0\"\ndescription=\"Jira\"\nmcp_pattern=\"mcp__jira__*\"\n[strategy]\ntype=\"json_extract\"\nfields=[\"key\"]")
	writeProfile(t, dir, "notion.toml", "[profile]\nid=\"mcp__notion\"\nversion=\"1.0.0\"\ndescription=\"Notion\"\nmcp_pattern=\"mcp__notion__*\"\n[strategy]\ntype=\"text_truncate\"")
	profs := Load()
	if len(profs) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profs))
	}
	if patternsOverlap(profs[0].Patterns[0], profs[1].Patterns[0]) {
		t.Error("jira vs notion should not overlap")
	}

	dir2 := isolateProfiles(t)
	writeProfile(t, dir2, "broad.toml", "[profile]\nid=\"mcp__jira\"\nversion=\"1.0.0\"\ndescription=\"broad\"\nmcp_pattern=\"mcp__jira__*\"\n[strategy]\ntype=\"json_extract\"\nfields=[\"key\"]")
	writeProfile(t, dir2, "narrow.toml", "[profile]\nid=\"mcp__jira__search\"\nversion=\"1.0.0\"\ndescription=\"narrow\"\nmcp_pattern=\"mcp__jira__search*\"\n[strategy]\ntype=\"text_truncate\"")
	p2 := Load()
	overlap := false
	for _, pa := range p2[0].Patterns {
		for _, pb := range p2[1].Patterns {
			if patternsOverlap(pa, pb) {
				overlap = true
			}
		}
	}
	if !overlap {
		t.Error("overlapping jira patterns should conflict")
	}
}

// ── cmdList ───────────────────────────────────────────────────────────────────

func TestCmdListMachineReadable(t *testing.T) {
	dir := isolateProfiles(t)
	writeProfile(t, dir, "jira.toml", "[profile]\nid=\"mcp__jira\"\nversion=\"1.0.0\"\ndescription=\"Jira\"\nmcp_pattern=\"mcp__jira__*\"\n[strategy]\ntype=\"text_truncate\"")
	writeProfile(t, dir, "grafana.toml", "[profile]\nid=\"mcp__grafana\"\nversion=\"1.0.0\"\ndescription=\"Grafana\"\nmcp_pattern=\"mcp__grafana__*\"\n[strategy]\ntype=\"text_truncate\"")
	out := captureStdout(func() { cmdList([]string{"--machine-readable"}) })
	ids := strings.Fields(strings.TrimSpace(out))
	joined := " " + strings.Join(ids, " ") + " "
	if !strings.Contains(joined, " jira ") || !strings.Contains(joined, " grafana ") {
		t.Errorf("machine-readable ids: %q", out)
	}

	empty := isolateProfiles(t)
	_ = empty
	if got := strings.TrimSpace(captureStdout(func() { cmdList([]string{"--machine-readable"}) })); got != "" {
		t.Errorf("empty store should print nothing: %q", got)
	}
}

func TestCmdListShortNames(t *testing.T) {
	dir := isolateProfiles(t)
	writeProfile(t, dir, "jira.toml", "[profile]\nid=\"mcp__jira\"\nversion=\"1.0.0\"\ndescription=\"Jira issues\"\nmcp_pattern=\"mcp__jira__*\"\n[strategy]\ntype=\"text_truncate\"")
	out := captureStdout(func() { cmdList(nil) })
	if !strings.Contains(out, "jira") || !strings.Contains(out, "Jira issues") {
		t.Errorf("table missing short name/desc: %q", out)
	}
	if strings.Contains(out, "mcp__jira  ") {
		t.Errorf("table should show short name, not full id: %q", out)
	}

	dir2 := isolateProfiles(t)
	writeProfile(t, dir2, "g.toml", "[profile]\nid=\"mcp__grafana\"\nshort_name=\"gf\"\nversion=\"1.0.0\"\ndescription=\"Grafana\"\nmcp_pattern=\"mcp__grafana__*\"\n[strategy]\ntype=\"text_truncate\"")
	if !strings.Contains(captureStdout(func() { cmdList(nil) }), "gf") {
		t.Error("explicit short_name should appear")
	}
}

// ── cmdRemove ─────────────────────────────────────────────────────────────────

func TestCmdRemove(t *testing.T) {
	for _, arg := range []string{"grafana", "mcp__grafana"} {
		comm := t.TempDir()
		os.Setenv("RECALL_COMMUNITY_PROFILES_PATH", comm)
		os.Setenv("RECALL_USER_PROFILES_PATH", filepath.Join(t.TempDir(), "none-u"))
		os.Setenv("RECALL_BUNDLED_PROFILES_PATH", filepath.Join(t.TempDir(), "none-b"))
		gdir := filepath.Join(comm, "mcp__grafana")
		os.MkdirAll(gdir, 0o755)
		os.WriteFile(filepath.Join(gdir, "default.toml"), []byte("[profile]\nid=\"mcp__grafana\"\nversion=\"1.0.0\"\ndescription=\"Grafana\"\nmcp_pattern=\"mcp__grafana__*\"\n[strategy]\ntype=\"text_truncate\""), 0o644)
		captureStdout(func() { cmdRemove([]string{arg}) })
		if _, err := os.Stat(filepath.Join(gdir, "default.toml")); err == nil {
			t.Errorf("remove by %q left the file", arg)
		}
		os.Unsetenv("RECALL_COMMUNITY_PROFILES_PATH")
		os.Unsetenv("RECALL_USER_PROFILES_PATH")
		os.Unsetenv("RECALL_BUNDLED_PROFILES_PATH")
	}
}

// ── network commands (httptest) ───────────────────────────────────────────────

func fakeCatalog(t *testing.T) (comm string, cleanup func()) {
	t.Helper()
	manifest := `{"profiles":[
	  {"id":"mcp__grafana","short_name":"grafana","version":"1.0.0","description":"Grafana dashboards and alerts","mcp_pattern":"mcp__grafana__*","file":"profiles/mcp__grafana/default.toml","mcp_url":"https://github.com/grafana/mcp-grafana","author":"sakebomb"},
	  {"id":"mcp__jira","short_name":"jira","version":"1.0.0","description":"Jira issue tracking","mcp_pattern":"mcp__jira__*","file":"profiles/mcp__jira/default.toml","mcp_url":"https://github.com/atlassian/jira-mcp","author":"atlassian"}
	]}`
	toml := func(id string) string {
		return "[profile]\nid = \"" + id + "\"\nversion = \"1.0.0\"\ndescription = \"Test\"\nmcp_pattern = \"" + id + "__*\"\n[strategy]\ntype = \"text_truncate\""
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "manifest.json"):
			io.WriteString(w, manifest)
		case strings.Contains(r.URL.Path, "mcp__grafana"):
			io.WriteString(w, toml("mcp__grafana"))
		case strings.Contains(r.URL.Path, "mcp__jira"):
			io.WriteString(w, toml("mcp__jira"))
		default:
			http.Error(w, "not found", 404)
		}
	}))
	comm = t.TempDir()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfg, []byte("[profiles]\nverify_signature = \"skip\"\n"), 0o644)
	os.Setenv("RECALL_COMMUNITY_PROFILES_PATH", comm)
	os.Setenv("RECALL_MANIFEST_URL", srv.URL+"/manifest.json")
	os.Setenv("RECALL_PROFILE_BASE_URL", srv.URL+"/")
	os.Setenv("RECALL_CONFIG_PATH", cfg)
	config.Reset()
	return comm, func() {
		srv.Close()
		os.Unsetenv("RECALL_COMMUNITY_PROFILES_PATH")
		os.Unsetenv("RECALL_MANIFEST_URL")
		os.Unsetenv("RECALL_PROFILE_BASE_URL")
		os.Unsetenv("RECALL_CONFIG_PATH")
		config.Reset()
	}
}

func TestCmdSeedAll(t *testing.T) {
	comm, cleanup := fakeCatalog(t)
	defer cleanup()
	out := captureStdout(func() { cmdSeed([]string{"--all"}) })
	for _, want := range []string{"mcp__grafana installed", "mcp__jira installed", "2 profile(s) installed", "0 already installed", "2 total available"} {
		if !strings.Contains(out, want) {
			t.Errorf("seed --all missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "Detected MCPs") {
		t.Error("--all should skip MCP detection")
	}
	for _, id := range []string{"mcp__grafana", "mcp__jira"} {
		if _, err := os.Stat(filepath.Join(comm, id, "default.toml")); err != nil {
			t.Errorf("%s not written", id)
		}
	}

	// skip already-installed
	out2 := captureStdout(func() { cmdSeed([]string{"--all"}) })
	if !strings.Contains(out2, "mcp__grafana: already installed") || !strings.Contains(out2, "2 already installed") {
		t.Errorf("re-seed should report already-installed:\n%s", out2)
	}
}

func TestCmdInstallByName(t *testing.T) {
	comm, cleanup := fakeCatalog(t)
	defer cleanup()
	for _, arg := range []string{"grafana", "mcp__grafana"} {
		os.RemoveAll(filepath.Join(comm, "mcp__grafana"))
		captureStdout(func() { cmdInstall([]string{arg}) })
		if _, err := os.Stat(filepath.Join(comm, "mcp__grafana", "default.toml")); err != nil {
			t.Errorf("install by %q failed", arg)
		}
	}
}

func TestCmdAvailable(t *testing.T) {
	comm, cleanup := fakeCatalog(t)
	defer cleanup()
	out := captureStdout(func() { cmdAvailable(nil) })
	for _, want := range []string{"grafana", "jira", "2 available, 0 installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("available missing %q\n%s", want, out)
		}
	}

	os.MkdirAll(filepath.Join(comm, "mcp__grafana"), 0o755)
	os.WriteFile(filepath.Join(comm, "mcp__grafana", "default.toml"), []byte("[profile]\nid=\"mcp__grafana\"\nversion=\"1.0.0\"\ndescription=\"g\"\nmcp_pattern=\"mcp__grafana__*\"\n[strategy]\ntype=\"text_truncate\""), 0o644)
	out2 := captureStdout(func() { cmdAvailable(nil) })
	if !strings.Contains(out2, "installed") || !strings.Contains(out2, "2 available, 1 installed") {
		t.Errorf("available installed-marking:\n%s", out2)
	}

	out3 := captureStdout(func() { cmdAvailable([]string{"--verbose"}) })
	if !strings.Contains(out3, "https://github.com/grafana/mcp-grafana") || !strings.Contains(out3, "MCP URL") {
		t.Errorf("verbose should show MCP URL:\n%s", out3)
	}
}

// ── verifyManifest (fake gh) ──────────────────────────────────────────────────

// fakeGh writes a `gh` shim to a temp dir and prepends it to PATH. The shim
// exits 0 for `gh --version`, and exits with `failCode` (writing stderrMsg) for
// anything else (i.e. `gh attestation verify`). Returns a restore func.
func fakeGh(t *testing.T, failCode int, stderrMsg string) func() {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 0; fi\n"
	if stderrMsg != "" {
		script += "echo '" + stderrMsg + "' 1>&2\n"
	}
	script += "exit " + itoa2(failCode) + "\n"
	os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755)
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	return func() { os.Setenv("PATH", oldPath) }
}

// Repo scope alone would accept an attestation from any workflow in the
// profiles repo, so the exact signer SAN must be pinned on every verify (#206).
func TestVerifyManifestPinsSignerIdentity(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 0; fi\necho \"$@\" > " + argsFile + "\nexit 0\n"
	os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755)
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	defer os.Setenv("PATH", oldPath)

	tmp := filepath.Join(dir, "manifest.json")
	os.WriteFile(tmp, []byte("{}"), 0o644)
	if err := verifyManifest(tmp, "error"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("gh was not invoked: %v", err)
	}
	if !strings.Contains(string(got), "--cert-identity "+signerIdentity) {
		t.Errorf("verify did not pin the signer identity: %q", got)
	}
	want := "https://github.com/" + communityRepo + "/.github/workflows/manifest.yml@refs/heads/main"
	if signerIdentity != want {
		t.Errorf("signerIdentity = %q, want %q", signerIdentity, want)
	}
}

func noGh(t *testing.T) func() {
	t.Helper()
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // empty dir → no gh
	return func() { os.Setenv("PATH", oldPath) }
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestVerifyManifest(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "m.json")
	os.WriteFile(tmp, []byte(`{"profiles":[]}`), 0o644)

	// skip → no error, no gh call (even with a failing gh present)
	restore := fakeGh(t, 1, "boom")
	if err := verifyManifest(tmp, "skip"); err != nil {
		t.Errorf("skip should be a no-op: %v", err)
	}
	restore()

	// warn + gh fails → warns to stderr, no error
	restore = fakeGh(t, 1, "error: no attestation found")
	warnOut := captureStderr(func() {
		if err := verifyManifest(tmp, "warn"); err != nil {
			t.Errorf("warn should not error: %v", err)
		}
	})
	restore()
	if !strings.Contains(warnOut, "manifest signature verification failed") {
		t.Errorf("warn stderr: %q", warnOut)
	}

	// error + gh fails → returns error
	restore = fakeGh(t, 1, "verification failed")
	if err := verifyManifest(tmp, "error"); err == nil || !strings.Contains(err.Error(), "manifest signature verification failed") {
		t.Errorf("error mode should return err, got %v", err)
	}
	restore()

	// warn + no gh → warns "gh CLI not found", no error
	restore = noGh(t)
	nogOut := captureStderr(func() {
		if err := verifyManifest(tmp, "warn"); err != nil {
			t.Errorf("warn+nogh should not error: %v", err)
		}
	})
	if !strings.Contains(nogOut, "gh CLI not found") {
		t.Errorf("no-gh stderr: %q", nogOut)
	}
	// error + no gh → fatal: "error" means verification must succeed (#208)
	err := verifyManifest(tmp, "error")
	var verr *ManifestVerificationError
	if !errors.As(err, &verr) {
		t.Errorf("error+nogh should be fatal, got %v", err)
	} else if !strings.Contains(verr.Error(), "cannot be verified") ||
		strings.Contains(verr.Error(), "verification failed") {
		t.Errorf("unavailable verifier must not read as tampering: %q", verr)
	}
	restore()

	// gh too old for the flags we pin with → tooling gap, not tampering
	restore = fakeGh(t, 1, "unknown flag: --cert-identity")
	oldOut := captureStderr(func() {
		if err := verifyManifest(tmp, "warn"); err != nil {
			t.Errorf("warn+old-gh should not error: %v", err)
		}
	})
	if !strings.Contains(oldOut, "does not support the flags") {
		t.Errorf("old-gh stderr: %q", oldOut)
	}
	if err := verifyManifest(tmp, "error"); !errors.As(err, &verr) {
		t.Errorf("error+old-gh should be fatal, got %v", err)
	}
	restore()

	// warn + gh ok → silent, no error
	restore = fakeGh(t, 0, "")
	okOut := captureStderr(func() {
		if err := verifyManifest(tmp, "warn"); err != nil {
			t.Errorf("warn+ok should not error: %v", err)
		}
	})
	restore()
	if strings.TrimSpace(okOut) != "" {
		t.Errorf("warn+ok should be silent: %q", okOut)
	}
}

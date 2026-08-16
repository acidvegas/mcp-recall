// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/shared.go

package profiles

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mcprecall/internal/config"
)

const (
	manifestURL    = "https://raw.githubusercontent.com/sakebomb/mcp-recall-profiles/main/manifest.json"
	profileBaseURL = "https://raw.githubusercontent.com/sakebomb/mcp-recall-profiles/main/"
	communityRepo  = "sakebomb/mcp-recall-profiles"

	signerWorkflowPath = ".github/workflows/manifest.yml"
	signerRef          = "refs/heads/main"

	// signerIdentity is the exact signing identity we accept for the manifest,
	// matched against the certificate's SubjectAlternativeName.
	//
	// Repo scope alone is too weak: it accepts an attestation from *any*
	// workflow in communityRepo, so any workflow there able to obtain an OIDC
	// token could mint a trust root we would honour.
	//
	// --signer-workflow is not sufficient either: it compiles to a
	// prefix-anchored SAN match with no terminator, so a truncated
	// ".../workflows/man" also passes, and since the real SAN is
	// ".../manifest.yml@refs/heads/<ref>" the prefix stops short of the ref —
	// an attestation signed from any branch still matches. --cert-identity
	// matches the whole SAN exactly, which pins the ref too.
	//
	// Deliberately strict: if that workflow ever attests from a tag or a
	// renamed default branch, verification fails loudly rather than silently
	// widening what we trust.
	signerIdentity = "https://github.com/" + communityRepo + "/" + signerWorkflowPath + "@" + signerRef
)

var (
	safeIDRe   = regexp.MustCompile(`^[a-z0-9_-]+$`)
	safeFileRe = regexp.MustCompile(`^profiles/[a-z0-9_-]+/[a-z0-9_.-]+\.toml$`)
	sanitizeRe = regexp.MustCompile(`[\x00-\x1f\x7f]|\x{9b}|\x1b\[[0-9;]*[a-zA-Z]`)
)

func assertSafeID(id string) error {
	if !safeIDRe.MatchString(id) {
		return fmt.Errorf(`invalid profile id %q: must match ^[a-z0-9_-]+$`, id)
	}
	return nil
}

func assertSafeFile(file string) error {
	if !safeFileRe.MatchString(file) {
		return fmt.Errorf(`invalid profile file path %q: must match profiles/<id>/<name>.toml`, file)
	}
	return nil
}

// sanitize strips ANSI escapes and control characters.
func sanitize(v string) string { return sanitizeRe.ReplaceAllString(v, "") }

// ManifestEntry is one row of the community catalog manifest.
type ManifestEntry struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	MCPPattern  json.RawMessage `json:"mcp_pattern"`
	File        string          `json:"file"`
	SHA256      string          `json:"sha256"`
	Author      string          `json:"author"`
	ShortName   string          `json:"short_name"`
	MCPURL      string          `json:"mcp_url"`
}

func (e ManifestEntry) patterns() []string {
	var one string
	if json.Unmarshal(e.MCPPattern, &one) == nil {
		return []string{one}
	}
	var many []string
	_ = json.Unmarshal(e.MCPPattern, &many)
	return many
}

func manifestShortName(e ManifestEntry) string {
	if e.ShortName != "" {
		return e.ShortName
	}
	return strings.TrimPrefix(e.ID, "mcp__")
}

func httpGet(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("fetch failed (%s): %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func manifestEndpoint() string {
	if u := os.Getenv("RECALL_MANIFEST_URL"); u != "" {
		return u
	}
	return manifestURL
}

func profileBase() string {
	if u := os.Getenv("RECALL_PROFILE_BASE_URL"); u != "" {
		return u
	}
	return profileBaseURL
}

func fetchProfileContent(file string) (string, error) {
	return httpGet(profileBase() + file)
}

func verifyHash(content, expected, id string) error {
	if expected == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(content))
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("profile %s: hash mismatch (expected %s…, got %s…)", id, first8(expected), first8(actual))
	}
	return nil
}

func first8(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ManifestVerificationError marks a manifest that could not be trusted, as
// opposed to one that could not be fetched. Callers that degrade to a
// local-only view on a network error must still hard-fail on this (#234).
type ManifestVerificationError struct{ msg string }

func (e *ManifestVerificationError) Error() string { return e.msg }

// unsupportedFlagRe matches the stderr shapes gh uses when it doesn't
// recognise a flag we pass.
var unsupportedFlagRe = regexp.MustCompile(`(?i)unknown (flag|command|shorthand flag)`)

// reportUnavailable handles a verification that could not run — gh absent from
// PATH, or too old for the flags we pin with. That is a tooling gap, not
// evidence of tampering, so its diagnosis must never collapse into the
// "signature did not verify" message.
//
// In "error" mode verification is mandatory (#208): "error" means verification
// must *succeed*, so an unavailable verifier is fatal. "warn" logs the gap and
// proceeds. --skip-verify is the documented escape hatch and is named in the
// failure so it is actionable.
func reportUnavailable(mode, reason string) error {
	if mode == "error" {
		return &ManifestVerificationError{msg: fmt.Sprintf(
			"[recall] manifest signature cannot be verified: %s. "+
				`verify_signature = "error" requires verification to succeed — `+
				"install or upgrade gh, or pass --skip-verify to proceed without it.", reason)}
	}
	fmt.Fprintf(os.Stderr, "[recall] manifest signature verification skipped: %s\n", reason)
	return nil
}

// verifyManifest shells out to `gh attestation verify`, degrading gracefully if
// gh is absent or too old to support the flags we pin with.
func verifyManifest(manifestPath, mode string) error {
	if mode == "skip" {
		return nil
	}
	if exec.Command("gh", "--version").Run() != nil {
		return reportUnavailable(mode, "gh CLI not found in PATH")
	}
	cmd := exec.Command("gh", "attestation", "verify", manifestPath,
		"--repo", communityRepo,
		"--cert-identity", signerIdentity)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())

		// A gh too old for these flags exits non-zero just like a bad signature
		// does. Reporting that as "verification failed" would read as a tampered
		// manifest, so treat it as the tooling gap it is. Covers both an unknown
		// --cert-identity and, on older still, no `attestation` subcommand at
		// all; the remedy is the same either way.
		if unsupportedFlagRe.MatchString(errText) {
			return reportUnavailable(mode, "this gh CLI does not support the flags we verify with (upgrade gh)")
		}

		msg := "[recall] manifest signature verification failed"
		if errText != "" {
			msg += ": " + errText
		}
		if mode == "error" {
			return &ManifestVerificationError{msg: msg}
		}
		fmt.Fprintln(os.Stderr, msg)
	}
	return nil
}

func fetchManifest(skipVerify bool) ([]ManifestEntry, error) {
	text, err := httpGet(manifestEndpoint())
	if err != nil {
		return nil, err
	}
	if !skipVerify {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("mcp-recall-manifest-%d.json", os.Getpid()))
		if os.WriteFile(tmp, []byte(text), 0o644) == nil {
			cfg := config.Load()
			if err := verifyManifest(tmp, cfg.Profiles.VerifySignature); err != nil {
				os.Remove(tmp)
				return nil, err
			}
			os.Remove(tmp)
		}
	}
	var data struct {
		Profiles []ManifestEntry `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil, err
	}
	return data.Profiles, nil
}

func saveToCommunityDir(profileID, content string) (string, error) {
	dir := filepath.Join(communityDir(), profileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fp := filepath.Join(dir, "default.toml")
	return fp, os.WriteFile(fp, []byte(content), 0o644)
}

// resolveManifestEntry resolves a name-or-id to a catalog entry, prompting on
// ambiguity when attached to a TTY.
func resolveManifestEntry(nameOrID string, entries []ManifestEntry) (ManifestEntry, bool) {
	for _, e := range entries {
		if e.ID == nameOrID {
			return e, true
		}
	}
	var matches []ManifestEntry
	for _, e := range entries {
		if manifestShortName(e) == nameOrID {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], true
	case 0:
		fmt.Fprintf(os.Stderr, "Profile %q not found.\n", nameOrID)
		fmt.Println("Run: mcp-recall profiles available")
		return ManifestEntry{}, false
	}
	if !isTTY() {
		var ids []string
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		fmt.Fprintf(os.Stderr, "Error: %q is ambiguous. Matches: %s. Use the full id.\n", nameOrID, strings.Join(ids, ", "))
		return ManifestEntry{}, false
	}
	fmt.Printf("\nMultiple profiles match %q:\n", nameOrID)
	for i, e := range matches {
		fmt.Printf("  %d. %s %s %s\n", i+1, padEnd(sanitize(manifestShortName(e)), 22), padEnd(firstOf(e.patterns()), 32), firstChars(sanitize(e.Description), 40))
	}
	n := promptNumber(fmt.Sprintf("Pick one (1-%d): ", len(matches)), 1, len(matches))
	return matches[n-1], true
}

func firstOf(xs []string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return ""
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func promptNumber(msg string, min, max int) int {
	reader := bufio.NewReader(os.Stdin)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Print(msg)
		line, _ := reader.ReadString('\n')
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= min && n <= max {
			return n
		}
		fmt.Fprintf(os.Stderr, "Invalid choice. Enter a number between %d and %d.\n", min, max)
	}
	fmt.Fprintln(os.Stderr, "Too many invalid attempts.")
	os.Exit(1)
	return 0
}

func padEnd(s string, n int) string {
	if l := runeLen(s); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}

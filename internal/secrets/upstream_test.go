// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/secrets/upstream_test.go
// Port of the upstream v1.14.5 tests/secrets.test.ts vectors for #274, #276
// and #280 (left boundary, OpenAI watermark, Bearer/Stripe false positives).

package secrets

import (
	"slices"
	"strings"
	"testing"
)

func TestNoFalsePositiveOnSlugsAndProse(t *testing.T) {
	for _, s := range []string{
		// #274: fused "risk-"/"task-"/"disk-" slugs
		"risk-mitigation-through-controlled-rollout",
		"task-notification-and-output-file-tracking",
		"docs/risk-based-pr-splitting-and-continuous-review.md",
		"disk-usage-monitoring-and-alerting-playbook",
		"memoree/knowledge/2026-05-23-task-archiving-as-a-standard-practice-71c096",
		"risk-mitigation-none-526433-state-lockdown-procedures-x",
		// #276: standalone sk- slugs
		"sk-project-notes-draft-for-final-review.md",
		"sk-1042-add-retry-logic-to-worker-queue",
		"sk-2024-Q3-roadmap-planning-notes-for-team",
		"sk-ui-redesign-migration-checklist-v2",
		// slugs embedding a real key prefix
		"task-admin-console-access-policy-and-review-doc",
		"risk-proj-alpha-beta-gamma-delta-epsilon-zeta",
		"risk-ant-icipation-and-mitigation-planning-doc-x",
		// #280: Bearer and Stripe prose
		"use Bearer authentication-for-all-internal-endpoints",
		"Bearer tokens/are/documented/in/the/api/reference",
		"see docs/Bearer token-refresh-and-rotation-policy.md",
		"risk_live_assessmentdocumentationandreview",
		"network_test_configurationandvalidationdata",
		"work_test_dataconfigurationvaluesforstaging",
	} {
		if got := Find(s); len(got) != 0 || Contains(s) {
			t.Errorf("false positive on %q: %v", s, got)
		}
	}
}

func TestOpenAIPermissiveBodies(t *testing.T) {
	// #274 false-negative controls: base64url bodies contain - and _.
	for _, key := range []string{
		"sk-proj-Ab3-dEfGh1jKlM2nOpQr5tUvWxYz7aBcDeFgH9jKlMnOpQrStUvWxYz",
		"sk-proj-Ab3_dEfGh1jKlM2nOpQr5tUvWxYz7aBcDeFgH9jKlMnOpQrStUvWxYz",
		"sk-svcacct-Ab3dEfGh1jKlM2nOpQr5tUvWxYz7aBcDeFgH9jKlMnOpQrStUv",
		"sk-admin-Ab3dEfGh1jKlM2nOpQr5tUvWxYz7aBcDeFgH9jKlMnOpQrStUvWx",
		"sk-None-Ab3dEfGh1jKlM2nOpQr5tUvWxYz7aBcDeFgH9jKlMnOpQrStUvWxY",
	} {
		if !slices.Contains(Find(key), "OpenAI API key") || !Contains("OPENAI_API_KEY="+key) {
			t.Errorf("missed OpenAI key %q", key)
		}
	}
}

const (
	windowMin     = 16
	windowMax     = 120
	unknownPrefix = "newtype-"
	marker        = "T3BlbkFJ"
)

func b64url(n int) string {
	const cs = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(cs[(i*37+11)%len(cs)])
	}
	return b.String()
}

func markerKey(prefix string, pad int) string {
	return "sk-" + prefix + b64url(pad) + marker + b64url(pad)
}

func TestOpenAIWatermark(t *testing.T) {
	// #276: the watermark arm is prefix-agnostic, so unknown future prefixes and
	// both window edges are caught.
	for _, key := range []string{
		markerKey("proj-", 74),
		markerKey("svcacct-", 74),
		markerKey("admin-", 74),
		markerKey("", 74),
		markerKey(unknownPrefix, 74),
		markerKey(unknownPrefix, windowMax-len(unknownPrefix)),
		markerKey(unknownPrefix, windowMin),
		markerKey("proj-", 20),
	} {
		if !slices.Contains(Find(key), "OpenAI API key") || !Contains("OPENAI_API_KEY="+key) {
			t.Errorf("missed watermarked key %q", key)
		}
	}
	upper := markerKey(unknownPrefix, windowMax-len(unknownPrefix))
	lower := markerKey(unknownPrefix, windowMin)
	if strings.Index(upper, marker)-len("sk-") != windowMax {
		t.Error("upper fixture not on the window's upper edge")
	}
	if len(lower)-strings.Index(lower, marker)-len(marker) != windowMin {
		t.Error("lower fixture not on the window's lower edge")
	}
	if !slices.Contains(Find("the key is "+markerKey("proj-", 74)+" — do not commit it"), "OpenAI API key") {
		t.Error("missed watermarked key in prose")
	}
}

func TestOpenRouterLabelled(t *testing.T) {
	key := "sk-or-v1-" + strings.Repeat("9f3a", 16)
	hits := Find(key)
	if !slices.Contains(hits, "OpenRouter API key") || slices.Contains(hits, "OpenAI API key") {
		t.Errorf("OpenRouter key labelled %v", hits)
	}
}

// The watermark arm can match an sk-ant- key carrying the marker; the code
// lookahead must still keep it out of the OpenAI label.
func TestAnthropicWithWatermarkNotOpenAI(t *testing.T) {
	hits := Find(markerKey("ant-", 40))
	if !slices.Contains(hits, "Anthropic API key") || slices.Contains(hits, "OpenAI API key") {
		t.Errorf("sk-ant- with watermark labelled %v", hits)
	}
}

// A rejected sk-ant- candidate earlier in the text must not hide a real OpenAI
// key later.
func TestOpenAIAfterRejectedCandidate(t *testing.T) {
	text := "sk-ant-" + strings.Repeat("A", 40) + " and sk-" + strings.Repeat("B", 40)
	if !slices.Contains(Find(text), "OpenAI API key") {
		t.Error("OpenAI key after an Anthropic key was missed")
	}
}

func TestBearerArms(t *testing.T) {
	const jwtHeader = "eyJhbGciOiJIUzI1NiJ9"
	if len(jwtHeader) >= 32 {
		t.Fatal("JWT header must be shorter than the opaque arm's floor")
	}
	jwt := "Bearer " + jwtHeader + ".eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	opaque := "Bearer 7f3a9c2E1b8d4F6a0c5E9b1d3A7f2c8e"
	for _, s := range []string{jwt, opaque} {
		if !slices.Contains(Find("Authorization: "+s), "Generic Bearer token") {
			t.Errorf("missed %q", s)
		}
	}
}

func TestStripeTestRestrictedKey(t *testing.T) {
	if !slices.Contains(Find("rk_test_"+strings.Repeat("A", 24)), "Stripe secret/restricted key") {
		t.Error("missed rk_test_")
	}
}

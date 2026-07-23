// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/secrets/secrets_test.go

package secrets

import (
	"slices"
	"testing"
)

func rep(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}

func TestContainsSecretPositives(t *testing.T) {
	positives := []string{
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
		"-----BEGIN PRIVATE KEY-----\nMIIE...",
		"-----BEGIN EC PRIVATE KEY-----\nMIIE...",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC...",
		"token: ghp_" + rep("A", 36),
		"Authorization: github_pat_" + rep("A", 82),
		"gho_" + rep("A", 36),
		"OPENAI_API_KEY=sk-" + rep("A", 32),
		"OPENAI_API_KEY=sk-proj-" + rep("A", 40),
		"ANTHROPIC_API_KEY=sk-ant-" + rep("A", 32),
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"Authorization: Bearer " + rep("A", 32),
		`{"type": "service_account", "project_id": "my-proj"}`,
		`{"type":"service_account"}`,
		"DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=" + rep("A", 88) + "==",
		"STRIPE_SECRET_KEY=sk_live_" + rep("A", 24),
		"rk_live_" + rep("A", 24),
		"sk_test_" + rep("A", 24),
		"SENDGRID_API_KEY=SG." + rep("A", 22) + "." + rep("B", 43),
		"TWILIO_ACCOUNT_SID=AC" + rep("a", 32),
		"NPM_TOKEN=npm_" + rep("A", 36),
	}
	for _, p := range positives {
		if !Contains(p) {
			t.Errorf("expected secret in: %q", p)
		}
	}
}

func TestContainsSecretNegatives(t *testing.T) {
	negatives := []string{
		"Hello, world!",
		"some normal tool output with numbers 12345",
		"",
		"Authorization: Bearer short",
		`{"type": "user"}`,
		`{"type": "authorized_user"}`,
		"DefaultEndpointsProtocol=https;AccountName=myaccount",
		"pk_live_" + rep("A", 24), // publishable, not secret
		"SG.short.value",
		"AC" + rep("a", 31),   // short Twilio SID
		"npm_" + rep("A", 35), // short npm token
	}
	for _, n := range negatives {
		if Contains(n) {
			t.Errorf("should NOT flag: %q", n)
		}
	}
}

func TestOpenAIProjectAndAnthropic(t *testing.T) {
	// sk-proj- → OpenAI
	if !slices.Contains(Find("sk-proj-"+rep("A", 40)), "OpenAI API key") {
		t.Error("sk-proj- should be OpenAI")
	}
	// sk-ant- → Anthropic, NOT OpenAI
	hits := Find("sk-ant-" + rep("A", 32))
	if !slices.Contains(hits, "Anthropic API key") || slices.Contains(hits, "OpenAI API key") {
		t.Errorf("sk-ant- classification wrong: %v", hits)
	}
}

func TestFindSecrets(t *testing.T) {
	if got := Find("normal output"); len(got) != 0 {
		t.Errorf("clean → %v", got)
	}
	if !slices.Contains(Find("-----BEGIN RSA PRIVATE KEY-----\nMIIE..."), "PEM private key") {
		t.Error("PEM name")
	}
	multi := Find("-----BEGIN PRIVATE KEY-----\nAKIAIOSFODNN7EXAMPLE")
	if !slices.Contains(multi, "PEM private key") || !slices.Contains(multi, "AWS access key ID") || len(multi) < 2 {
		t.Errorf("multiple matches: %v", multi)
	}
	single := Find("ghp_" + rep("A", 36))
	if !slices.Contains(single, "GitHub PAT (classic)") || slices.Contains(single, "PEM private key") || slices.Contains(single, "AWS access key ID") {
		t.Errorf("single match should not include others: %v", single)
	}
}

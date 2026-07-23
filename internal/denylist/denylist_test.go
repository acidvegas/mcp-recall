// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/denylist/denylist_test.go

package denylist

import (
	"slices"
	"testing"

	"mcprecall/internal/config"
)

func base() config.Config {
	return config.Config{Denylist: config.Denylist{
		Additional: []string{}, OverrideDefaults: []string{}, Allowlist: []string{},
	}}
}

func withDenylist(additional, override, allow []string) config.Config {
	c := base()
	c.Denylist.Additional = additional
	c.Denylist.OverrideDefaults = override
	c.Denylist.Allowlist = allow
	return c
}

func TestMatchesPattern(t *testing.T) {
	cases := []struct {
		name, pattern string
		want          bool
	}{
		{"mcp__github__get_file", "mcp__github__get_file", true},
		{"mcp__recall__search", "mcp__recall__*", true},
		{"mcp__recall__retrieve", "mcp__recall__*", true},
		{"mcp__github__search", "mcp__recall__*", false},
		{"mcp__get_secret_value", "*secret*", true},
		{"my_token_store", "*token*", true},
		{"mcp__playwright__snapshot", "*secret*", false},
		{"get_password", "*password", true},
		{"get_password_hash", "*password", false},
		// regex special chars in non-wildcard segments are escaped (dot is literal)
		{"mcp_recall_search", "mcp.recall.search", false},
		{"mcp.recall.search", "mcp.recall.search", true},
		{"mcp__recall__search", "mcp__recall__search", true},
	}
	for _, c := range cases {
		if got := MatchesPattern(c.name, c.pattern); got != c.want {
			t.Errorf("MatchesPattern(%q,%q) = %v, want %v", c.name, c.pattern, got, c.want)
		}
	}
}

func TestBuiltinPatterns(t *testing.T) {
	for _, p := range []string{
		"mcp__recall__*", "mcp__1password__*", "mcp__bitwarden__*", "mcp__lastpass__*",
		"mcp__dashlane__*", "mcp__keeper__*", "mcp__hashicorp_vault__*", "mcp__vault__*",
		"mcp__doppler__*", "mcp__infisical__*",
	} {
		if !slices.Contains(BuiltinPatterns, p) {
			t.Errorf("BuiltinPatterns missing %q", p)
		}
	}
}

func TestDeniesPasswordManagers(t *testing.T) {
	c := base()
	denied := []string{
		"mcp__bitwarden__get_item", "mcp__bitwarden__list_logins", "mcp__lastpass__get_account",
		"mcp__dashlane__get_login", "mcp__keeper__get_record", "mcp__hashicorp_vault__read",
		"mcp__hashicorp_vault__list", "mcp__vault__get", "mcp__doppler__get_config", "mcp__infisical__list_items",
		"mcp__recall__search", "mcp__recall__retrieve", "mcp__1password__item_lookup",
	}
	for _, tn := range denied {
		if !IsDenied(tn, c) {
			t.Errorf("expected denied: %s", tn)
		}
	}
	for _, tn := range []string{"mcp__github__list_issues", "mcp__playwright__snapshot"} {
		if IsDenied(tn, c) {
			t.Errorf("expected allowed: %s", tn)
		}
	}
}

func TestDeniesSensitivePatterns(t *testing.T) {
	c := base()
	for _, tn := range []string{
		"mcp__get_secret", "mcp__read_token", "mcp__fetch_credentials", "mcp__get_api_key",
		"mcp__oauth_callback", "mcp__authenticate_user",
	} {
		if !IsDenied(tn, c) {
			t.Errorf("expected denied: %s", tn)
		}
	}
}

func TestNarrowedPatterns(t *testing.T) {
	c := base()
	// no longer blocks 'key' in non-credential context
	for _, tn := range []string{"mcp__jira__get_project_keys", "mcp__notion__get_keyboard_shortcuts", "mcp__db__get_primary_key"} {
		if IsDenied(tn, c) {
			t.Errorf("should allow: %s", tn)
		}
	}
	// still blocks specific key patterns
	for _, tn := range []string{"mcp__aws__get_api_key", "mcp__aws__get_access_key", "mcp__tls__get_private_key", "mcp__jwt__get_signing_key"} {
		if !IsDenied(tn, c) {
			t.Errorf("should deny: %s", tn)
		}
	}
	// 'auth' context
	for _, tn := range []string{"mcp__jira__get_author", "mcp__github__list_authors"} {
		if IsDenied(tn, c) {
			t.Errorf("should allow author: %s", tn)
		}
	}
	for _, tn := range []string{"mcp__oauth_callback", "mcp__get_auth_token", "mcp__authenticate_user"} {
		if !IsDenied(tn, c) {
			t.Errorf("should deny auth: %s", tn)
		}
	}
	// 'env' context
	for _, tn := range []string{"mcp__github__get_environments", "mcp__vercel__list_envs", "mcp__aws__describe_environment"} {
		if IsDenied(tn, c) {
			t.Errorf("should allow env: %s", tn)
		}
	}
	for _, tn := range []string{"mcp__get_env_var", "mcp__read_dotenv"} {
		if !IsDenied(tn, c) {
			t.Errorf("should deny env var: %s", tn)
		}
	}
}

func TestAdditionalOverrideAllowlist(t *testing.T) {
	add := withDenylist([]string{"mcp__custom__*"}, nil, nil)
	if !IsDenied("mcp__custom__do_thing", add) || IsDenied("mcp__playwright__snapshot", add) {
		t.Error("additional should extend builtins")
	}

	ov := withDenylist([]string{"mcp__extra__*"}, []string{"mcp__custom__*"}, nil)
	if !IsDenied("mcp__custom__do_thing", ov) {
		t.Error("override pattern should apply")
	}
	if IsDenied("mcp__1password__item_lookup", ov) {
		t.Error("override should disable builtins")
	}
	if !IsDenied("mcp__extra__thing", ov) {
		t.Error("additional still applies with override")
	}
	if !IsDenied("mcp__recall__search", withDenylist(nil, []string{}, nil)) {
		t.Error("empty override falls back to builtins")
	}

	al := withDenylist(nil, nil, []string{"mcp__vault__read_metadata"})
	if IsDenied("mcp__vault__read_metadata", al) {
		t.Error("allowlist should override deny")
	}
	if !IsDenied("mcp__vault__get_secret", al) {
		t.Error("other vault tools still denied")
	}
	wild := withDenylist(nil, nil, []string{"mcp__custom_auth_*"})
	if IsDenied("mcp__custom_auth_list", wild) || IsDenied("mcp__custom_auth_detail", wild) {
		t.Error("wildcard allowlist should un-block prefix")
	}
	if !IsDenied("mcp__get_secret", base()) {
		t.Error("empty allowlist has no effect")
	}
}

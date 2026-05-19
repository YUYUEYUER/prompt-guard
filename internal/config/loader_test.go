package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigUsesStreamingFriendlyAndSafeDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.WriteTimeout != "0s" {
		t.Fatalf("WriteTimeout = %q, want %q", cfg.Server.WriteTimeout, "0s")
	}
	if cfg.Admin.Enabled {
		t.Fatalf("Admin.Enabled = true, want false")
	}
}

func TestValidateRequiresAdminTokenWhenAdminEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Upstream.BaseURL = "http://127.0.0.1:8080"
	cfg.Policy.InspectPaths = []string{"/v1/chat/completions"}
	cfg.Rules = []Rule{
		{
			ID:        "r1",
			Enabled:   true,
			Endpoints: []string{"/v1/chat/completions"},
			Scopes:    []string{"user"},
			Match: MatchConfig{
				Type:  "contains_any",
				Words: []string{"test"},
			},
			Action: ActionConfig{
				Type: "block",
			},
		},
	}
	cfg.Admin.Enabled = true
	cfg.Admin.BearerToken = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want admin token validation error")
	}
}

func TestValidateAllowsEmptyBlockResponses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Upstream.BaseURL = "http://127.0.0.1:8080"
	cfg.Policy.Mode = "enforce"
	cfg.Policy.InspectPaths = []string{"/v1/chat/completions"}
	cfg.Rules = []Rule{
		{
			ID:        "r1",
			Enabled:   true,
			Endpoints: []string{"/v1/chat/completions"},
			Scopes:    []string{"user"},
			Match: MatchConfig{
				Type:  "contains_any",
				Words: []string{"test"},
			},
			Action: ActionConfig{
				Type:         "block",
				StatusCode:   200,
				ResponseMode: "empty",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestLoadAppliesQuickstartEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("server:\n" +
		"  listen: \":8099\"\n" +
		"upstream:\n" +
		"  base_url: \"http://127.0.0.1:8080\"\n" +
		"policy:\n" +
		"  mode: \"dry-run\"\n" +
		"  fail_mode: \"fail_open\"\n" +
		"  request_body_limit: \"2MB\"\n" +
		"  inspect_paths:\n" +
		"    - \"/v1/chat/completions\"\n" +
		"headers:\n" +
		"  forward_request_id: true\n" +
		"  request_id_header: \"X-Request-Id\"\n" +
		"  decision_header: \"X-Prompt-Guard-Decision\"\n" +
		"  hits_header: \"X-Prompt-Guard-Hits\"\n" +
		"audit:\n" +
		"  enabled: false\n" +
		"  format: \"json\"\n" +
		"  log_full_text: false\n" +
		"  evidence_max_chars: 80\n" +
		"admin:\n" +
		"  enabled: false\n" +
		"  bearer_token: \"\"\n" +
		"metrics:\n" +
		"  enabled: false\n" +
		"  path: \"/metrics\"\n" +
		"rules:\n" +
		"  - id: \"r1\"\n" +
		"    enabled: true\n" +
		"    endpoints:\n" +
		"      - \"/v1/chat/completions\"\n" +
		"    scopes:\n" +
		"      - \"user\"\n" +
		"    match:\n" +
		"      type: \"contains_any\"\n" +
		"      words:\n" +
		"        - \"test\"\n" +
		"    action:\n" +
		"      type: \"block\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("PROMPT_GUARD_UPSTREAM_BASE_URL", "http://127.0.0.1:3000")
	t.Setenv("PROMPT_GUARD_POLICY_MODE", "enforce")
	t.Setenv("PROMPT_GUARD_DEFAULT_BLOCK_STATUS_CODE", "200")
	t.Setenv("PROMPT_GUARD_DEFAULT_BLOCK_RESPONSE_MODE", "empty")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.Upstream.BaseURL, "http://127.0.0.1:3000"; got != want {
		t.Fatalf("Upstream.BaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.Policy.Mode, "enforce"; got != want {
		t.Fatalf("Policy.Mode = %q, want %q", got, want)
	}
	if got, want := cfg.Rules[0].Action.StatusCode, 200; got != want {
		t.Fatalf("Action.StatusCode = %d, want %d", got, want)
	}
	if got, want := cfg.Rules[0].Action.ResponseMode, "empty"; got != want {
		t.Fatalf("Action.ResponseMode = %q, want %q", got, want)
	}
}

func TestValidateRequiresMaxEditDistanceForFuzzyMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Upstream.BaseURL = "http://127.0.0.1:8080"
	cfg.Policy.InspectPaths = []string{"/v1/chat/completions"}
	cfg.Rules = []Rule{
		{
			ID:        "r1",
			Enabled:   true,
			Endpoints: []string{"/v1/chat/completions"},
			Scopes:    []string{"user"},
			Match: MatchConfig{
				Type:  "fuzzy_contains_any",
				Words: []string{"ignore previous instructions"},
			},
			Action: ActionConfig{
				Type: "block",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want fuzzy max_edit_distance validation error")
	}
}

func TestValidateAcceptsFuzzyContainsAnyRule(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Upstream.BaseURL = "http://127.0.0.1:8080"
	cfg.Policy.InspectPaths = []string{"/v1/chat/completions"}
	cfg.Rules = []Rule{
		{
			ID:        "r1",
			Enabled:   true,
			Endpoints: []string{"/v1/chat/completions"},
			Scopes:    []string{"user"},
			Match: MatchConfig{
				Type:            "fuzzy_contains_any",
				Words:           []string{"ignore previous instructions"},
				MaxEditDistance: 2,
			},
			Action: ActionConfig{
				Type: "block",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

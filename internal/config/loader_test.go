package config

import "testing"

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

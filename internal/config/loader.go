package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Listen == "" {
		return errors.New("server.listen is required")
	}
	if c.Upstream.BaseURL == "" {
		return errors.New("upstream.base_url is required")
	}
	u, err := url.Parse(c.Upstream.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("upstream.base_url must be a valid http/https url")
	}
	if _, err := ParseDuration(c.Server.ReadTimeout); err != nil {
		return fmt.Errorf("invalid server.read_timeout: %w", err)
	}
	if _, err := ParseDuration(c.Server.WriteTimeout); err != nil {
		return fmt.Errorf("invalid server.write_timeout: %w", err)
	}
	if _, err := ParseDuration(c.Server.IdleTimeout); err != nil {
		return fmt.Errorf("invalid server.idle_timeout: %w", err)
	}
	if _, err := ParseDuration(c.Upstream.Timeout); err != nil {
		return fmt.Errorf("invalid upstream.timeout: %w", err)
	}
	if _, err := ParseDuration(c.Upstream.KeepAlive); err != nil {
		return fmt.Errorf("invalid upstream.keep_alive: %w", err)
	}
	if _, err := ParseByteSize(c.Policy.RequestBodyLimit); err != nil {
		return fmt.Errorf("invalid policy.request_body_limit: %w", err)
	}
	if c.Policy.Mode != "dry-run" && c.Policy.Mode != "enforce" {
		return errors.New("policy.mode must be dry-run or enforce")
	}
	if c.Policy.FailMode != "fail_open" && c.Policy.FailMode != "fail_close" {
		return errors.New("policy.fail_mode must be fail_open or fail_close")
	}
	if c.Metrics.Enabled && c.Metrics.Path == "" {
		return errors.New("metrics.path is required when metrics.enabled is true")
	}
	if c.Admin.Enabled && strings.TrimSpace(c.Admin.BearerToken) == "" {
		return errors.New("admin.bearer_token is required when admin.enabled is true")
	}

	seenIDs := map[string]struct{}{}
	for i := range c.Rules {
		rule := &c.Rules[i]
		if rule.ID == "" {
			return errors.New("rules[].id is required")
		}
		if _, exists := seenIDs[rule.ID]; exists {
			return fmt.Errorf("duplicate rule id: %s", rule.ID)
		}
		seenIDs[rule.ID] = struct{}{}
		if len(rule.Endpoints) == 0 {
			return fmt.Errorf("rule %s must define at least one endpoint", rule.ID)
		}
		if len(rule.Scopes) == 0 {
			return fmt.Errorf("rule %s must define at least one scope", rule.ID)
		}
		switch rule.Match.Type {
		case "contains_any", "exact":
			if len(rule.Match.Words) == 0 {
				return fmt.Errorf("rule %s must define match.words", rule.ID)
			}
		case "regex":
			if len(rule.Match.Patterns) == 0 {
				return fmt.Errorf("rule %s must define match.patterns", rule.ID)
			}
			for _, pattern := range rule.Match.Patterns {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("rule %s has invalid regex %q: %w", rule.ID, pattern, err)
				}
			}
		default:
			return fmt.Errorf("rule %s has unsupported match.type %q", rule.ID, rule.Match.Type)
		}
		switch rule.Action.Type {
		case "block":
			if rule.Action.StatusCode == 0 {
				rule.Action.StatusCode = 403
			}
			if rule.Action.Message == "" {
				rule.Action.Message = "request blocked by prompt policy"
			}
		case "log_only", "tag_and_pass":
		default:
			return fmt.Errorf("rule %s has unsupported action.type %q", rule.ID, rule.Action.Type)
		}
	}

	return nil
}

func ParseDuration(value string) (time.Duration, error) {
	return time.ParseDuration(strings.TrimSpace(value))
}

func ParseByteSize(value string) (int64, error) {
	s := strings.TrimSpace(strings.ToUpper(value))
	multipliers := []struct {
		Suffix string
		Scale  int64
	}{
		{"KB", 1024},
		{"MB", 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"B", 1},
	}

	for _, item := range multipliers {
		if strings.HasSuffix(s, item.Suffix) {
			var amount int64
			if _, err := fmt.Sscanf(strings.TrimSuffix(s, item.Suffix), "%d", &amount); err != nil {
				return 0, fmt.Errorf("invalid byte size %q", value)
			}
			return amount * item.Scale, nil
		}
	}

	var raw int64
	if _, err := fmt.Sscanf(s, "%d", &raw); err != nil {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	return raw, nil
}

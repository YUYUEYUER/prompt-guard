package config

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Policy   PolicyConfig   `yaml:"policy"`
	Headers  HeadersConfig  `yaml:"headers"`
	Audit    AuditConfig    `yaml:"audit"`
	Admin    AdminConfig    `yaml:"admin"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Rules    []Rule         `yaml:"rules"`
}

type ServerConfig struct {
	Listen         string `yaml:"listen"`
	ReadTimeout    string `yaml:"read_timeout"`
	WriteTimeout   string `yaml:"write_timeout"`
	IdleTimeout    string `yaml:"idle_timeout"`
	MaxHeaderBytes int    `yaml:"max_header_bytes"`
}

type UpstreamConfig struct {
	BaseURL             string `yaml:"base_url"`
	Timeout             string `yaml:"timeout"`
	KeepAlive           string `yaml:"keep_alive"`
	MaxIdleConns        int    `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int    `yaml:"max_idle_conns_per_host"`
}

type PolicyConfig struct {
	Mode                         string       `yaml:"mode"`
	FailMode                     string       `yaml:"fail_mode"`
	RequestBodyLimit             string       `yaml:"request_body_limit"`
	InspectPaths                 []string     `yaml:"inspect_paths"`
	Bypass                       BypassConfig `yaml:"bypass"`
	SkipOnUnknownContentEncoding bool         `yaml:"skip_on_unknown_content_encoding"`
	SkipOnUnknownSchema          bool         `yaml:"skip_on_unknown_schema"`
	EarlyRejectOversize          bool         `yaml:"early_reject_oversize"`
}

type BypassConfig struct {
	APIKeys        []string `yaml:"api_keys"`
	APIKeyPrefixes []string `yaml:"api_key_prefixes"`
	ClientIPs      []string `yaml:"client_ips"`
}

type HeadersConfig struct {
	ForwardRequestID bool   `yaml:"forward_request_id"`
	RequestIDHeader  string `yaml:"request_id_header"`
	DecisionHeader   string `yaml:"decision_header"`
	HitsHeader       string `yaml:"hits_header"`
}

type AuditConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Format           string `yaml:"format"`
	LogFullText      bool   `yaml:"log_full_text"`
	EvidenceMaxChars int    `yaml:"evidence_max_chars"`
}

type AdminConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Listen      string `yaml:"listen"`
	BearerToken string `yaml:"bearer_token"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type Rule struct {
	ID          string       `yaml:"id"`
	Enabled     bool         `yaml:"enabled"`
	Description string       `yaml:"description"`
	Priority    int          `yaml:"priority"`
	Endpoints   []string     `yaml:"endpoints"`
	Scopes      []string     `yaml:"scopes"`
	Match       MatchConfig  `yaml:"match"`
	Action      ActionConfig `yaml:"action"`
}

type MatchConfig struct {
	Type     string   `yaml:"type"`
	Words    []string `yaml:"words"`
	Patterns []string `yaml:"patterns"`
}

type ActionConfig struct {
	Type       string `yaml:"type"`
	StatusCode int    `yaml:"status_code"`
	Message    string `yaml:"message"`
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Listen:         ":8099",
			ReadTimeout:    "15s",
			WriteTimeout:   "0s",
			IdleTimeout:    "120s",
			MaxHeaderBytes: 1 << 20,
		},
		Upstream: UpstreamConfig{
			Timeout:             "180s",
			KeepAlive:           "30s",
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
		},
		Policy: PolicyConfig{
			Mode:                         "dry-run",
			FailMode:                     "fail_open",
			RequestBodyLimit:             "2MB",
			SkipOnUnknownContentEncoding: true,
			SkipOnUnknownSchema:          true,
			EarlyRejectOversize:          true,
		},
		Headers: HeadersConfig{
			ForwardRequestID: true,
			RequestIDHeader:  "X-Request-Id",
			DecisionHeader:   "X-Prompt-Guard-Decision",
			HitsHeader:       "X-Prompt-Guard-Hits",
		},
		Audit: AuditConfig{
			Enabled:          true,
			Format:           "json",
			LogFullText:      false,
			EvidenceMaxChars: 80,
		},
		Admin: AdminConfig{
			Enabled: false,
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
	}
}

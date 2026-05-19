package server

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleProxyFailOpenReplaysBodyWhenAvailable(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("upstream read body: %v", err)
		}
		if got, want := string(body), `{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`; got != want {
			t.Fatalf("upstream body = %s, want %s", got, want)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfgPath := writeTempConfig(t, upstream.URL)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app, err := New(cfgPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	originalBody := []byte(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(originalBody))
	req.Body = failingReadCloser{}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(originalBody)), nil
	}

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if !upstreamCalled {
		t.Fatal("expected upstream to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Prompt-Guard-Decision"); got != "skip" {
		t.Fatalf("decision header = %q, want %q", got, "skip")
	}
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("boom")
}

func (failingReadCloser) Close() error {
	return nil
}

func writeTempConfig(t *testing.T, upstreamURL string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("server:\n" +
		"  listen: \":8099\"\n" +
		"  read_timeout: \"15s\"\n" +
		"  write_timeout: \"0s\"\n" +
		"  idle_timeout: \"120s\"\n" +
		"  max_header_bytes: 1048576\n" +
		"upstream:\n" +
		"  base_url: \"" + upstreamURL + "\"\n" +
		"  timeout: \"30s\"\n" +
		"  keep_alive: \"30s\"\n" +
		"  max_idle_conns: 10\n" +
		"  max_idle_conns_per_host: 10\n" +
		"policy:\n" +
		"  mode: \"dry-run\"\n" +
		"  fail_mode: \"fail_open\"\n" +
		"  request_body_limit: \"2MB\"\n" +
		"  inspect_paths:\n" +
		"    - \"/v1/chat/completions\"\n" +
		"  bypass:\n" +
		"    api_keys: []\n" +
		"    api_key_prefixes: []\n" +
		"    client_ips: []\n" +
		"  skip_on_unknown_content_encoding: true\n" +
		"  skip_on_unknown_schema: true\n" +
		"  early_reject_oversize: true\n" +
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
		"  - id: \"block-system-prompt-leak\"\n" +
		"    enabled: true\n" +
		"    description: \"Block prompt leakage attempts.\"\n" +
		"    priority: 100\n" +
		"    endpoints:\n" +
		"      - \"/v1/chat/completions\"\n" +
		"    scopes:\n" +
		"      - \"user\"\n" +
		"    match:\n" +
		"      type: \"contains_any\"\n" +
		"      words:\n" +
		"        - \"ignore previous instructions\"\n" +
		"    action:\n" +
		"      type: \"block\"\n" +
		"      status_code: 403\n" +
		"      message: \"request blocked by prompt policy\"\n")

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

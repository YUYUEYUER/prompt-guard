package server

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"prompt-guard/internal/audit"
	"prompt-guard/internal/config"
	"prompt-guard/internal/engine"
	"prompt-guard/internal/extractor"
	"prompt-guard/internal/inspect"
	"prompt-guard/internal/model"
	"prompt-guard/internal/normalize"
	"prompt-guard/internal/proxy"
)

type App struct {
	configPath string
	logger     *slog.Logger
	metrics    *Metrics

	reloadMu sync.Mutex
	state    atomic.Pointer[runtimeState]
}

type runtimeState struct {
	cfg       *config.Config
	bodyLimit int64
	audit     *audit.Logger
	inspector *inspect.Service
	proxy     *proxy.Service
	mux       *http.ServeMux
}

func New(configPath string, logger *slog.Logger) (*App, error) {
	app := &App{
		configPath: configPath,
		logger:     logger,
		metrics:    NewMetrics(),
	}
	if err := app.reload(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) ListenAddr() string {
	state := a.state.Load()
	if state == nil || state.cfg == nil {
		return ":8099"
	}
	return state.cfg.Server.Listen
}

func (a *App) ReadTimeout() time.Duration {
	return a.serverDuration(func(cfg *config.Config) string { return cfg.Server.ReadTimeout }, 15*time.Second)
}

func (a *App) WriteTimeout() time.Duration {
	return a.serverDuration(func(cfg *config.Config) string { return cfg.Server.WriteTimeout }, 120*time.Second)
}

func (a *App) IdleTimeout() time.Duration {
	return a.serverDuration(func(cfg *config.Config) string { return cfg.Server.IdleTimeout }, 120*time.Second)
}

func (a *App) MaxHeaderBytes() int {
	state := a.state.Load()
	if state == nil || state.cfg == nil || state.cfg.Server.MaxHeaderBytes == 0 {
		return 1 << 20
	}
	return state.cfg.Server.MaxHeaderBytes
}

func (a *App) Handler() http.Handler {
	state := a.state.Load()
	if state == nil || state.mux == nil {
		return http.NotFoundHandler()
	}
	return state.mux
}

func (a *App) serverDuration(selector func(*config.Config) string, fallback time.Duration) time.Duration {
	state := a.state.Load()
	if state == nil || state.cfg == nil {
		return fallback
	}
	duration, err := config.ParseDuration(selector(state.cfg))
	if err != nil {
		return fallback
	}
	return duration
}

func (a *App) reload() error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}

	bodyLimit, err := cfg.RequestBodyLimitBytes()
	if err != nil {
		return err
	}

	normalizer := normalize.New()
	ruleEngine, err := engine.New(cfg, normalizer.Normalize)
	if err != nil {
		return err
	}
	inspectService := inspect.New(cfg, extractor.DefaultExtractors(), normalizer, ruleEngine)
	proxyService, err := proxy.New(cfg.Upstream, a.logger)
	if err != nil {
		return err
	}

	state := &runtimeState{
		cfg:       cfg,
		bodyLimit: bodyLimit,
		audit:     audit.New(a.logger, cfg.Audit),
		inspector: inspectService,
		proxy:     proxyService,
	}
	state.mux = a.buildMux(state)
	a.state.Store(state)
	return nil
}

func (a *App) buildMux(state *runtimeState) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/readyz", a.handleReady)
	if state.cfg.Metrics.Enabled {
		mux.Handle(state.cfg.Metrics.Path, a.metrics.Handler())
	}
	if state.cfg.Admin.Enabled {
		mux.HandleFunc("/admin/reload", a.handleReload)
	}
	mux.HandleFunc("/", a.handleProxy)
	return mux
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleReady(w http.ResponseWriter, _ *http.Request) {
	if a.state.Load() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *App) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
		return
	}

	state := a.state.Load()
	if state == nil || !state.cfg.Admin.Enabled {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]string{"type": "not_found", "message": "not found"},
		})
		return
	}
	if state.cfg.Admin.BearerToken != "" && !authorized(r, state.cfg.Admin.BearerToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"type": "unauthorized", "message": "invalid admin token"},
		})
		return
	}

	if err := a.reload(); err != nil {
		a.logger.Error("config reload failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "reload_failed", "message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (a *App) handleProxy(w http.ResponseWriter, r *http.Request) {
	a.metrics.requestsTotal.Add(1)

	state := a.state.Load()
	if state == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"type": "not_ready", "message": "server not ready"},
		})
		return
	}

	requestID := ensureRequestID(r, state.cfg.Headers.RequestIDHeader)
	w.Header().Set(state.cfg.Headers.RequestIDHeader, requestID)

	if !state.cfg.ShouldInspectPath(r.URL.Path) || r.Method != http.MethodPost || !isJSONRequest(r.Header.Get("Content-Type")) {
		w.Header().Set(state.cfg.Headers.DecisionHeader, model.DecisionAllow)
		state.proxy.ServeHTTP(w, r)
		return
	}

	apiKey := resolveAPIKey(r.Header)
	apiKeyHash := hashAPIKey(apiKey)
	if isBypassed(state.cfg, apiKey, clientIP(r)) {
		w.Header().Set(state.cfg.Headers.DecisionHeader, model.DecisionAllow)
		state.proxy.ServeHTTP(w, r)
		return
	}

	contentEncoding := strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Encoding")))
	if contentEncoding != "" && contentEncoding != "identity" {
		if state.cfg.Policy.SkipOnUnknownContentEncoding {
			a.metrics.skippedTotal.Add(1)
			w.Header().Set(state.cfg.Headers.DecisionHeader, model.DecisionSkip)
			state.proxy.ServeHTTP(w, r)
			return
		}
		if state.cfg.Policy.FailMode == "fail_open" {
			state.proxy.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{
			"error": map[string]string{"type": "unsupported_content_encoding", "message": "content encoding not supported"},
		})
		return
	}

	if state.cfg.Policy.EarlyRejectOversize && r.ContentLength > state.bodyLimit && r.ContentLength > 0 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": map[string]string{"type": "payload_too_large", "message": "request body exceeds configured limit"},
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, state.bodyLimit+1))
	if err != nil {
		a.metrics.extractErrorsTotal.Add(1)
		a.logger.Error("failed to read request body", slog.String("error", err.Error()))
		if state.cfg.Policy.FailMode == "fail_open" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"type": "bad_request", "message": "unable to read request body"},
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": "unable to inspect request body"},
		})
		return
	}
	_ = r.Body.Close()

	if int64(len(body)) > state.bodyLimit {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": map[string]string{"type": "payload_too_large", "message": "request body exceeds configured limit"},
		})
		return
	}

	inspectHeaders := r.Header.Clone()
	inspectHeaders.Set("X-API-Key-Hash", apiKeyHash)
	inspectRequest := &model.InspectionRequest{
		Method:      r.Method,
		Path:        r.URL.Path,
		ContentType: r.Header.Get("Content-Type"),
		Body:        body,
		Headers:     inspectHeaders,
		ClientIP:    clientIP(r),
		RequestID:   requestID,
	}

	result, err := state.inspector.Inspect(r.Context(), inspectRequest)
	if err != nil {
		a.metrics.extractErrorsTotal.Add(1)
		a.logger.Error("inspection failed", slog.String("request_id", requestID), slog.String("error", err.Error()))
		if state.cfg.Policy.FailMode == "fail_open" {
			result = &model.InspectionResult{
				Decision:   model.DecisionAllow,
				Skipped:    true,
				SkipReason: "inspection_error_fail_open",
				Meta: model.RequestMeta{
					Path:       r.URL.Path,
					APIKeyHash: apiKeyHash,
					ClientIP:   clientIP(r),
				},
			}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"type": "inspection_failed", "message": "request inspection failed"},
			})
			return
		}
	}

	result.Meta.Path = r.URL.Path
	if result.Meta.APIKeyHash == "" {
		result.Meta.APIKeyHash = apiKeyHash
	}
	if result.Meta.ClientIP == "" {
		result.Meta.ClientIP = clientIP(r)
	}
	state.audit.LogDecision(requestID, result)

	a.metrics.inspectedTotal.Add(1)
	a.metrics.inspectionDurationMicros.Add(uint64(result.Duration.Microseconds()))
	if result.Skipped {
		a.metrics.skippedTotal.Add(1)
	}

	w.Header().Set(state.cfg.Headers.DecisionHeader, result.Decision)
	w.Header().Set(state.cfg.Headers.HitsHeader, strconv.Itoa(len(result.MatchedRules)))
	if result.Decision == model.DecisionBlock {
		a.metrics.blockedTotal.Add(1)
		blocked := firstMatch(result)
		writeBlockedResponse(w, requestID, blocked)
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	if r.GetBody != nil {
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	state.proxy.ServeHTTP(w, r)
}

func firstMatch(result *model.InspectionResult) model.MatchResult {
	if result != nil && len(result.MatchedRules) > 0 {
		return result.MatchedRules[0]
	}
	return model.MatchResult{
		RuleID:       "unknown",
		StatusCode:   http.StatusForbidden,
		ResponseBody: "request blocked by prompt policy",
	}
}

func writeBlockedResponse(w http.ResponseWriter, requestID string, match model.MatchResult) {
	statusCode := match.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusForbidden
	}
	writeJSON(w, statusCode, map[string]any{
		"error": map[string]any{
			"type":       "prompt_policy_violation",
			"code":       "PROMPT_GUARD_BLOCKED",
			"message":    match.ResponseBody,
			"rule_id":    match.RuleID,
			"request_id": requestID,
		},
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func isJSONRequest(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/json") || strings.Contains(ct, "application/vnd.api+json")
}

func ensureRequestID(r *http.Request, header string) string {
	if existing := strings.TrimSpace(r.Header.Get(header)); existing != "" {
		return existing
	}
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return "req_" + hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func resolveAPIKey(headers http.Header) string {
	if key := strings.TrimSpace(headers.Get("X-API-Key")); key != "" {
		return key
	}
	if auth := strings.TrimSpace(headers.Get("Authorization")); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func hashAPIKey(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func isBypassed(cfg *config.Config, apiKey string, ip string) bool {
	for _, candidate := range cfg.Policy.Bypass.APIKeys {
		if apiKey != "" && apiKey == candidate {
			return true
		}
	}
	for _, prefix := range cfg.Policy.Bypass.APIKeyPrefixes {
		if apiKey != "" && strings.HasPrefix(apiKey, prefix) {
			return true
		}
	}
	for _, allowed := range cfg.Policy.Bypass.ClientIPs {
		if ip == allowed {
			return true
		}
	}
	return false
}

func authorized(r *http.Request, token string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return false
	}
	expected := "Bearer " + token
	return auth == expected
}

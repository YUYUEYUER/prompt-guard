package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/YUYUEYUER/prompt-guard/internal/config"
	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

type configResponse struct {
	ConfigPath string        `json:"config_path"`
	YAML       string        `json:"yaml"`
	Summary    configSummary `json:"summary"`
	Metrics    Snapshot      `json:"metrics"`
	Backups    []backupInfo  `json:"backups,omitempty"`
}

type configSummary struct {
	Listen        string   `json:"listen"`
	UpstreamBase  string   `json:"upstream_base_url"`
	Mode          string   `json:"mode"`
	InspectPaths  []string `json:"inspect_paths"`
	RulesCount    int      `json:"rules_count"`
	EnabledRules  int      `json:"enabled_rules"`
	AdminEnabled  bool     `json:"admin_enabled"`
	TokenRequired bool     `json:"token_required"`
}

type configUpdateRequest struct {
	YAML   string `json:"yaml"`
	Reason string `json:"reason,omitempty"`
}

type configPreviewResponse struct {
	ConfigPath      string        `json:"config_path"`
	CurrentSummary  configSummary `json:"current_summary"`
	ProposedSummary configSummary `json:"proposed_summary"`
	Diff            string        `json:"diff"`
}

type rollbackRequest struct {
	BackupID string `json:"backup_id"`
}

type testRequest struct {
	Path        string `json:"path"`
	Text        string `json:"text"`
	Body        string `json:"body"`
	Role        string `json:"role"`
	Scope       string `json:"scope"`
	ContentType string `json:"content_type"`
}

type testResponse struct {
	Path           string              `json:"path"`
	ContentType    string              `json:"content_type"`
	RequestBody    string              `json:"request_body"`
	Decision       string              `json:"decision"`
	Skipped        bool                `json:"skipped"`
	SkipReason     string              `json:"skip_reason,omitempty"`
	FragmentsCount int                 `json:"fragments_count"`
	DurationMS     float64             `json:"duration_ms"`
	MatchedRules   []model.MatchResult `json:"matched_rules"`
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	state, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		raw, err := os.ReadFile(a.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"type": "read_failed", "message": err.Error()},
			})
			return
		}
		backups, _ := a.listBackups(20)
		writeJSON(w, http.StatusOK, configResponse{
			ConfigPath: a.configPath,
			YAML:       string(raw),
			Summary:    summarizeConfig(state),
			Metrics:    a.metrics.Snapshot(),
			Backups:    backups,
		})
	case http.MethodPut:
		payload, err := decodeConfigUpdate(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"type": "bad_request", "message": err.Error()},
			})
			return
		}
		if _, err := a.saveConfig([]byte(payload.YAML), payload.Reason); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"type": "config_invalid", "message": err.Error()},
			})
			return
		}

		updatedState := a.state.Load()
		raw, err := os.ReadFile(a.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"type": "read_failed", "message": err.Error()},
			})
			return
		}

		backups, _ := a.listBackups(20)
		writeJSON(w, http.StatusOK, configResponse{
			ConfigPath: a.configPath,
			YAML:       string(raw),
			Summary:    summarizeConfig(updatedState),
			Metrics:    a.metrics.Snapshot(),
			Backups:    backups,
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
	}
}

func (a *App) handleConfigPreview(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
		return
	}

	payload, err := decodeConfigUpdate(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": err.Error()},
		})
		return
	}

	response, err := a.previewConfig([]byte(payload.YAML))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "config_invalid", "message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleConfigApply(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
		return
	}

	payload, err := decodeConfigUpdate(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": err.Error()},
		})
		return
	}

	if _, err := a.saveConfig([]byte(payload.YAML), payload.Reason); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "config_invalid", "message": err.Error()},
		})
		return
	}
	a.writeCurrentConfigResponse(w)
}

func (a *App) handleConfigRollback(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
		return
	}

	var payload rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": "invalid json body"},
		})
		return
	}
	if strings.TrimSpace(payload.BackupID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": "backup_id is required"},
		})
		return
	}

	content, err := a.readBackup(payload.BackupID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "rollback_failed", "message": err.Error()},
		})
		return
	}

	if _, err := a.saveConfig(content, "rollback:"+payload.BackupID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "rollback_failed", "message": err.Error()},
		})
		return
	}
	a.writeCurrentConfigResponse(w)
}

func (a *App) handleTest(w http.ResponseWriter, r *http.Request) {
	state, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{"type": "method_not_allowed", "message": "method not allowed"},
		})
		return
	}

	var payload testRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": "invalid json body"},
		})
		return
	}

	req, body, err := buildTestInspectionRequest(payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "bad_request", "message": err.Error()},
		})
		return
	}

	result, err := state.inspector.Inspect(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"type": "inspection_failed", "message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, testResponse{
		Path:           req.Path,
		ContentType:    req.ContentType,
		RequestBody:    body,
		Decision:       result.Decision,
		Skipped:        result.Skipped,
		SkipReason:     result.SkipReason,
		FragmentsCount: result.FragmentsCount,
		DurationMS:     float64(result.Duration.Microseconds()) / 1000.0,
		MatchedRules:   result.MatchedRules,
	})
}

func summarizeConfig(state *runtimeState) configSummary {
	if state == nil || state.cfg == nil {
		return configSummary{}
	}
	return summarizeLoadedConfig(state.cfg)
}

func summarizeLoadedConfig(cfg *config.Config) configSummary {
	if cfg == nil {
		return configSummary{}
	}
	enabledRules := 0
	for _, rule := range cfg.Rules {
		if rule.Enabled {
			enabledRules++
		}
	}
	return configSummary{
		Listen:        cfg.Server.Listen,
		UpstreamBase:  cfg.Upstream.BaseURL,
		Mode:          cfg.Policy.Mode,
		InspectPaths:  append([]string(nil), cfg.Policy.InspectPaths...),
		RulesCount:    len(cfg.Rules),
		EnabledRules:  enabledRules,
		AdminEnabled:  cfg.Admin.Enabled,
		TokenRequired: strings.TrimSpace(cfg.Admin.BearerToken) != "",
	}
}

func decodeConfigUpdate(r *http.Request) (configUpdateRequest, error) {
	if isJSONRequest(r.Header.Get("Content-Type")) {
		var payload configUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return configUpdateRequest{}, errors.New("invalid json body")
		}
		if strings.TrimSpace(payload.YAML) == "" {
			return configUpdateRequest{}, errors.New("yaml is required")
		}
		return payload, nil
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return configUpdateRequest{}, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return configUpdateRequest{}, errors.New("yaml is required")
	}
	return configUpdateRequest{YAML: string(raw)}, nil
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (*runtimeState, bool) {
	state := a.state.Load()
	if state == nil || state.cfg == nil || !state.cfg.Admin.Enabled {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]string{"type": "not_found", "message": "not found"},
		})
		return nil, false
	}
	if state.cfg.Admin.BearerToken != "" && !authorized(r, state.cfg.Admin.BearerToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"type": "unauthorized", "message": "invalid admin token"},
		})
		return nil, false
	}
	return state, true
}

func (a *App) saveConfig(content []byte, reason string) (*backupInfo, error) {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	return a.saveConfigLocked(content, reason)
}

func buildTestInspectionRequest(payload testRequest) (*model.InspectionRequest, string, error) {
	path := strings.TrimSpace(payload.Path)
	if path == "" {
		path = "/v1/chat/completions"
	}
	contentType := strings.TrimSpace(payload.ContentType)
	if contentType == "" {
		contentType = "application/json"
	}

	body := strings.TrimSpace(payload.Body)
	if body == "" {
		var err error
		body, err = synthesizeTestBody(path, payload)
		if err != nil {
			return nil, "", err
		}
	}

	req := &model.InspectionRequest{
		Method:      http.MethodPost,
		Path:        path,
		ContentType: contentType,
		Body:        []byte(body),
		Headers:     make(http.Header),
		ClientIP:    "ui-test",
		RequestID:   "ui-test",
	}
	return req, body, nil
}

func synthesizeTestBody(path string, payload testRequest) (string, error) {
	text := payload.Text
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text or body is required")
	}

	role := strings.TrimSpace(payload.Role)
	scope := strings.TrimSpace(payload.Scope)
	if role == "" {
		role = scope
	}
	if role == "" {
		role = "user"
	}

	var body any
	switch path {
	case "/v1/chat/completions":
		body = map[string]any{
			"model": "ui-test",
			"messages": []map[string]any{
				{
					"role":    role,
					"content": text,
				},
			},
		}
	case "/v1/responses":
		if scope == "instructions" || role == "instructions" {
			body = map[string]any{
				"model":        "ui-test",
				"instructions": text,
			}
		} else {
			body = map[string]any{
				"model": "ui-test",
				"input": text,
			}
		}
	case "/v1/messages":
		if scope == "system" || role == "system" {
			body = map[string]any{
				"model":  "ui-test",
				"system": text,
			}
		} else {
			body = map[string]any{
				"model": "ui-test",
				"messages": []map[string]any{
					{
						"role":    role,
						"content": text,
					},
				},
			}
		}
	default:
		return "", fmt.Errorf("unsupported test path %q", path)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) writeCurrentConfigResponse(w http.ResponseWriter) {
	raw, err := os.ReadFile(a.configPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"type": "read_failed", "message": err.Error()},
		})
		return
	}
	backups, _ := a.listBackups(20)
	writeJSON(w, http.StatusOK, configResponse{
		ConfigPath: a.configPath,
		YAML:       string(raw),
		Summary:    summarizeConfig(a.state.Load()),
		Metrics:    a.metrics.Snapshot(),
		Backups:    backups,
	})
}

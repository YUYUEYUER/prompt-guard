package audit

import (
	"context"
	"log/slog"

	"github.com/YUYUEYUER/prompt-guard/internal/config"
	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

type Logger struct {
	logger       *slog.Logger
	enabled      bool
	logFullText  bool
	evidenceSize int
}

func New(logger *slog.Logger, cfg config.AuditConfig) *Logger {
	return &Logger{
		logger:       logger,
		enabled:      cfg.Enabled,
		logFullText:  cfg.LogFullText,
		evidenceSize: cfg.EvidenceMaxChars,
	}
}

func (l *Logger) LogDecision(requestID string, result *model.InspectionResult) {
	if !l.enabled || result == nil {
		return
	}

	attrs := []slog.Attr{
		slog.String("request_id", requestID),
		slog.String("decision", result.Decision),
		slog.Int("fragments_count", result.FragmentsCount),
		slog.Duration("inspection_duration", result.Duration),
		slog.String("path", result.Meta.Path),
		slog.String("model", result.Meta.Model),
		slog.String("api_key_hash", result.Meta.APIKeyHash),
		slog.String("client_ip", result.Meta.ClientIP),
	}

	if result.Skipped {
		attrs = append(attrs, slog.Bool("skipped", true), slog.String("skip_reason", result.SkipReason))
	}

	ruleIDs := make([]string, 0, len(result.MatchedRules))
	for _, match := range result.MatchedRules {
		ruleIDs = append(ruleIDs, match.RuleID)
	}
	if len(ruleIDs) > 0 {
		attrs = append(attrs, slog.Any("rule_ids", ruleIDs))
	}

	l.logger.LogAttrs(context.Background(), slog.LevelInfo, "inspection completed", attrs...)
}

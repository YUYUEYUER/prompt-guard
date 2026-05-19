package model

import (
	"net/http"
	"time"
)

const (
	DecisionAllow      = "allow"
	DecisionBlock      = "block"
	DecisionLogOnly    = "log_only"
	DecisionTagAndPass = "tag_and_pass"
	DecisionSkip       = "skip"
)

type InspectionRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
	Headers     http.Header
	ClientIP    string
	RequestID   string
}

type RequestMeta struct {
	Path       string
	Model      string
	APIKeyHash string
	ClientIP   string
}

type TextFragment struct {
	Scope      string
	Path       string
	Role       string
	Original   string
	Normalized string
}

type ExtractionResult struct {
	Fragments []TextFragment
	Meta      RequestMeta
}

type MatchResult struct {
	RuleID       string
	Action       string
	Scope        string
	Path         string
	Evidence     string
	StatusCode   int
	ResponseBody string
}

type InspectionResult struct {
	Decision       string
	MatchedRules   []MatchResult
	FragmentsCount int
	Duration       time.Duration
	Skipped        bool
	SkipReason     string
	Meta           RequestMeta
}

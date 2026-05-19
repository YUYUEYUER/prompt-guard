package extractor

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

var ErrUnknownSchema = errors.New("unknown request schema")

type Extractor interface {
	Name() string
	Match(path string, contentType string) bool
	Extract(req *model.InspectionRequest) (*model.ExtractionResult, error)
}

func DefaultExtractors() []Extractor {
	return []Extractor{
		&OpenAIChatExtractor{},
		&OpenAIResponsesExtractor{},
		&AnthropicMessagesExtractor{},
	}
}

func decodeObject(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func collectText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := collectText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := stringValue(typed["text"]); text != "" {
			return text
		}
		if text := collectText(typed["content"]); text != "" {
			return text
		}
		if text := collectText(typed["input"]); text != "" {
			return text
		}
	}
	return ""
}

func contentTypeIsJSON(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/json") || strings.Contains(ct, "application/vnd.api+json")
}

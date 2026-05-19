package extractor

import (
	"fmt"

	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

type AnthropicMessagesExtractor struct{}

func (e *AnthropicMessagesExtractor) Name() string { return "anthropic_messages" }

func (e *AnthropicMessagesExtractor) Match(path string, contentType string) bool {
	return path == "/v1/messages" && contentTypeIsJSON(contentType)
}

func (e *AnthropicMessagesExtractor) Extract(req *model.InspectionRequest) (*model.ExtractionResult, error) {
	payload, err := decodeObject(req.Body)
	if err != nil {
		return nil, err
	}

	fragments := make([]model.TextFragment, 0, 4)
	if system := collectText(payload["system"]); system != "" {
		fragments = append(fragments, model.TextFragment{
			Scope:    "system",
			Path:     "system",
			Role:     "system",
			Original: system,
		})
	}

	rawMessages, ok := payload["messages"].([]any)
	if ok {
		for i, raw := range rawMessages {
			msg, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			role := stringValue(msg["role"])
			scope := role
			if scope == "" {
				scope = "user"
			}
			text := collectText(msg["content"])
			if text == "" {
				continue
			}
			fragments = append(fragments, model.TextFragment{
				Scope:    scope,
				Path:     fmt.Sprintf("messages[%d].content", i),
				Role:     role,
				Original: text,
			})
		}
	}

	if len(fragments) == 0 {
		return nil, ErrUnknownSchema
	}

	return &model.ExtractionResult{
		Fragments: fragments,
		Meta: model.RequestMeta{
			Path:  req.Path,
			Model: stringValue(payload["model"]),
		},
	}, nil
}

package extractor

import (
	"fmt"

	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

type OpenAIChatExtractor struct{}

func (e *OpenAIChatExtractor) Name() string { return "openai_chat" }

func (e *OpenAIChatExtractor) Match(path string, contentType string) bool {
	return path == "/v1/chat/completions" && contentTypeIsJSON(contentType)
}

func (e *OpenAIChatExtractor) Extract(req *model.InspectionRequest) (*model.ExtractionResult, error) {
	payload, err := decodeObject(req.Body)
	if err != nil {
		return nil, err
	}

	rawMessages, ok := payload["messages"].([]any)
	if !ok || len(rawMessages) == 0 {
		return nil, ErrUnknownSchema
	}

	fragments := make([]model.TextFragment, 0, len(rawMessages))
	for i, raw := range rawMessages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(msg["role"])
		text := collectText(msg["content"])
		if text == "" {
			continue
		}
		scope := role
		if scope == "" {
			scope = "user"
		}
		fragments = append(fragments, model.TextFragment{
			Scope:    scope,
			Path:     fmt.Sprintf("messages[%d].content", i),
			Role:     role,
			Original: text,
		})
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

package extractor

import (
	"fmt"

	"prompt-guard/internal/model"
)

type OpenAIResponsesExtractor struct{}

func (e *OpenAIResponsesExtractor) Name() string { return "openai_responses" }

func (e *OpenAIResponsesExtractor) Match(path string, contentType string) bool {
	return path == "/v1/responses" && contentTypeIsJSON(contentType)
}

func (e *OpenAIResponsesExtractor) Extract(req *model.InspectionRequest) (*model.ExtractionResult, error) {
	payload, err := decodeObject(req.Body)
	if err != nil {
		return nil, err
	}

	fragments := make([]model.TextFragment, 0, 4)
	if instructions := collectText(payload["instructions"]); instructions != "" {
		fragments = append(fragments, model.TextFragment{
			Scope:    "instructions",
			Path:     "instructions",
			Original: instructions,
		})
	}

	switch input := payload["input"].(type) {
	case string:
		if input != "" {
			fragments = append(fragments, model.TextFragment{
				Scope:    "user",
				Path:     "input",
				Original: input,
			})
		}
	case []any:
		for i, item := range input {
			switch typed := item.(type) {
			case string:
				if typed == "" {
					continue
				}
				fragments = append(fragments, model.TextFragment{
					Scope:    "user",
					Path:     fmt.Sprintf("input[%d]", i),
					Original: typed,
				})
			case map[string]any:
				role := stringValue(typed["role"])
				scope := role
				if scope == "" {
					scope = "user"
				}
				text := collectText(typed["content"])
				if text == "" {
					text = collectText(typed["text"])
				}
				if text == "" {
					text = collectText(typed["input_text"])
				}
				if text == "" {
					continue
				}
				fragments = append(fragments, model.TextFragment{
					Scope:    scope,
					Path:     fmt.Sprintf("input[%d]", i),
					Role:     role,
					Original: text,
				})
			}
		}
	case map[string]any:
		text := collectText(input)
		if text != "" {
			fragments = append(fragments, model.TextFragment{
				Scope:    "user",
				Path:     "input",
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

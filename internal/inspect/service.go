package inspect

import (
	"context"
	"errors"
	"time"

	"prompt-guard/internal/config"
	"prompt-guard/internal/engine"
	"prompt-guard/internal/extractor"
	"prompt-guard/internal/model"
	"prompt-guard/internal/normalize"
)

type Service struct {
	cfg        *config.Config
	extractors []extractor.Extractor
	normalizer *normalize.Service
	engine     *engine.Engine
}

func New(cfg *config.Config, extractors []extractor.Extractor, normalizer *normalize.Service, engine *engine.Engine) *Service {
	return &Service{
		cfg:        cfg,
		extractors: extractors,
		normalizer: normalizer,
		engine:     engine,
	}
}

func (s *Service) Inspect(ctx context.Context, req *model.InspectionRequest) (*model.InspectionResult, error) {
	start := time.Now()

	var active extractor.Extractor
	for _, candidate := range s.extractors {
		if candidate.Match(req.Path, req.ContentType) {
			active = candidate
			break
		}
	}
	if active == nil {
		return &model.InspectionResult{
			Decision:   model.DecisionSkip,
			Skipped:    true,
			SkipReason: "no_extractor",
			Duration:   time.Since(start),
		}, nil
	}

	extraction, err := active.Extract(req)
	if err != nil {
		if errors.Is(err, extractor.ErrUnknownSchema) && s.cfg.Policy.SkipOnUnknownSchema {
			return &model.InspectionResult{
				Decision:   model.DecisionSkip,
				Skipped:    true,
				SkipReason: "unknown_schema",
				Duration:   time.Since(start),
			}, nil
		}
		return nil, err
	}

	extraction.Meta.APIKeyHash = req.Headers.Get("X-API-Key-Hash")
	extraction.Meta.ClientIP = req.ClientIP

	for i := range extraction.Fragments {
		extraction.Fragments[i].Normalized = s.normalizer.Normalize(extraction.Fragments[i].Original)
	}

	matches, err := s.engine.Evaluate(ctx, extraction.Fragments, extraction.Meta)
	if err != nil {
		return nil, err
	}

	decision := decide(matches)
	if s.cfg.Policy.Mode == "dry-run" && decision == model.DecisionBlock {
		decision = model.DecisionLogOnly
	}

	return &model.InspectionResult{
		Decision:       decision,
		MatchedRules:   matches,
		FragmentsCount: len(extraction.Fragments),
		Duration:       time.Since(start),
		Meta:           extraction.Meta,
	}, nil
}

func decide(matches []model.MatchResult) string {
	decision := model.DecisionAllow
	for _, match := range matches {
		switch match.Action {
		case model.DecisionBlock:
			return model.DecisionBlock
		case model.DecisionTagAndPass:
			decision = model.DecisionTagAndPass
		case model.DecisionLogOnly:
			if decision == model.DecisionAllow {
				decision = model.DecisionLogOnly
			}
		}
	}
	return decision
}

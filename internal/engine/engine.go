package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"prompt-guard/internal/config"
	"prompt-guard/internal/model"
)

type Engine struct {
	rules []compiledRule
}

type compiledRule struct {
	id         string
	enabled    bool
	priority   int
	endpoints  map[string]struct{}
	scopes     map[string]struct{}
	matchType  string
	words      []string
	patterns   []*regexp.Regexp
	actionType string
	statusCode int
	message    string
}

func New(cfg *config.Config, normalize func(string) string) (*Engine, error) {
	rules := make([]compiledRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		compiled := compiledRule{
			id:         rule.ID,
			enabled:    rule.Enabled,
			priority:   rule.Priority,
			endpoints:  toSet(rule.Endpoints),
			scopes:     toSet(rule.Scopes),
			matchType:  rule.Match.Type,
			actionType: rule.Action.Type,
			statusCode: rule.Action.StatusCode,
			message:    rule.Action.Message,
		}
		if !rule.Enabled {
			rules = append(rules, compiled)
			continue
		}

		switch rule.Match.Type {
		case "contains_any", "exact":
			for _, word := range rule.Match.Words {
				compiled.words = append(compiled.words, normalize(word))
			}
		case "regex":
			for _, pattern := range rule.Match.Patterns {
				re, err := regexp.Compile(pattern)
				if err != nil {
					return nil, err
				}
				compiled.patterns = append(compiled.patterns, re)
			}
		}

		rules = append(rules, compiled)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].priority > rules[j].priority
	})

	return &Engine{rules: rules}, nil
}

func (e *Engine) Evaluate(_ context.Context, fragments []model.TextFragment, meta model.RequestMeta) ([]model.MatchResult, error) {
	results := make([]model.MatchResult, 0)
	for _, rule := range e.rules {
		if !rule.enabled {
			continue
		}
		if _, ok := rule.endpoints[meta.Path]; !ok {
			continue
		}
		for _, fragment := range fragments {
			if !scopeMatches(rule.scopes, fragment.Scope) {
				continue
			}
			if evidence, matched := rule.matches(fragment); matched {
				results = append(results, model.MatchResult{
					RuleID:       rule.id,
					Action:       rule.actionType,
					Scope:        fragment.Scope,
					Path:         fragment.Path,
					Evidence:     evidence,
					StatusCode:   rule.statusCode,
					ResponseBody: rule.message,
				})
			}
		}
	}
	return results, nil
}

func (r compiledRule) matches(fragment model.TextFragment) (string, bool) {
	switch r.matchType {
	case "contains_any":
		for _, word := range r.words {
			if strings.Contains(fragment.Normalized, word) {
				return word, true
			}
		}
	case "exact":
		for _, word := range r.words {
			if fragment.Normalized == word {
				return word, true
			}
		}
	case "regex":
		for _, pattern := range r.patterns {
			if pattern.MatchString(fragment.Original) || pattern.MatchString(fragment.Normalized) {
				return pattern.String(), true
			}
		}
	}
	return "", false
}

func toSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

func scopeMatches(scopes map[string]struct{}, scope string) bool {
	if _, ok := scopes["all"]; ok {
		return true
	}
	_, ok := scopes[scope]
	return ok
}

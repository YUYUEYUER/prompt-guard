package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/YUYUEYUER/prompt-guard/internal/config"
	"github.com/YUYUEYUER/prompt-guard/internal/model"
)

type Engine struct {
	rules []compiledRule
}

type compiledRule struct {
	id                  string
	enabled             bool
	priority            int
	endpoints           map[string]struct{}
	scopes              map[string]struct{}
	matchType           string
	words               []string
	patterns            []*regexp.Regexp
	maxEditDistance     int
	actionType          string
	statusCode          int
	message             string
	responseMode        string
	responseContentType string
}

func New(cfg *config.Config, normalize func(string) string) (*Engine, error) {
	rules := make([]compiledRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		compiled := compiledRule{
			id:                  rule.ID,
			enabled:             rule.Enabled,
			priority:            rule.Priority,
			endpoints:           toSet(rule.Endpoints),
			scopes:              toSet(rule.Scopes),
			matchType:           rule.Match.Type,
			maxEditDistance:     rule.Match.MaxEditDistance,
			actionType:          rule.Action.Type,
			statusCode:          rule.Action.StatusCode,
			message:             rule.Action.Message,
			responseMode:        rule.Action.ResponseMode,
			responseContentType: rule.Action.ResponseContentType,
		}
		if !rule.Enabled {
			rules = append(rules, compiled)
			continue
		}

		switch rule.Match.Type {
		case "contains_any", "exact", "fuzzy_contains_any":
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
					RuleID:              rule.id,
					Action:              rule.actionType,
					Scope:               fragment.Scope,
					Path:                fragment.Path,
					Evidence:            evidence,
					StatusCode:          rule.statusCode,
					ResponseBody:        rule.message,
					ResponseMode:        rule.responseMode,
					ResponseContentType: rule.responseContentType,
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
	case "fuzzy_contains_any":
		for _, word := range r.words {
			if evidence, matched := fuzzyContains(fragment.Normalized, word, r.maxEditDistance); matched {
				return evidence, true
			}
		}
	}
	return "", false
}

func fuzzyContains(text string, pattern string, maxEditDistance int) (string, bool) {
	if maxEditDistance < 0 || pattern == "" || text == "" {
		return "", false
	}
	if strings.Contains(text, pattern) {
		return pattern, true
	}

	textRunes := []rune(text)
	patternRunes := []rune(pattern)
	minWindow := len(patternRunes) - maxEditDistance
	if minWindow < 1 {
		minWindow = 1
	}
	maxWindow := len(patternRunes) + maxEditDistance
	if maxWindow > len(textRunes) {
		maxWindow = len(textRunes)
	}
	if minWindow > maxWindow {
		return "", false
	}

	bestDistance := maxEditDistance + 1
	bestEvidence := ""
	for windowLen := minWindow; windowLen <= maxWindow; windowLen++ {
		for start := 0; start+windowLen <= len(textRunes); start++ {
			candidate := textRunes[start : start+windowLen]
			distance, ok := levenshteinDistanceWithin(candidate, patternRunes, maxEditDistance)
			if !ok || distance > maxEditDistance {
				continue
			}
			if distance < bestDistance {
				bestDistance = distance
				bestEvidence = string(candidate)
				if distance == 0 {
					return bestEvidence, true
				}
			}
		}
	}

	if bestEvidence == "" {
		return "", false
	}
	return bestEvidence, true
}

func levenshteinDistanceWithin(a []rune, b []rune, maxEditDistance int) (int, bool) {
	if abs(len(a)-len(b)) > maxEditDistance {
		return 0, false
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > maxEditDistance {
			return 0, false
		}
		prev, curr = curr, prev
	}

	if prev[len(b)] > maxEditDistance {
		return 0, false
	}
	return prev[len(b)], true
}

func min3(a int, b int, c int) int {
	if a > b {
		a = b
	}
	if a > c {
		return c
	}
	return a
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
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

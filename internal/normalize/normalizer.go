package normalize

import (
	"strings"
	"unicode"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Normalize(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))

	for _, r := range strings.ToLower(text) {
		if isZeroWidthRune(r) {
			continue
		}
		if unicode.IsSpace(r) || r == '\u3000' {
			continue
		}
		builder.WriteRune(r)
	}

	return builder.String()
}

func isZeroWidthRune(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return true
	default:
		return false
	}
}

package config

import (
	"slices"
)

func (c *Config) RequestBodyLimitBytes() (int64, error) {
	return ParseByteSize(c.Policy.RequestBodyLimit)
}

func (c *Config) ShouldInspectPath(path string) bool {
	return slices.Contains(c.Policy.InspectPaths, path)
}

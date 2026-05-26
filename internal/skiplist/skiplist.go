package skiplist

import (
	"path/filepath"
	"strings"

	"github.com/UberMorgott/morgue/internal/config"
)

// SkipList determines whether files should be skipped during processing.
type SkipList struct {
	enabled        bool
	categories     map[string]bool // category -> enabled
	patterns       map[string][]string
	customSkip     map[string]bool
	customInclude  map[string]bool
}

// New creates a SkipList from the given config.
func New(cfg config.Config) *SkipList {
	sl := &SkipList{
		enabled:       cfg.SkipSystemLibs,
		categories:    make(map[string]bool),
		patterns:      builtinPatterns,
		customSkip:    make(map[string]bool),
		customInclude: make(map[string]bool),
	}

	// All categories enabled by default
	for cat := range builtinPatterns {
		sl.categories[cat] = true
	}

	// Apply per-category overrides
	for cat, enabled := range cfg.SkipCategories {
		sl.categories[cat] = enabled
	}

	for _, p := range cfg.CustomSkip {
		sl.customSkip[strings.ToLower(p)] = true
	}
	for _, p := range cfg.CustomInclude {
		sl.customInclude[strings.ToLower(p)] = true
	}

	return sl
}

// ShouldSkip returns true if the given filename should be skipped, along with the category.
func (sl *SkipList) ShouldSkip(filename string) (bool, string) {
	lower := strings.ToLower(filename)

	// Force-include overrides everything
	if sl.customInclude[lower] {
		return false, ""
	}

	// Custom skip patterns
	if sl.customSkip[lower] {
		return true, "custom"
	}

	// Master toggle
	if !sl.enabled {
		return false, ""
	}

	// Check built-in patterns by category
	for cat, patterns := range sl.patterns {
		if !sl.categories[cat] {
			continue
		}
		for _, pattern := range patterns {
			matched, _ := filepath.Match(strings.ToLower(pattern), lower)
			if matched {
				return true, cat
			}
		}
	}

	return false, ""
}

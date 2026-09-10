package releasenotes

import (
	"fmt"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// RequiredCategories is the set of section titles .github/release.yml must
// define, mirroring docs/changelog-policy.md. GitHub renders one section per
// category, populated by the PR labels listed for it. "Other Changes" is the
// catch-all and must carry the "*" label so no merged PR is dropped from the
// generated notes.
var RequiredCategories = []string{
	"Security",
	"Added",
	"Fixed",
	"Breaking Changes",
	"Deprecated",
	"Documentation",
	"Dependencies",
	"Other Changes",
}

// catchAllCategory is the title that must own the "*" label.
const catchAllCategory = "Other Changes"

type releaseConfig struct {
	Changelog struct {
		Categories []struct {
			Title  string   `yaml:"title"`
			Labels []string `yaml:"labels"`
		} `yaml:"categories"`
	} `yaml:"changelog"`
}

// ValidateReleaseConfig parses the bytes of a .github/release.yml and returns a
// non-nil error if it does not define every RequiredCategories title, or if the
// "Other Changes" catch-all does not list the "*" label. Keeping this in code
// means a category cannot be dropped from the config without a red build
// (TestReleaseConfigHasRequiredCategories).
func ValidateReleaseConfig(data []byte) error {
	var cfg releaseConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse release config: %w", err)
	}

	got := make(map[string][]string, len(cfg.Changelog.Categories))
	for _, c := range cfg.Changelog.Categories {
		got[strings.TrimSpace(c.Title)] = c.Labels
	}

	var missing []string
	for _, want := range RequiredCategories {
		if _, ok := got[want]; !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(".github/release.yml is missing required categories: %s", strings.Join(missing, ", "))
	}

	if !contains(got[catchAllCategory], "*") {
		return fmt.Errorf(".github/release.yml: the %q category must list the \"*\" label so no PR is dropped", catchAllCategory)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

package instill

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const categoriesFileName = ".categories.json"

// LoadCategories reads the category registry from the library root.
func LoadCategories(libraryPath string) map[string][]string {
	return LoadCategoriesWithWarnings(libraryPath, os.Stderr)
}

// LoadCategoriesWithWarnings reads the category registry and writes fallback warnings to stderr.
func LoadCategoriesWithWarnings(libraryPath string, stderr io.Writer) map[string][]string {
	categories, err := loadCategories(libraryPath)
	if err != nil {
		if stderr != nil {
			_, _ = fmt.Fprintln(stderr, "warning: "+err.Error())
		}
		return map[string][]string{}
	}
	return categories
}

func loadCategories(libraryPath string) (map[string][]string, error) {
	contents, err := os.ReadFile(filepath.Join(libraryPath, categoriesFileName)) //nolint:gosec // The configured skill library path is the intended trust boundary.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]string{}, fmt.Errorf("category registry not found: %s", categoriesFileName)
		}
		return map[string][]string{}, fmt.Errorf("cannot read category registry: %v", err)
	}

	var raw map[string][]string
	if err := json.Unmarshal(contents, &raw); err != nil {
		return map[string][]string{}, fmt.Errorf("cannot parse category registry: %v", err)
	}
	if raw == nil {
		return map[string][]string{}, fmt.Errorf("cannot parse category registry: expected object")
	}

	categories := make(map[string][]string, len(raw))
	rawCategories := make([]string, 0, len(raw))
	for category := range raw {
		rawCategories = append(rawCategories, category)
	}
	sort.Strings(rawCategories)
	for _, rawCategory := range rawCategories {
		skills := raw[rawCategory]
		if skills == nil {
			return map[string][]string{}, fmt.Errorf("cannot parse category registry: category %q must be an array", rawCategory)
		}
		category := strings.TrimSpace(rawCategory)
		if category == "" {
			continue
		}
		if _, exists := categories[category]; exists {
			return map[string][]string{}, fmt.Errorf("cannot parse category registry: duplicate category %q", category)
		}
		categories[category] = normalizeSkills(skills)
	}
	return categories, nil
}

// CategoryForSkill returns the category path assigned to skillName.
func CategoryForSkill(categories map[string][]string, skillName string) string {
	matches := make([]string, 0, 1)
	for category, skills := range categories {
		for _, skill := range skills {
			if skill == skillName {
				matches = append(matches, category)
				break
			}
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

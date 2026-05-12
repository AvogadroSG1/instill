package instill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillExists reports whether name resolves to a library skill with SKILL.md.
func SkillExists(libraryPath string, name string) (bool, error) {
	if !IsValidSkillName(name) {
		return false, nil
	}

	info, err := os.Stat(filepath.Join(libraryPath, name, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewExitError(ExitEnvironment, "error: cannot read skill: "+err.Error())
	}
	return !info.IsDir(), nil
}

// ListLibrarySkills returns all valid library skill names sorted alphabetically.
func ListLibrarySkills(libraryPath string) ([]string, error) {
	entries, err := os.ReadDir(libraryPath)
	if err != nil {
		return nil, NewExitError(ExitEnvironment, "error: cannot read library: "+err.Error())
	}

	skills := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		exists, err := SkillExists(libraryPath, name)
		if err != nil {
			return nil, err
		}
		if exists {
			skills = append(skills, name)
		}
	}
	sort.Strings(skills)
	return skills, nil
}

// FilterSkills returns skills whose names contain filter, case-insensitively.
func FilterSkills(skills []string, filter string) []string {
	if filter == "" {
		return append([]string{}, skills...)
	}

	filter = strings.ToLower(filter)
	filtered := make([]string, 0, len(skills))
	for _, skill := range skills {
		if strings.Contains(strings.ToLower(skill), filter) {
			filtered = append(filtered, skill)
		}
	}
	return filtered
}

package instill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillSourcePath returns the absolute directory path for a skill in the library.
// For flat skills ("docker") it returns libraryPath/docker.
// For qualified group skills ("superpowers/brainstorming") it returns
// libraryPath/superpowers/brainstorming.
func SkillSourcePath(libraryPath string, name string) (string, error) {
	if !IsValidSkillName(name) {
		return "", NewExitError(ExitGeneral, "error: invalid skill name: "+name)
	}
	return filepath.Join(libraryPath, filepath.FromSlash(name)), nil
}

// SkillExists reports whether name resolves to a library skill with SKILL.md.
func SkillExists(libraryPath string, name string) (bool, error) {
	source, err := SkillSourcePath(libraryPath, name)
	if err != nil {
		return false, nil //nolint:nilerr // invalid name is not an error, just absent
	}

	info, err := os.Stat(filepath.Join(source, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewExitError(ExitEnvironment, "error: cannot read skill: "+err.Error())
	}
	return !info.IsDir(), nil
}

// ListLibrarySkills returns all valid library skill names sorted alphabetically.
// It discovers two layouts:
//   - Flat:  library/<name>/SKILL.md  → skill name "<name>"
//   - Group: library/<group>/<name>/SKILL.md  → skill name "<group>/<name>"
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

		// Flat skill: library/<name>/SKILL.md
		exists, err := SkillExists(libraryPath, name)
		if err != nil {
			return nil, err
		}
		if exists {
			skills = append(skills, name)
			continue
		}

		// Group directory: scan one level deeper for child skills
		children, readErr := os.ReadDir(filepath.Join(libraryPath, name))
		if readErr != nil {
			continue // unreadable directory — skip silently
		}
		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			qualified := name + "/" + child.Name()
			childExists, err := SkillExists(libraryPath, qualified)
			if err != nil {
				return nil, err
			}
			if childExists {
				skills = append(skills, qualified)
			}
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

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

// maxSkillDepth bounds recursion into the library so a symlinked directory
// cycle cannot loop forever (os.ReadDir/os.Stat follow symlinks).
const maxSkillDepth = 32

// ListLibrarySkills returns all valid library skill names sorted alphabetically.
// It walks the library to arbitrary depth: any directory containing SKILL.md is
// a leaf skill whose slash-joined relative path is its name (e.g. "docker",
// "superpowers/brainstorming", "cloud/azure/azure-cli"). A directory without
// SKILL.md is treated as a category node and recursed into; recursion stops at
// each leaf so a skill's internal directories are never scanned.
func ListLibrarySkills(libraryPath string) ([]string, error) {
	skills := make([]string, 0)
	if err := walkLibrarySkills(libraryPath, "", &skills, 0); err != nil {
		return nil, err
	}
	sort.Strings(skills)
	return skills, nil
}

func walkLibrarySkills(libraryPath, rel string, out *[]string, depth int) error {
	if depth > maxSkillDepth {
		return nil
	}

	dir := filepath.Join(libraryPath, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if rel == "" {
			return NewExitError(ExitEnvironment, "error: cannot read library: "+err.Error())
		}
		return nil // unreadable subdirectory — skip silently
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		childRel := entry.Name()
		if rel != "" {
			childRel = rel + "/" + entry.Name()
		}

		// A directory with SKILL.md is a leaf skill; do not recurse into it.
		if hasSkillMarker(filepath.Join(libraryPath, filepath.FromSlash(childRel))) {
			if IsValidSkillName(childRel) {
				*out = append(*out, childRel)
			}
			continue
		}

		if err := walkLibrarySkills(libraryPath, childRel, out, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// hasSkillMarker reports whether dir contains a SKILL.md file.
func hasSkillMarker(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

// skillLinkName returns the symlink filename used in the project's skills dir.
// Group skills ("superpowers/brainstorming") become colon-separated
// ("superpowers:brainstorming") so the skills dir stays flat. Flat skills are
// returned unchanged.
func skillLinkName(name string) string {
	return strings.ReplaceAll(name, "/", ":")
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

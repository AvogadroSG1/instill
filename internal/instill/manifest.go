package instill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Manifest struct {
	Skills []string `json:"skills"`
}

// ReadManifest reads, validates, normalizes, and returns a project manifest.
func ReadManifest(path string) (Manifest, error) {
	//nolint:gosec // Manifest path is discovered under the selected project root.
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read manifest: %v", err))
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
	}
	if err := ValidateSkillNames(manifest.Skills); err != nil {
		return Manifest{}, err
	}

	manifest.Skills = normalizeSkills(manifest.Skills)
	return manifest, nil
}

// WriteManifestAtomic writes a normalized manifest using a unique temp file and rename.
func WriteManifestAtomic(path string, manifest Manifest) error {
	manifest.Skills = normalizeSkills(manifest.Skills)
	if err := ValidateSkillNames(manifest.Skills); err != nil {
		return err
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", err))
	}
	data = append(data, '\n')

	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write manifest: %v", err))
	}

	return nil
}

// ValidateSkillNames rejects manifest entries that are not single skill identifiers.
func ValidateSkillNames(skills []string) error {
	for _, skill := range skills {
		if !IsValidSkillName(skill) {
			return NewExitError(ExitGeneral, "error: malformed manifest: invalid skill name: "+skill)
		}
	}
	return nil
}

// IsValidSkillName reports whether skill is a safe path element or a
// qualified name with exactly one slash (e.g. "superpowers/brainstorming").
// Each segment must be a non-empty, non-traversal, single path component.
func IsValidSkillName(skill string) bool {
	if filepath.IsAbs(skill) {
		return false
	}
	parts := strings.SplitN(skill, "/", 3)
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		if strings.Contains(part, "\\") {
			return false
		}
		if filepath.Clean(part) != part {
			return false
		}
	}
	return true
}

func normalizeSkills(skills []string) []string {
	seen := make(map[string]struct{}, len(skills))
	normalized := make([]string, 0, len(skills))
	for _, skill := range skills {
		if skill == "" {
			continue
		}
		if _, ok := seen[skill]; ok {
			continue
		}
		seen[skill] = struct{}{}
		normalized = append(normalized, skill)
	}
	sort.Strings(normalized)
	return normalized
}

// HasManifest reports whether root contains an instill manifest.
func HasManifest(root string) bool {
	_, err := os.Stat(filepath.Join(root, claudeDirName, manifestFileName))
	return err == nil
}

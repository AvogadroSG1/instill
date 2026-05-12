package instill

import (
	"fmt"
	"io"
)

// PickSkillsOptions configures manifest skill selection changes.
type PickSkillsOptions struct {
	Project     Project
	LibraryPath string
	Add         []string
	Remove      []string
	Stdout      io.Writer
}

// SkillSelectionOptions configures full manifest selection changes.
type SkillSelectionOptions struct {
	Project     Project
	LibraryPath string
	Skills      []string
	Stdout      io.Writer
}

// PickSkills applies additive and removal changes to a project manifest.
func PickSkills(opts PickSkillsOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	if len(opts.Add) == 0 && len(opts.Remove) == 0 {
		return NewExitError(ExitGeneral, "error: no skills specified")
	}

	addSkills := normalizeSkills(opts.Add)
	removeSkills := normalizeSkills(opts.Remove)
	if err := ValidateSkillNames(addSkills); err != nil {
		return err
	}
	if err := ValidateSkillNames(removeSkills); err != nil {
		return err
	}
	if err := validateKnownSkills(opts.LibraryPath, append(addSkills, removeSkills...)); err != nil {
		return err
	}

	manifest, err := ReadManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}

	current := make(map[string]struct{}, len(manifest.Skills)+len(addSkills))
	for _, skill := range manifest.Skills {
		current[skill] = struct{}{}
	}

	added := []string{}
	for _, skill := range addSkills {
		if _, ok := current[skill]; ok {
			continue
		}
		current[skill] = struct{}{}
		added = append(added, skill)
	}

	removed := []string{}
	for _, skill := range removeSkills {
		if _, ok := current[skill]; !ok {
			continue
		}
		delete(current, skill)
		removed = append(removed, skill)
	}

	next := make([]string, 0, len(current))
	for skill := range current {
		next = append(next, skill)
	}
	next = normalizeSkills(next)
	updated := Manifest{Skills: next}
	if err := WriteManifestAtomic(opts.Project.ManifestPath, updated); err != nil {
		return err
	}
	for _, skill := range added {
		if err := writeLine(stdout, "added:   "+skill); err != nil {
			return err
		}
	}
	for _, skill := range removed {
		if err := writeLine(stdout, "removed: "+skill); err != nil {
			return err
		}
	}
	if err := ReconcileManifest(opts.Project, updated, opts.LibraryPath, stdout); err != nil {
		return err
	}
	return writeLine(stdout, fmt.Sprintf("manifest: %d skills", len(updated.Skills)))
}

// ApplySkillSelection replaces the manifest with the selected skill set and reconciles symlinks.
func ApplySkillSelection(opts SkillSelectionOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	selectedSkills := normalizeSkills(opts.Skills)
	if err := ValidateSkillNames(selectedSkills); err != nil {
		return err
	}
	if err := validateKnownSkills(opts.LibraryPath, selectedSkills); err != nil {
		return err
	}

	manifest, err := ReadManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}

	current := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		current[skill] = struct{}{}
	}
	next := make(map[string]struct{}, len(selectedSkills))
	for _, skill := range selectedSkills {
		next[skill] = struct{}{}
	}

	added := []string{}
	for _, skill := range selectedSkills {
		if _, ok := current[skill]; !ok {
			added = append(added, skill)
		}
	}

	removed := []string{}
	for _, skill := range manifest.Skills {
		if _, ok := next[skill]; !ok {
			removed = append(removed, skill)
		}
	}

	updated := Manifest{Skills: selectedSkills}
	if err := WriteManifestAtomic(opts.Project.ManifestPath, updated); err != nil {
		return err
	}
	for _, skill := range added {
		if err := writeLine(stdout, "added:   "+skill); err != nil {
			return err
		}
	}
	for _, skill := range removed {
		if err := writeLine(stdout, "removed: "+skill); err != nil {
			return err
		}
	}
	if err := ReconcileManifest(opts.Project, updated, opts.LibraryPath, stdout); err != nil {
		return err
	}
	return writeLine(stdout, fmt.Sprintf("manifest: %d skills", len(updated.Skills)))
}

func validateKnownSkills(libraryPath string, skills []string) error {
	for _, skill := range normalizeSkills(skills) {
		exists, err := SkillExists(libraryPath, skill)
		if err != nil {
			return err
		}
		if !exists {
			return NewExitError(ExitGeneral, "error: unknown skill: "+skill+" - run 'instill show-library' to see available skills")
		}
	}
	return nil
}

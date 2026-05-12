package instill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Reconcile reads the project manifest and reconciles symlinks to match it.
func Reconcile(project Project, libraryPath string, stdout io.Writer) error {
	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		return err
	}
	return ReconcileManifest(project, manifest, libraryPath, stdout)
}

// ReconcileManifest reconciles symlinks to match a previously validated manifest.
func ReconcileManifest(project Project, manifest Manifest, libraryPath string, stdout io.Writer) error {
	if err := os.MkdirAll(project.SymlinkDir, 0o755); err != nil { //nolint:gosec // Project symlink directory must be user-accessible in the repository.
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot create symlink directory: %v", err))
	}

	changed := false
	selected := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		selected[skill] = struct{}{}
	}

	existing, err := listExistingSymlinks(project.SymlinkDir)
	if err != nil {
		return err
	}

	for _, name := range existing {
		if _, ok := selected[name]; ok {
			continue
		}
		if err := removeSymlink(filepath.Join(project.SymlinkDir, name)); err != nil {
			return err
		}
		changed = true
	}

	finalSkills := make([]string, 0, len(manifest.Skills))
	for _, name := range manifest.Skills {
		exists, err := SkillExists(libraryPath, name)
		if err != nil {
			return err
		}
		if exists {
			finalSkills = append(finalSkills, name)
			continue
		}

		target := filepath.Join(project.SymlinkDir, name)
		if _, err := os.Lstat(target); err == nil {
			if err := removeSymlink(target); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(stdout, "removed: %s (no longer in library)\n", name); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		changed = true
	}

	for _, name := range finalSkills {
		target := filepath.Join(project.SymlinkDir, name)
		source := filepath.Join(libraryPath, name)
		if linkPointsTo(target, source) {
			continue
		}

		if _, err := os.Lstat(target); err == nil {
			if err := removeSymlink(target); err != nil {
				return err
			}
		}
		if err := os.Symlink(source, target); err != nil {
			return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot create symlink: %v", err))
		}
		if _, err := fmt.Fprintf(stdout, "created: %s -> %s\n", name, source); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		changed = true
	}

	normalized := normalizeSkills(finalSkills)
	if !sameStrings(manifest.Skills, normalized) {
		if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: normalized}); err != nil {
			return err
		}
		changed = true
	}

	if changed {
		if _, err := fmt.Fprintf(stdout, "ok: %d skills linked\n", len(normalized)); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
	}

	return nil
}

func listExistingSymlinks(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read symlink directory: %v", err))
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read symlink: %v", err))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			names = append(names, entry.Name())
		}
	}

	return names, nil
}

func removeSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot remove symlink: %v", err))
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return NewExitError(ExitFilesystem, "error: cannot remove non-symlink: "+path)
	}
	if err := os.Remove(path); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot remove symlink: %v", err))
	}
	return nil
}

func linkPointsTo(linkPath string, source string) bool {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	return target == source
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

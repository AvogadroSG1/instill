package instill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	return ReconcileManifestWithPrevious(project, manifest, manifest, libraryPath, stdout)
}

// ReconcileManifestWithPrevious reconciles symlinks and permissions.
// The previous manifest is the ownership boundary for permissions that can be revoked.
func ReconcileManifestWithPrevious(
	project Project,
	previousManifest Manifest,
	manifest Manifest,
	libraryPath string,
	stdout io.Writer,
) error {
	changed := false
	previousSkills := append([]string(nil), previousManifest.Skills...)
	selected := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		selected[skill] = struct{}{}
	}

	if err := ensureReconcileDirs(project); err != nil {
		return err
	}

	existing, err := listExistingSymlinks(project.SymlinkDir)
	if err != nil {
		return err
	}

	for _, name := range existing {
		if _, ok := selected[name]; ok {
			continue
		}
		if err := removeSymlink(filepath.Join(project.SymlinkDir, filepath.FromSlash(name)), project.SymlinkDir); err != nil {
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

		target := filepath.Join(project.SymlinkDir, filepath.FromSlash(name))
		if _, err := os.Lstat(target); err == nil {
			if err := removeSymlink(target, project.SymlinkDir); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(stdout, "removed: %s (no longer in library)\n", name); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		changed = true
	}

	for _, name := range finalSkills {
		target := filepath.Join(project.SymlinkDir, filepath.FromSlash(name))
		source, err := SkillSourcePath(libraryPath, name)
		if err != nil {
			return err
		}
		if linkPointsTo(target, source) {
			continue
		}

		// For qualified names (one slash), ensure the parent directory exists.
		if parent := filepath.Dir(target); parent != project.SymlinkDir {
			if err := ensureRealDirectory(parent, "skill group directory"); err != nil {
				return err
			}
		}

		if _, err := os.Lstat(target); err == nil {
			if err := removeSymlink(target, project.SymlinkDir); err != nil {
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
	if !slices.Equal(manifest.Skills, normalized) {
		if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: normalized}); err != nil {
			return err
		}
		changed = true
	}

	settingsLocalPath := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	if _, err := reconcileSettingsLocalPermissions(settingsLocalPath, previousSkills, finalSkills); err != nil {
		return err
	}

	if changed {
		if _, err := fmt.Fprintf(stdout, "ok: %d skills linked\n", len(normalized)); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
	}

	return nil
}

func ensureReconcileDirs(project Project) error {
	if err := ensureRealDirectory(filepath.Join(project.Root, claudeDirName), ".claude directory"); err != nil {
		return err
	}
	if err := ensureRealDirectory(project.SymlinkDir, "symlink directory"); err != nil {
		return err
	}
	return nil
}

func validateReconcile(project Project, previousManifest, manifest Manifest) error {
	if err := ensureReconcileDirs(project); err != nil {
		return err
	}

	settingsLocalPath := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	return validateSettingsLocalPermissions(settingsLocalPath, previousManifest.Skills, manifest.Skills)
}

func ensureRealDirectory(path, label string) error {
	//nolint:gosec // Project metadata directories must be user-accessible in the repository.
	if err := os.MkdirAll(path, 0o755); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot create %s: %v", label, err))
	}

	info, err := os.Lstat(path)
	if err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot inspect %s: %v", label, err))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: refusing to write through symlinked %s", label))
	}
	if !info.IsDir() {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: %s is not a directory", label))
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
		entryPath := filepath.Join(path, entry.Name())
		info, err := os.Lstat(entryPath)
		if err != nil {
			return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read symlink: %v", err))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			names = append(names, entry.Name())
			continue
		}
		if !info.IsDir() {
			continue
		}
		// Real subdirectory — scan one level for nested symlinks (group dir pattern).
		children, err := os.ReadDir(entryPath)
		if err != nil {
			return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read skill group directory: %v", err))
		}
		for _, child := range children {
			childPath := filepath.Join(entryPath, child.Name())
			childInfo, err := os.Lstat(childPath)
			if err != nil {
				return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read nested symlink: %v", err))
			}
			if childInfo.Mode()&os.ModeSymlink != 0 {
				names = append(names, entry.Name()+"/"+child.Name())
			}
		}
	}

	return names, nil
}

// removeSymlink removes the symlink at path. If guardDir is non-empty, it
// best-effort removes the parent directory when it becomes empty — but only
// if the parent is not guardDir itself (prevents removing the skills root).
func removeSymlink(path string, guardDir string) error {
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
	if parent := filepath.Dir(path); guardDir != "" && parent != guardDir {
		// Best-effort prune: remove group dir when the last child symlink is gone.
		_ = os.Remove(parent)
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

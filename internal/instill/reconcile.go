package instill

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Reconcile reads the legacy project manifest and reconciles symlinks to match it.
// Deprecated: supported commands must use the APM-backed sync path instead.
func Reconcile(project Project, libraryPath string, stdout io.Writer) error {
	return withRootLocks(context.Background(), []string{libraryPath, project.Root}, func(ctx context.Context, held *heldLocks) error {
		return reconcileAuthoritativeLocked(ctx, held, project, nil, libraryPath, stdout)
	})
}

// ReconcileManifest uses manifest as the previous ownership boundary and
// reconciles to the authoritative legacy manifest reread under the Project lock.
// Deprecated: supported commands must use the APM-backed sync path instead.
func ReconcileManifest(project Project, previous Manifest, libraryPath string, stdout io.Writer) error {
	return withRootLocks(context.Background(), []string{libraryPath, project.Root}, func(ctx context.Context, held *heldLocks) error {
		return reconcileAuthoritativeLocked(ctx, held, project, &previous, libraryPath, stdout)
	})
}

// ReconcileManifestWithPrevious reconciles legacy symlinks and permissions.
// Deprecated: supported commands must use the APM-backed sync path instead.
// The previous manifest is the ownership boundary for permissions that can be revoked.
// The planned final value is retained for API compatibility; the authoritative
// final manifest reread under lock supersedes a stale caller plan.
// A permission-only settings.local.json change counts as a change and prints the ok line;
// a formatting-only normalization (bytes differ but the Skill permission set does not) stays silent.
func ReconcileManifestWithPrevious(
	project Project,
	previous Manifest,
	plannedFinal Manifest,
	libraryPath string,
	stdout io.Writer,
) error {
	_ = plannedFinal
	return withRootLocks(context.Background(), []string{libraryPath, project.Root}, func(ctx context.Context, held *heldLocks) error {
		return reconcileAuthoritativeLocked(ctx, held, project, &previous, libraryPath, stdout)
	})
}

func reconcileAuthoritativeLocked(
	ctx context.Context,
	held *heldLocks,
	project Project,
	previous *Manifest,
	libraryPath string,
	stdout io.Writer,
) error {
	emitMutationTestEvent("dependent-read:reconcile-manifest")
	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		return err
	}
	ownership := manifest
	if previous != nil {
		ownership = *previous
	}
	return reconcileManifestWithPreviousLocked(ctx, held, project, ownership, manifest, libraryPath, stdout)
}

func reconcileManifestWithPreviousLocked(
	ctx context.Context,
	held *heldLocks,
	project Project,
	previousManifest Manifest,
	manifest Manifest,
	libraryPath string,
	stdout io.Writer,
) error {
	if err := held.requireContext(ctx, libraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, project.Root); err != nil {
		return err
	}
	changed := false
	previousSkills := append([]string(nil), previousManifest.Skills...)

	if err := ensureReconcileDirs(project); err != nil {
		return err
	}

	// Determine which manifest skills still exist in the library.
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
		if _, err := fmt.Fprintf(stdout, "removed: %s (no longer in library)\n", name); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		changed = true
	}

	// selected is the set of link names that should exist after reconcile.
	selected := make(map[string]struct{}, len(finalSkills))
	for _, skill := range finalSkills {
		selected[skillLinkName(skill)] = struct{}{}
	}

	// Reconcile .claude/skills (primary) with full output, then .agents/skills silently.
	claudeChanged, err := reconcileOneSymlinkDir(project.SymlinkDir, selected, finalSkills, libraryPath, stdout)
	if err != nil {
		return err
	}
	changed = changed || claudeChanged

	agentsChanged, err := reconcileOneSymlinkDir(project.AgentsSymlinkDir, selected, finalSkills, libraryPath, io.Discard)
	if err != nil {
		return err
	}
	changed = changed || agentsChanged

	normalized := normalizeSkills(finalSkills)
	if !slices.Equal(manifest.Skills, normalized) {
		if err := writeManifestAtomicLocked(ctx, held, project.Root, project.ManifestPath, Manifest{Skills: normalized}); err != nil {
			return err
		}
		changed = true
	}

	settingsLocalPath := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	settingsChanged, err := reconcileSettingsLocalPermissionsLocked(ctx, held, project.Root, settingsLocalPath, previousSkills, finalSkills)
	if err != nil {
		return err
	}
	changed = changed || settingsChanged

	if changed {
		if _, err := fmt.Fprintf(stdout, "ok: %d skills linked\n", len(normalized)); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
	}

	return nil
}

// reconcileOneSymlinkDir removes orphan symlinks and creates/updates symlinks for
// finalSkills in dir. selected is the set of link names that should remain.
func reconcileOneSymlinkDir(
	dir string,
	selected map[string]struct{},
	finalSkills []string,
	libraryPath string,
	stdout io.Writer,
) (bool, error) {
	changed := false

	existing, err := listExistingSymlinks(dir)
	if err != nil {
		return false, err
	}

	for _, name := range existing {
		if _, ok := selected[name]; ok {
			continue
		}
		if err := removeSymlink(filepath.Join(dir, filepath.FromSlash(name)), dir); err != nil {
			return false, err
		}
		changed = true
	}

	for _, name := range finalSkills {
		target := filepath.Join(dir, skillLinkName(name))
		source, err := SkillSourcePath(libraryPath, name)
		if err != nil {
			return false, err
		}
		if linkPointsTo(target, source) {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			if err := removeSymlink(target, dir); err != nil {
				return false, err
			}
		}
		if err := os.Symlink(source, target); err != nil {
			return false, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot create symlink: %v", err))
		}
		if _, err := fmt.Fprintf(stdout, "created: %s -> %s\n", name, source); err != nil {
			return false, NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		changed = true
	}

	return changed, nil
}

func ensureReconcileDirs(project Project) error {
	if err := ensureRealDirectory(filepath.Join(project.Root, claudeDirName), ".claude directory"); err != nil {
		return err
	}
	if err := ensureRealDirectory(project.SymlinkDir, ".claude/skills directory"); err != nil {
		return err
	}
	if err := ensureRealDirectory(filepath.Join(project.Root, agentsDirName), ".agents directory"); err != nil {
		return err
	}
	if err := ensureRealDirectory(project.AgentsSymlinkDir, ".agents/skills directory"); err != nil {
		return err
	}
	return nil
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

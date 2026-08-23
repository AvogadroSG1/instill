package instill

import (
	"context"
	"path/filepath"
	"strings"
)

func AddRemoteSkill(ctx context.Context, root, repository string, runner CommandRunner) error {
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	entry, err := resolveRemoteSkill(boundedCtx, repository, runner)
	if err != nil {
		return err
	}
	// Deterministically classify an already-done bounded context as ExitGeneral
	// before entering withRootLocks, rather than letting it fall into ADR-0006
	// lock-acquisition classification (ADR 0007).
	if err := boundedContextError(boundedCtx, entry.Repository); err != nil {
		return err
	}
	return withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
		if err := boundedContextError(ctx, entry.Repository); err != nil {
			return err
		}
		entries, plugins, err := loadTypedPackageCatalogs(root)
		if err != nil {
			return err
		}
		for _, existing := range entries {
			if existing.Name == entry.Name {
				return NewExitError(ExitGeneral, "error: skill already exists: "+entry.Name)
			}
			if existing.Source == "git" && stableCatalogGitIdentity(existing) == stableCatalogGitIdentity(entry) {
				return NewExitError(ExitGeneral, "error: remote package already exists: "+existing.Name)
			}
		}
		if err := validateTypedGitCatalogs(append(entries, entry), plugins); err != nil {
			return err
		}
		return writeCatalogLocked(ctx, held, root, LibraryTypeSkill, append(entries, entry))
	})
}

func UpdateRemoteSkill(ctx context.Context, root, name string, runner CommandRunner) error {
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name != name {
			continue
		}
		if entry.Source != "git" {
			return NewExitError(ExitGeneral, "error: skill is not remotely sourced: "+name)
		}
		repository := strings.TrimSuffix(strings.TrimPrefix(entry.Repository, "https://github.com/"), ".git")
		updated, err := resolveRemoteSkill(boundedCtx, repository, runner)
		if err != nil {
			return err
		}
		// Deterministically classify an already-done bounded context as
		// ExitGeneral before entering withRootLocks (ADR 0007).
		if err := boundedContextError(boundedCtx, updated.Repository); err != nil {
			return err
		}
		snapshot := entry
		return withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
			emitMutationTestEvent("revalidation:remote-skill:" + name)
			if err := boundedContextError(ctx, updated.Repository); err != nil {
				return err
			}
			lockedEntries, err := LoadCatalog(root, LibraryTypeSkill)
			if err != nil {
				return err
			}
			lockedIndex := -1
			for index, locked := range lockedEntries {
				if locked.Name == name {
					lockedIndex = index
					if !catalogRowSnapshotEqual(snapshot, locked) {
						return concurrentCatalogConflict("skill", name)
					}
					updated.Category = locked.Category
					updated.Description = locked.Description
					break
				}
			}
			if lockedIndex < 0 {
				return concurrentCatalogConflict("skill", name)
			}
			if exactCatalogGitIdentity(updated) == exactCatalogGitIdentity(lockedEntries[lockedIndex]) {
				return nil
			}
			if err := rejectCrossCatalogGitIdentityLocked(ctx, held, root, LibraryTypeSkill, updated); err != nil {
				return err
			}
			lockedEntries[lockedIndex] = updated
			return writeCatalogLocked(ctx, held, root, LibraryTypeSkill, lockedEntries)
		})
	}
	return NewExitError(ExitGeneral, "error: unknown skill: "+name)
}

func concurrentCatalogConflict(typ string, name string) error {
	return NewExitError(ExitGeneral, "error: concurrent catalog change for "+typ+" "+name+"; review the current catalog and rerun")
}

func resolveRemoteSkill(ctx context.Context, repository string, runner CommandRunner) (CatalogEntry, error) {
	snapshot, url, err := openGitSnapshot(ctx, repository, runner)
	if err != nil {
		return CatalogEntry{}, err
	}
	defer snapshot.close()
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(url, "https://github.com/"), ".git"), "/")
	name := parts[1]
	path := "skills/" + name
	if _, err := snapshot.regularFile(path + "/SKILL.md"); err != nil {
		if isBoundedContextError(err) {
			return CatalogEntry{}, err
		}
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: remote skill is missing "+path+"/SKILL.md at "+snapshot.sha)
	}
	return CatalogEntry{Type: LibraryTypeSkill, Name: name, Path: path, Source: "git", Repository: url, Ref: snapshot.sha}, nil
}

func remoteSkillPath(entry CatalogEntry) string { return filepath.ToSlash(entry.Path) }

func isCanonicalRemoteSkill(entry CatalogEntry) bool {
	repository := strings.TrimSuffix(strings.TrimPrefix(entry.Repository, "https://github.com/"), ".git")
	if !canonicalGitHubRepository(entry.Repository) {
		return false
	}
	parts := strings.Split(repository, "/")
	return entry.Repository == "https://github.com/"+repository+".git" &&
		entry.Name == parts[1] && entry.Path == "skills/"+parts[1]
}

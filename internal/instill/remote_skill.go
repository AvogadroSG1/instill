package instill

import (
	"path/filepath"
	"strings"
)

func AddRemoteSkill(root, repository string, runner CommandRunner) error {
	entry, err := resolveRemoteSkill(repository, runner)
	if err != nil {
		return err
	}
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	if err != nil {
		return err
	}
	for _, existing := range entries {
		if existing.Name == entry.Name {
			return NewExitError(ExitGeneral, "error: skill already exists: "+entry.Name)
		}
	}
	if err := rejectCrossCatalogGitIdentity(root, LibraryTypeSkill, entry); err != nil {
		return err
	}
	return WriteCatalog(root, LibraryTypeSkill, append(entries, entry))
}

func UpdateRemoteSkill(root, name string, runner CommandRunner) error {
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	if err != nil {
		return err
	}
	for i, entry := range entries {
		if entry.Name != name {
			continue
		}
		if entry.Source != "git" {
			return NewExitError(ExitGeneral, "error: skill is not remotely sourced: "+name)
		}
		repository := strings.TrimSuffix(strings.TrimPrefix(entry.Repository, "https://github.com/"), ".git")
		updated, err := resolveRemoteSkill(repository, runner)
		if err != nil {
			return err
		}
		updated.Category = entry.Category
		updated.Description = entry.Description
		if err := rejectCrossCatalogGitIdentity(root, LibraryTypeSkill, updated); err != nil {
			return err
		}
		entries[i] = updated
		return WriteCatalog(root, LibraryTypeSkill, entries)
	}
	return NewExitError(ExitGeneral, "error: unknown skill: "+name)
}

func resolveRemoteSkill(repository string, runner CommandRunner) (CatalogEntry, error) {
	snapshot, url, err := openGitSnapshot(repository, runner)
	if err != nil {
		return CatalogEntry{}, err
	}
	defer snapshot.close()
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(url, "https://github.com/"), ".git"), "/")
	name := parts[1]
	path := "skills/" + name
	if _, err := snapshot.regularFile(path + "/SKILL.md"); err != nil {
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

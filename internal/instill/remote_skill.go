package instill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]+$`)
var fullGitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

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
		entries[i] = updated
		return WriteCatalog(root, LibraryTypeSkill, entries)
	}
	return NewExitError(ExitGeneral, "error: unknown skill: "+name)
}

func resolveRemoteSkill(repository string, runner CommandRunner) (CatalogEntry, error) {
	repository = strings.TrimSuffix(repository, ".git")
	if !githubRepositoryPattern.MatchString(repository) {
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: repository must be a GitHub owner/repo")
	}
	if runner == nil {
		runner = defaultCommandRunner
	}
	parts := strings.Split(repository, "/")
	name := parts[1]
	url := "https://github.com/" + repository + ".git"
	output, err := runner("git", "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return CatalogEntry{}, remoteGitError(err, output)
	}
	sha, err := remoteHeadSHA(string(output))
	if err != nil {
		return CatalogEntry{}, err
	}
	dir, err := os.MkdirTemp("", "instill-git-")
	if err != nil {
		return CatalogEntry{}, NewExitError(ExitFilesystem, "error: cannot create temporary git directory: "+err.Error())
	}
	defer func() {
		// Cleanup is best-effort and MUST NOT replace the primary operation result.
		_ = os.RemoveAll(dir)
	}()
	if output, err := runner("git", "clone", "--no-checkout", url, dir); err != nil {
		return CatalogEntry{}, remoteGitError(err, output)
	}
	if output, err := runner("git", "-C", dir, "fetch", "--depth", "1", "origin", sha); err != nil {
		return CatalogEntry{}, remoteGitError(err, output)
	}
	path := "skills/" + name
	if output, err := runner("git", "-C", dir, "cat-file", "-e", sha+":"+path+"/SKILL.md"); err != nil {
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: remote skill is missing "+path+"/SKILL.md at "+sha+": "+strings.TrimSpace(string(output)))
	}
	return CatalogEntry{Type: LibraryTypeSkill, Name: name, Path: path, Source: "git", Repository: url, Ref: sha}, nil
}

func remoteHeadSHA(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "HEAD" && isFullGitSHA(fields[0]) {
			return fields[0], nil
		}
	}
	return "", NewExitError(ExitGeneral, "error: could not resolve remote default branch to a full commit SHA")
}

func remoteGitError(err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message != "" {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot access remote repository: %v\n%s", err, message))
	}
	return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot access remote repository: %v", err))
}

func isFullGitSHA(value string) bool { return fullGitSHAPattern.MatchString(value) }

func remoteSkillPath(entry CatalogEntry) string { return filepath.ToSlash(entry.Path) }

func isCanonicalRemoteSkill(entry CatalogEntry) bool {
	repository := strings.TrimSuffix(strings.TrimPrefix(entry.Repository, "https://github.com/"), ".git")
	if !githubRepositoryPattern.MatchString(repository) {
		return false
	}
	parts := strings.Split(repository, "/")
	return entry.Repository == "https://github.com/"+repository+".git" &&
		entry.Name == parts[1] && entry.Path == "skills/"+parts[1]
}

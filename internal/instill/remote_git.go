package instill

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]+$`)
var fullGitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type gitSnapshot struct {
	dir    string
	sha    string
	runner CommandRunner
}

func openGitSnapshot(repository string, runner CommandRunner) (*gitSnapshot, string, error) {
	repository = strings.TrimSuffix(repository, ".git")
	if !githubRepositoryPattern.MatchString(repository) {
		return nil, "", NewExitError(ExitGeneral, "error: repository must be a GitHub owner/repo")
	}
	repository = strings.ToLower(repository)
	url := "https://github.com/" + repository + ".git"
	if runner == nil {
		runner = defaultCommandRunner
	}
	output, err := runner("git", "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return nil, "", remoteGitError(err, output)
	}
	sha, err := remoteHeadSHA(string(output))
	if err != nil {
		return nil, "", err
	}
	dir, err := os.MkdirTemp("", "instill-git-")
	if err != nil {
		return nil, "", NewExitError(ExitFilesystem, "error: cannot create temporary git directory: "+err.Error())
	}
	snapshot := &gitSnapshot{dir: dir, sha: sha, runner: runner}
	if output, err = runner("git", "init", dir); err != nil {
		snapshot.close()
		return nil, "", remoteGitError(err, output)
	}
	if output, err = runner("git", "-C", dir, "remote", "add", "origin", url); err != nil {
		snapshot.close()
		return nil, "", remoteGitError(err, output)
	}
	if output, err = runner("git", "-C", dir, "fetch", "--depth", "1", "origin", sha); err != nil {
		snapshot.close()
		return nil, "", remoteGitError(err, output)
	}
	return snapshot, url, nil
}

func (s *gitSnapshot) close() {
	// Cleanup is best-effort and MUST NOT replace the primary operation result.
	_ = os.RemoveAll(s.dir)
}

func (s *gitSnapshot) regularFile(file string, maxBytes ...int64) ([]byte, error) {
	mode, typ, err := s.object(file)
	if err != nil {
		return nil, err
	}
	if typ != "blob" || (mode != "100644" && mode != "100755") {
		return nil, NewExitError(ExitGeneral, "error: remote path is not a regular file: "+file)
	}
	if len(maxBytes) > 0 {
		output, err := s.runner("git", "-C", s.dir, "cat-file", "-s", s.sha+":"+file)
		if err != nil {
			return nil, NewExitError(ExitGeneral, "error: cannot inspect remote file "+file+": "+strings.TrimSpace(string(output)))
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil || size < 0 {
			return nil, NewExitError(ExitGeneral, "error: cannot inspect remote file size: "+file)
		}
		if size > maxBytes[0] {
			return nil, NewExitError(ExitGeneral, "error: remote file exceeds size limit: "+file)
		}
	}
	output, err := s.runner("git", "-C", s.dir, "show", s.sha+":"+file)
	if err != nil {
		return nil, NewExitError(ExitGeneral, "error: cannot read remote file "+file+": "+strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (s *gitSnapshot) requireTree(dir string) error {
	mode, typ, err := s.object(dir)
	if err != nil {
		return err
	}
	if mode != "040000" || typ != "tree" {
		return NewExitError(ExitGeneral, "error: remote path is not a directory: "+dir)
	}
	return nil
}

func (s *gitSnapshot) object(name string) (string, string, error) {
	output, err := s.runner("git", "-C", s.dir, "ls-tree", s.sha, "--", name)
	if err != nil {
		return "", "", NewExitError(ExitGeneral, "error: remote path is missing: "+name)
	}
	line := strings.TrimSpace(string(output))
	metadata, listed, ok := strings.Cut(line, "\t")
	fields := strings.Fields(metadata)
	if !ok || listed != name || len(fields) != 3 {
		return "", "", NewExitError(ExitGeneral, "error: remote path is missing: "+name)
	}
	return fields[0], fields[1], nil
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

func normalizedGitPath(value string) string {
	return path.Clean(strings.ReplaceAll(value, "\\", "/"))
}

func stableGitIdentity(repository, packagePath string) string {
	return "git:" + strings.ToLower(repository) + ":" + normalizedGitPath(packagePath)
}

func exactGitIdentity(repository, packagePath, ref string) string {
	return "git:" + repository + ":" + packagePath + ":" + ref
}

func stableCatalogGitIdentity(entry CatalogEntry) string {
	return stableGitIdentity(entry.Repository, entry.Path)
}

func exactCatalogGitIdentity(entry CatalogEntry) string {
	return exactGitIdentity(entry.Repository, entry.Path, entry.Ref)
}

func canonicalGitHubRepository(repository string) bool {
	prefix := "https://github.com/"
	if !strings.HasPrefix(repository, prefix) || !strings.HasSuffix(repository, ".git") || repository != strings.ToLower(repository) {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(repository, prefix), ".git")
	return githubRepositoryPattern.MatchString(name)
}

func rejectCrossCatalogGitIdentity(root string, owner LibraryType, entry CatalogEntry) error {
	other := LibraryTypeSkill
	if owner == LibraryTypeSkill {
		other = LibraryTypePlugin
	}
	entries, err := LoadCatalog(root, other)
	if err != nil {
		return err
	}
	identity := stableCatalogGitIdentity(entry)
	for _, existing := range entries {
		if existing.Source == "git" && stableCatalogGitIdentity(existing) == identity {
			return NewExitError(ExitGeneral, "error: remote package identity is already owned by "+string(other)+" "+existing.Name)
		}
	}
	return nil
}

func validateTypedGitCatalogs(skills, plugins []CatalogEntry) error {
	skillOwners := make(map[string]string)
	for _, entry := range skills {
		if entry.Source == "git" {
			skillOwners[stableCatalogGitIdentity(entry)] = entry.Name
		}
	}
	for _, entry := range plugins {
		if entry.Source != "git" {
			continue
		}
		if skill, ok := skillOwners[stableCatalogGitIdentity(entry)]; ok {
			return NewExitError(ExitGeneral, "error: ambiguous remote package identity is owned by skill "+skill+" and plugin "+entry.Name)
		}
	}
	return nil
}

package instill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]+$`)
var fullGitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// remoteGitTimeout is the total production deadline applied to one remote Git
// resolution (ADR 0007). It is a var, not a const, so tests can override it;
// production callers MUST NOT change it.
var remoteGitTimeout = 60 * time.Second //nolint:gochecknoglobals // ADR 0007 test override seam.

// gitCommandWaitDelay bounds how long a killed default git subprocess is
// given to exit after its context completes, so a child process holding
// inherited descriptors cannot extend the resolution bound (ADR 0007).
const gitCommandWaitDelay = 5 * time.Second

type gitSnapshot struct {
	// ctx propagates the bounded resolution deadline to every subsequent
	// invocation issued through this snapshot (ADR 0007).
	ctx      context.Context
	dir      string
	sha      string
	url      string
	runner   CommandRunner
	executor *boundedGitExecutor
}

// boundedGitExecutor runs git via exec.CommandContext so the bounded
// resolution deadline actually kills the subprocess, and tracks outstanding
// invocations so a caller that abandons a blocked call can still reap the
// process before removing the temporary directory it may be writing into
// (ADR 0007 implementation notes).
type boundedGitExecutor struct {
	ctx context.Context
	wg  sync.WaitGroup
}

func newBoundedGitExecutor(ctx context.Context) *boundedGitExecutor {
	return &boundedGitExecutor{ctx: ctx}
}

func (e *boundedGitExecutor) run(name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(e.ctx, name, args...) //nolint:gosec // Command name is fixed to git; args are controlled by the caller.
	cmd.WaitDelay = gitCommandWaitDelay
	return cmd.CombinedOutput()
}

// wait blocks until every invocation issued through this executor has
// returned, so cleanup does not race a subprocess still writing to disk.
func (e *boundedGitExecutor) wait() {
	e.wg.Wait()
}

// runGitBounded executes one git invocation and returns ctx.Err() as soon as
// ctx completes, even if the runner has not returned. ctx is checked before
// launching and again once the runner returns, so an already-done context
// always wins the race against a runner result that happened to be ready at
// the same instant — select chooses randomly between ready cases, so relying
// on it alone would make an already-cancelled call nondeterministically
// proceed (ADR 0007). executor, when non-nil, is the default (real
// subprocess) path; its WaitGroup is incremented here, on the caller's
// goroutine, before the worker goroutine is launched, so a concurrent
// gitSnapshot.close cannot observe a zero count and remove the directory
// before the worker has registered.
func runGitBounded(ctx context.Context, executor *boundedGitExecutor, runner CommandRunner, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type outcome struct {
		output []byte
		err    error
	}
	if executor != nil {
		executor.wg.Add(1)
	}
	done := make(chan outcome, 1)
	go func() {
		if executor != nil {
			defer executor.wg.Done()
		}
		output, err := runner(name, args...)
		done <- outcome{output, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return out.output, out.err
	}
}

// isBoundedContextError reports whether err is (or wraps) the deadline or
// cancellation sentinel produced by a bounded remote Git operation.
func isBoundedContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// classifyGitError maps a git invocation failure to its exit classification.
// Context failures are classified ExitGeneral, preserve their cause, and name
// the canonical repository url per ADR 0007; other failures keep the existing
// remote access error shape.
func classifyGitError(err error, output []byte, url string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return newExitErrorWithCause(ExitGeneral, "error: remote git operation timed out for repository "+url, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return newExitErrorWithCause(ExitGeneral, "error: remote git operation cancelled for repository "+url, context.Canceled)
	}
	return remoteGitError(err, output)
}

// boundedContextError returns a classified ADR 0007 error naming url when ctx
// has already completed, or nil when the operation may proceed.
func boundedContextError(ctx context.Context, url string) error {
	if err := ctx.Err(); err != nil {
		return classifyGitError(err, nil, url)
	}
	return nil
}

func openGitSnapshot(ctx context.Context, repository string, runner CommandRunner) (*gitSnapshot, string, error) {
	repository = strings.TrimSuffix(repository, ".git")
	if !githubRepositoryPattern.MatchString(repository) {
		return nil, "", NewExitError(ExitGeneral, "error: repository must be a GitHub owner/repo")
	}
	repository = strings.ToLower(repository)
	url := "https://github.com/" + repository + ".git"

	var executor *boundedGitExecutor
	if runner == nil {
		executor = newBoundedGitExecutor(ctx)
		runner = executor.run
	}

	output, err := runGitBounded(ctx, executor, runner, "git", "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return nil, "", classifyGitError(err, output, url)
	}
	sha, err := remoteHeadSHA(string(output))
	if err != nil {
		return nil, "", err
	}
	dir, err := os.MkdirTemp("", "instill-git-")
	if err != nil {
		return nil, "", NewExitError(ExitFilesystem, "error: cannot create temporary git directory: "+err.Error())
	}
	snapshot := &gitSnapshot{ctx: ctx, dir: dir, sha: sha, url: url, runner: runner, executor: executor}
	if output, err = runGitBounded(ctx, executor, runner, "git", "init", dir); err != nil {
		snapshot.close()
		return nil, "", classifyGitError(err, output, url)
	}
	if output, err = runGitBounded(ctx, executor, runner, "git", "-C", dir, "remote", "add", "origin", url); err != nil {
		snapshot.close()
		return nil, "", classifyGitError(err, output, url)
	}
	if output, err = runGitBounded(ctx, executor, runner, "git", "-C", dir, "fetch", "--depth", "1", "origin", sha); err != nil {
		snapshot.close()
		return nil, "", classifyGitError(err, output, url)
	}
	return snapshot, url, nil
}

func (s *gitSnapshot) close() {
	// Cleanup is best-effort and MUST NOT replace the primary operation result.
	// A killed default subprocess is reaped before its directory is removed so
	// cleanup does not race a process still writing to it (ADR 0007).
	if s.executor != nil {
		s.executor.wait()
	}
	_ = os.RemoveAll(s.dir)
}

// run executes one git invocation scoped to this snapshot's directory and
// bounded context, translating a completed context into a classified error.
func (s *gitSnapshot) run(args ...string) ([]byte, error) {
	output, err := runGitBounded(s.ctx, s.executor, s.runner, "git", args...)
	if err != nil && isBoundedContextError(err) {
		return nil, classifyGitError(err, output, s.url)
	}
	return output, err
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
		output, err := s.run("-C", s.dir, "cat-file", "-s", s.sha+":"+file)
		if err != nil {
			if isBoundedContextError(err) {
				return nil, err
			}
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
	output, err := s.run("-C", s.dir, "show", s.sha+":"+file)
	if err != nil {
		if isBoundedContextError(err) {
			return nil, err
		}
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
	output, err := s.run("-C", s.dir, "ls-tree", s.sha, "--", name)
	if err != nil {
		if isBoundedContextError(err) {
			return "", "", err
		}
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

func rejectCrossCatalogGitIdentityLocked(ctx context.Context, held *heldLocks, root string, owner LibraryType, entry CatalogEntry) error {
	if err := held.requireContext(ctx, root); err != nil {
		return err
	}
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

func catalogRowSnapshotEqual(left CatalogEntry, right CatalogEntry) bool {
	return left.Name == right.Name &&
		left.Source == right.Source &&
		left.Repository == right.Repository &&
		stableCatalogGitIdentity(left) == stableCatalogGitIdentity(right) &&
		left.Ref == right.Ref
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

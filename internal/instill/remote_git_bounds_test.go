package instill

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// remoteOpWaitBound bounds every test wait on a remote operation. A test that
// hits it proves the operation is unbounded; the bound never defines ordering.
const remoteOpWaitBound = 5 * time.Second

// blockingRemoteRunner blocks any git command containing blockOn until release
// closes, signalling started once. Other commands delegate to base; a nil base
// fails them so a blocked resolution cannot silently make progress elsewhere.
func blockingRemoteRunner(started chan<- struct{}, release <-chan struct{}, blockOn string, base CommandRunner) CommandRunner {
	var signal sync.Once
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		if strings.Contains(command, blockOn) {
			if started != nil {
				signal.Do(func() { close(started) })
			}
			<-release
			return nil, errors.New("blocked command released: " + command)
		}
		if base == nil {
			return nil, errors.New("unexpected command while blocked: " + command)
		}
		return base(name, args...)
	}
}

func awaitRemoteOp(t *testing.T, op func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- op() }()
	select {
	case err := <-done:
		return err
	case <-time.After(remoteOpWaitBound):
		t.Fatal("remote Git operation did not return within the wait bound; resolution is unbounded")
		return nil
	}
}

func assertBoundedRemoteError(t *testing.T, err error, cause error, wantSubstring string, url string) {
	t.Helper()
	if err == nil {
		t.Fatal("remote operation error = nil, want bounded failure")
	}
	if got := ExitCode(err); got != ExitGeneral {
		t.Fatalf("ExitCode() = %d, want ExitGeneral: %v", got, err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, %v) = false: %v", cause, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), wantSubstring) {
		t.Fatalf("error = %q, want substring %q", err, wantSubstring)
	}
	if !strings.Contains(err.Error(), url) {
		t.Fatalf("error = %q, want repository URL %q", err, url)
	}
}

func TestRemoteOperationsTerminateWhenRemoteCommandBlocks(t *testing.T) {
	seedSkill := CatalogEntry{
		Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git",
		Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA,
	}
	seedPlugin := CatalogEntry{
		Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git",
		Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA,
	}
	tests := []struct {
		name string
		url  string
		typ  LibraryType
		seed func(t *testing.T, library string)
		run  func(ctx context.Context, library string, runner CommandRunner) error
	}{
		{
			name: "add skill",
			url:  "https://github.com/owner/example.git",
			typ:  LibraryTypeSkill,
			run: func(ctx context.Context, library string, runner CommandRunner) error {
				return AddRemoteSkill(ctx, library, "owner/example", runner)
			},
		},
		{
			name: "update skill",
			url:  "https://github.com/owner/example.git",
			typ:  LibraryTypeSkill,
			seed: func(t *testing.T, library string) {
				requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{seedSkill}))
			},
			run: func(ctx context.Context, library string, runner CommandRunner) error {
				return UpdateRemoteSkill(ctx, library, "example", runner)
			},
		},
		{
			name: "add plugin",
			url:  "https://github.com/owner/repo.git",
			typ:  LibraryTypePlugin,
			run: func(ctx context.Context, library string, runner CommandRunner) error {
				return AddRemotePlugin(ctx, library, "owner/repo", "plugin", runner)
			},
		},
		{
			name: "update plugin",
			url:  "https://github.com/owner/repo.git",
			typ:  LibraryTypePlugin,
			seed: func(t *testing.T, library string) {
				requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{seedPlugin}))
			},
			run: func(ctx context.Context, library string, runner CommandRunner) error {
				return UpdateRemotePlugin(ctx, library, "plugin", runner)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			library := t.TempDir()
			if test.seed != nil {
				test.seed(t, library)
			}
			before, err := LoadCatalog(library, test.typ)
			requireNoError(t, err)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			runner := blockingRemoteRunner(nil, release, "ls-remote", nil)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			opErr := awaitRemoteOp(t, func() error { return test.run(ctx, library, runner) })

			assertBoundedRemoteError(t, opErr, context.DeadlineExceeded, "timed out", test.url)
			after, err := LoadCatalog(library, test.typ)
			requireNoError(t, err)
			requireEqual(t, before, after)
		})
	}
}

func TestRemoteSkillResolutionTerminatesWhenFetchBlocks(t *testing.T) {
	library := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	runner := blockingRemoteRunner(started, release, " fetch ", remoteSkillRunner(t, remoteSkillSHA))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- AddRemoteSkill(ctx, library, "owner/example", runner) }()
	select {
	case <-started:
	case <-time.After(remoteOpWaitBound):
		t.Fatal("timed out waiting for the fetch phase to be reached")
	}
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(remoteOpWaitBound):
		t.Fatal("remote Git operation did not return after cancellation during fetch")
	}
	assertBoundedRemoteError(t, err, context.Canceled, "cancel", "https://github.com/owner/example.git")
	entries, loadErr := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, loadErr)
	requireEqual(t, 0, len(entries))
}

func TestRemoteSkillResolutionTerminatesOnCallerCancellation(t *testing.T) {
	library := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	runner := blockingRemoteRunner(started, release, "ls-remote", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- AddRemoteSkill(ctx, library, "owner/example", runner) }()
	select {
	case <-started:
	case <-time.After(remoteOpWaitBound):
		t.Fatal("timed out waiting for blocked ls-remote signal")
	}
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(remoteOpWaitBound):
		t.Fatal("remote Git operation did not return after caller cancellation")
	}
	assertBoundedRemoteError(t, err, context.Canceled, "cancel", "https://github.com/owner/example.git")
}

// This test overrides the production deadline and MUST NOT run in parallel.
func TestProductionRemoteGitDeadlineAppliesWithoutCallerDeadline(t *testing.T) {
	previous := remoteGitTimeout
	remoteGitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { remoteGitTimeout = previous })

	library := t.TempDir()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	runner := blockingRemoteRunner(nil, release, "ls-remote", nil)

	err := awaitRemoteOp(t, func() error {
		return AddRemoteSkill(context.Background(), library, "owner/example", runner)
	})

	assertBoundedRemoteError(t, err, context.DeadlineExceeded, "timed out", "https://github.com/owner/example.git")
}

func TestProductionRemoteGitTimeoutIsSixtySeconds(t *testing.T) {
	requireEqual(t, 60*time.Second, remoteGitTimeout)
}

// This test installs the package mutation event hook and MUST NOT run in parallel.
func TestRemoteSkillUpdateAbortsWhenCancelledDuringLockedRevalidation(t *testing.T) {
	library := t.TempDir()
	seed := CatalogEntry{
		Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git",
		Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA,
	}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{seed}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previousHook := mutationTestEventHook
	mutationTestEventHook = func(event string) {
		if strings.HasPrefix(event, "revalidation:remote-skill:") {
			cancel()
		}
	}
	t.Cleanup(func() { mutationTestEventHook = previousHook })

	err := awaitRemoteOp(t, func() error {
		return UpdateRemoteSkill(ctx, library, "example", remoteSkillRunner(t, refreshedRemoteSkillSHA))
	})

	assertBoundedRemoteError(t, err, context.Canceled, "cancel", "https://github.com/owner/example.git")
	entries, loadErr := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, loadErr)
	requireEqual(t, remoteSkillSHA, entries[0].Ref)
}

func TestRemoteSkillFetchStaysShallowAndSHAPinned(t *testing.T) {
	library := t.TempDir()
	var fetches []string
	clones := 0
	base := remoteSkillRunner(t, remoteSkillSHA)
	runner := func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		if strings.Contains(command, " clone ") {
			clones++
		}
		if strings.Contains(command, " fetch ") {
			fetches = append(fetches, command)
		}
		return base(name, args...)
	}

	requireNoError(t, AddRemoteSkill(context.Background(), library, "owner/example", runner))

	requireEqual(t, 0, clones)
	requireEqual(t, 1, len(fetches))
	if !strings.HasSuffix(fetches[0], " fetch --depth 1 origin "+remoteSkillSHA) {
		t.Fatalf("fetch = %q, want shallow SHA-pinned fetch", fetches[0])
	}
}

package instill

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCanonicalRootsAreAbsoluteSortedAndDeduplicated(t *testing.T) {
	base := t.TempDir()
	roots, err := canonicalRoots([]string{
		filepath.Join(base, "beta", "..", "beta"),
		filepath.Join(base, "alpha"),
		filepath.Join(base, "beta"),
	})
	if err != nil {
		t.Fatalf("canonicalRoots() error = %v", err)
	}

	want := []canonicalRoot{
		{path: filepath.Join(base, "alpha"), key: filepath.Join(base, "alpha")},
		{path: filepath.Join(base, "beta"), key: filepath.Join(base, "beta")},
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("canonicalRoots() = %#v, want %#v", roots, want)
	}
}

func TestCanonicalComparisonKeyCaseFoldsWindowsPaths(t *testing.T) {
	got := canonicalComparisonKey(`C:\Users\Peter\Library`, "windows")
	want := `c:\users\peter\library`
	if got != want {
		t.Fatalf("canonicalComparisonKey() = %q, want %q", got, want)
	}
}

func TestHeldLocksVerifyOwnershipAndDoNotReacquireReleasedRoot(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{}
	err := withRootLocksUsing(context.Background(), []string{root}, time.Second, provider, func(ctx context.Context, held *heldLocks) error {
		if err := held.requireContext(ctx, root); err != nil {
			return err
		}
		if err := held.release(ctx, root); err != nil {
			return err
		}
		if err := held.requireContext(ctx, root); err == nil || !strings.Contains(err.Error(), "not held") {
			t.Fatalf("require() after release error = %v, want not-held error", err)
		}
		if err := held.release(ctx, root); err == nil || !strings.Contains(err.Error(), "not held") {
			t.Fatalf("second release() error = %v, want not-held error", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withRootLocksUsing() error = %v", err)
	}
	if got, want := provider.opened, []string{filepath.Join(root, lockFileName)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened lock paths = %v, want %v", got, want)
	}
}

func TestRootLockAcquisitionUsesOneDeadlineAndStopsAfterCancellation(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{blocked: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := withRootLocksUsing(ctx, []string{root, filepath.Join(root, "other")}, time.Second, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after cancellation")
		return nil
	})
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(error) = %d, want %d: %v", ExitCode(err), ExitFilesystem, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled cause", err)
	}
	if len(provider.opened) != 0 {
		t.Fatalf("opened %v after context cancellation, want none", provider.opened)
	}
}

func TestRootLockChecksCancellationBeforeEveryTry(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	provider := &recordingLockProvider{
		try: func(_ int, _ int) (bool, error) {
			cancel()
			return false, nil
		},
	}
	err := withRootLocksUsing(ctx, []string{root}, time.Second, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after cancellation")
		return nil
	})
	if !errors.Is(err, context.Canceled) || provider.attempts != 1 {
		t.Fatalf("error = %v, attempts = %d; want cancellation after one try", err, provider.attempts)
	}
}

func TestRootLockDeadlineWinsSuccessfulTryRace(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	provider := &recordingLockProvider{
		try: func(_ int, _ int) (bool, error) {
			cancel()
			return true, nil
		},
	}
	err := withRootLocksUsing(ctx, []string{root}, time.Second, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after acquisition context completed")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	wantEvents := []string{"unlock:0", "close:0"}
	if !reflect.DeepEqual(provider.events, wantEvents) {
		t.Fatalf("cleanup events = %v, want %v", provider.events, wantEvents)
	}
}

func TestNestedAcquisitionOfHeldRootFailsBeforeProviderAccess(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{}
	err := withRootLocksUsing(context.Background(), []string{root}, time.Second, provider, func(ctx context.Context, _ *heldLocks) error {
		nestedErr := withRootLocksUsing(ctx, []string{root}, time.Second, provider, func(context.Context, *heldLocks) error {
			t.Fatal("nested callback ran")
			return nil
		})
		if !errors.Is(nestedErr, errRecursiveRootLock) {
			t.Fatalf("nested error = %v, want errRecursiveRootLock", nestedErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer withRootLocksUsing() error = %v", err)
	}
	if len(provider.opened) != 1 {
		t.Fatalf("provider opens = %v, want only outer acquisition", provider.opened)
	}
}

func TestRootLocksReleaseDuringPanicUnwind(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("callback panic was not propagated")
			}
		}()
		_ = withRootLocksUsing(context.Background(), []string{root}, time.Second, provider, func(context.Context, *heldLocks) error {
			panic("test panic")
		})
	}()
	wantEvents := []string{"unlock:0", "close:0"}
	if !reflect.DeepEqual(provider.events, wantEvents) {
		t.Fatalf("cleanup events = %v, want %v", provider.events, wantEvents)
	}
}

func TestRootLockAcquisitionTimesOutForCompleteSet(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{blocked: true}
	err := withRootLocksUsing(context.Background(), []string{root}, 40*time.Millisecond, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after timeout")
		return nil
	})
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(error) = %d, want %d: %v", ExitCode(err), ExitFilesystem, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded cause", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(root, lockFileName)) {
		t.Fatalf("error = %v, want canonical lock path", err)
	}
}

func TestProductionRootLockTimeoutIsTenSeconds(t *testing.T) {
	if rootLockTimeout != 10*time.Second {
		t.Fatalf("rootLockTimeout = %v, want 10s", rootLockTimeout)
	}
	root := t.TempDir()
	holder := startRootLockProcess(t, root)
	holder.waitFor(t, "attempt")
	holder.waitFor(t, "acquired")
	started := time.Now()
	err := withRootLocks(context.Background(), []string{root}, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran while helper process held the root")
		return nil
	})
	elapsed := time.Since(started)
	holder.release(t)
	holder.wait(t)
	if !errors.Is(err, context.DeadlineExceeded) || ExitCode(err) != ExitFilesystem {
		t.Fatalf("withRootLocks() error = %v, want filesystem-classified deadline", err)
	}
	if elapsed < 9*time.Second || elapsed > 12*time.Second {
		t.Fatalf("production timeout elapsed = %v, want scheduler-tolerant 10s", elapsed)
	}
}

func TestUnsupportedRootLockErrorIsFilesystemClassified(t *testing.T) {
	root := t.TempDir()
	provider := &recordingLockProvider{tryErr: errors.New("operation not supported")}
	err := withRootLocksUsing(context.Background(), []string{root}, time.Second, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after unsupported lock error")
		return nil
	})
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(error) = %d, want %d: %v", ExitCode(err), ExitFilesystem, err)
	}
	if !strings.Contains(err.Error(), filepath.Join(root, lockFileName)) || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v, want lock path and unsupported limitation", err)
	}
}

func TestPartialAcquisitionFailureReleasesInReverseAndJoinsCleanupErrors(t *testing.T) {
	base := t.TempDir()
	alpha := filepath.Join(base, "alpha")
	beta := filepath.Join(base, "beta")
	gamma := filepath.Join(base, "gamma")
	provider := &recordingLockProvider{
		failOpenAt: 2,
		unlockErr: map[int]error{
			1: errors.New("beta unlock failed"),
		},
		closeErr: map[int]error{
			0: errors.New("alpha close failed"),
		},
	}

	err := withRootLocksUsing(context.Background(), []string{gamma, alpha, beta}, time.Second, provider, func(context.Context, *heldLocks) error {
		t.Fatal("callback ran after partial acquisition failure")
		return nil
	})
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(error) = %d, want %d: %v", ExitCode(err), ExitFilesystem, err)
	}
	for _, message := range []string{"open failed", "beta unlock failed", "alpha close failed"} {
		if !strings.Contains(err.Error(), message) {
			t.Errorf("error = %v, want %q", err, message)
		}
	}
	wantEvents := []string{"unlock:1", "close:1", "unlock:0", "close:0"}
	if !reflect.DeepEqual(provider.events, wantEvents) {
		t.Fatalf("cleanup events = %v, want %v", provider.events, wantEvents)
	}
}

func TestRootLocksExcludeSameRootAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	first := startRootLockProcess(t, root)
	first.waitFor(t, "attempt")
	first.waitFor(t, "acquired")

	second := startRootLockWaiterProcess(t, root)
	second.waitFor(t, "attempt")
	second.waitFor(t, "contended")
	second.checkBlocked(t)
	first.release(t)
	first.wait(t)
	second.waitFor(t, "acquired")
	second.release(t)
	second.wait(t)
}

func TestRootLocksAllowUnrelatedRootsAcrossProcesses(t *testing.T) {
	first := startRootLockProcess(t, t.TempDir())
	first.waitFor(t, "attempt")
	first.waitFor(t, "acquired")

	second := startRootLockProcess(t, t.TempDir())
	second.waitFor(t, "attempt")
	second.waitFor(t, "acquired")
	second.release(t)
	second.wait(t)
	first.release(t)
	first.wait(t)
}

func TestOppositeOrderRootSetsCompleteAcrossProcesses(t *testing.T) {
	alpha := t.TempDir()
	beta := t.TempDir()
	first := startRootLockProcess(t, beta, alpha)
	first.waitFor(t, "attempt")
	first.waitFor(t, "acquired")
	second := startRootLockWaiterProcess(t, alpha, beta)
	second.waitFor(t, "attempt")
	second.waitFor(t, "contended")
	second.checkBlocked(t)
	first.release(t)
	first.wait(t)
	second.waitFor(t, "acquired")
	second.release(t)
	second.wait(t)
}

func TestEarlyReleaseAllowsLibraryMutationWhileProjectRemainsLocked(t *testing.T) {
	library := t.TempDir()
	project := t.TempDir()
	var projectWaiter *rootLockProcess
	err := withRootLocks(context.Background(), []string{library, project}, func(ctx context.Context, held *heldLocks) error {
		if err := held.release(ctx, library); err != nil {
			return err
		}
		libraryWaiter := startRootLockProcess(t, library)
		libraryWaiter.waitFor(t, "attempt")
		libraryWaiter.waitFor(t, "acquired")
		libraryWaiter.release(t)
		libraryWaiter.wait(t)

		projectWaiter = startRootLockWaiterProcess(t, project)
		projectWaiter.waitFor(t, "attempt")
		projectWaiter.waitFor(t, "contended")
		projectWaiter.checkBlocked(t)
		return nil
	})
	if err != nil {
		t.Fatalf("withRootLocks() error = %v", err)
	}
	projectWaiter.waitFor(t, "acquired")
	projectWaiter.release(t)
	projectWaiter.wait(t)
}

func TestRootLockIsReleasedWhenProcessCrashes(t *testing.T) {
	root := t.TempDir()
	first := startRootLockProcess(t, root)
	first.waitFor(t, "attempt")
	first.waitFor(t, "acquired")
	if err := first.command.Process.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	_ = first.command.Wait()

	second := startRootLockProcess(t, root)
	second.waitFor(t, "attempt")
	second.waitFor(t, "acquired")
	second.release(t)
	second.wait(t)
	if _, err := os.Stat(filepath.Join(root, lockFileName)); err != nil {
		t.Fatalf("persistent lock file Stat() error = %v", err)
	}
}

func TestRootLockHelperProcess(t *testing.T) {
	encoded := os.Getenv("INSTILL_TEST_LOCK_ROOTS")
	if encoded == "" {
		return
	}
	roots := strings.Split(encoded, string(os.PathListSeparator))
	if os.Getenv("INSTILL_TEST_LOCK_WAITER") == "1" {
		runRootLockWaiterHelper(roots)
		return
	}
	fmt.Println("attempt")
	err := withRootLocks(context.Background(), roots, func(context.Context, *heldLocks) error {
		fmt.Println("acquired")
		_, err := io.ReadFull(os.Stdin, make([]byte, 1))
		return err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runRootLockWaiterHelper(roots []string) {
	contended := make(chan struct{})
	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	mutationTestEventHook = func(event string) {
		if strings.HasPrefix(event, "lock-contended:") {
			select {
			case <-contended:
			default:
				close(contended)
			}
		}
	}
	fmt.Println("attempt")
	go func() {
		done <- withRootLocks(context.Background(), roots, func(context.Context, *heldLocks) error {
			close(acquired)
			fmt.Println("acquired")
			<-release
			return nil
		})
	}()
	<-contended
	fmt.Println("contended")
	commands := bufio.NewReader(os.Stdin)
	for {
		command, err := commands.ReadByte()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		switch command {
		case 'c':
			select {
			case <-acquired:
				fmt.Println("acquired-before-release")
			default:
				fmt.Println("blocked")
			}
		case 'x':
			close(release)
			if err := <-done; err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
}

type rootLockProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	lines   <-chan string
	stderr  *strings.Builder
}

func startRootLockProcess(t *testing.T, roots ...string) *rootLockProcess {
	return startRootLockProcessWithEnv(t, nil, roots...)
}

func startRootLockWaiterProcess(t *testing.T, roots ...string) *rootLockProcess {
	return startRootLockProcessWithEnv(t, []string{"INSTILL_TEST_LOCK_WAITER=1"}, roots...)
}

func startRootLockProcessWithEnv(t *testing.T, extraEnv []string, roots ...string) *rootLockProcess {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestRootLockHelperProcess$")
	command.Env = append(os.Environ(), "INSTILL_TEST_LOCK_ROOTS="+strings.Join(roots, string(os.PathListSeparator)))
	command.Env = append(command.Env, extraEnv...)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	stderr := &strings.Builder{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return &rootLockProcess{command: command, stdin: stdin, lines: lines, stderr: stderr}
}

func (p *rootLockProcess) checkBlocked(t *testing.T) {
	t.Helper()
	if _, err := p.stdin.Write([]byte{'c'}); err != nil {
		t.Fatalf("blocked query Write() error = %v", err)
	}
	p.waitFor(t, "blocked")
}

func (p *rootLockProcess) waitFor(t *testing.T, want string) {
	t.Helper()
	select {
	case got, ok := <-p.lines:
		if !ok {
			t.Fatalf("process output closed waiting for %q: %s", want, p.stderr.String())
		}
		if got != want {
			t.Fatalf("process output = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for process output %q", want)
	}
}

func (p *rootLockProcess) release(t *testing.T) {
	t.Helper()
	if _, err := p.stdin.Write([]byte{'x'}); err != nil {
		t.Fatalf("release Write() error = %v", err)
	}
	if err := p.stdin.Close(); err != nil {
		t.Fatalf("release Close() error = %v", err)
	}
}

func (p *rootLockProcess) wait(t *testing.T) {
	t.Helper()
	if err := p.command.Wait(); err != nil {
		t.Fatalf("process Wait() error = %v: %s", err, p.stderr.String())
	}
}

type recordingLockProvider struct {
	opened     []string
	events     []string
	failOpenAt int
	blocked    bool
	tryErr     error
	unlockErr  map[int]error
	closeErr   map[int]error
	try        func(index int, attempt int) (bool, error)
	attempts   int
}

func (p *recordingLockProvider) open(path string) (rootLockHandle, error) {
	index := len(p.opened)
	if p.failOpenAt > 0 && index == p.failOpenAt {
		return nil, errors.New("open failed")
	}
	p.opened = append(p.opened, path)
	return &recordingLockHandle{provider: p, index: index}, nil
}

type recordingLockHandle struct {
	provider *recordingLockProvider
	index    int
}

func (h *recordingLockHandle) tryLock() (bool, error) {
	h.provider.attempts++
	if h.provider.try != nil {
		return h.provider.try(h.index, h.provider.attempts)
	}
	return !h.provider.blocked, h.provider.tryErr
}

func (h *recordingLockHandle) unlock() error {
	h.provider.events = append(h.provider.events, "unlock:"+string(rune('0'+h.index)))
	return h.provider.unlockErr[h.index]
}

func (h *recordingLockHandle) close() error {
	h.provider.events = append(h.provider.events, "close:"+string(rune('0'+h.index)))
	return h.provider.closeErr[h.index]
}

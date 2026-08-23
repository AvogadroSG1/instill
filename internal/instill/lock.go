package instill

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	lockFileName      = ".instill.lock"
	rootLockTimeout   = 10 * time.Second
	lockRetryInterval = 25 * time.Millisecond
)

var errRecursiveRootLock = errors.New("recursive advisory root lock acquisition")

var mutationTestEventHook func(string) //nolint:gochecknoglobals // Deterministic helper-process test instrumentation.

func emitMutationTestEvent(event string) {
	if mutationTestEventHook != nil {
		mutationTestEventHook(event)
	}
}

type canonicalRoot struct {
	path string
	key  string
}

type rootLockHandle interface {
	tryLock() (bool, error)
	unlock() error
	close() error
}

type rootLockProvider interface {
	open(path string) (rootLockHandle, error)
}

type heldRootLock struct {
	root   canonicalRoot
	handle rootLockHandle
	held   bool
}

type heldLocks struct {
	locks []*heldRootLock
	byKey map[string]*heldRootLock
}

type heldRootsContextKey struct{}

type heldRootsContext struct {
	owners map[*heldLocks]struct{}
	roots  map[string]struct{}
}

func withRootLocks(ctx context.Context, roots []string, fn func(context.Context, *heldLocks) error) error {
	return withRootLocksUsing(ctx, roots, rootLockTimeout, platformRootLockProvider{}, fn)
}

func withRootLocksUsing(
	ctx context.Context,
	roots []string,
	timeout time.Duration,
	provider rootLockProvider,
	fn func(context.Context, *heldLocks) error,
) (result error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	canonical, err := canonicalRoots(roots)
	if err != nil {
		return filesystemError("error: cannot resolve lock root", err)
	}
	parentState, _ := ctx.Value(heldRootsContextKey{}).(heldRootsContext)
	for _, root := range canonical {
		if _, held := parentState.roots[root.key]; held {
			return filesystemError("error: recursive advisory lock acquisition for "+filepath.Join(root.path, lockFileName), errRecursiveRootLock)
		}
	}

	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	held := &heldLocks{
		locks: make([]*heldRootLock, 0, len(canonical)),
		byKey: make(map[string]*heldRootLock, len(canonical)),
	}
	for _, root := range canonical {
		if err := acquireCtx.Err(); err != nil {
			return joinLockFailure(lockContextError(root, ctx, err), held.releaseAll())
		}
		lockPath := filepath.Join(root.path, lockFileName)
		emitMutationTestEvent("lock-request:" + lockPath)
		handle, err := provider.open(lockPath)
		if err != nil {
			return joinLockFailure(filesystemError("error: cannot open advisory lock "+lockPath, err), held.releaseAll())
		}
		lock := &heldRootLock{root: root, handle: handle}
		if err := acquireRootLock(acquireCtx, ctx, lockPath, lock); err != nil {
			closeErr := handle.close()
			return joinLockFailure(err, closeErr, held.releaseAll())
		}
		lock.held = true
		if err := acquireCtx.Err(); err != nil {
			return joinLockFailure(lockContextError(root, ctx, err), releaseHeldRoot(lock), held.releaseAll())
		}
		held.locks = append(held.locks, lock)
		held.byKey[root.key] = lock
		emitMutationTestEvent("lock-acquired:" + lockPath)
	}
	if len(canonical) > 0 {
		if err := acquireCtx.Err(); err != nil {
			return joinLockFailure(lockContextError(canonical[len(canonical)-1], ctx, err), held.releaseAll())
		}
	}
	emitMutationTestEvent("lock-set-acquired")
	callbackState := heldRootsContext{
		owners: make(map[*heldLocks]struct{}, len(parentState.owners)+1),
		roots:  make(map[string]struct{}, len(parentState.roots)+len(canonical)),
	}
	for owner := range parentState.owners {
		callbackState.owners[owner] = struct{}{}
	}
	for key := range parentState.roots {
		callbackState.roots[key] = struct{}{}
	}
	callbackState.owners[held] = struct{}{}
	for _, root := range canonical {
		callbackState.roots[root.key] = struct{}{}
	}
	callbackCtx := context.WithValue(ctx, heldRootsContextKey{}, callbackState)
	defer func() {
		if releaseErr := held.releaseAll(); releaseErr != nil {
			result = filesystemError("error: cannot release advisory root locks", result, releaseErr)
		}
	}()
	return fn(callbackCtx, held)
}

func acquireRootLock(acquireCtx context.Context, callerCtx context.Context, path string, lock *heldRootLock) error {
	for {
		if err := acquireCtx.Err(); err != nil {
			return lockContextError(canonicalRoot{path: filepath.Dir(path)}, callerCtx, err)
		}
		ok, err := lock.handle.tryLock()
		if err != nil {
			return filesystemError("error: advisory locking is unsupported or failed for "+path, err)
		}
		if ok {
			return nil
		}
		emitMutationTestEvent("lock-contended:" + path)

		timer := time.NewTimer(lockRetryInterval)
		select {
		case <-acquireCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return lockContextError(canonicalRoot{path: filepath.Dir(path)}, callerCtx, acquireCtx.Err())
		case <-timer.C:
		}
	}
}

func lockContextError(root canonicalRoot, callerCtx context.Context, err error) error {
	path := filepath.Join(root.path, lockFileName)
	if errors.Is(err, context.Canceled) {
		return filesystemError("error: advisory lock acquisition cancelled while waiting for "+path, context.Canceled)
	}
	if callerCtx != nil && errors.Is(callerCtx.Err(), context.Canceled) {
		return filesystemError("error: advisory lock acquisition cancelled while waiting for "+path, context.Canceled)
	}
	return filesystemError("error: advisory lock acquisition timed out while waiting for "+path, context.DeadlineExceeded)
}

func joinLockFailure(err error, cleanupErrors ...error) error {
	causes := []error{err}
	causes = append(causes, cleanupErrors...)
	return filesystemError("error: cannot acquire advisory root locks", causes...)
}

func canonicalRoots(roots []string) ([]canonicalRoot, error) {
	byKey := make(map[string]canonicalRoot, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", root, err)
		}
		absolute = filepath.Clean(absolute)
		key := canonicalComparisonKey(absolute, runtime.GOOS)
		if _, exists := byKey[key]; !exists {
			byKey[key] = canonicalRoot{path: absolute, key: key}
		}
	}
	canonical := make([]canonicalRoot, 0, len(byKey))
	for _, root := range byKey {
		canonical = append(canonical, root)
	}
	sort.Slice(canonical, func(i, j int) bool {
		return canonical[i].key < canonical[j].key
	})
	return canonical, nil
}

func canonicalComparisonKey(path string, goos string) string {
	if goos == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func (h *heldLocks) requireContext(ctx context.Context, root string) error {
	state, _ := ctx.Value(heldRootsContextKey{}).(heldRootsContext)
	if _, ok := state.owners[h]; !ok {
		return filesystemError("error: advisory lock capability used without its callback context")
	}
	canonical, err := canonicalRoots([]string{root})
	if err != nil {
		return filesystemError("error: cannot verify advisory lock ownership", err)
	}
	lock, ok := h.byKey[canonical[0].key]
	if !ok || !lock.held {
		return filesystemError("error: required root lock is not held: " + canonical[0].path)
	}
	return nil
}

func (h *heldLocks) release(ctx context.Context, root string) error {
	state, _ := ctx.Value(heldRootsContextKey{}).(heldRootsContext)
	if _, ok := state.owners[h]; !ok {
		return filesystemError("error: advisory lock capability used without its callback context")
	}
	canonical, err := canonicalRoots([]string{root})
	if err != nil {
		return filesystemError("error: cannot resolve advisory lock for release", err)
	}
	lock, ok := h.byKey[canonical[0].key]
	if !ok || !lock.held {
		return filesystemError("error: required root lock is not held: " + canonical[0].path)
	}
	if err := releaseHeldRoot(lock); err != nil {
		return filesystemError("error: cannot release advisory lock "+filepath.Join(lock.root.path, lockFileName), err)
	}
	return nil
}

func (h *heldLocks) releaseAll() error {
	var errs []error
	for i := len(h.locks) - 1; i >= 0; i-- {
		lock := h.locks[i]
		if !lock.held {
			continue
		}
		if err := releaseHeldRoot(lock); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Join(lock.root.path, lockFileName), err))
		}
	}
	return errors.Join(errs...)
}

func releaseHeldRoot(lock *heldRootLock) error {
	unlockErr := lock.handle.unlock()
	closeErr := lock.handle.close()
	lock.held = false
	emitMutationTestEvent("lock-released:" + filepath.Join(lock.root.path, lockFileName))
	return errors.Join(unlockErr, closeErr)
}

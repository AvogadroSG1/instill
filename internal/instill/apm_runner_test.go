package instill

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnsureAPMInstallsWithBrewWhenMissing(t *testing.T) {
	calls := []string{}
	runner := func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		calls = append(calls, command)

		switch command {
		case "apm --version":
			if len(calls) == 1 {
				return nil, exec.ErrNotFound
			}
			return []byte("apm 0.1.0\n"), nil
		case "brew --version", "brew install apm":
			return []byte("ok\n"), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}

	err := EnsureAPM(runner)

	requireNoError(t, err)
	requireEqual(t, []string{"apm --version", "brew --version", "brew install apm", "apm --version"}, calls)
}

func TestEnsureAPMReturnsExitEnvironmentWhenBrewMissing(t *testing.T) {
	runner := func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		switch command {
		case "apm --version", "brew --version":
			return nil, exec.ErrNotFound
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}

	err := EnsureAPM(runner)

	if err == nil {
		t.Fatal("EnsureAPM() error = nil, want missing brew error")
	}
	requireEqual(t, ExitEnvironment, ExitCode(err))
	requireEqual(t, "error: brew required to install apm; install from https://brew.sh", ErrorMessage(err))
}

func TestEnsureAPMUpgradesWhenVersionOutdated(t *testing.T) {
	calls := []string{}
	runner := func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		calls = append(calls, command)

		switch command {
		case "apm --version":
			if len(calls) == 1 {
				return []byte("apm 0.0.9\n"), nil
			}
			return []byte("apm 0.1.0\n"), nil
		case "brew upgrade apm":
			return []byte("ok\n"), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}

	err := EnsureAPM(runner)

	requireNoError(t, err)
	requireEqual(t, []string{"apm --version", "brew upgrade apm", "apm --version"}, calls)
}

func TestEnsureAPMAcceptsDescriptiveVersionOutput(t *testing.T) {
	calls := []string{}
	runner := func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		calls = append(calls, command)
		if command != "apm --version" {
			t.Fatalf("unexpected command: %s", command)
		}
		return []byte("Agent Package Manager (APM) CLI version 0.26.0 (64a7fb5)\n"), nil
	}

	err := EnsureAPM(runner)

	requireNoError(t, err)
	requireEqual(t, []string{"apm --version"}, calls)
}

func TestEnsureAPMReturnsExitEnvironmentWhenVersionCheckFailsForOtherReason(t *testing.T) {
	want := errors.New("permission denied")
	runner := func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		switch command {
		case "apm --version":
			return nil, want
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}

	err := EnsureAPM(runner)

	if err == nil {
		t.Fatal("EnsureAPM() error = nil, want apm version failure")
	}
	requireEqual(t, ExitEnvironment, ExitCode(err))
	requireEqual(t, "error: apm command failed: permission denied", ErrorMessage(err))
}

func TestRunAPMInstallIncludesOutputOnFailure(t *testing.T) {
	runner := func(name string, args ...string) ([]byte, error) {
		return []byte("error: unresolved dependency 'foo/bar'\n"), errors.New("exit status 2")
	}

	err := RunAPMInstall(runner, "/tmp/project")

	if err == nil {
		t.Fatal("RunAPMInstall() error = nil, want error")
	}
	msg := ErrorMessage(err)
	if !strings.Contains(msg, "apm command failed") {
		t.Fatalf("error message missing 'apm command failed': %s", msg)
	}
	if !strings.Contains(msg, "unresolved dependency 'foo/bar'") {
		t.Fatalf("error message missing apm output: %s", msg)
	}
}

func TestRunAPMInstallEmptyOutputStillReportsExitCode(t *testing.T) {
	runner := func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}

	err := RunAPMInstall(runner, "/tmp/project")

	if err == nil {
		t.Fatal("RunAPMInstall() error = nil, want error")
	}
	msg := ErrorMessage(err)
	if !strings.Contains(msg, "exit status 1") {
		t.Fatalf("error message missing exit code: %s", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Fatalf("error message should not have trailing output section: %s", msg)
	}
}

func TestRunAPMCompileIncludesOutputOnFailure(t *testing.T) {
	runner := func(name string, args ...string) ([]byte, error) {
		return []byte("compile error: invalid skill reference\n"), errors.New("exit status 2")
	}

	err := RunAPMCompile(runner, "/tmp/project")

	if err == nil {
		t.Fatal("RunAPMCompile() error = nil, want error")
	}
	msg := ErrorMessage(err)
	if !strings.Contains(msg, "invalid skill reference") {
		t.Fatalf("error message missing apm output: %s", msg)
	}
}

func TestRunAPMPruneRunsFromProjectRootWithoutRootOption(t *testing.T) {
	projectRoot := t.TempDir()
	binDir := t.TempDir()
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	apmPath := filepath.Join(binDir, "apm")
	script := "#!/bin/sh\npwd > \"$APM_TEST_CWD_FILE\"\n"
	requireNoError(t, os.WriteFile(apmPath, []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APM_TEST_CWD_FILE", cwdFile)

	err := RunAPMPrune(nil, projectRoot)

	requireNoError(t, err)
	cwd, readErr := os.ReadFile(cwdFile)
	requireNoError(t, readErr)
	requireEqual(t, projectRoot, strings.TrimSpace(string(cwd)))
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}

func requireEqual[T any](t *testing.T, want, got T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

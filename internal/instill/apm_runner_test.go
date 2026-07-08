package instill

import (
	"errors"
	"os/exec"
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

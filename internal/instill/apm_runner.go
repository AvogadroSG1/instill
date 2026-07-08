package instill

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	APMBrewFormula = "apm"
	MinAPMVersion  = "0.1.0"
)

type CommandRunner func(name string, args ...string) ([]byte, error)

func EnsureAPM(runner CommandRunner) error {
	if runner == nil {
		runner = defaultCommandRunner
	}

	version, err := apmVersion(runner)
	if err == nil {
		if versionAtLeast(version, MinAPMVersion) {
			return nil
		}
		if err := runCommand(runner, "brew", "upgrade", APMBrewFormula); err != nil {
			return wrapCommandError("brew", err)
		}
		_, err = apmVersion(runner)
		return wrapAPMEnvironmentError(err)
	}
	if !isCommandMissing(err) {
		return wrapAPMEnvironmentError(err)
	}

	if err := ensureBrew(runner); err != nil {
		return err
	}
	if err := runCommand(runner, "brew", "install", APMBrewFormula); err != nil {
		return wrapCommandError("brew", err)
	}
	_, err = apmVersion(runner)
	return wrapAPMEnvironmentError(err)
}

func RunAPMInstall(runner CommandRunner, root string) error {
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := runCommand(runner, "apm", "install", "--project", root); err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func RunAPMCompile(runner CommandRunner, root string) error {
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := runCommand(runner, "apm", "compile", "--project", root); err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func RunAPMPrune(runner CommandRunner, root string) error {
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := runCommand(runner, "apm", "prune", "--project", root); err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func defaultCommandRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nolint:gosec // Command names are fixed and args are controlled by the caller.
}

func ensureBrew(runner CommandRunner) error {
	if err := runCommand(runner, "brew", "--version"); err != nil {
		if isCommandMissing(err) {
			return NewExitError(ExitEnvironment, "error: brew required to install apm; install from https://brew.sh")
		}
		return wrapCommandError("brew", err)
	}
	return nil
}

func apmVersion(runner CommandRunner) (string, error) {
	output, err := runner("apm", "--version")
	if err != nil {
		return "", err
	}

	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) == 0 {
		return "", NewExitError(ExitGeneral, "error: apm --version returned empty output")
	}

	return strings.TrimPrefix(fields[len(fields)-1], "v"), nil
}

func runCommand(runner CommandRunner, name string, args ...string) error {
	_, err := runner(name, args...)
	return err
}

func wrapCommandError(name string, err error) error {
	if err == nil {
		return nil
	}
	if isCommandMissing(err) && name == "brew" {
		return NewExitError(ExitEnvironment, "error: brew required to install apm; install from https://brew.sh")
	}
	return NewExitError(ExitGeneral, fmt.Sprintf("error: %s command failed: %v", name, err))
}

func wrapAPMEnvironmentError(err error) error {
	if err == nil {
		return nil
	}
	return NewExitError(ExitEnvironment, fmt.Sprintf("error: apm command failed: %v", err))
}

func isCommandMissing(err error) bool {
	return err == exec.ErrNotFound || errors.Is(err, exec.ErrNotFound)
}

func versionAtLeast(actual string, minimum string) bool {
	actualParts := parseVersion(actual)
	minimumParts := parseVersion(minimum)
	maxLen := len(actualParts)
	if len(minimumParts) > maxLen {
		maxLen = len(minimumParts)
	}

	for idx := 0; idx < maxLen; idx++ {
		actualValue := versionPart(actualParts, idx)
		minimumValue := versionPart(minimumParts, idx)
		if actualValue > minimumValue {
			return true
		}
		if actualValue < minimumValue {
			return false
		}
	}

	return true
}

func parseVersion(value string) []int {
	parts := strings.Split(value, ".")
	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return []int{0}
		}
		parsed = append(parsed, number)
	}
	return parsed
}

func versionPart(parts []int, idx int) int {
	if idx >= len(parts) {
		return 0
	}
	return parts[idx]
}

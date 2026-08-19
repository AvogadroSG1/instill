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
	if err := runCommand(runner, "apm", "install", "--legacy-skill-paths", "--root", root); err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func RunAPMCompile(runner CommandRunner, root string) error {
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := runCommand(runner, "apm", "compile", "--root", root); err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func RunAPMPrune(runner CommandRunner, root string) error {
	var err error
	if runner == nil {
		err = runCommandInDir(root, "apm", "prune")
	} else {
		err = runCommand(runner, "apm", "prune")
	}
	if err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func defaultCommandRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nolint:gosec // Command names are fixed and args are controlled by the caller.
}

func runCommandInDir(root string, name string, args ...string) error {
	command := exec.Command(name, args...) //nolint:gosec // Command names are fixed and args are controlled by the caller.
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return &commandError{name: name, err: err, output: output}
	}
	return nil
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

	for idx := len(fields) - 1; idx >= 0; idx-- {
		candidate := strings.TrimPrefix(fields[idx], "v")
		if isDottedNumericVersion(candidate) {
			return candidate, nil
		}
	}

	return "", NewExitError(ExitGeneral, "error: apm --version did not contain a semantic version")
}

func isDottedNumericVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func runCommand(runner CommandRunner, name string, args ...string) error {
	output, err := runner(name, args...)
	if err != nil {
		return &commandError{name: name, err: err, output: output}
	}
	return nil
}

type commandError struct {
	name   string
	err    error
	output []byte
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

func wrapCommandError(name string, err error) error {
	if err == nil {
		return nil
	}
	if isCommandMissing(err) && name == "brew" {
		return NewExitError(ExitEnvironment, "error: brew required to install apm; install from https://brew.sh")
	}
	msg := fmt.Sprintf("error: %s command failed: %v", name, err)
	if cmdErr, ok := errors.AsType[*commandError](err); ok {
		if trimmed := strings.TrimSpace(string(cmdErr.output)); trimmed != "" {
			msg += "\n" + trimmed
		}
	}
	return NewExitError(ExitGeneral, msg)
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

package instill

import (
	"errors"
	"fmt"
)

const (
	ExitSuccess     = 0
	ExitGeneral     = 1
	ExitEnvironment = 2
	ExitFilesystem  = 3
)

type ExitError struct {
	Code    int
	Message string
	Cause   error
}

// Error returns the user-facing message associated with the exit error.
func (e ExitError) Error() string {
	return e.Message
}

// Unwrap preserves the programmatic cause of an exit-classified error.
func (e ExitError) Unwrap() error {
	return e.Cause
}

// NewExitError creates an error that maps to a documented CLI exit code.
func NewExitError(code int, message string) error {
	return ExitError{
		Code:    code,
		Message: message,
	}
}

func newExitErrorWithCause(code int, message string, cause error) error {
	return ExitError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

func filesystemError(message string, causes ...error) error {
	cause := errors.Join(causes...)
	if cause == nil {
		return NewExitError(ExitFilesystem, message)
	}
	return newExitErrorWithCause(ExitFilesystem, fmt.Sprintf("%s: %v", message, cause), cause)
}

// ExitCode maps an error to the process exit code required by the spec.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}

	var exitErr ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	return ExitGeneral
}

// ErrorMessage returns the user-facing message for an error.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	var exitErr ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Message
	}

	return err.Error()
}

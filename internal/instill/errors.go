package instill

import "errors"

const (
	ExitSuccess     = 0
	ExitGeneral     = 1
	ExitEnvironment = 2
	ExitFilesystem  = 3
)

type ExitError struct {
	Code    int
	Message string
}

// Error returns the user-facing message associated with the exit error.
func (e ExitError) Error() string {
	return e.Message
}

// NewExitError creates an error that maps to a documented CLI exit code.
func NewExitError(code int, message string) error {
	return ExitError{
		Code:    code,
		Message: message,
	}
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

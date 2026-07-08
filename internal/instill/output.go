package instill

import (
	"fmt"
	"io"
)

func writeLine(writer io.Writer, line string) error {
	if writer == nil {
		return nil
	}
	if _, err := fmt.Fprintln(writer, line); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
	}
	return nil
}

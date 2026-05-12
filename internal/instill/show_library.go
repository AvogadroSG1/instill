package instill

import (
	"fmt"
	"io"
)

// ShowLibraryOptions configures library listing output.
type ShowLibraryOptions struct {
	LibraryPath string
	Manifest    *Manifest
	Filter      string
	Stdout      io.Writer
}

// ShowLibrary lists library skills, optionally annotated with project selection state.
func ShowLibrary(opts ShowLibraryOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	skills, err := ListLibrarySkills(opts.LibraryPath)
	if err != nil {
		return err
	}
	skills = FilterSkills(skills, opts.Filter)

	selected := map[string]struct{}{}
	if opts.Manifest != nil {
		for _, skill := range opts.Manifest.Skills {
			selected[skill] = struct{}{}
		}
	}

	selectedVisible := 0
	for _, skill := range skills {
		if opts.Manifest == nil {
			if err := writeLine(stdout, skill); err != nil {
				return err
			}
			continue
		}

		marker := "[ ]"
		if _, ok := selected[skill]; ok {
			marker = "[✓]"
			selectedVisible++
		}
		if err := writeLine(stdout, marker+" "+skill); err != nil {
			return err
		}
	}

	if opts.Manifest == nil {
		return writeLine(stdout, fmt.Sprintf("%d skills", len(skills)))
	}

	return writeLine(stdout, fmt.Sprintf("%d skills  (%d selected)", len(skills), selectedVisible))
}

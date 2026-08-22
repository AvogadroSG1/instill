package instill

import (
	"fmt"
	"io"
	"strings"
)

// SetTargetsOptions configures target agent updates on an existing project.
type SetTargetsOptions struct {
	Project         Project
	Targets         []string
	Stdout          io.Writer
	manifestMetrics *manifestIOMetrics
}

// SetProjectTargets updates the targets in the project's APM manifest.
func SetProjectTargets(opts SetTargetsOptions) error {
	document, err := loadManifestDocumentObserved(opts.Project.ManifestPath, opts.manifestMetrics)
	if err != nil {
		return err
	}
	targets := normalizeStringSlice(opts.Targets)
	if err := document.setTargets(targets, false); err != nil {
		return err
	}
	if err := document.repairIdentity(opts.Project.Root, false); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}
	if opts.Stdout != nil {
		if len(targets) == 0 {
			return writeLine(opts.Stdout, "ok: targets cleared")
		}
		return writeLine(opts.Stdout, fmt.Sprintf("ok: targets set to %s", strings.Join(targets, ", ")))
	}
	return nil
}

// GetProjectTargets retrieves the currently configured targets for a project.
func GetProjectTargets(project Project) ([]string, error) {
	manifest, err := ReadAPMManifest(project.ManifestPath)
	if err != nil {
		return nil, err
	}
	return manifest.Targets, nil
}

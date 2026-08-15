package instill

import (
	"fmt"
	"io"
	"strings"
)

// SetTargetsOptions configures target agent updates on an existing project.
type SetTargetsOptions struct {
	Project Project
	Targets []string
	Stdout  io.Writer
}

// SetProjectTargets updates the targets in the project's APM manifest.
func SetProjectTargets(opts SetTargetsOptions) error {
	manifest, err := ReadAPMManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	manifest.Targets = normalizeStringSlice(opts.Targets)
	if err := WriteAPMManifestAtomic(opts.Project.ManifestPath, manifest); err != nil {
		return err
	}
	if opts.Stdout != nil {
		if len(manifest.Targets) == 0 {
			return writeLine(opts.Stdout, "ok: targets cleared")
		}
		return writeLine(opts.Stdout, fmt.Sprintf("ok: targets set to %s", strings.Join(manifest.Targets, ", ")))
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

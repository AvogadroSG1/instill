package instill

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if err := rejectPiTargets(opts.Targets); err != nil {
		return err
	}
	return withRootLocks(context.Background(), []string{opts.Project.Root}, func(ctx context.Context, held *heldLocks) error {
		return setProjectTargetsLocked(ctx, held, opts)
	})
}

func setProjectTargetsLocked(ctx context.Context, held *heldLocks, opts SetTargetsOptions) error {
	if err := held.requireContext(ctx, opts.Project.Root); err != nil {
		return err
	}
	document, err := loadManifestDocumentObserved(opts.Project.ManifestPath, opts.manifestMetrics)
	if err != nil {
		return err
	}
	targets := normalizeAPMProjectTargets(opts.Project.Root, opts.Targets)
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
	return normalizeAPMProjectTargets(project.Root, manifest.Targets), nil
}

func projectHasPi(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".pi"))
	return err == nil && info.IsDir()
}

func rejectPiTargets(targets []string) error {
	for _, target := range normalizeStringSlice(targets) {
		if target == "pi" {
			return NewExitError(ExitGeneral, "error: pi MCP support is activated by the .pi directory, not apm.yml targets")
		}
	}
	return nil
}

func normalizeAPMProjectTargets(root string, targets []string) []string {
	filtered := make([]string, 0, len(targets))
	hadPi := false
	for _, target := range normalizeStringSlice(targets) {
		if target == "pi" {
			hadPi = true
		} else {
			filtered = append(filtered, target)
		}
	}
	if len(filtered) == 0 && projectHasPi(root) {
		detected := DetectHarnessTargets(root)
		if hadPi || (len(detected) == 1 && detected[0] == "agent-skills") {
			return detected
		}
	}
	return normalizeStringSlice(filtered)
}

func prepareAPMProjectTargets(root string, document *manifestDocument) ([]string, error) {
	targets := normalizeAPMProjectTargets(root, document.projection.Targets)
	if !equalStringSlices(targets, document.projection.Targets) {
		if err := document.setTargets(targets, false); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func prepareStandaloneAPMManifest(root string) (APMManifest, error) {
	document, err := loadManifestDocument(ProjectAPMPath(root))
	if err != nil {
		return APMManifest{}, err
	}
	targets, err := prepareAPMProjectTargets(root, document)
	if err != nil {
		return APMManifest{}, err
	}
	manifest := document.projection
	manifest.Targets = targets
	return manifest, document.write()
}

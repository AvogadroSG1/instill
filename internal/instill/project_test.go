package instill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectFindsAPMManifestAtProjectRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "feature")
	requireNoError(t, os.MkdirAll(nested, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(root, "apm.yml"), []byte("dependencies: {}\n"), 0o644))

	project, found, err := FindProject(nested)

	requireNoError(t, err)
	requireEqual(t, true, found)
	requireEqual(t, root, project.Root)
	requireEqual(t, filepath.Join(root, "apm.yml"), project.ManifestPath)
}

func TestDetectHarnessTargetsReturnsNilForEmptyDir(t *testing.T) {
	root := t.TempDir()
	targets := DetectHarnessTargets(root)
	requireEqual(t, 0, len(targets))
}

func TestDetectHarnessTargetsReturnsSingleHarness(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))

	targets := DetectHarnessTargets(root)

	requireEqual(t, 1, len(targets))
	requireEqual(t, "claude", targets[0])
}

func TestDetectHarnessTargetsReturnsMultipleHarnessesSorted(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".gemini"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".hermes"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".pi"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".antigravity"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".opencode"), 0o755))

	targets := DetectHarnessTargets(root)

	requireEqual(t, 7, len(targets))
	requireEqual(t, "antigravity", targets[0])
	requireEqual(t, "claude", targets[1])
	requireEqual(t, "codex", targets[2])
	requireEqual(t, "gemini", targets[3])
	requireEqual(t, "hermes", targets[4])
	requireEqual(t, "opencode", targets[5])
	requireEqual(t, "pi", targets[6])
}

func TestDetectHarnessTargetsIgnoresFiles(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, ".claude"), []byte("not a dir"), 0o644))

	targets := DetectHarnessTargets(root)

	requireEqual(t, 0, len(targets))
}

func TestFindLegacyProjectFindsJSONManifestAtProjectRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "feature")
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	requireNoError(t, os.MkdirAll(nested, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".claude", "skill-manifest.json"), []byte("{\"skills\":[]}\n"), 0o644))

	project, found, err := FindLegacyProject(nested)

	requireNoError(t, err)
	requireEqual(t, true, found)
	requireEqual(t, root, project.Root)
	requireEqual(t, filepath.Join(root, ".claude", "skill-manifest.json"), project.ManifestPath)
}

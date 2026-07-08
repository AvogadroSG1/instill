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

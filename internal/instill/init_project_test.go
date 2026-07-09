package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInitProjectCreatesAPMManifestWithoutLegacyFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}},
	})

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Runner:      recordingRunner(nil, nil),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if manifest.Name != filepath.Base(root) {
		t.Fatalf("manifest name = %q, want %q", manifest.Name, filepath.Base(root))
	}
	if manifest.Version != "0.1.0" {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, "0.1.0")
	}
	if len(manifest.Dependencies.APM) != 0 {
		t.Fatalf("manifest dependencies.apm = %#v, want empty", manifest.Dependencies.APM)
	}
	assertPathMissing(t, filepath.Join(root, ".claude", "skill-manifest.json"))
	assertPathMissing(t, filepath.Join(root, ".claude", "skills"))
	assertPathMissing(t, filepath.Join(root, ".agents", "skills"))
}

func TestInitProjectWithSkillsWritesSkillPathsAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}},
	})

	calls := []string{}
	runner := recordingRunner(&calls, nil)
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Skills:      []string{"docker"},
		Runner:      runner,
		Stdout:      &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	want := filepath.Join(library, "skills", "docker")
	if len(manifest.Dependencies.APM) != 1 || manifest.Dependencies.APM[0] != want {
		t.Fatalf("manifest dependencies.apm = %#v, want [%q]", manifest.Dependencies.APM, want)
	}
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --root " + root,
	})
}

func TestInitProjectWritesTargetsWhenMultipleHarnessesDetected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}},
	})

	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Skills:      []string{"docker"},
		Runner:      recordingRunner(nil, nil),
		Stdout:      &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 2 {
		t.Fatalf("manifest targets = %#v, want 2 entries", manifest.Targets)
	}
	requireEqual(t, "claude", manifest.Targets[0])
	requireEqual(t, "codex", manifest.Targets[1])
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists; err = %v", path, err)
	}
}

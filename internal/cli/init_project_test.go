package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestInitProjectCLIWritesAPMManifest(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init", "--skills", "docker"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "apm.yml")); err != nil {
		t.Fatalf("Stat(apm.yml) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skill-manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy manifest exists; err = %v", err)
	}
}

func TestInitProjectCLIUsesUnifiedPicker(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var captured instill.PickTUIOptions
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init"},
		cwd:    root,
		runner: recordingRunner(nil),
		isTTY: func(*os.File) bool {
			return true
		},
		pickTUI: func(opts instill.PickTUIOptions) error {
			captured = opts
			return nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if captured.InitialType != instill.LibraryTypeSkill {
		t.Fatalf("InitialType = %q, want %q", captured.InitialType, instill.LibraryTypeSkill)
	}
	if captured.Runner == nil {
		t.Fatal("captured.Runner = nil, want injected runner")
	}
}

func TestInitProjectCLIExistingAPMManifestDoesNotRequireTTY(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{})
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(os.DevNull) error = %v", err)
	}
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Fatalf("Close(stdin) error = %v", err)
		}
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdin:  stdin,
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init"},
		cwd:    root,
		runner: recordingRunner(nil),
		isTTY: func(*os.File) bool {
			return false
		},
	})

	if code != instill.ExitGeneral {
		t.Fatalf("execute() = %d, want %d; stderr = %q", code, instill.ExitGeneral, stderr.String())
	}
	if got := stderr.String(); !bytes.Contains([]byte(got), []byte("manifest already exists")) {
		t.Fatalf("stderr = %q, want manifest exists error", got)
	}
}

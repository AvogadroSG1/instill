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

	var captured instill.PickSkillsTUIOptions
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
		initPicker: func(opts instill.PickSkillsTUIOptions) (instill.InitialSkillSelectionPlan, bool, error) {
			captured = opts
			return instill.InitialSkillSelectionPlan{}, true, nil
		},
		targetPicker: func(opts instill.TargetPickerOptions) ([]string, bool, error) {
			return []string{"codex"}, true, nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if captured.Runner == nil {
		t.Fatal("captured.Runner = nil, want injected runner")
	}
}

func TestInitProjectCLIExistingAPMManifestDoesNotRequireTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("INSTILL_LIBRARY_PATH", "")
	t.Setenv("SKILL_LIBRARY_PATH", "")
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

func TestInitProjectCLIWithTargetsFlag(t *testing.T) {
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
		args:   []string{"init", "--skills", "docker", "--targets", "codex,opencode,hermes"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 3 || manifest.Targets[0] != "codex" || manifest.Targets[1] != "opencode" || manifest.Targets[2] != "hermes" {
		t.Fatalf("manifest.Targets = %#v, want [codex, opencode, hermes]", manifest.Targets)
	}
}

func TestInitProjectCLIInteractivePromptsForTargets(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var capturedTargetOpts instill.TargetPickerOptions
	targetPickerCalled := false

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init", "--skills", "docker"},
		cwd:    root,
		runner: recordingRunner(nil),
		isTTY: func(*os.File) bool {
			return true
		},
		targetPicker: func(opts instill.TargetPickerOptions) ([]string, bool, error) {
			targetPickerCalled = true
			capturedTargetOpts = opts
			return []string{"pi", "antigravity"}, true, nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !targetPickerCalled {
		t.Fatal("targetPicker was not called, want called")
	}
	if len(capturedTargetOpts.Available) != 6 {
		t.Fatalf("len(Available) = %d, want 6", len(capturedTargetOpts.Available))
	}

	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 2 || manifest.Targets[0] != "pi" || manifest.Targets[1] != "antigravity" {
		t.Fatalf("manifest.Targets = %#v, want [pi, antigravity]", manifest.Targets)
	}
}

func TestInitProjectCLIInteractiveTargetPickerCancelled(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{})
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
		isTTY: func(*os.File) bool {
			return true
		},
		targetPicker: func(opts instill.TargetPickerOptions) ([]string, bool, error) {
			return nil, false, nil
		},
	})

	if code == 0 {
		t.Fatal("execute() = 0, want non-zero when cancelled")
	}
	if _, err := os.Stat(filepath.Join(root, "apm.yml")); !os.IsNotExist(err) {
		t.Fatal("apm.yml should not exist when target picker cancelled")
	}
}

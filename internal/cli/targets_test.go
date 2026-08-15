package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestTargetsCLIExplicitSet(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{
		Targets: []string{"claude"},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"targets", "codex", "opencode"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "ok: targets set to codex, opencode") {
		t.Fatalf("stdout = %q, want 'ok: targets set to codex, opencode'", got)
	}

	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 2 || manifest.Targets[0] != "codex" || manifest.Targets[1] != "opencode" {
		t.Fatalf("manifest.Targets = %#v, want [codex, opencode]", manifest.Targets)
	}
}

func TestTargetsCLIInteractivePicker(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{
		Targets: []string{"claude"},
	})

	pickerCalled := false
	var capturedOpts instill.TargetPickerOptions

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"targets"},
		cwd:    root,
		runner: recordingRunner(nil),
		isTTY: func(*os.File) bool {
			return true
		},
		targetPicker: func(opts instill.TargetPickerOptions) ([]string, bool, error) {
			pickerCalled = true
			capturedOpts = opts
			return []string{"hermes", "pi"}, true, nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !pickerCalled {
		t.Fatal("targetPicker not called, want called")
	}
	if len(capturedOpts.Selected) != 1 || capturedOpts.Selected[0] != "claude" {
		t.Fatalf("capturedOpts.Selected = %#v, want [claude]", capturedOpts.Selected)
	}

	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 2 || manifest.Targets[0] != "hermes" || manifest.Targets[1] != "pi" {
		t.Fatalf("manifest.Targets = %#v, want [hermes, pi]", manifest.Targets)
	}
}

func TestTargetsCLINonInteractiveList(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{
		Targets: []string{"codex", "opencode"},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"targets"},
		cwd:    root,
		runner: recordingRunner(nil),
		isTTY: func(*os.File) bool {
			return false
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || lines[0] != "codex" || lines[1] != "opencode" {
		t.Fatalf("stdout lines = %#v, want [codex, opencode]", lines)
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyShowLibraryExitsWithLibraryShowGuidance(t *testing.T) {
	root := createLegacyProject(t, `{"skills":["docker"]}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want silence", stdout.String())
	}
	if !strings.Contains(stderr.String(), "show-library has been replaced by 'instill library show'") {
		t.Fatalf("stderr = %q, want migration guidance naming instill library show", stderr.String())
	}
}

func TestLegacyShowLibraryDoesNotMutateProjectState(t *testing.T) {
	root := createLegacyProject(t, `{"skills":["docker","missing"]}`)
	skillsDir := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(skills) error = %v", err)
	}
	if err := os.Symlink("/legacy/orphan", filepath.Join(skillsDir, "orphan")); err != nil {
		t.Fatalf("Symlink(orphan) error = %v", err)
	}
	manifestPath := filepath.Join(root, ".claude", "skill-manifest.json")
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest after) error = %v", err)
	}
	if string(afterManifest) != string(beforeManifest) {
		t.Fatalf("manifest changed to %q, want %q", afterManifest, beforeManifest)
	}
	if _, err := os.Lstat(filepath.Join(skillsDir, "docker")); !os.IsNotExist(err) {
		t.Fatalf("docker symlink exists after legacy command; err = %v", err)
	}
	if target, err := os.Readlink(filepath.Join(skillsDir, "orphan")); err != nil || target != "/legacy/orphan" {
		t.Fatalf("orphan symlink = %q, %v; want unchanged /legacy/orphan", target, err)
	}
}

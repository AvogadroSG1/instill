package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyCheckSkillsExitsWithSyncGuidance(t *testing.T) {
	root := createLegacyProject(t, `{"skills":["docker"]}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"check-skills"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want silence", stdout.String())
	}
	if !strings.Contains(stderr.String(), "check-skills has been replaced by 'instill sync'") {
		t.Fatalf("stderr = %q, want migration guidance naming instill sync", stderr.String())
	}
}

func TestLegacyCheckSkillsDoesNotMutateProjectState(t *testing.T) {
	root := createLegacyProject(t, `{"skills":["docker"]}`)
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
		args:   []string{"check-skills"},
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

func createLegacyProject(t *testing.T, manifest string) string {
	t.Helper()

	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "skill-manifest.json"), []byte(manifest+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill-manifest.json) error = %v", err)
	}
	return root
}

func createLibrary(t *testing.T, names ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(SKILL.md) error = %v", err)
		}
	}
	return root
}

func createProject(t *testing.T, skills []string) string {
	t.Helper()

	root := createLegacyProject(t, legacyManifest(skills))
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude/skills) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.agents/skills) error = %v", err)
	}
	return root
}

func legacyManifest(skills []string) string {
	manifest := `{"skills":[`
	for i, skill := range skills {
		if i > 0 {
			manifest += ","
		}
		manifest += `"` + skill + `"`
	}
	return manifest + `]}`
}

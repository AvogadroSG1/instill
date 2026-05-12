package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSkillsNoManifestIsSilentSuccess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"check-skills"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("stdout = %q stderr = %q, want silence", stdout.String(), stderr.String())
	}
}

func TestCheckSkillsMalformedManifestExitsOne(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".claude", "skill-manifest.json"),
		[]byte("{"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("SKILL_LIBRARY_PATH", "")

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
}

func TestCheckSkillsCreatesSymlink(t *testing.T) {
	library := createLibrary(t, "docker")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"check-skills"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if _, err := os.Readlink(filepath.Join(root, ".claude", "skills", "docker")); err != nil {
		t.Fatalf("Readlink(docker) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "created: docker -> ") {
		t.Fatalf("stdout = %q, want created line", stdout.String())
	}
}

func TestCheckSkillsMissingConfigExitsTwo(t *testing.T) {
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", "")
	t.Setenv("HOME", t.TempDir())

	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(os.DevNull) error = %v", err)
	}
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Fatalf("Close(os.DevNull) error = %v", err)
		}
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdin:  stdin,
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"check-skills"},
		cwd:    root,
	})

	if code != 2 {
		t.Fatalf("execute() = %d, want 2; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want silence", stdout.String())
	}
	if !strings.Contains(stderr.String(), "error: no library path configured") {
		t.Fatalf("stderr = %q, want config error", stderr.String())
	}
}

func TestCheckSkillsFilesystemErrorExitsThree(t *testing.T) {
	library := createLibrary(t, "docker")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	collision := filepath.Join(root, ".claude", "skills", "docker")
	if err := os.WriteFile(collision, []byte("not a symlink"), 0o644); err != nil {
		t.Fatalf("WriteFile(collision) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"check-skills"},
		cwd:    root,
	})

	if code != 3 {
		t.Fatalf("execute() = %d, want 3; stderr = %q", code, stderr.String())
	}
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

	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude/skills) error = %v", err)
	}
	manifest := `{"skills":[`
	for i, skill := range skills {
		if i > 0 {
			manifest += ","
		}
		manifest += `"` + skill + `"`
	}
	manifest += `]}`
	if err := os.WriteFile(
		filepath.Join(claudeDir, "skill-manifest.json"),
		[]byte(manifest),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	return root
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestShowLibraryCLIOutsideProject(t *testing.T) {
	library := createLibrary(t, "docker")
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "docker\n1 skills\n" {
		t.Fatalf("stdout = %q, want plain library", stdout.String())
	}
}

func TestShowLibraryCLIInsideProjectWithFilter(t *testing.T) {
	library := createLibrary(t, "docker", "golang-cli")
	root := createProject(t, []string{"golang-cli"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library", "--filter", "go"},
		cwd:    filepath.Join(root, ".claude"),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	want := "[✓] golang-cli\n1 skills  (1 selected)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestShowLibraryCLIWithCategory(t *testing.T) {
	library := createLibrary(t, "azure-blob-storage", "docker", "golang-cli")
	root := createProject(t, []string{"docker", "golang-cli"})
	writeCategories(t, library, `{
		"cloud/azure": ["azure-blob-storage"],
		"cloud": ["docker"],
		"golang": ["golang-cli"]
	}`)
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library", "--category", "cloud"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	want := "[ ] azure-blob-storage\n[✓] docker\n2 skills  (1 selected)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestShowLibraryCLIReconcilesMissingManifestSkill(t *testing.T) {
	library := createLibrary(t, "docker")
	root := createProject(t, []string{"docker", "missing"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	if err := os.Symlink(filepath.Join(library, "missing"), filepath.Join(root, ".claude", "skills", "missing")); err != nil {
		t.Fatalf("Symlink(missing) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"show-library"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	want := "removed: missing (no longer in library)\n[✓] docker\n1 skills  (1 selected)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "skills", "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing symlink remains; err = %v", err)
	}
}

func writeCategories(t *testing.T, library string, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(library, ".categories.json"), []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(.categories.json) error = %v", err)
	}
}

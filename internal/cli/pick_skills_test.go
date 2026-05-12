package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestPickSkillsCLIAddsAndRemoves(t *testing.T) {
	library := createLibrary(t, "docker", "golang-cli")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick-skills", "golang-cli", "--remove", "docker"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added:   golang-cli") ||
		!strings.Contains(stdout.String(), "removed: docker") ||
		!strings.Contains(stdout.String(), "manifest: 1 skills") {
		t.Fatalf("stdout = %q, want add/remove/manifest lines", stdout.String())
	}
}

func TestPickSkillsCLINoManifestExitsOne(t *testing.T) {
	t.Setenv("SKILL_LIBRARY_PATH", createLibrary(t, "docker"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick-skills", "docker"},
		cwd:    t.TempDir(),
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no manifest found") {
		t.Fatalf("stderr = %q, want no manifest error", stderr.String())
	}
}

func TestPickSkillsCLINoArgsLaunchesTUI(t *testing.T) {
	library := createLibrary(t, "docker", "golang-cli")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	launched := false
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick-skills"},
		cwd:    root,
		pickSkillsTUI: func(opts instill.PickSkillsTUIOptions) error {
			launched = true
			if opts.Project.Root != root {
				t.Fatalf("project root = %q, want %q", opts.Project.Root, root)
			}
			if opts.LibraryPath != library {
				t.Fatalf("library = %q, want %q", opts.LibraryPath, library)
			}
			return instill.ApplySkillSelection(instill.SkillSelectionOptions{
				Project:     opts.Project,
				LibraryPath: opts.LibraryPath,
				Skills:      []string{"docker", "golang-cli"},
				Stdout:      opts.Stdout,
			})
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !launched {
		t.Fatal("pick-skills TUI did not launch")
	}
	if !strings.Contains(stdout.String(), "added:   golang-cli") ||
		!strings.Contains(stdout.String(), "manifest: 2 skills") {
		t.Fatalf("stdout = %q, want TUI diff output", stdout.String())
	}
}

func TestPickSkillsCLIUnknownSkillMakesNoChanges(t *testing.T) {
	library := createLibrary(t, "docker")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick-skills", "missing"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	manifest, err := os.ReadFile(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if !strings.Contains(string(manifest), "docker") || strings.Contains(string(manifest), "missing") {
		t.Fatalf("manifest = %q, want unchanged docker only", string(manifest))
	}
}

func TestPickSkillsCLIUnknownRemoveMakesNoChanges(t *testing.T) {
	library := createLibrary(t, "docker")
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick-skills", "--remove", "docker,missing"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	manifest, err := os.ReadFile(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if !strings.Contains(string(manifest), "docker") {
		t.Fatalf("manifest = %q, want unchanged docker", string(manifest))
	}
	if !strings.Contains(stderr.String(), "unknown skill: missing") {
		t.Fatalf("stderr = %q, want unknown remove skill error", stderr.String())
	}
}

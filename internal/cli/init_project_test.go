package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestInitProjectCLISucceedsWithSkills(t *testing.T) {
	library := createLibrary(t, "docker", "golang-cli")
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init", "--skills", "golang-cli,docker"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	manifest, err := os.ReadFile(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if !strings.Contains(string(manifest), `"docker"`) || !strings.Contains(string(manifest), `"golang-cli"`) {
		t.Fatalf("manifest = %q, want both skills", string(manifest))
	}
	if _, err := os.Readlink(filepath.Join(root, ".claude", "skills", "docker")); err != nil {
		t.Fatalf("Readlink(docker) error = %v", err)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want silence", stderr.String())
	}
}

func TestInitProjectCLIExistingManifestExitsOne(t *testing.T) {
	root := createProject(t, []string{})
	t.Setenv("SKILL_LIBRARY_PATH", createLibrary(t, "docker"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "manifest already exists") {
		t.Fatalf("stderr = %q, want existing manifest error", stderr.String())
	}
}

func TestInitProjectCLIForceOverwritesManifest(t *testing.T) {
	root := createProject(t, []string{"docker"})
	t.Setenv("SKILL_LIBRARY_PATH", createLibrary(t, "docker"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	launched := false
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init", "--force"},
		cwd:    root,
		isTTY: func(*os.File) bool {
			return true
		},
		pickSkillsTUI: func(opts instill.PickSkillsTUIOptions) error {
			launched = true
			if _, err := os.Stat(filepath.Join(root, ".claude", "skill-manifest.json")); err != nil {
				t.Fatalf("manifest not created before TUI: %v", err)
			}
			return instill.ApplySkillSelection(instill.SkillSelectionOptions{
				Project:     opts.Project,
				LibraryPath: opts.LibraryPath,
				Skills:      []string{},
				Stdout:      opts.Stdout,
			})
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !launched {
		t.Fatal("init --force did not launch pick-skills TUI")
	}
	manifest, err := os.ReadFile(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if strings.Contains(string(manifest), "docker") {
		t.Fatalf("manifest = %q, want docker removed", string(manifest))
	}
}

func TestInitProjectCLINoSkillsLaunchesTUIAfterProjectFiles(t *testing.T) {
	library := createLibrary(t, "docker")
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	launched := false
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init"},
		cwd:    root,
		isTTY: func(*os.File) bool {
			return true
		},
		pickSkillsTUI: func(opts instill.PickSkillsTUIOptions) error {
			launched = true
			if _, err := os.Stat(filepath.Join(root, ".claude", "skill-manifest.json")); err != nil {
				t.Fatalf("manifest not created before TUI: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".claude", "skills")); err != nil {
				t.Fatalf("skills dir not created before TUI: %v", err)
			}
			return instill.ApplySkillSelection(instill.SkillSelectionOptions{
				Project:     opts.Project,
				LibraryPath: opts.LibraryPath,
				Skills:      []string{"docker"},
				Stdout:      opts.Stdout,
			})
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !launched {
		t.Fatal("init did not launch pick-skills TUI")
	}
	if !strings.Contains(stdout.String(), "initialized: .claude/skill-manifest.json") ||
		!strings.Contains(stdout.String(), "added:   docker") ||
		!strings.Contains(stdout.String(), "manifest: 1 skills") {
		t.Fatalf("stdout = %q, want init output and TUI diff", stdout.String())
	}
}

func TestInitProjectCLINoSkillsNonTTYExitsTwoWithoutProjectWrites(t *testing.T) {
	library := createLibrary(t, "docker")
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)
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
	})

	if code != 2 {
		t.Fatalf("execute() = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "error: pick-skills TUI requires a terminal") {
		t.Fatalf("stderr = %q, want TUI terminal error", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf(".claude exists after non-TTY init; err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore exists after non-TTY init; err = %v", err)
	}
}

func TestInitProjectCLIInvalidSkillsExitsOneWithoutProjectWrites(t *testing.T) {
	library := createLibrary(t, "docker")
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"init", "--skills", "docker,missing"},
		cwd:    root,
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf(".claude exists after invalid skills; err = %v", err)
	}
	if !strings.Contains(stderr.String(), "unknown skill: missing") {
		t.Fatalf("stderr = %q, want unknown skill error", stderr.String())
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCategorizeCLIWritesRegistry(t *testing.T) {
	library := createLibrary(t, "docker", "golang-cli", "mystery")
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"categorize"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(library, ".categories.json"))
	if err != nil {
		t.Fatalf("ReadFile(.categories.json) error = %v", err)
	}
	if !strings.Contains(string(contents), `"cloud"`) ||
		!strings.Contains(string(contents), `"docker"`) ||
		!strings.Contains(string(contents), `"golang"`) ||
		!strings.Contains(string(contents), `"golang-cli"`) {
		t.Fatalf(".categories.json = %q, want cloud/docker and golang/golang-cli", string(contents))
	}
	if !strings.Contains(stdout.String(), "uncategorized: mystery\n") {
		t.Fatalf("stdout = %q, want uncategorized mystery", stdout.String())
	}
}

func TestCategorizeCLIMalformedRegistryExitsOne(t *testing.T) {
	library := createLibrary(t, "docker")
	writeCategories(t, library, `{`)
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"categorize"},
		cwd:    t.TempDir(),
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no output before failed write", stdout.String())
	}
	if !strings.Contains(stderr.String(), "error: cannot load category registry") {
		t.Fatalf("stderr = %q, want registry load error", stderr.String())
	}
}

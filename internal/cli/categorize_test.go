package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyCategorizeExitsWithLibraryScanGuidance(t *testing.T) {
	library := createLibrary(t, "docker")
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
		t.Fatalf("stdout = %q, want silence", stdout.String())
	}
	if !strings.Contains(stderr.String(), "categorize has been replaced by typed library catalogs") {
		t.Fatalf("stderr = %q, want typed catalog guidance", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(library, ".categories.json")); !os.IsNotExist(err) {
		t.Fatalf(".categories.json exists after legacy command; err = %v", err)
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestStatusCLIReportsDrift(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{
			{typ: "skill", name: "docker", path: "docker/SKILL.md"},
			{typ: "skill", name: "golang-cli", path: "golang-cli/SKILL.md"},
		},
	})
	root := createAPMProjectRoot(t, instill.APMManifest{
		Dependencies: instill.APMDependencies{
			APM: []string{
				filepath.Join(library, "skills", "docker"),
				filepath.Join(library, "skills", "missing"),
			},
		},
	})
	if err := os.WriteFile(filepath.Join(root, "apm.lock.yaml"), []byte("instructions: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(apm.lock.yaml) error = %v", err)
	}
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"status"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "available in library: skill golang-cli") {
		t.Fatalf("stdout = %q, want available skill drift", stdout.String())
	}
}

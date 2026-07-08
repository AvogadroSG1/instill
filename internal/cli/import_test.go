package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestImportCLIOldInstillEnsuresAPMBeforeImporting(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "cloud/azure/azure-cli",
			path: "cloud/azure/azure-cli/SKILL.md",
		}},
	})
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude/skills) error = %v", err)
	}
	if err := instill.WriteManifestAtomic(filepath.Join(root, ".claude", "skill-manifest.json"), instill.Manifest{
		Skills: []string{"cloud/azure/azure-cli"},
	}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	if err := os.Symlink(
		filepath.Join(library, "skills", "cloud", "azure", "azure-cli"),
		filepath.Join(root, ".claude", "skills", "cloud:azure:azure-cli"),
	); err != nil {
		t.Fatalf("Symlink(skill) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	calls := []string{}
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"import", "old-instill"},
		cwd:    root,
		runner: recordingRunner(&calls),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if len(calls) == 0 || calls[0] != "apm --version" {
		t.Fatalf("calls = %#v, want EnsureAPM before import", calls)
	}
	if _, err := os.Stat(filepath.Join(root, "apm.yml")); err != nil {
		t.Fatalf("Stat(apm.yml) error = %v", err)
	}
}

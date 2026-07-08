package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

type catalogFixture struct {
	typ  string
	name string
	path string
}

type cliCatalogLibrarySeed struct {
	skills []catalogFixture
}

func createCatalogLibrary(t *testing.T, seed cliCatalogLibrarySeed) string {
	t.Helper()

	root := t.TempDir()
	skillEntries := make([]instill.CatalogEntry, 0, len(seed.skills))
	for _, fixture := range seed.skills {
		skillEntries = append(skillEntries, instill.CatalogEntry{
			Type: instill.LibraryTypeSkill,
			Name: fixture.name,
			Path: fixture.path,
		})
		target := filepath.Join(root, "skills", fixture.path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", target, err)
		}
		if err := os.WriteFile(target, []byte("# skill "+fixture.name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", target, err)
		}
	}
	if err := instill.WriteCatalog(root, instill.LibraryTypeSkill, skillEntries); err != nil {
		t.Fatalf("WriteCatalog() error = %v", err)
	}
	return root
}

func createAPMProjectRoot(t *testing.T, manifest instill.APMManifest) string {
	t.Helper()

	root := t.TempDir()
	if err := instill.WriteAPMManifestAtomic(filepath.Join(root, "apm.yml"), manifest); err != nil {
		t.Fatalf("WriteAPMManifestAtomic() error = %v", err)
	}
	return root
}

func recordingRunner(calls *[]string) instill.CommandRunner {
	return func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if calls != nil {
			*calls = append(*calls, command)
		}
		if command == "apm --version" {
			return []byte("apm 0.1.0\n"), nil
		}
		return []byte("ok\n"), nil
	}
}

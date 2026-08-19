package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestPickCLIAddsSkillDependency(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	root := createAPMProjectRoot(t, instill.APMManifest{})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "skill", "docker"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadFile(apm.yml) error = %v", err)
	}
	if !strings.Contains(string(data), filepath.Join(library, "skills", "docker")) {
		t.Fatalf("manifest = %q, want docker dependency", string(data))
	}
}

func TestPickCLIInteractivePassesRunnerToTUI(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	root := createAPMProjectRoot(t, instill.APMManifest{})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	calls := []string{}
	var captured instill.PickTUIOptions
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick"},
		cwd:    root,
		runner: recordingRunner(&calls),
		pickTUI: func(opts instill.PickTUIOptions) error {
			captured = opts
			return nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if captured.Runner == nil {
		t.Fatal("captured.Runner = nil, want injected runner")
	}
	if captured.InitialType != instill.LibraryTypeSkill {
		t.Fatalf("InitialType = %q, want %q", captured.InitialType, instill.LibraryTypeSkill)
	}
	if len(calls) != 0 {
		t.Fatalf("runner calls = %#v, want no direct calls before interactive selection", calls)
	}
}

func TestPickCLIInteractiveStartsAtMCPType(t *testing.T) {
	library := createPickCatalogLibrary(t, []instill.CatalogEntry{{
		Type:      instill.LibraryTypeMCP,
		Name:      "github",
		Transport: "stdio",
		Command:   "github-mcp",
	}})
	root := createAPMProjectRoot(t, instill.APMManifest{})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var captured instill.PickTUIOptions
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "mcp"},
		cwd:    root,
		runner: recordingRunner(nil),
		pickTUI: func(opts instill.PickTUIOptions) error {
			captured = opts
			return nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if captured.InitialType != instill.LibraryTypeMCP {
		t.Fatalf("InitialType = %q, want %q", captured.InitialType, instill.LibraryTypeMCP)
	}
	if captured.Runner == nil {
		t.Fatal("captured.Runner = nil, want injected runner")
	}
}

func TestPickCLIAddsMCPDependency(t *testing.T) {
	library := createPickCatalogLibrary(t, []instill.CatalogEntry{{
		Type:      instill.LibraryTypeMCP,
		Name:      "github",
		Transport: "stdio",
		Command:   "github-mcp",
		Args:      []string{"serve"},
	}})
	root := createAPMProjectRoot(t, instill.APMManifest{})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "mcp", "github"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "github" {
		t.Fatalf("MCP dependencies = %#v, want github", manifest.Dependencies.MCP)
	}
}

func TestPickCLIRemovesMCPDependency(t *testing.T) {
	library := createPickCatalogLibrary(t, []instill.CatalogEntry{{
		Type:      instill.LibraryTypeMCP,
		Name:      "github",
		Transport: "stdio",
		Command:   "github-mcp",
	}})
	root := createAPMProjectRoot(t, instill.APMManifest{
		Dependencies: instill.APMDependencies{
			MCP: []instill.MCPDependency{{
				Name:    "github",
				Command: "github-mcp",
			}},
		},
	})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "mcp", "--remove", "github"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Dependencies.MCP) != 0 {
		t.Fatalf("MCP dependencies = %#v, want none", manifest.Dependencies.MCP)
	}
}

func TestPickCLIRemoveModeUsesAllNamesAsRemovals(t *testing.T) {
	library := createPickCatalogLibrary(t, []instill.CatalogEntry{{
		Type:      instill.LibraryTypeMCP,
		Name:      "github",
		Transport: "stdio",
		Command:   "github-mcp",
	}, {
		Type:      instill.LibraryTypeMCP,
		Name:      "sentry",
		Transport: "stdio",
		Command:   "sentry-mcp",
	}})
	root := createAPMProjectRoot(t, instill.APMManifest{
		Dependencies: instill.APMDependencies{
			MCP: []instill.MCPDependency{{
				Name:    "github",
				Command: "github-mcp",
			}, {
				Name:    "sentry",
				Command: "sentry-mcp",
			}},
		},
	})
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "mcp", "--remove", "github", "sentry"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	manifest, err := instill.ReadAPMManifest(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Dependencies.MCP) != 0 {
		t.Fatalf("MCP dependencies = %#v, want none", manifest.Dependencies.MCP)
	}
}

func TestPickCLIAddsPluginDependency(t *testing.T) {
	root := t.TempDir()
	library := t.TempDir()
	requireNoError := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	requireNoError(instill.WriteCatalog(library, instill.LibraryTypePlugin, []instill.CatalogEntry{
		{
			Type:        instill.LibraryTypePlugin,
			Name:        "shortcuts-playground/claude",
			Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
			Description: "Shortcuts playground",
		},
	}))
	requireNoError(instill.WriteAPMManifestAtomic(filepath.Join(root, "apm.yml"), instill.APMManifest{}))
	t.Setenv("SKILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"pick", "--type", "plugin", "shortcuts-playground/claude"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "apm.yml"))
	if err != nil {
		t.Fatalf("ReadFile(apm.yml) error = %v", err)
	}
	if !strings.Contains(string(data), filepath.Join(library, "plugins", "shortcuts-playground", "claude")) {
		t.Fatalf("manifest = %q, want shortcuts-playground/claude dependency", string(data))
	}
}

func createPickCatalogLibrary(t *testing.T, mcpEntries []instill.CatalogEntry) string {
	t.Helper()

	root := t.TempDir()
	if err := instill.WriteCatalog(root, instill.LibraryTypeMCP, mcpEntries); err != nil {
		t.Fatalf("WriteCatalog(mcp) error = %v", err)
	}
	return root
}

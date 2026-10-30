package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

const cliRemoteSHA = "3333333333333333333333333333333333333333"

func TestLibraryCommandHelpListsSubcommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "--help"},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}

	help := stdout.String()
	for _, command := range []string{"scan", "add", "show"} {
		if !strings.Contains(help, command) {
			t.Fatalf("help = %q, want subcommand %q", help, command)
		}
	}
}

func TestLibraryCommandScanReportsReconciliation(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{
			{typ: "skill", name: "docker", path: "docker/SKILL.md"},
			{typ: "skill", name: "stale", path: "stale/SKILL.md"},
		},
	})
	// Remove stale skill file so it will be reported as removed
	requireNoError := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	requireNoError(os.Remove(filepath.Join(library, "skills", "stale", "SKILL.md")))

	// Add a new skill on disk
	newSkillPath := filepath.Join(library, "skills", "workflow", "write-readable-docs", "SKILL.md")
	requireNoError(os.MkdirAll(filepath.Dir(newSkillPath), 0o755))
	requireNoError(os.WriteFile(newSkillPath, []byte("# new skill\n"), 0o644))

	t.Setenv("INSTILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "scan", "--type", "skill"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}

	got := stdout.String()
	if !strings.Contains(got, "added: workflow/write-readable-docs\n") {
		t.Fatalf("stdout = %q, want added: workflow/write-readable-docs", got)
	}
	if !strings.Contains(got, "removed: stale (content not found)\n") {
		t.Fatalf("stdout = %q, want removed: stale (content not found)", got)
	}
}

func TestLibraryCommandScanAcceptsWrappedMCPServerConfig(t *testing.T) {
	library := t.TempDir()
	if err := instill.WriteCatalog(library, instill.LibraryTypeMCP, []instill.CatalogEntry{}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(library, "mcp", "slack", "config.json")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte(`{"mcpServers":{"slack":{"type":"http","url":"https://example.test/mcp"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "scan"},
		cwd:    t.TempDir(),
	})
	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stdout = %q; stderr = %q", code, stdout.String(), stderr.String())
	}

	if got, want := stdout.String(), "added: slack\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	entries, err := instill.LoadCatalog(library, instill.LibraryTypeMCP)
	if err != nil {
		t.Fatal(err)
	}
	want := []instill.CatalogEntry{{
		Type:      instill.LibraryTypeMCP,
		Name:      "slack",
		Transport: "http",
		URL:       "https://example.test/mcp",
	}}
	if len(entries) != 1 {
		t.Fatalf("LoadCatalog(mcp) returned %d entries, want 1: %#v", len(entries), entries)
	}
	got := entries[0]
	matches := got.Type == want[0].Type &&
		got.Name == want[0].Name &&
		got.Transport == want[0].Transport &&
		got.URL == want[0].URL
	if !matches {
		t.Fatalf("LoadCatalog(mcp) = %#v, want %#v", got, want[0])
	}
}

func TestLibraryCommandShowSkipsAPMBootstrap(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "show", "--type", "skill"},
		cwd:    t.TempDir(),
		runner: func(name string, args ...string) ([]byte, error) {
			t.Fatalf("runner called unexpectedly: %s %v", name, args)
			return nil, nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "docker\n1 entries\n" {
		t.Fatalf("stdout = %q, want skill catalog listing", got)
	}
}

func TestLibraryCommandShowPlugin(t *testing.T) {
	root := t.TempDir()
	requireNoError := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	requireNoError(instill.WriteCatalog(root, instill.LibraryTypePlugin, []instill.CatalogEntry{
		{
			Type:        instill.LibraryTypePlugin,
			Name:        "shortcuts-playground/claude",
			Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
			Description: "Shortcuts playground",
		},
	}))
	t.Setenv("INSTILL_LIBRARY_PATH", root)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "show", "--type", "plugin"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "shortcuts-playground/claude\n1 entries\n" {
		t.Fatalf("stdout = %q, want plugin catalog listing", got)
	}
}

func TestLibraryPluginMarketplaceCommands(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)
	runner := cliMarketplaceRunner(t, cliRemoteSHA, map[string]string{
		".claude-plugin/marketplace.json": `{"name":"mattpocock","plugins":[{"name":"mattpocock-skills","source":"./","description":"Matt Pocock skills"}]}`,
		".claude-plugin/plugin.json":      `{"name":"mattpocock-skills"}`,
	})

	var stdout, stderr bytes.Buffer
	code := execute(commandConfig{stdout: &stdout, stderr: &stderr, args: []string{"library", "add", "plugin-marketplace", "owner/repo"}, runner: runner})
	if code != 0 {
		t.Fatalf("add execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "mattpocock/mattpocock-skills" {
		t.Fatalf("entries = %#v, want one mattpocock/mattpocock-skills row", entries)
	}

	stdout.Reset()
	stderr.Reset()
	code = execute(commandConfig{stdout: &stdout, stderr: &stderr, args: []string{"library", "update", "plugin-marketplace", "mattpocock"}, runner: runner})
	if code != 0 {
		t.Fatalf("update execute() = %d, want 0; stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args: []string{"library", "add", "--type", "plugin", "--repository", "owner/repo"},
		runner: func(name string, args ...string) ([]byte, error) {
			t.Fatalf("runner called unexpectedly: %s %v", name, args)
			return nil, nil
		},
	})
	if code == 0 || !strings.Contains(stderr.String(), "library add plugin-marketplace") {
		t.Fatalf("execute() = %d, stderr = %q, want plugin-marketplace guidance", code, stderr.String())
	}
}

func TestLibraryAddCodexPluginMarketplace(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)
	runner := cliMarketplaceRunner(t, cliRemoteSHA, map[string]string{
		".agents/plugins/marketplace.json":        `{"name":"stack","plugins":[{"name":"stack","source":{"source":"local","path":"./plugins/stack"}}]}`,
		"plugins/stack/.codex-plugin/plugin.json": `{"name":"stack","description":"Stack plugin"}`,
	})

	var stdout, stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "add", "plugin-marketplace", "owner/repo"},
		runner: runner,
	})
	if code != 0 {
		t.Fatalf("add execute() = %d, stderr = %q", code, stderr.String())
	}

	entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one Codex marketplace plugin", entries)
	}
	entry := entries[0]
	if entry.Name != "stack/stack" ||
		entry.Category != "stack" ||
		entry.Path != "marketplaces/stack/plugins/stack/.codex-plugin/plugin.json" ||
		entry.Source != "marketplace" ||
		entry.Repository != "https://github.com/owner/repo.git" ||
		entry.Ref != cliRemoteSHA ||
		entry.Description != "Stack plugin" ||
		entry.MarketplaceFormat != instill.PluginMarketplaceFormatCodex {
		t.Fatalf("entry = %#v, want one Codex marketplace plugin", entry)
	}
	if stdout.Len() == 0 {
		t.Fatal("add command produced no success report")
	}
}

func TestLibraryAddMarketplaceRequiresFormatWhenBothIndexesExist(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)
	runner := cliMarketplaceRunner(t, cliRemoteSHA, map[string]string{
		".claude-plugin/marketplace.json":           `{"name":"claude-mkt","plugins":[{"name":"claude","source":"./plugins/claude"}]}`,
		"plugins/claude/.claude-plugin/plugin.json": `{"name":"claude"}`,
		".agents/plugins/marketplace.json":          `{"name":"codex-mkt","plugins":[{"name":"codex","source":{"source":"local","path":"./plugins/codex"}}]}`,
		"plugins/codex/.codex-plugin/plugin.json":   `{"name":"codex"}`,
	})

	var stdout, stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "add", "plugin-marketplace", "owner/repo"},
		runner: runner,
	})
	if code == 0 {
		t.Fatalf("add without format = %d, stderr = %q, want an error", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ambiguous") {
		t.Fatalf("add without format stderr = %q, want a format ambiguity error", stderr.String())
	}
	if entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin); err != nil || len(entries) != 0 {
		t.Fatalf("catalog after ambiguous add = %#v, err = %v; want no entries", entries, err)
	}

	stdout.Reset()
	stderr.Reset()
	code = execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "add", "plugin-marketplace", "--format", "codex", "owner/repo"},
		runner: runner,
	})
	if code != 0 {
		t.Fatalf("Codex add = %d, stderr = %q", code, stderr.String())
	}
	entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin)
	if err != nil || len(entries) != 1 || entries[0].Name != "codex-mkt/codex" ||
		entries[0].MarketplaceFormat != instill.PluginMarketplaceFormatCodex {
		t.Fatalf("Codex catalog entries = %#v, err = %v", entries, err)
	}
	before, err := os.ReadFile(filepath.Join(library, "plugins", "catalog.csv"))
	if err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "add", "plugin-marketplace", "--format", "claude", "owner/repo"},
		runner: runner,
	})
	if code == 0 || !strings.Contains(stderr.String(), "plugin marketplace repository already exists") {
		t.Fatalf("second marketplace add = %d, stderr = %q", code, stderr.String())
	}
	after, err := os.ReadFile(filepath.Join(library, "plugins", "catalog.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("catalog changed after duplicate repository add")
	}
}
func cliMarketplaceRunner(t *testing.T, sha string, files map[string]string) instill.CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/repo.git HEAD":
			return []byte(sha + "\tHEAD\n"), nil
		case strings.HasPrefix(command, "git init "):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/repo.git"):
			return nil, nil
		case strings.Contains(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, " checkout --force --detach "+sha):
			var workTree string
			for _, arg := range args {
				if strings.HasPrefix(arg, "--work-tree=") {
					workTree = strings.TrimPrefix(arg, "--work-tree=")
				}
			}
			for rel, content := range files {
				path := filepath.Join(workTree, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			return nil, nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}
}

// TestLibraryAddRemoteSkillPropagatesCommandContext proves the ADR 0007 CLI
// wiring: the remote add command passes cmd.Context() into the domain, so an
// already-cancelled command context aborts the operation with
// context.Canceled instead of running to completion.
func TestLibraryAddRemoteSkillPropagatesCommandContext(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	cfg := commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "add", "--type", "skill", "--repository", "owner/example"},
		runner: func(name string, args ...string) ([]byte, error) {
			return []byte("0123456789abcdef0123456789abcdef01234567\tHEAD\n"), nil
		},
	}
	root := newRootCommand(cfg)
	err := root.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteContext() error = %v, want context.Canceled", err)
	}
	if instill.ExitCode(err) != instill.ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", instill.ExitCode(err), instill.ExitGeneral)
	}
	entries, loadErr := instill.LoadCatalog(library, instill.LibraryTypeSkill)
	if loadErr != nil {
		t.Fatalf("LoadCatalog() error = %v", loadErr)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want no catalog mutation after cancellation", entries)
	}
}

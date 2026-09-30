package instill

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPiMCPBOMSourceAndTargetedWrite(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
	path := piProjectMCPPath(root)
	original := []byte("\xef\xbb\xbf" + `{// user comment
 "mcpServers":{"probe":{"command":"old","disabled":false}},"_instillManagedServers":["probe"]}`)
	requireNoError(t, os.WriteFile(path, original, 0600))
	before, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "new", DefaultEnabled: new(false)}
	err = withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(ctx, held, root, mcpEnableSnapshot{PiMerged: before}, newMCPInstallPolicy([]CatalogEntry{entry}, []MCPDependency{{Name: "probe"}}, true), true)
	})
	requireNoError(t, err)
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	if !bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) || !bytes.Contains(data, []byte("user comment")) {
		t.Fatal("targeted write removed BOM or comments")
	}
	state, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, state, "probe", true, "false")
}

func TestPiMCPCamelCaseCodexTOMLCollision(t *testing.T) {
	home := isolateMCPEnvironment(t)
	root := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
	writeTypedLibraryMarker(t, filepath.Join(home, ".codex", "config.toml"), "[mcpServers.legacy]\ncommand=\"provider\"\ndisabled=true\n")
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"imports": []string{"codex"}})
	original := readFile(t, piProjectMCPPath(root))
	before, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, before, "legacy", true, "true")
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "legacy", Transport: "stdio", Command: "instill", DefaultEnabled: new(false)}
	requireNoError(t, withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(ctx, held, root, mcpEnableSnapshot{PiMerged: before}, newMCPInstallPolicy([]CatalogEntry{entry}, []MCPDependency{{Name: "legacy"}}, true), true)
	}))
	requireEqual(t, original, readFile(t, piProjectMCPPath(root)))
}

func TestPiMCPNormalizedAgentPluginLoopback(t *testing.T) {
	for _, host := range []string{"[0:0:0:0:0:0:0:1]", "127.1", "2130706433"} {
		t.Run(host, func(t *testing.T) {
			isolateMCPEnvironment(t)
			root := t.TempDir()
			requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
			plugin := filepath.Join(root, "plugin")
			writePiTestJSON(t, filepath.Join(plugin, "plugin.json"), map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "loopback"})
			writePiTestJSON(t, filepath.Join(plugin, "mcp.json"), map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": map[string]any{"server": map[string]any{"type": "streamable-http", "url": "http://" + host + "/mcp"}}})
			writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"settings": map[string]any{"agentPluginPaths": []string{"plugin"}}})
			before, err := capturePiMCPEnableState(root)
			requireNoError(t, err)
			assertMCPFlag(t, before, "loopback__server", false, "")
		})
	}
}

func TestMCPEnableClaudeDirectoryMaterializedByAPM(t *testing.T) {
	home := isolateMCPEnvironment(t)
	library := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "off", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, []CatalogEntry{entry}))
	project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0", Targets: []string{"claude"}, Dependencies: APMDependencies{MCP: []MCPDependency{mcpDependencyFromCatalog(entry)}}})
	assertPathMissing(t, filepath.Join(project.Root, ".claude"))
	runner := func(name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "--version" {
			return []byte("0.32.0"), nil
		}
		if len(args) > 0 && args[0] == "install" {
			requireNoError(t, os.Mkdir(filepath.Join(project.Root, ".claude"), 0700))
			writePiTestJSON(t, filepath.Join(project.Root, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"off": map[string]any{"command": "installed"}}})
		}
		return nil, nil
	}
	requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}}))
	absolute, err := canonicalClaudeProjectRoot(project.Root)
	requireNoError(t, err)
	path := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	_, disabled, err := claudeApplicableState(path, data, absolute)
	requireNoError(t, err)
	requireEqual(t, []string{"off"}, disabled)
}

func TestMCPEnableClaudeMissingParentKeepsSharedLock(t *testing.T) {
	home := isolateMCPEnvironment(t)
	library := t.TempDir()
	configDir := filepath.Join(home, "new", "state")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	catalog := []CatalogEntry{{Type: LibraryTypeMCP, Name: "alpha", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}, {Type: LibraryTypeMCP, Name: "beta", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}}
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, catalog))
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		requireNoError(t, os.Mkdir(filepath.Join(root, ".claude"), 0700))
	}
	created := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	contended := make(chan struct{}, 1)
	results := make(chan error, 2)
	mutationTestEventHook = func(event string) {
		if event == "lock-contended:"+filepath.Join(home, lockFileName) {
			select {
			case contended <- struct{}{}:
			default:
			}
		}
	}
	t.Cleanup(func() { mutationTestEventHook = nil })
	run := func(index int, name string) {
		results <- withMCPMutationLocks(context.Background(), library, roots[index], func(ctx context.Context, held *heldLocks, catalog []CatalogEntry) error {
			if err := releaseMCPLibraryLock(ctx, held, library, roots[index]); err != nil {
				return err
			}
			if index == 1 {
				close(secondEntered)
			}
			writePiTestJSON(t, filepath.Join(roots[index], ".mcp.json"), map[string]any{"mcpServers": map[string]any{name: map[string]any{"command": "probe"}}})
			if err := reconcileClaudeMCPEnableState(ctx, held, roots[index], mcpEnableSnapshot{}, newMCPInstallPolicy(catalog, nil, true)); err != nil {
				return err
			}
			if index == 0 {
				close(created)
				<-releaseFirst
			}
			return nil
		})
	}
	go run(0, "alpha")
	select {
	case <-created:
	case err := <-results:
		t.Fatalf("first mutation failed: %v", err)
	}
	go run(1, "beta")
	select {
	case <-secondEntered:
		t.Error("second project bypassed shared state lock after parent creation")
	case <-contended:
	}
	close(releaseFirst)
	requireNoError(t, <-results)
	requireNoError(t, <-results)
	mutationTestEventHook = nil
	path := filepath.Join(configDir, ".claude.json")
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	for index, name := range []string{"alpha", "beta"} {
		absolute, err := canonicalClaudeProjectRoot(roots[index])
		requireNoError(t, err)
		_, disabled, err := claudeApplicableState(path, data, absolute)
		requireNoError(t, err)
		requireEqual(t, []string{name}, disabled)
	}
	info, err := os.Stat(configDir)
	requireNoError(t, err)
	requireEqual(t, os.FileMode(0700), info.Mode().Perm())
}

package instill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPiMCPImportPrecedenceAndCacheExclusion(t *testing.T) {
	home := isolateMCPEnvironment(t)
	root := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
	writePiTestJSON(t, filepath.Join(home, ".cursor", "mcp.json"), map[string]any{"mcpServers": map[string]any{"collision": map[string]any{"command": "cursor", "disabled": true}}})
	writePiTestJSON(t, filepath.Join(home, ".claude", "mcp.json"), map[string]any{"mcpServers": map[string]any{"collision": map[string]any{"command": "claude", "disabled": false}}})
	writePiTestJSON(t, filepath.Join(root, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"collision": map[string]any{"command": "project", "disabled": false}}})
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"imports": []string{"cursor", "claude-code"}, "mcpServers": map[string]any{"collision": map[string]any{"command": "override"}}})
	writePiTestJSON(t, filepath.Join(root, ".pi", "mcp-cache.json"), map[string]any{"mcpServers": map[string]any{"cache-only": map[string]any{"disabled": true}}})
	state, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, state, "collision", true, "true")
	if _, exists := state["cache-only"]; exists {
		t.Fatal("cache was interpreted as configuration")
	}
}

func TestPiMCPNearestAncestorBoundaryAndHostDiscovery(t *testing.T) {
	home := isolateMCPEnvironment(t)
	outer := filepath.Join(home, "workspace")
	nearest := filepath.Join(outer, "projects")
	root := filepath.Join(nearest, "project")
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".pi"), 0700))
	writePiTestJSON(t, filepath.Join(home, ".config", "mcp", "mcp.json"), map[string]any{"settings": map[string]any{"ancestorConfigRoots": []string{outer, nearest}, "hostConfigDiscovery": "on"}})
	writePiTestJSON(t, filepath.Join(home, ".pi", "agent", "mcp-adapter.json"), map[string]any{"settings": map[string]any{"hostConfigDiscovery": "off"}})
	writePiTestJSON(t, filepath.Join(outer, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"outside-boundary": map[string]any{}}})
	writePiTestJSON(t, filepath.Join(nearest, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"ancestor": map[string]any{"disabled": false}}})
	writePiTestJSON(t, filepath.Join(root, ".mcp.json"), map[string]any{"settings": map[string]any{"ancestorConfigRoots": []string{home}}})
	writePiTestJSON(t, filepath.Join(home, ".cursor", "mcp.json"), map[string]any{"mcpServers": map[string]any{"host": map[string]any{"disabled": true}}})
	state, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, state, "ancestor", true, "false")
	if _, exists := state["outside-boundary"]; exists {
		t.Fatal("ancestor discovery exceeded nearest approved boundary")
	}
	if _, exists := state["host"]; exists {
		t.Fatal("higher off setting failed to override host discovery on")
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"settings": map[string]any{"hostConfigDiscovery": "on"}})
	state, err = capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, state, "host", true, "true")
}

func TestPiMCPInstalledPackageSourcesAndClaudePluginDefaults(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
	packageRoot := filepath.Join(root, ".pi", "npm", "node_modules", "@scope", "example")
	writePiTestJSON(t, filepath.Join(packageRoot, "package.json"), map[string]any{"name": "@scope/example", "pi": map[string]any{"mcp": "servers.json"}})
	writePiTestJSON(t, filepath.Join(packageRoot, "servers.json"), map[string]any{"mcpServers": map[string]any{"one..server": map[string]any{"command": "package", "disabled": true}}})
	writePiTestJSON(t, filepath.Join(root, ".pi", "settings.json"), map[string]any{"packages": []string{"npm:@scope/example@1.2.3"}})
	pluginRoot := filepath.Join(root, "claude-plugin")
	writePiTestJSON(t, filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"), map[string]any{"name": "example-plugin"})
	writePiTestJSON(t, filepath.Join(pluginRoot, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"plugin-only": map[string]any{"command": "plugin", "disabled": true}, "shadow-name": map[string]any{"command": "plugin"}}})
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"claudePlugins": []any{map[string]any{"path": "claude-plugin", "mcp": true}}, "mcpServers": map[string]any{"shadow_name": map[string]any{"command": "native", "disabled": false}}})
	state, err := capturePiMCPEnableState(root)
	requireNoError(t, err)
	assertMCPFlag(t, state, "scope_example__one_server", true, "true")
	assertMCPFlag(t, state, "plugin-only", true, "true")
	assertMCPFlag(t, state, "shadow_name", true, "false")
	if _, exists := state["shadow-name"]; exists {
		t.Fatal("Claude plugin namespace collision was not shadowed")
	}
}

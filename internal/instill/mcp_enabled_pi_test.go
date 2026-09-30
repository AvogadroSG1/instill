package instill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writePiTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPiMCPProviderDefinitions(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(root, "plugin")
	writePiTestJSON(t, filepath.Join(plugin, "plugin.json"), map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "sample-plugin"})
	writePiTestJSON(t, filepath.Join(plugin, "mcp.json"), map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": map[string]any{"from-plugin": map[string]any{"type": "stdio", "command": "plugin"}}})
	pkg := filepath.Join(root, "package")
	writePiTestJSON(t, filepath.Join(pkg, "package.json"), map[string]any{"name": "sample-package", "pi": map[string]any{"mcp": []string{"servers.json"}}})
	writePiTestJSON(t, filepath.Join(pkg, "servers.json"), map[string]any{"mcpServers": map[string]any{"from-package": map[string]any{"command": "package", "disabled": false}}})
	writePiTestJSON(t, filepath.Join(root, ".pi", "settings.json"), map[string]any{"packages": []string{"../package"}})
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"settings": map[string]any{"agentPluginPaths": []string{"plugin"}}})
	before, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := before["sample-plugin__from-plugin"]; !exists || value.Present {
		t.Fatalf("plugin projection = %#v, exists %v", value, exists)
	}
	if value, exists := before["sample-package__from-package"]; !exists || !value.Present || string(value.Value) != "false" {
		t.Fatalf("package projection = %#v, exists %v", value, exists)
	}
}

func TestPiMCPCodexImport(t *testing.T) {
	home := isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[mcp_servers.imported]\nenabled = false\ndisabled = true\n[mcp_servers.enabled-only]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"imports": []string{"codex"}, "mcpServers": map[string]any{}})
	before, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := before["imported"]
	if !ok || !value.Present {
		t.Fatalf("missing TOML imported server")
	}
	var disabled bool
	if err := json.Unmarshal(value.Value, &disabled); err != nil || !disabled {
		t.Fatalf("disabled=%s, want true", value.Value)
	}
	if value, exists := before["enabled-only"]; !exists || value.Present {
		t.Fatalf("native enabled was incorrectly converted to disabled: %#v", value)
	}
}

func TestPiMCPDoesNotClaimInheritedCollision(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"mcpServers": map[string]any{"existing": map[string]any{"command": "user"}}})
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "existing", Transport: "stdio", Command: "instill", DefaultEnabled: new(false)}
	beforeMap, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	before := mcpEnableSnapshot{PiMerged: beforeMap}
	policy := newMCPInstallPolicy([]CatalogEntry{entry}, []MCPDependency{{Name: "existing"}}, true)
	err = withRootLocks(context.Background(), []string{root}, func(locked context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(locked, held, root, before, policy, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(piProjectMCPPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	servers, ok := object[piMCPServersKey].(map[string]any)
	if !ok {
		t.Fatalf("missing server map: %s", data)
	}
	existing, ok := servers["existing"].(map[string]any)
	if !ok {
		t.Fatalf("missing existing server: %s", data)
	}
	if existing["command"] != "user" {
		t.Fatalf("inherited collision was overwritten: %s", data)
	}
	if _, ok := object[piManagedKey]; ok {
		t.Fatalf("inherited collision was claimed: %s", data)
	}
}

func TestPiMCPDefaultsAndTransports(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries := []CatalogEntry{{Type: LibraryTypeMCP, Name: "off", Transport: "stdio", Command: "run-off", DefaultEnabled: new(false)}, {Type: LibraryTypeMCP, Name: "on", Transport: "stdio", Command: "run-on", DefaultEnabled: new(true)}, {Type: LibraryTypeMCP, Name: "events", Transport: "sse", URL: "https://example.test/events", DefaultEnabled: new(true)}}
	deps := []MCPDependency{{Name: "off"}, {Name: "on"}, {Name: "events"}}
	before := mcpEnableSnapshot{PiMerged: map[string]mcpEnableValue{}}
	policy := newMCPInstallPolicy(entries, deps, true)
	err := withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(ctx, held, root, before, policy, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(piProjectMCPPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	servers, ok := object[piMCPServersKey].(map[string]any)
	if !ok {
		t.Fatalf("missing mcpServers: %s", data)
	}
	if servers["off"].(map[string]any)["disabled"] != true {
		t.Fatalf("off should be disabled: %s", data)
	}
	if servers["on"].(map[string]any)["disabled"] != false {
		t.Fatalf("on should be enabled: %s", data)
	}
	events := servers["events"].(map[string]any)
	if events["httpTransport"] != "sse" || events["url"] != "https://example.test/events" {
		t.Fatalf("bad SSE definition: %s", data)
	}
	managed, ok := object[piManagedKey].([]any)
	if !ok || len(managed) != 3 {
		t.Fatalf("bad managed ownership: %s", data)
	}
}

func TestPiMCPExclusiveIgnoresProjectFile(t *testing.T) {
	isolateMCPEnvironment(t)
	t.Setenv("PI_MCP_CONFIG_MODE", "exclusive")
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"mcpServers": map[string]any{"project": map[string]any{"disabled": true}}})
	state, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state["project"]; ok {
		t.Fatalf("exclusive mode read project config: %#v", state)
	}
}

func TestPiMCPMalformedOwnershipRejected(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"_instillManagedServers": nil})
	if _, err := capturePiMCPEnableState(root); err == nil {
		t.Fatal("expected malformed ownership error")
	}
}

func TestPiMCPOwnedStateAndURLCredentials(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"mcp-servers": map[string]any{"svc": map[string]any{"url": "https://old.test", "disabled": false, "headers": map[string]any{"Authorization": "secret"}, "oauth": map[string]any{}, "directTools": []string{"x"}, "lifecycle": "keep"}}, "_instillManagedServers": []string{"svc"}})
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "svc", Transport: "http", URL: "https://new.test", DefaultEnabled: new(true)}
	beforeMap, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	policy := newMCPInstallPolicy([]CatalogEntry{entry}, []MCPDependency{{Name: "svc"}}, true)
	err = withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(ctx, held, root, mcpEnableSnapshot{PiMerged: beforeMap}, policy, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(piProjectMCPPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	servers := object[piLegacyServersKey].(map[string]any)
	svc := servers["svc"].(map[string]any)
	if svc["disabled"] != false || svc["url"] != "https://new.test" || svc["directTools"].([]any)[0] != "x" {
		t.Fatalf("owned fields not preserved/reconciled: %s", data)
	}
	if _, ok := svc["headers"]; ok {
		t.Fatalf("URL credentials survived endpoint change: %s", data)
	}
	if _, ok := svc["oauth"]; ok {
		t.Fatalf("OAuth survived endpoint change: %s", data)
	}
}

func TestPiMCPRemovalPrunesOnlyRecordedServer(t *testing.T) {
	isolateMCPEnvironment(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePiTestJSON(t, piProjectMCPPath(root), map[string]any{"mcpServers": map[string]any{"owned": map[string]any{"command": "x"}, "user": map[string]any{"command": "y"}}, "_instillManagedServers": []string{"owned"}})
	beforeMap, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	policy := newMCPInstallPolicy(nil, nil, true)
	err = withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return reconcilePiMCPConfig(ctx, held, root, mcpEnableSnapshot{PiMerged: beforeMap}, policy, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(piProjectMCPPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	servers := object[piMCPServersKey].(map[string]any)
	if _, ok := servers["owned"]; ok {
		t.Fatalf("owned server remained: %s", data)
	}
	if _, ok := servers["user"]; !ok {
		t.Fatalf("unrecorded server was pruned: %s", data)
	}
	if _, ok := object[piManagedKey]; ok {
		t.Fatalf("empty ownership marker remained: %s", data)
	}
}

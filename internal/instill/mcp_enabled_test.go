package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateMCPEnvironment(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT", "OPENCODE_DISABLE_PROJECT_CONFIG", "PI_PACKAGE_DIR", "PI_MCP_CONFIG_MODE"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	return home
}

func TestMCPEnableOpenCodeLayersAndRootOmission(t *testing.T) {
	home := isolateMCPEnvironment(t)
	root := t.TempDir()
	rootJSON := `{"mcp":{"root-on":{"command":["old"],"enabled":true},"root-off":{"enabled":false},"root-omitted":{"command":["old"]},"layered":{"command":["old"],"enabled":false}},"user":7}`
	writeTypedLibraryMarker(t, filepath.Join(root, "opencode.json"), rootJSON)
	higher := `{// preserve comment
 "mcp":{"layered":{"command":["higher"]}}}`
	writeTypedLibraryMarker(t, filepath.Join(root, "opencode.jsonc"), higher)
	global := `{"mcp":{"global-off":{"enabled":false},"global-omitted":{"command":["global"]}}}`
	writeTypedLibraryMarker(t, filepath.Join(home, ".config", "opencode", "opencode.json"), global)
	hidden := `{"mcp":{"hidden":{"enabled":false}}}`
	writeTypedLibraryMarker(t, filepath.Join(root, ".opencode", "opencode.jsonc"), hidden)
	beforeRoot, merged, err := captureOpenCodeMCPEnableState(root)
	requireNoError(t, err)
	requireEqual(t, "false", string(merged["layered"].Value))
	before := mcpEnableSnapshot{OpenCodeRoot: beforeRoot, OpenCodeMerged: merged}
	emitted := `{// post APM comment
 "mcp":{"root-on":{"command":["new"],"enabled":false},"root-off":{"command":["new"],"enabled":true},"root-omitted":{"command":["new"],"enabled":true},"layered":{"enabled":true},"global-off":{"enabled":true},"global-omitted":{"enabled":true},"hidden":{"enabled":true},"new-off":{"enabled":true},"new-on":{},"ordinary":{"enabled":true}},"user":9}`
	requireNoError(t, os.WriteFile(filepath.Join(root, "opencode.json"), []byte(emitted), 0640))
	requireNoError(t, os.Chmod(filepath.Join(root, "opencode.json"), 0640))
	catalog := []CatalogEntry{}
	for _, name := range []string{"root-on", "root-off", "root-omitted", "layered", "global-off", "global-omitted", "hidden", "new-off", "new-on"} {
		catalog = append(catalog, CatalogEntry{Name: name, DefaultEnabled: new(name == "root-off" || name == "new-on")})
	}
	requireNoError(t, reconcileOpenCodeMCPEnableState(root, before, newMCPInstallPolicy(catalog, nil, false), true))
	data, _, err := readMCPFile(filepath.Join(root, "opencode.json"), false)
	requireNoError(t, err)
	got, err := projectMCPJSONState("output", data, "mcp", "enabled")
	requireNoError(t, err)
	for _, name := range []string{"root-off", "layered", "global-off", "hidden", "new-off"} {
		requireEqual(t, "false", string(got[name].Value))
	}
	for _, name := range []string{"root-on", "new-on", "ordinary"} {
		requireEqual(t, "true", string(got[name].Value))
	}
	for _, name := range []string{"root-omitted", "global-omitted"} {
		if got[name].Present {
			t.Fatalf("%s lost omission", name)
		}
	}
	if !bytes.Contains(data, []byte("post APM comment")) || !bytes.Contains(data, []byte(`"new"`)) {
		t.Fatal("repair dropped comments or new connection")
	}
	requireEqual(t, higher, readFile(t, filepath.Join(root, "opencode.jsonc")))
	requireEqual(t, hidden, readFile(t, filepath.Join(root, ".opencode", "opencode.jsonc")))
	requireEqual(t, global, readFile(t, filepath.Join(home, ".config", "opencode", "opencode.json")))
	info, err := os.Stat(filepath.Join(root, "opencode.json"))
	requireNoError(t, err)
	requireEqual(t, os.FileMode(0640), info.Mode().Perm())
}

func TestMCPEnableClaudeProjectToggleOnly(t *testing.T) {
	home := isolateMCPEnvironment(t)
	root := t.TempDir()
	library := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".claude"), 0700))
	absolute, err := canonicalClaudeProjectRoot(root)
	requireNoError(t, err)
	path := filepath.Join(home, ".claude.json")
	original := map[string]any{"mcpServers": map[string]any{"global": map[string]any{"command": "global"}}, "projects": map[string]any{absolute: map[string]any{"disabledMcpServers": []string{"previous", "previous"}, "enabledMcpjsonServers": []string{"approval"}, "disabledMcpjsonServers": []string{"denied"}, "enableAllProjectMcpServers": false}, "/other": map[string]any{"disabledMcpServers": []string{"peer"}}}, "unknown": "keep"}
	raw, err := json.Marshal(original)
	requireNoError(t, err)
	requireNoError(t, os.WriteFile(path, raw, 0640))
	writeTypedLibraryMarker(t, filepath.Join(root, ".mcp.json"), `{"mcpServers":{"existing":{}}}`)
	catalog := []CatalogEntry{}
	for _, name := range []string{"previous", "existing", "global", "new-off", "new-on", "ordinary"} {
		entry := CatalogEntry{Type: LibraryTypeMCP, Name: name, Transport: "stdio", Command: "probe"}
		if name != "ordinary" {
			entry.DefaultEnabled = new(name == "new-on")
		}
		catalog = append(catalog, entry)
	}
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, catalog))
	err = withMCPMutationLocks(context.Background(), library, root, func(ctx context.Context, held *heldLocks, catalog []CatalogEntry) error {
		existing, err := captureClaudeMCPEnableState(root)
		if err != nil {
			return err
		}
		// An external Claude change during APM must survive the final re-read.
		original["external"] = "during install"
		raw, err := json.Marshal(original)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0640); err != nil {
			return err
		}
		writeTypedLibraryMarker(t, filepath.Join(root, ".mcp.json"), `{"mcpServers":{"previous":{},"existing":{},"global":{},"new-off":{},"new-on":{},"ordinary":{}}}`)
		return reconcileMCPEnableState(ctx, held, root, mcpEnableSnapshot{ClaudeExisting: existing}, newMCPInstallPolicy(catalog, nil, false), true)
	})
	requireNoError(t, err)
	var got map[string]any
	requireNoError(t, json.Unmarshal([]byte(readFile(t, path)), &got))
	projects := got["projects"].(map[string]any)
	project := projects[absolute].(map[string]any)
	requireEqual(t, []any{"previous", "previous", "new-off"}, project["disabledMcpServers"].([]any))
	for _, key := range []string{"enabledMcpjsonServers", "disabledMcpjsonServers", "enableAllProjectMcpServers"} {
		expected, _ := json.Marshal(original["projects"].(map[string]any)[absolute].(map[string]any)[key])
		actual, _ := json.Marshal(project[key])
		if !bytes.Equal(expected, actual) {
			t.Fatalf("approval key %s changed", key)
		}
	}
	requireEqual(t, "during install", got["external"])
	requireEqual(t, []any{"peer"}, projects["/other"].(map[string]any)["disabledMcpServers"].([]any))
	requireEqual(t, "global", got["mcpServers"].(map[string]any)["global"].(map[string]any)["command"])
}

func TestMCPEnableNoOpAndSafeIO(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	policy := newMCPInstallPolicy([]CatalogEntry{{Name: "missing", DefaultEnabled: new(false)}}, nil, false)
	requireNoError(t, reconcileOpenCodeMCPEnableState(root, mcpEnableSnapshot{}, policy, true))
	assertPathMissing(t, filepath.Join(root, "opencode.json"))
	path := filepath.Join(root, "opencode.json")
	original := []byte(`{"mcp":{"peer":{"enabled":false}},"unknown":null}`)
	requireNoError(t, os.WriteFile(path, original, 0600))
	beforeRoot, merged, err := captureOpenCodeMCPEnableState(root)
	requireNoError(t, err)
	requireNoError(t, reconcileOpenCodeMCPEnableState(root, mcpEnableSnapshot{OpenCodeRoot: beforeRoot, OpenCodeMerged: merged}, policy, true))
	requireEqual(t, string(original), readFile(t, path))
	mutationTestEventHook = func(event string) {
		if event == "mcp-before-write:"+path {
			requireNoError(t, os.WriteFile(path, []byte(`{"external":true}`), 0600))
		}
	}
	t.Cleanup(func() { mutationTestEventHook = nil })
	err = writeMCPFile(path, original, []byte(`{"edited":true}`), 0600)
	if err == nil || !strings.Contains(err.Error(), "MCP enable state changed before writing") {
		t.Fatalf("conflict: %v", err)
	}
	requireEqual(t, `{"external":true}`, readFile(t, path))
	mutationTestEventHook = nil
	linkDir := filepath.Join(root, ".codex")
	requireNoError(t, os.Symlink(t.TempDir(), linkDir))
	_, _, err = readMCPFile(filepath.Join(linkDir, "config.toml"), true)
	if err == nil || !strings.Contains(err.Error(), "refusing to replace symlinked MCP configuration") {
		t.Fatalf("metadata symlink: %v", err)
	}
}

func TestMCPEnableSharedClaudeStateSerializesProjects(t *testing.T) {
	home := isolateMCPEnvironment(t)
	library := t.TempDir()
	catalog := []CatalogEntry{{Type: LibraryTypeMCP, Name: "alpha", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}, {Type: LibraryTypeMCP, Name: "beta", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}}
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, catalog))
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		requireNoError(t, os.Mkdir(filepath.Join(root, ".claude"), 0700))
	}
	statePath := filepath.Join(home, ".claude.json")
	requireNoError(t, os.WriteFile(statePath, []byte(`{"unknown":{"keep":true},"projects":{"/peer":{"disabledMcpServers":["user"]}}}`), 0600))
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	contended := make(chan struct{}, 1)
	mutationTestEventHook = func(event string) {
		if event == "lock-contended:"+filepath.Join(home, lockFileName) {
			select {
			case contended <- struct{}{}:
			default:
			}
		}
	}
	t.Cleanup(func() { mutationTestEventHook = nil })
	results := make(chan error, 2)
	run := func(index int, name string) {
		results <- withMCPMutationLocks(context.Background(), library, roots[index], func(ctx context.Context, held *heldLocks, catalog []CatalogEntry) error {
			if err := releaseMCPLibraryLock(ctx, held, library, roots[index]); err != nil {
				return err
			}
			if index == 0 {
				close(firstEntered)
				<-releaseFirst
			} else {
				close(secondEntered)
			}
			before, err := captureClaudeMCPEnableState(roots[index])
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{name: map[string]any{"command": "probe"}}})
			if err := os.WriteFile(filepath.Join(roots[index], ".mcp.json"), raw, 0600); err != nil {
				return err
			}
			return reconcileClaudeMCPEnableState(ctx, held, roots[index], mcpEnableSnapshot{ClaudeExisting: before}, newMCPInstallPolicy(catalog, nil, false))
		})
	}
	go run(0, "alpha")
	<-firstEntered
	go run(1, "beta")
	<-contended
	select {
	case <-secondEntered:
		t.Fatal("second project entered while shared state lock was held")
	default:
	}
	close(releaseFirst)
	requireNoError(t, <-results)
	requireNoError(t, <-results)
	mutationTestEventHook = nil
	var state map[string]any
	requireNoError(t, json.Unmarshal([]byte(readFile(t, statePath)), &state))
	projects := state["projects"].(map[string]any)
	for index, name := range []string{"alpha", "beta"} {
		absolute, err := canonicalClaudeProjectRoot(roots[index])
		requireNoError(t, err)
		requireEqual(t, []any{name}, projects[absolute].(map[string]any)["disabledMcpServers"].([]any))
	}
	requireEqual(t, true, state["unknown"].(map[string]any)["keep"])
	requireEqual(t, []any{"user"}, projects["/peer"].(map[string]any)["disabledMcpServers"].([]any))
}

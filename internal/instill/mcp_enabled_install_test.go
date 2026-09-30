package instill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tomledit "github.com/smm-h/go-toml-edit"
)

func mcpNativeTestProject(t *testing.T, catalog []CatalogEntry) (Project, string) {
	t.Helper()
	library := t.TempDir()
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, catalog))
	manifest := APMManifest{Name: "project", Version: "0.1.0", Targets: []string{"claude", "codex", "opencode"}}
	for _, entry := range catalog {
		manifest.Dependencies.MCP = append(manifest.Dependencies.MCP, mcpDependencyFromCatalog(entry))
	}
	project := createAPMProject(t, manifest)
	for _, dir := range []string{".claude", ".codex", ".opencode", ".pi"} {
		requireNoError(t, os.MkdirAll(filepath.Join(project.Root, dir), 0700))
	}
	return project, library
}

// Emulates the destructive native entry replacement performed by APM, not a
// command-argument echo. The assertions below inspect repaired on-disk state.
func rewritingMCPRunner(t *testing.T, root string) CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		if name != "apm" {
			return nil, fmt.Errorf("unexpected executable %s", name)
		}
		if len(args) > 0 && args[0] == "--version" {
			return []byte("0.32.0"), nil
		}
		if len(args) == 0 || args[0] != "install" {
			return nil, nil
		}
		manifest, err := ReadAPMManifest(ProjectAPMPath(root))
		if err != nil {
			return nil, err
		}
		opencode := map[string]any{}
		claude := map[string]any{}
		var codex strings.Builder
		codex.WriteString("# generated connection config\n[unrelated]\nkeep = true\n")
		for _, dependency := range manifest.Dependencies.MCP {
			command := "installed-" + dependency.Name
			opencode[dependency.Name] = map[string]any{"type": "local", "command": []string{command}, "enabled": true}
			claude[dependency.Name] = map[string]any{"command": command}
			fmt.Fprintf(&codex, "\n[mcp_servers.%q]\ncommand = %q\nenabled = true\nrequired = true\n", dependency.Name, command)
		}
		writePiTestJSON(t, filepath.Join(root, "opencode.json"), map[string]any{"mcp": opencode, "unknown": "post-install"})
		writePiTestJSON(t, filepath.Join(root, ".mcp.json"), map[string]any{"mcpServers": claude})
		if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte(codex.String()), 0600); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func assertMCPFlag(t *testing.T, state map[string]mcpEnableValue, name string, present bool, value string) {
	t.Helper()
	got, exists := state[name]
	if !exists || got.Present != present || present && string(got.Value) != value {
		t.Fatalf("%s state = %#v, exists=%v; want present=%v value=%s", name, got, exists, present, value)
	}
}

func TestMCPEnableSyncIndependentChoicesAndRecreation(t *testing.T) {
	home := isolateMCPEnvironment(t)
	catalog := []CatalogEntry{{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "updated", DefaultEnabled: new(true)}, {Type: LibraryTypeMCP, Name: "omitted", Transport: "stdio", Command: "updated", DefaultEnabled: new(false)}, {Type: LibraryTypeMCP, Name: "absent", Transport: "stdio", Command: "updated", DefaultEnabled: new(false)}, {Type: LibraryTypeMCP, Name: "ordinary", Transport: "stdio", Command: "updated"}, {Type: LibraryTypeMCP, Name: "unclaimed", Transport: "stdio", Command: "updated", DefaultEnabled: new(false)}}
	project, library := mcpNativeTestProject(t, catalog)
	writePiTestJSON(t, filepath.Join(project.Root, "opencode.json"), map[string]any{"mcp": map[string]any{"probe": map[string]any{"enabled": false}, "omitted": map[string]any{}}})
	requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".codex", "config.toml"), []byte("[mcp_servers.probe]\ncommand=\"old\"\nenabled=true\n[mcp_servers.omitted]\ncommand=\"old\"\n"), 0600))
	writePiTestJSON(t, filepath.Join(project.Root, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"probe": map[string]any{}, "omitted": map[string]any{}}})
	absolute, err := canonicalClaudeProjectRoot(project.Root)
	requireNoError(t, err)
	statePath := filepath.Join(home, ".claude.json")
	writePiTestJSON(t, statePath, map[string]any{"projects": map[string]any{absolute: map[string]any{"disabledMcpServers": []string{"probe"}, "enabledMcpjsonServers": []string{"approval"}}}, "unknown": "keep"})
	writePiTestJSON(t, piProjectMCPPath(project.Root), map[string]any{"mcpServers": map[string]any{"probe": map[string]any{"command": "old", "disabled": false, "directTools": []string{"tool"}}, "omitted": map[string]any{"command": "old"}, "unclaimed": map[string]any{"command": "user", "disabled": true}}, piManagedKey: []string{"omitted", "probe"}})
	sync := func() {
		requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: rewritingMCPRunner(t, project.Root), Stdout: &bytes.Buffer{}}))
	}
	inspect := func() {
		oc, cx, err := func() (map[string]mcpEnableValue, map[string]mcpEnableValue, error) {
			data, _, err := readMCPFile(filepath.Join(project.Root, "opencode.json"), false)
			if err != nil {
				return nil, nil, err
			}
			oc, err := projectMCPJSONState("output", data, "mcp", "enabled")
			if err != nil {
				return nil, nil, err
			}
			cx, _, err := captureCodexMCPEnableState(project.Root)
			return oc, cx, err
		}()
		requireNoError(t, err)
		assertMCPFlag(t, oc, "probe", true, "false")
		assertMCPFlag(t, cx, "probe", true, "true")
		assertMCPFlag(t, oc, "omitted", false, "")
		assertMCPFlag(t, cx, "omitted", false, "")
		assertMCPFlag(t, oc, "absent", true, "false")
		assertMCPFlag(t, cx, "absent", true, "false")
		assertMCPFlag(t, cx, "ordinary", true, "true") // APM's ordinary behavior remains untouched.
		object, _, _, err := piReadObject(piProjectMCPPath(project.Root), false)
		requireNoError(t, err)
		_, servers, err := piServerMap("pi", object)
		requireNoError(t, err)
		probe, err := mcpJSONObject("pi", servers["probe"], "probe")
		requireNoError(t, err)
		requireEqual(t, "false", string(probe["disabled"]))
		requireEqual(t, `"updated"`, string(probe["command"]))
		var directTools []string
		requireNoError(t, json.Unmarshal(probe["directTools"], &directTools))
		requireEqual(t, []string{"tool"}, directTools)
		omitted, err := mcpJSONObject("pi", servers["omitted"], "omitted")
		requireNoError(t, err)
		if _, exists := omitted["disabled"]; exists {
			t.Fatal("Pi omitted flag gained default")
		}
		ordinary, err := mcpJSONObject("pi", servers["ordinary"], "ordinary")
		requireNoError(t, err)
		if _, exists := ordinary["disabled"]; exists {
			t.Fatal("Pi nil default gained flag")
		}
		unclaimed, err := mcpJSONObject("pi", servers["unclaimed"], "unclaimed")
		requireNoError(t, err)
		requireEqual(t, `"user"`, string(unclaimed["command"]))
		managed, err := piManagedNames("pi", object)
		requireNoError(t, err)
		if slices.Contains(managed, "unclaimed") {
			t.Fatal("user server claimed")
		}
		data, _, err := readMCPFile(statePath, false)
		requireNoError(t, err)
		_, disabled, err := claudeApplicableState(statePath, data, absolute)
		requireNoError(t, err)
		requireEqual(t, []string{"probe", "absent", "unclaimed"}, disabled)
		var state map[string]any
		requireNoError(t, json.Unmarshal(data, &state))
		requireEqual(t, "keep", state["unknown"])
		doc, err := tomledit.Parse([]byte(readFile(t, filepath.Join(project.Root, ".codex", "config.toml"))))
		requireNoError(t, err)
		required, err := doc.GetBool(tomledit.JoinPath([]tomledit.PathSegment{{Kind: tomledit.SegmentKey, Key: "mcp_servers"}, {Kind: tomledit.SegmentKey, Key: "probe"}, {Kind: tomledit.SegmentKey, Key: "required"}}))
		requireNoError(t, err)
		if !required {
			t.Fatal("post-install unrelated server setting lost")
		}
	}
	sync()
	inspect()
	for i := range catalog {
		if catalog[i].DefaultEnabled != nil {
			catalog[i].DefaultEnabled = new(!*catalog[i].DefaultEnabled)
		}
	}
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, catalog))
	sync()
	inspect()
	object, _, _, err := piReadObject(piProjectMCPPath(project.Root), false)
	requireNoError(t, err)
	var pi map[string]any
	requireNoError(t, json.Unmarshal(piRaw(object), &pi))
	delete(pi["mcpServers"].(map[string]any), "probe")
	writePiTestJSON(t, piProjectMCPPath(project.Root), pi)
	sync()
	data, _, err := readMCPFile(piProjectMCPPath(project.Root), false)
	requireNoError(t, err)
	state, err := projectMCPJSONState("pi", data, "mcpServers", "disabled")
	requireNoError(t, err)
	assertMCPFlag(t, state, "probe", true, "true")
	cx, _, err := captureCodexMCPEnableState(project.Root)
	requireNoError(t, err)
	assertMCPFlag(t, cx, "probe", true, "true")
}

func TestMCPEnablePartialAPMFailure(t *testing.T) {
	for _, breakRestore := range []bool{false, true} {
		t.Run(fmt.Sprint(breakRestore), func(t *testing.T) {
			home := isolateMCPEnvironment(t)
			catalog := []CatalogEntry{{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "updated", DefaultEnabled: new(true)}, {Type: LibraryTypeMCP, Name: "new", Transport: "stdio", Command: "updated", DefaultEnabled: new(false)}}
			project, library := mcpNativeTestProject(t, catalog)
			writePiTestJSON(t, filepath.Join(project.Root, "opencode.json"), map[string]any{"mcp": map[string]any{"probe": map[string]any{"enabled": false}}})
			requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".codex", "config.toml"), []byte("[mcp_servers.probe]\nenabled=false\n"), 0600))
			piBefore := `{"mcpServers":{"probe":{"command":"old","disabled":false}},"_instillManagedServers":["probe"]}`
			requireNoError(t, os.WriteFile(piProjectMCPPath(project.Root), []byte(piBefore), 0600))
			normal := rewritingMCPRunner(t, project.Root)
			runner := func(name string, args ...string) ([]byte, error) {
				output, err := normal(name, args...)
				if len(args) > 0 && args[0] == "install" {
					if breakRestore {
						requireNoError(t, os.WriteFile(filepath.Join(project.Root, "opencode.json"), []byte("{"), 0600))
					}
					return output, errors.New("partial install failed")
				}
				return output, err
			}
			err := SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}})
			if err == nil || !strings.Contains(err.Error(), "partial install failed") {
				t.Fatalf("install failure lost: %v", err)
			}
			expected := ExitGeneral
			if breakRestore {
				expected = ExitFilesystem
				if !strings.Contains(err.Error(), "restoration failed") || !strings.Contains(err.Error(), "opencode.json") {
					t.Fatalf("combined restoration failure = %v", err)
				}
			}
			requireEqual(t, expected, ExitCode(err))
			cx, _, err := captureCodexMCPEnableState(project.Root)
			requireNoError(t, err)
			assertMCPFlag(t, cx, "probe", true, "false")
			assertMCPFlag(t, cx, "new", true, "true")
			if !breakRestore {
				data, _, err := readMCPFile(filepath.Join(project.Root, "opencode.json"), false)
				requireNoError(t, err)
				oc, err := projectMCPJSONState("oc", data, "mcp", "enabled")
				requireNoError(t, err)
				assertMCPFlag(t, oc, "probe", true, "false")
				assertMCPFlag(t, oc, "new", true, "true")
			}
			requireEqual(t, piBefore, readFile(t, piProjectMCPPath(project.Root)))
			assertPathMissing(t, filepath.Join(home, ".claude.json"))
		})
	}
}

func TestMCPEnableMalformedInputPreventsInstall(t *testing.T) {
	for _, path := range []string{"opencode.json", ".codex/config.toml", ".mcp.json", ".pi/mcp-adapter.json"} {
		t.Run(path, func(t *testing.T) {
			isolateMCPEnvironment(t)
			project, library := mcpNativeTestProject(t, []CatalogEntry{{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}})
			target := filepath.Join(project.Root, filepath.FromSlash(path))
			original := []byte("{")
			if strings.HasSuffix(path, ".toml") {
				original = []byte("[mcp_servers.probe]\nenabled=\"bad\"\n")
			}
			if strings.HasSuffix(path, "mcp-adapter.json") {
				original = []byte(`{"_instillManagedServers":null}`)
			}
			requireNoError(t, os.WriteFile(target, original, 0600))
			started := false
			runner := func(name string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "--version" {
					return []byte("0.32.0"), nil
				}
				started = true
				return nil, nil
			}
			err := SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}})
			if err == nil || started {
				t.Fatalf("malformed input reached APM: started=%v error=%v", started, err)
			}
			requireEqual(t, string(original), readFile(t, target))
		})
	}
}

func TestMCPEnableInstallEntrypoints(t *testing.T) {
	for _, operation := range []string{"standalone", "sync", "skill", "plugin", "instruction", "prompt", "selection", "init"} {
		t.Run(operation, func(t *testing.T) {
			isolateMCPEnvironment(t)
			foreign := CatalogEntry{Type: LibraryTypeMCP, Name: "foreign", Transport: "stdio", Command: "old"}
			project, library := mcpNativeTestProject(t, []CatalogEntry{foreign})
			requireNoError(t, WriteCatalog(library, LibraryTypeMCP, nil)) // Existing unowned dependency is still installed by APM.
			writePiTestJSON(t, filepath.Join(project.Root, "opencode.json"), map[string]any{"mcp": map[string]any{"foreign": map[string]any{"enabled": false}}})
			requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".codex", "config.toml"), []byte("[mcp_servers.foreign]\nenabled=false\n"), 0600))
			writeTypedLibraryMarker(t, filepath.Join(library, "skills", "skill", "SKILL.md"), "---\nname: skill\ndescription: fixture\n---\ncontent")
			writeTypedLibraryMarker(t, filepath.Join(library, "plugins", "plugin", "plugin.json"), `{"name":"plugin"}`)
			writeTypedLibraryMarker(t, filepath.Join(library, "instructions", "instruction", "INSTRUCTION.md"), "content")
			writeTypedLibraryMarker(t, filepath.Join(library, "prompts", "prompt", "PROMPT.md"), "content")
			requireNoError(t, ScanLibrary(library, nil))
			runner := rewritingMCPRunner(t, project.Root)
			var err error
			switch operation {
			case "standalone":
				err = RunAPMInstall(runner, project.Root)
			case "sync":
				err = SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}})
			case "selection":
				err = ApplySkillSelection(SkillSelectionOptions{Project: project, LibraryPath: library, Skills: []string{"skill"}, Runner: runner})
			case "init":
				err = InitProject(InitProjectOptions{Root: project.Root, LibraryPath: library, Skills: []string{"skill"}, Force: true, Runner: runner})
			default:
				err = Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryType(operation), Add: []string{operation}, Runner: runner})
			}
			requireNoError(t, err)
			data, _, err := readMCPFile(filepath.Join(project.Root, "opencode.json"), false)
			requireNoError(t, err)
			state, err := projectMCPJSONState("oc", data, "mcp", "enabled")
			requireNoError(t, err)
			assertMCPFlag(t, state, "foreign", true, "false")
			if !bytes.Contains(data, []byte("installed-foreign")) {
				t.Fatal("old connection file restored instead of only its flag")
			}
			cx, _, err := captureCodexMCPEnableState(project.Root)
			requireNoError(t, err)
			assertMCPFlag(t, cx, "foreign", true, "false")
		})
	}
}

func TestPiMCPExclusivePreflightAndRemovalOnlyInit(t *testing.T) {
	isolateMCPEnvironment(t)
	catalog := []CatalogEntry{{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}}
	project, library := mcpNativeTestProject(t, catalog)
	t.Setenv("PI_MCP_CONFIG_MODE", " ExClUsIvE ")
	started := false
	runner := func(name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "--version" {
			return []byte("0.32.0"), nil
		}
		started = true
		return nil, nil
	}
	err := SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}})
	if err == nil || ExitCode(err) != ExitEnvironment || started {
		t.Fatalf("exclusive absence preflight = %v, started=%v", err, started)
	}
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	writePiTestJSON(t, piProjectMCPPath(project.Root), map[string]any{"mcpServers": map[string]any{"probe": map[string]any{"command": "probe"}, "user": map[string]any{"command": "user"}}, piManagedKey: []string{"probe"}})
	err = InitProject(InitProjectOptions{Root: project.Root, LibraryPath: library, Force: true, Runner: runner})
	requireNoError(t, err)
	if started {
		t.Fatal("removal-only init invoked APM without package dependencies")
	}
	object, _, _, err := piReadObject(piProjectMCPPath(project.Root), false)
	requireNoError(t, err)
	_, servers, err := piServerMap("pi", object)
	requireNoError(t, err)
	if _, exists := servers["probe"]; exists {
		t.Fatal("reinitialization orphaned owned Pi entry")
	}
	if _, exists := servers["user"]; !exists {
		t.Fatal("reinitialization removed user entry")
	}
}

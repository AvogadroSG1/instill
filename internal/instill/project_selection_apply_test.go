package instill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyProjectSelectionReplacesManifestAndProjectOutputs(t *testing.T) {
	home := isolateMCPEnvironment(t)
	library := overwriteTestLibrary(t, []CatalogEntry{
		{Type: LibraryTypeSkill, Name: "chosen-skill", Path: "chosen-skill/SKILL.md", Description: "selected"},
		{Type: LibraryTypeSkill, Name: "stale-skill", Path: "stale-skill/SKILL.md", Description: "not selected"},
		{Type: LibraryTypePlugin, Name: "chosen-plugin", Path: "chosen-plugin/.claude-plugin/plugin.json", Description: "selected"},
		{Type: LibraryTypePlugin, Name: "stale-plugin", Path: "stale-plugin/.claude-plugin/plugin.json", Description: "not selected"},
		{Type: LibraryTypeMCP, Name: "chosen-server", Transport: "stdio", Command: "/usr/bin/true", Args: []string{"chosen"}},
		{Type: LibraryTypeMCP, Name: "stale-server", Transport: "stdio", Command: "/usr/bin/true", Args: []string{"stale"}},
		{Type: LibraryTypeInstruction, Name: "chosen-rule", Path: "chosen-rule/INSTRUCTION.md", ApplyTo: "**", Description: "selected"},
		{Type: LibraryTypeInstruction, Name: "stale-rule", Path: "stale-rule/INSTRUCTION.md", ApplyTo: "**", Description: "not selected"},
		{Type: LibraryTypePrompt, Name: "chosen-prompt", Path: "chosen-prompt/PROMPT.md", Description: "selected"},
		{Type: LibraryTypePrompt, Name: "stale-prompt", Path: "stale-prompt/PROMPT.md", Description: "not selected"},
	})
	project := overwriteTestProject(t, []byte(`name: overwrite-test
version: 0.1.0
targets: [claude, codex, opencode]
dependencies:
  apm:
    - ../foreign-skill
    - git: https://example.invalid/foreign.git
      path: package
      ref: main
  mcp:
    - name: foreign-server
      transport: stdio
      command: /old/foreign
`))
	writeOverwriteJSON(t, filepath.Join(project.Root, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{"foreign-server": map[string]any{"command": "/old/foreign"}, "manual": map[string]any{"command": "/manual"}},
		"unrelated":  "preserve-claude-setting",
	})
	writeOverwriteFile(t, filepath.Join(project.Root, ".codex", "config.toml"), []byte("[mcp_servers.\"chosen-server\"]\ncommand = \"/old\"\nenabled = false\n\n[mcp_servers.manual]\ncommand = \"/manual\"\nenabled = true\n"), 0600)
	writeOverwriteJSON(t, filepath.Join(project.Root, "opencode.json"), map[string]any{
		"mcp": map[string]any{
			"foreign-server": map[string]any{"command": []string{"old"}},
			"chosen-server":  map[string]any{"command": []string{"old"}, "enabled": true},
			"manual":         map[string]any{"command": []string{"manual"}},
		},
		"plugins":   []string{"manual-plugin.ts"},
		"unrelated": map[string]any{"preserve": true},
	})
	writeOverwriteJSON(t, filepath.Join(project.Root, "opencode.jsonc"), map[string]any{
		"mcp": map[string]any{"chosen-server": map[string]any{"enabled": false}},
	})
	for path, content := range map[string]string{
		".agents/skills/stale-skill/SKILL.md":     "old skill",
		".agents/skills/manual/SKILL.md":          "manual skill",
		".claude/skills/manual/SKILL.md":          "manual Claude skill",
		".cursor/commands/manual.md":              "deselected target output",
		".apm/instructions/stale.instructions.md": "old instruction",
		".apm/prompts/stale.prompt.md":            "old prompt",
		"apm_modules/_local/foreign/old-file":     "pruned package",
	} {
		writeOverwriteFile(t, filepath.Join(project.Root, path), []byte(content), 0644)
	}
	globalSentinels := map[string][]byte{
		filepath.Join(home, ".claude.json"):                            []byte(`{"global":"claude-sentinel"}`),
		filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"): []byte(`{"global":"windsurf-sentinel"}`),
	}
	for path, content := range globalSentinels {
		writeOverwriteFile(t, path, content, 0600)
	}

	var output bytes.Buffer
	runner := newOverwriteStatefulRunner(t, project.Root, library)
	err := ApplyProjectSelection(ProjectSelectionOptions{
		Project: project, LibraryPath: library,
		Selection: ProjectSelection{
			Skills: []string{"chosen-skill"}, Plugins: []string{"chosen-plugin"}, MCPs: []string{"chosen-server"},
			Instructions: []string{"chosen-rule"}, Prompts: []string{"chosen-prompt"},
		},
		Runner: runner.Run, Stdout: &output,
	})
	if err != nil {
		t.Fatalf("ApplyProjectSelection() error = %v", err)
	}
	if got, want := output.String(), "ok: replaced project selection and reconciled targets\n"; got != want {
		t.Fatalf("success output = %q, want %q", got, want)
	}

	manifest, err := ReadAPMManifest(ProjectAPMPath(project.Root))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dependencies.APM) != 2 || manifest.Dependencies.APM[0].Local != filepath.Join(library, "skills", "chosen-skill") || manifest.Dependencies.APM[1].Local != filepath.Join(library, "plugins", "chosen-plugin") {
		t.Fatalf("replacement packages = %#v; want only selected Skill and Plugin paths", manifest.Dependencies.APM)
	}
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "chosen-server" {
		t.Fatalf("replacement MCPs = %#v; want only chosen-server", manifest.Dependencies.MCP)
	}
	for _, path := range []string{
		".agents/skills/stale-skill", ".agents/skills/manual", ".claude/skills/manual", ".cursor/commands/manual.md",
		".apm/instructions/stale.instructions.md", ".apm/prompts/stale.prompt.md", "apm_modules/_local/foreign",
	} {
		if _, err := os.Lstat(filepath.Join(project.Root, path)); !os.IsNotExist(err) {
			t.Errorf("stale project output %s remains (Lstat error %v)", path, err)
		}
	}
	for path, want := range map[string]string{
		".agents/skills/chosen-skill/SKILL.md":          "# chosen-skill",
		".claude/skills/chosen-skill/SKILL.md":          "# chosen-skill",
		".apm/instructions/chosen-rule.instructions.md": "---\napplyTo: \"**\"\n---\n# chosen-rule",
		".apm/prompts/chosen-prompt.prompt.md":          "---\ndescription: selected prompt\n---\n# chosen-prompt",
	} {
		data, err := os.ReadFile(filepath.Join(project.Root, path))
		if err != nil || string(data) != want {
			t.Errorf("selected output %s = %q, %v; want %q", path, data, err, want)
		}
	}
	generated, err := os.ReadFile(filepath.Join(project.Root, "AGENTS.md"))
	if err != nil || !bytes.HasPrefix(generated, []byte("<!-- Generated by APM CLI from .apm/ primitives -->")) {
		t.Errorf("compiled project context = %q, err %v", generated, err)
	}
	claude := readOverwriteJSON(t, filepath.Join(project.Root, ".mcp.json"))
	if got := sortedJSONMapKeys(t, claude["mcpServers"]); !slices.Equal(got, []string{"chosen-server"}) {
		t.Errorf("Claude MCP names = %v, want [chosen-server]", got)
	}
	if claude["unrelated"] != "preserve-claude-setting" {
		t.Errorf("unrelated Claude config = %#v", claude["unrelated"])
	}
	opencode := readOverwriteJSON(t, filepath.Join(project.Root, "opencode.json"))
	if got := sortedJSONMapKeys(t, opencode["mcp"]); !slices.Equal(got, []string{"chosen-server"}) {
		t.Errorf("OpenCode MCP names = %v, want [chosen-server]", got)
	}
	chosenOpenCode := jsonObject(t, jsonObject(t, opencode["mcp"])["chosen-server"])
	if got := chosenOpenCode["enabled"]; got != false {
		t.Errorf("OpenCode selected server enabled = %#v, want effective later project-layer value false", got)
	}
	codexDocument, _, err := readCodexDocument(filepath.Join(project.Root, ".codex", "config.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	codexState, err := codexDocumentEnableState(filepath.Join(project.Root, ".codex", "config.toml"), codexDocument)
	if err != nil {
		t.Fatal(err)
	}
	if got := codexState["chosen-server"]; !got.Present || string(got.Value) != "false" || len(codexState) != 1 {
		t.Errorf("Codex selected MCP state = %#v, want only chosen-server enabled=false", codexState)
	}
	if got := opencode["plugins"]; fmt.Sprint(got) != "[]" {
		t.Errorf("OpenCode plugins = %#v, want empty array", got)
	}
	if got := opencode["unrelated"]; fmt.Sprint(got) != "map[preserve:true]" {
		t.Errorf("unrelated OpenCode config = %#v", got)
	}
	for path, want := range globalSentinels {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("user-global sentinel %s changed: got %q, err %v", path, got, err)
		}
	}
}

func TestApplyProjectSelectionFailureAfterPublicationKeepsDesiredManifest(t *testing.T) {
	isolateMCPEnvironment(t)
	library := overwriteTestLibrary(t, []CatalogEntry{
		{Type: LibraryTypeMCP, Name: "new-server", Transport: "stdio", Command: "/usr/bin/true", Args: []string{"new"}},
	})
	project := overwriteTestProject(t, []byte(`name: retry-test
version: 0.1.0
targets: [claude]
dependencies:
  apm: [../foreign]
  mcp:
    - name: old-server
      transport: stdio
      command: /old
`))
	var output bytes.Buffer
	runner := newOverwriteStatefulRunner(t, project.Root, library)
	runner.failAt = "prune"
	err := ApplyProjectSelection(ProjectSelectionOptions{
		Project: project, LibraryPath: library,
		Selection: ProjectSelection{MCPs: []string{"new-server"}},
		Runner:    runner.Run, Stdout: &output,
	})
	if err == nil || !strings.Contains(err.Error(), "injected prune failure") {
		t.Fatalf("ApplyProjectSelection() error = %v; want injected post-publication prune failure", err)
	}
	if output.Len() != 0 || strings.Contains(output.String(), "ok: replaced project selection") {
		t.Fatalf("failure printed success output %q", output.String())
	}
	manifest, readErr := ReadAPMManifest(ProjectAPMPath(project.Root))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(manifest.Dependencies.APM) != 0 || len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "new-server" {
		t.Fatalf("published manifest = %#v; want only new-server and no package dependencies", manifest.Dependencies)
	}
}

func TestApplyProjectSelectionReconcilesPiRootAndPluginMCPs(t *testing.T) {
	isolateMCPEnvironment(t)
	library := overwriteTestLibrary(t, []CatalogEntry{
		{Type: LibraryTypePlugin, Name: "pi-plugin", Path: "pi-plugin/.claude-plugin/plugin.json", Description: "Pi package"},
		{Type: LibraryTypeMCP, Name: "root-server", Transport: "stdio", Command: "/usr/bin/true", Args: []string{"root"}, DefaultEnabled: new(true)},
	})
	packageManifest := []byte(`name: pi-plugin
version: 1.0.0
dependencies:
  mcp:
    - name: plugin-server
      transport: stdio
      command: /usr/bin/true
      args: [plugin]
`)
	writeOverwriteFile(t, filepath.Join(library, "plugins", "pi-plugin", "apm.yml"), packageManifest, 0644)
	project := overwriteTestProject(t, []byte(`name: pi-overwrite
version: 0.1.0
targets: [agent-skills]
dependencies:
  apm: [../foreign]
  mcp:
    - name: obsolete-server
      transport: stdio
      command: /old
`))
	if err := os.MkdirAll(filepath.Join(project.Root, ".pi"), 0755); err != nil {
		t.Fatal(err)
	}
	writeOverwriteJSON(t, piProjectMCPPath(project.Root), map[string]any{
		"mcpServers": map[string]any{
			"root-server":   map[string]any{"command": "/old-root", "args": []string{"old"}, "disabled": true, "directTools": []string{"search"}, "note": "root user metadata"},
			"plugin-server": map[string]any{"command": "/old-plugin", "args": []string{"old"}, "disabled": true, "directTools": []string{"inspect"}, "note": "plugin user metadata"},
			"manual-server": map[string]any{"command": "/manual", "args": []string{}, "disabled": false},
		},
		piManagedKey: []string{"manual-server"},
		"unrelated":  map[string]any{"keep": true},
	})

	var output bytes.Buffer
	runner := newOverwriteStatefulRunner(t, project.Root, library)
	if err := ApplyProjectSelection(ProjectSelectionOptions{
		Project: project, LibraryPath: library,
		Selection: ProjectSelection{Plugins: []string{"pi-plugin"}, MCPs: []string{"root-server"}},
		Runner:    runner.Run, Stdout: &output,
	}); err != nil {
		t.Fatalf("ApplyProjectSelection() error = %v", err)
	}
	pi := readOverwriteJSON(t, piProjectMCPPath(project.Root))
	servers := jsonObject(t, pi["mcpServers"])
	if got := sortedJSONMapKeys(t, servers); !slices.Equal(got, []string{"plugin-server", "root-server"}) {
		t.Fatalf("Pi MCP names = %v; want only root and selected Plugin servers", got)
	}
	for name, note := range map[string]string{"root-server": "root user metadata", "plugin-server": "plugin user metadata"} {
		server := jsonObject(t, servers[name])
		if server["command"] != "/usr/bin/true" {
			t.Errorf("Pi %s command = %#v", name, server["command"])
		}
		if server["disabled"] != true || server["note"] != note {
			t.Errorf("Pi %s user-owned metadata = %#v", name, server)
		}
		if len(jsonStringSlice(t, server["directTools"])) != 1 {
			t.Errorf("Pi %s directTools not preserved: %#v", name, server["directTools"])
		}
	}
	if got := jsonStringSlice(t, pi[piManagedKey]); !slices.Equal(got, []string{"plugin-server", "root-server"}) {
		t.Errorf("managed Pi names = %v; want exact sorted selected names", got)
	}
	if got := jsonObject(t, pi["unrelated"])["keep"]; got != true {
		t.Errorf("unrelated Pi config = %#v", pi["unrelated"])
	}
	manifest, err := ReadAPMManifest(ProjectAPMPath(project.Root))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "root-server" {
		t.Fatalf("root manifest MCPs include package-derived MCPs: %#v", manifest.Dependencies.MCP)
	}
}

type overwriteStatefulRunner struct {
	t       *testing.T
	root    string
	library string
	failAt  string
}

func newOverwriteStatefulRunner(t *testing.T, root, library string) *overwriteStatefulRunner {
	t.Helper()
	return &overwriteStatefulRunner{t: t, root: root, library: library}
}

func (r *overwriteStatefulRunner) Run(name string, args ...string) ([]byte, error) {
	if name != "apm" {
		return nil, fmt.Errorf("unexpected executable %q", name)
	}
	if len(args) == 1 && args[0] == "--version" {
		return []byte("apm 0.35.0\n"), nil
	}
	if len(args) == 0 {
		return nil, errors.New("missing APM command")
	}
	if r.failAt == args[0] {
		return nil, fmt.Errorf("injected %s failure", args[0])
	}
	switch args[0] {
	case "install":
		return nil, r.installFromManifest()
	case "prune":
		return nil, r.pruneUnselectedPackages()
	case "compile":
		return nil, r.compileProject()
	default:
		return nil, fmt.Errorf("unexpected APM command %q", args[0])
	}
}
func (r *overwriteStatefulRunner) pruneUnselectedPackages() error {
	manifest, err := ReadAPMManifest(ProjectAPMPath(r.root))
	if err != nil {
		return err
	}
	wanted := make(map[string]struct{}, len(manifest.Dependencies.APM))
	for _, dependency := range manifest.Dependencies.APM {
		if dependency.Local != "" {
			wanted[filepath.Base(filepath.Clean(dependency.Local))] = struct{}{}
		}
	}
	root := filepath.Join(r.root, "apm_modules", "_local")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, keep := wanted[entry.Name()]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (r *overwriteStatefulRunner) compileProject() error {
	content := []byte("<!-- Generated by APM CLI from .apm/ primitives -->\n\n# Compiled project context\n")
	return os.WriteFile(filepath.Join(r.root, "AGENTS.md"), content, 0644)
}

func (r *overwriteStatefulRunner) installFromManifest() error {
	manifest, err := ReadAPMManifest(ProjectAPMPath(r.root))
	if err != nil {
		return err
	}
	lockDependencies := make([]map[string]any, 0, len(manifest.Dependencies.APM))
	installedRoots := make(map[string]string, len(manifest.Dependencies.APM))
	for _, dependency := range manifest.Dependencies.APM {
		if dependency.Local == "" {
			return fmt.Errorf("test runner only materializes local packages, got %#v", dependency)
		}
		source := dependency.Local
		if !filepath.IsAbs(source) {
			source = filepath.Join(r.root, source)
		}
		base := filepath.Base(filepath.Clean(source))
		destination := filepath.Join(r.root, "apm_modules", "_local", base)
		if err := copyOverwriteTree(source, destination); err != nil {
			return err
		}
		installedRoots[base] = destination
		packageType := "skill"
		if _, err := os.Stat(filepath.Join(source, ".claude-plugin", "plugin.json")); err == nil {
			packageType = "plugin"
		}
		lockDependencies = append(lockDependencies, map[string]any{
			"name": base, "package_type": packageType, "source": "local", "local_path": source,
			"deployed_files": []string{},
		})
	}

	servers := make(map[string]MCPDependency, len(manifest.Dependencies.MCP))
	for _, dependency := range manifest.Dependencies.MCP {
		servers[dependency.Name] = dependency
	}
	for _, dependency := range manifest.Dependencies.APM {
		root := installedRoots[filepath.Base(filepath.Clean(dependency.Local))]
		manifestPath := ProjectAPMPath(root)
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue // Plain Skills need not be APM packages.
		} else if err != nil {
			return err
		}
		packageManifest, err := ReadAPMManifest(manifestPath)
		if err != nil {
			return err
		}
		for _, server := range packageManifest.Dependencies.MCP {
			if _, exists := servers[server.Name]; !exists {
				servers[server.Name] = server
			}
		}
	}
	if err := r.writeSelectedSkillOutputs(manifest); err != nil {
		return err
	}
	if err := r.writeNativeMCPOutputs(manifest, servers); err != nil {
		return err
	}
	return r.writeFreshLock(manifest, lockDependencies, servers)
}

func (r *overwriteStatefulRunner) writeSelectedSkillOutputs(manifest APMManifest) error {
	for _, dependency := range manifest.Dependencies.APM {
		source := dependency.Local
		if !filepath.IsAbs(source) {
			source = filepath.Join(r.root, source)
		}
		if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
			continue
		}
		name := filepath.Base(filepath.Clean(source))
		for _, rel := range []string{filepath.Join(".agents", "skills", name, "SKILL.md"), filepath.Join(".claude", "skills", name, "SKILL.md")} {
			path := filepath.Join(r.root, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte("# "+name), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *overwriteStatefulRunner) writeNativeMCPOutputs(manifest APMManifest, servers map[string]MCPDependency) error {
	for _, target := range manifest.Targets {
		switch target {
		case "claude":
			entries := make(map[string]any, len(servers))
			for name, dependency := range servers {
				entries[name] = map[string]any{"command": dependency.Command, "args": dependency.Args}
			}
			if err := r.patchJSON(filepath.Join(r.root, ".mcp.json"), "mcpServers", entries); err != nil {
				return err
			}
		case "codex":
			var config strings.Builder
			for name, dependency := range servers {
				fmt.Fprintf(&config, "[mcp_servers.%q]\ncommand = %q\n", name, dependency.Command)
				if dependency.Args != nil {
					args, err := json.Marshal(dependency.Args)
					if err != nil {
						return err
					}
					fmt.Fprintf(&config, "args = %s\n", args)
				}
				config.WriteString("enabled = true\n\n")
			}
			path := filepath.Join(r.root, ".codex", "config.toml")
			if err := os.WriteFile(path, []byte(config.String()), 0600); err != nil {
				return err
			}
		case "opencode":
			entries := make(map[string]any, len(servers))
			for name, dependency := range servers {
				command := append([]string{dependency.Command}, dependency.Args...)
				entries[name] = map[string]any{"type": "local", "command": command, "enabled": true}
			}
			if err := r.patchJSON(filepath.Join(r.root, "opencode.json"), "mcp", entries); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *overwriteStatefulRunner) patchJSON(path, key string, value any) error {
	object := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &object); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	object[key] = value
	encoded, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0644)
}

func (r *overwriteStatefulRunner) writeFreshLock(manifest APMManifest, dependencies []map[string]any, servers map[string]MCPDependency) error {
	mcpNames := make([]string, 0, len(servers))
	configs := make(map[string]any, len(servers))
	for name, server := range servers {
		mcpNames = append(mcpNames, name)
		configs[name] = map[string]any{"transport": server.Transport, "command": server.Command, "args": server.Args}
	}
	slices.Sort(mcpNames)
	targets := make(map[string][]string, len(manifest.Targets))
	for _, target := range manifest.Targets {
		targets[target] = append([]string{}, mcpNames...)
	}
	lock := map[string]any{
		"lockfile_version": "3", "dependencies": dependencies, "mcp_servers": mcpNames,
		"mcp_target_servers": targets, "mcp_configs": configs, "deployments": []any{},
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.root, "apm.lock.yaml"), data, 0644)
}

func overwriteTestLibrary(t *testing.T, entries []CatalogEntry) string {
	t.Helper()
	root := t.TempDir()
	byType := make(map[LibraryType][]CatalogEntry)
	for _, entry := range entries {
		byType[entry.Type] = append(byType[entry.Type], entry)
	}
	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin, LibraryTypeMCP, LibraryTypeInstruction, LibraryTypePrompt} {
		if err := WriteCatalog(root, typ, byType[typ]); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range entries {
		var rel string
		var body string
		switch entry.Type {
		case LibraryTypeSkill:
			rel, body = filepath.Join("skills", filepath.FromSlash(entry.Path)), "# "+entry.Name
		case LibraryTypePlugin:
			rel = filepath.Join("plugins", filepath.FromSlash(entry.Path))
			body = `{"name":"` + entry.Name + `"}`
		case LibraryTypeInstruction:
			rel, body = filepath.Join("instructions", filepath.FromSlash(entry.Path)), "---\napplyTo: \"**\"\n---\n# "+entry.Name
		case LibraryTypePrompt:
			rel, body = filepath.Join("prompts", filepath.FromSlash(entry.Path)), "---\ndescription: selected prompt\n---\n# "+entry.Name
		case LibraryTypeMCP:
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if entry.Type == LibraryTypePlugin {
			pluginRoot := filepath.Dir(filepath.Dir(filepath.Join(root, rel)))
			manifest := []byte("name: " + entry.Name + "\nversion: 1.0.0\ndependencies:\n  mcp: []\n")
			if err := os.WriteFile(filepath.Join(pluginRoot, "apm.yml"), manifest, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func overwriteTestProject(t *testing.T, manifest []byte) Project {
	t.Helper()
	project := createAPMProject(t, APMManifest{Name: "placeholder", Version: "0.1.0"})
	if err := os.WriteFile(ProjectAPMPath(project.Root), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	return project
}

func writeOverwriteFile(t *testing.T, path string, data []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func writeOverwriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeOverwriteFile(t, path, data, 0644)
}

func readOverwriteJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return object
}

func jsonObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("JSON value has type %T, want object", value)
	}
	return object
}

func sortedJSONMapKeys(t *testing.T, value any) []string {
	t.Helper()
	object := jsonObject(t, value)
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func jsonStringSlice(t *testing.T, value any) []string {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("JSON value has type %T, want array", value)
	}
	result := make([]string, 0, len(array))
	for _, item := range array {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("JSON array item has type %T, want string", item)
		}
		result = append(result, text)
	}
	return result
}

func copyOverwriteTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func TestEqualOverwriteMCPDefinitionNormalizesMapsAndRuntimeReferences(t *testing.T) {
	expected := CatalogEntry{
		Transport: "http",
		URL:       "https://example.test/${TOKEN}",
		Args:      []string{"--first", "--second"},
		Env:       []string{"Z=last", "A=first", "TOKEN=${TOKEN}"},
		Headers:   []string{"X-Api-Key=${TOKEN}", "Authorization=Bearer ${TOKEN}"},
	}
	actual := CatalogEntry{
		Transport: "http",
		URL:       "https://example.test/{env:TOKEN}",
		Args:      []string{"--first", "--second"},
		Env:       []string{"TOKEN={env:TOKEN}", "A=first", "Z=last"},
		Headers:   []string{"authorization=Bearer ${env:TOKEN}", "X-Api-Key=${env:TOKEN}"},
	}
	if !equalOverwriteMCPDefinition(expected, actual) {
		t.Fatalf("equivalent MCP definitions did not compare equal: expected=%#v actual=%#v", expected, actual)
	}
	actual.Args = []string{"--second", "--first"}
	if equalOverwriteMCPDefinition(expected, actual) {
		t.Fatal("MCP argument order was ignored")
	}
}

func TestCodexOverwriteMCPDefinitionsReadsEnvironmentBackedHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte(`[mcp_servers.bearer]
type = "http"
url = "https://example.test/bearer"
bearer_token_env_var = "TOKEN"

[mcp_servers.api_key]
type = "http"
url = "https://example.test/api-key"
http_headers = { "X-Static" = "preserve" }

[mcp_servers.api_key.env_http_headers]
"X-Api-Key" = "API_KEY"
`)
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	document, _, err := readCodexDocument(path, true)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := codexOverwriteMCPDefinitions(path, document)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]CatalogEntry{
		"bearer": {
			Type: LibraryTypeMCP, Name: "bearer", Transport: "http",
			URL: "https://example.test/bearer", Headers: []string{"Authorization=Bearer ${TOKEN}"},
		},
		"api_key": {
			Type: LibraryTypeMCP, Name: "api_key", Transport: "http",
			URL:     "https://example.test/api-key",
			Headers: []string{"X-Api-Key=${API_KEY}", "X-Static=preserve"},
		},
	}
	for name, want := range expected {
		got, exists := definitions[name]
		if !exists || !equalOverwriteMCPDefinition(want, got) {
			t.Errorf("Codex MCP definition %q = %#v, exists %t; want %#v", name, got, exists, want)
		}
	}
}

func TestEqualOverwriteMCPRuntimeDefinitionHandlesRemoteEnvReferences(t *testing.T) {
	t.Setenv("TOKEN", "")
	newEntries := func() (CatalogEntry, CatalogEntry) {
		expected := CatalogEntry{
			Transport: "http",
			URL:       "https://example.invalid/mcp",
			Env:       []string{"TOKEN=${TOKEN}"},
			Headers:   []string{"Authorization=Bearer ${TOKEN}", "X-Api-Key=${TOKEN}"},
		}
		actual := CatalogEntry{
			Transport: "http",
			URL:       "https://example.invalid/mcp",
			Headers:   []string{"X-Api-Key={env:TOKEN}", "Authorization=Bearer {env:TOKEN}"},
		}
		return expected, actual
	}

	httpTargets := []string{"antigravity", "claude", "codex", "copilot", "cursor", "gemini", "kiro", "opencode", "vscode"}
	for _, target := range httpTargets {
		t.Run(target+" accepts absent runtime environment", func(t *testing.T) {
			expected, actual := newEntries()
			if !equalOverwriteMCPRuntimeDefinition(expected, actual, target) {
				t.Fatalf("%s rejected equivalent HTTP MCP definition without runtime Env", target)
			}
		})
		t.Run(target+" accepts matching runtime environment", func(t *testing.T) {
			expected, actual := newEntries()
			actual.Env = []string{"TOKEN={env:TOKEN}"}
			if !equalOverwriteMCPRuntimeDefinition(expected, actual, target) {
				t.Fatalf("%s rejected equivalent HTTP MCP definition with matching runtime Env", target)
			}
		})
	}

	tests := []struct {
		name   string
		target string
		mutate func(*CatalogEntry)
		want   bool
	}{
		{name: "reject unexpected actual environment", target: "claude", mutate: func(actual *CatalogEntry) {
			actual.Env = []string{"EXTRA=unexpected"}
		}},
		{name: "reject changed actual environment", target: "claude", mutate: func(actual *CatalogEntry) {
			actual.Env = []string{"TOKEN=changed"}
		}},
		{name: "reject wrong URL", target: "claude", mutate: func(actual *CatalogEntry) {
			actual.URL = "https://example.invalid/wrong"
		}},
		{name: "reject wrong header", target: "claude", mutate: func(actual *CatalogEntry) {
			actual.Headers[0] = "X-Api-Key=wrong"
		}},
		{name: "reject missing header", target: "claude", mutate: func(actual *CatalogEntry) {
			actual.Headers = actual.Headers[:1]
		}},
		{name: "unknown target keeps environment strict", target: "unknown", want: false},
		{name: "stdio missing environment remains strict", target: "claude", want: false},
		{name: "stdio extra environment remains strict", target: "claude", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected, actual := newEntries()
			if tt.name == "unknown target keeps environment strict" {
				actual.Env = nil
			}
			if tt.name == "stdio missing environment remains strict" || tt.name == "stdio extra environment remains strict" {
				expected.Transport = "stdio"
				expected.URL = ""
				expected.Headers = nil
				expected.Env = []string{"TOKEN=${TOKEN}"}
				actual = CatalogEntry{Transport: "stdio"}
				if tt.name == "stdio extra environment remains strict" {
					actual.Env = []string{"EXTRA=unexpected"}
				}
			}
			if tt.mutate != nil {
				tt.mutate(&actual)
			}
			if got := equalOverwriteMCPRuntimeDefinition(expected, actual, tt.target); got != tt.want {
				t.Fatalf("runtime comparison = %t, want %t; expected=%#v actual=%#v", got, tt.want, expected, actual)
			}
		})
	}

	lockTests := []struct {
		name string
		env  []string
		want bool
	}{
		{name: "actual environment absent", env: nil, want: false},
		{name: "actual environment changed", env: []string{"TOKEN=changed"}, want: false},
		{name: "actual environment exactly present", env: []string{"TOKEN={env:TOKEN}"}, want: true},
	}
	for _, tt := range lockTests {
		t.Run("strict lock "+tt.name, func(t *testing.T) {
			expected, actual := newEntries()
			actual.Env = tt.env
			if got := equalOverwriteMCPDefinition(expected, actual); got != tt.want {
				t.Fatalf("strict lock comparison = %t, want %t; expected=%#v actual=%#v", got, tt.want, expected, actual)
			}
		})
	}
}

func TestEqualOverwriteMCPRuntimeDefinitionVerifiesInstallTimeHeaders(t *testing.T) {
	t.Setenv("TOKEN", "nonsecret")
	t.Setenv("EMPTY_TOKEN", "")
	t.Setenv("UNSET_TOKEN", "")
	if err := os.Unsetenv("UNSET_TOKEN"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NESTED_TOKEN", "${OTHER}")
	t.Setenv("NESTED_ENV_TOKEN", "{env:OTHER}")
	t.Setenv("OTHER", "other-nonsecret")

	const placeholder = "Authorization=Bearer ${TOKEN}"
	const resolved = "authorization=Bearer nonsecret"
	expected := CatalogEntry{
		Transport: "http",
		URL:       "https://example.invalid/mcp",
		Headers:   []string{placeholder},
	}
	for _, tt := range []struct {
		target          string
		resolvesHeaders bool
	}{
		{target: "antigravity", resolvesHeaders: true},
		{target: "claude", resolvesHeaders: true},
		{target: "gemini", resolvesHeaders: true},
		{target: "codex"},
		{target: "copilot"},
		{target: "cursor"},
		{target: "kiro"},
		{target: "opencode"},
		{target: "vscode"},
		{target: "unknown"},
	} {
		t.Run(tt.target, func(t *testing.T) {
			actual := expected
			if !equalOverwriteMCPRuntimeDefinition(expected, actual, tt.target) {
				t.Fatal("runtime comparison rejected preserved placeholder")
			}
			actual.Headers = []string{resolved}
			if got := equalOverwriteMCPRuntimeDefinition(expected, actual, tt.target); got != tt.resolvesHeaders {
				t.Fatalf("runtime comparison with resolved header = %t, want %t", got, tt.resolvesHeaders)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		template string
		headers  []string
		url      string
		want     bool
	}{
		{name: "dollar env form", template: "Authorization=Bearer ${env:TOKEN}", headers: []string{resolved}, want: true},
		{name: "brace env form", template: "Authorization=Bearer {env:TOKEN}", headers: []string{resolved}, want: true},
		{name: "mixed resolved and unresolved", template: "Authorization=${TOKEN}/${env:UNSET_TOKEN}/{env:OTHER}",
			headers: []string{"Authorization=nonsecret/${env:UNSET_TOKEN}/other-nonsecret"}, want: true},
		{name: "wrong value", headers: []string{"Authorization=Bearer wrong"}},
		{name: "literal with no template", template: "Authorization=Bearer static", headers: []string{resolved}},
		{name: "unset variable", template: "Authorization=Bearer ${UNSET_TOKEN}", headers: []string{resolved}},
		{name: "empty variable", template: "Authorization=Bearer ${EMPTY_TOKEN}", headers: []string{"Authorization=Bearer "}},
		{name: "wrong URL", headers: []string{resolved}, url: "https://example.invalid/wrong"},
		{name: "missing header"},
		{name: "extra header", headers: []string{resolved, "X-Extra=extra"}},
		{name: "wrong header name", headers: []string{"X-Other=Bearer nonsecret"}},
		{name: "single pass", template: "Authorization=Bearer ${NESTED_TOKEN}", headers: []string{"Authorization=Bearer ${OTHER}"}, want: true},
		{name: "recursive resolution", template: "Authorization=Bearer ${NESTED_TOKEN}", headers: []string{"Authorization=Bearer other-nonsecret"}},
		{name: "exact resolved value", template: "Authorization=Bearer ${NESTED_ENV_TOKEN}", headers: []string{"Authorization=Bearer {env:OTHER}"}, want: true},
		{name: "normalized resolved value is not exact", template: "Authorization=Bearer ${NESTED_ENV_TOKEN}", headers: []string{"Authorization=Bearer ${OTHER}"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want, actual := expected, expected
			if tt.template != "" {
				want.Headers = []string{tt.template}
			}
			actual.Headers = tt.headers
			if tt.url != "" {
				actual.URL = tt.url
			}
			if got := equalOverwriteMCPRuntimeDefinition(want, actual, "gemini"); got != tt.want {
				t.Fatalf("runtime comparison = %t, want %t", got, tt.want)
			}
		})
	}

	t.Run("duplicate marker header uses last value", func(t *testing.T) {
		want, actual := expected, expected
		want.Headers = []string{"X-Probe=old", "X-Probe=selected"}
		for _, target := range []string{"gemini", "codex"} {
			actual.Headers = []string{"x-probe=selected"}
			if !equalOverwriteMCPRuntimeDefinition(want, actual, target) {
				t.Errorf("%s rejected the last value of a duplicate marker header", target)
			}
			actual.Headers = []string{"x-probe=old"}
			if equalOverwriteMCPRuntimeDefinition(want, actual, target) {
				t.Errorf("%s accepted the overwritten value of a duplicate marker header", target)
			}
			actual.Headers = []string{"X-Probe=old", "x-probe=selected"}
			if equalOverwriteMCPRuntimeDefinition(want, actual, target) {
				t.Errorf("%s accepted an extra serialized header when the marker had duplicates", target)
			}
		}
	})

	t.Run("extra header with case variant", func(t *testing.T) {
		actual := expected
		actual.Headers = []string{"Authorization=Bearer wrong", resolved}
		if equalOverwriteMCPRuntimeDefinition(expected, actual, "gemini") {
			t.Fatal("runtime comparison accepted an extra header with the same case-insensitive name")
		}
	})

	t.Run("extra case variant cannot hide a missing variant of another header", func(t *testing.T) {
		t.Setenv("TOKEN", "ok")
		want, actual := expected, expected
		want.Headers = []string{"X-A=keep", "x-a=${TOKEN}", "X-B=keep"}
		actual.Headers = []string{"x-a=ok", "X-B=wrong", "x-b=keep"}
		if equalOverwriteMCPRuntimeDefinition(want, actual, "gemini") {
			t.Fatal("runtime comparison accepted a missing variant of X-A and an extra variant of X-B")
		}
		actual.Headers = []string{"x-a=ok", "X-A=wrong", "x-b=keep"}
		if equalOverwriteMCPRuntimeDefinition(want, actual, "gemini") {
			t.Fatal("runtime comparison accepted the wrong value for one variant of X-A")
		}
		actual.Headers = []string{"x-a=ok", "X-A=keep", "x-b=keep"}
		if !equalOverwriteMCPRuntimeDefinition(want, actual, "gemini") {
			t.Fatal("runtime comparison rejected equivalent header variants")
		}
	})

	t.Run("SSE uses HTTP headers", func(t *testing.T) {
		sse := expected
		sse.Transport = "sse"
		actual := sse
		actual.Headers = []string{resolved}
		if !equalOverwriteMCPRuntimeDefinition(sse, actual, "gemini") {
			t.Fatal("runtime comparison rejected an SSE server with a correctly resolved header")
		}
	})

	t.Run("lock comparison stays strict", func(t *testing.T) {
		actual := expected
		actual.Headers = []string{resolved}
		if equalOverwriteMCPDefinition(expected, actual) {
			t.Fatal("lock comparison accepted an expanded header")
		}
	})
}

func TestOverwriteJSONMCPDefinitionNormalizesNativeHTTP(t *testing.T) {
	const nativeHTTP = `{"httpUrl":"https://example.invalid/mcp","headers":{"X-Probe":"literal"}}`
	tests := []struct {
		name      string
		target    string
		raw       string
		wantURL   string
		wantHTTP  bool
		wantError bool
	}{
		{name: "Gemini httpUrl", target: "gemini", raw: nativeHTTP, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "Antigravity httpUrl", target: "antigravity", raw: nativeHTTP, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "Antigravity serverUrl", target: "antigravity", raw: `{"serverUrl":"https://example.invalid/mcp","headers":{"X-Probe":"literal"}}`, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "Antigravity rejects serverUrl with httpUrl", target: "antigravity", raw: `{"serverUrl":"https://example.invalid/a","httpUrl":"https://example.invalid/b"}`, wantError: true},
		{name: "Antigravity rejects serverUrl with url", target: "antigravity", raw: `{"serverUrl":"https://example.invalid/a","url":"https://example.invalid/b"}`, wantError: true},
		{name: "Antigravity rejects empty serverUrl with httpUrl", target: "antigravity", raw: `{"serverUrl":"","httpUrl":"https://example.invalid/mcp"}`, wantError: true},
		{name: "Antigravity rejects null serverUrl with url", target: "antigravity", raw: `{"serverUrl":null,"url":"https://example.invalid/mcp"}`, wantError: true},
		{name: "Antigravity rejects non-string serverUrl", target: "antigravity", raw: `{"serverUrl":{}}`, wantError: true},
		{name: "Gemini ignores non-string serverUrl", target: "gemini", raw: `{"httpUrl":"https://example.invalid/mcp","serverUrl":{}}`, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "unrelated target ignores non-string native aliases", target: "claude", raw: `{"url":"https://example.invalid/mcp","httpUrl":{},"serverUrl":[]}`, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "Gemini rejects ambiguous endpoint fields", target: "gemini", raw: `{"url":"https://example.invalid/a","httpUrl":"https://example.invalid/b"}`, wantError: true},
		{name: "Antigravity rejects ambiguous endpoint fields", target: "antigravity", raw: `{"url":"https://example.invalid/a","httpUrl":"https://example.invalid/b"}`, wantError: true},
		{name: "Gemini rejects an empty url alongside httpUrl", target: "gemini", raw: `{"url":"","httpUrl":"https://example.invalid/mcp"}`, wantError: true},
		{name: "Antigravity rejects an empty httpUrl alongside url", target: "antigravity", raw: `{"url":"https://example.invalid/mcp","httpUrl":""}`, wantError: true},
		{name: "Gemini rejects a null url alongside httpUrl", target: "gemini", raw: `{"url":null,"httpUrl":"https://example.invalid/mcp"}`, wantError: true},
		{name: "Gemini rejects non-string httpUrl", target: "gemini", raw: `{"httpUrl":{}}`, wantError: true},
		{name: "wrong endpoint does not match selection", target: "gemini", raw: `{"httpUrl":"https://example.invalid/wrong","headers":{"X-Probe":"literal"}}`, wantURL: "https://example.invalid/wrong"},
		{name: "unrelated target does not alias httpUrl", target: "claude", raw: nativeHTTP},
		{name: "unrelated target ignores non-string httpUrl", target: "claude", raw: `{"url":"https://example.invalid/mcp","httpUrl":{}}`, wantURL: "https://example.invalid/mcp", wantHTTP: true},
		{name: "Kiro infers HTTP from URL", target: "kiro", raw: `{"url":"https://example.invalid/mcp"}`, wantURL: "https://example.invalid/mcp", wantHTTP: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := overwriteJSONMCPDefinition("remote-probe", json.RawMessage(tt.raw), tt.target)
			if (err != nil) != tt.wantError {
				t.Fatalf("overwriteJSONMCPDefinition error = %v, wantError %t", err, tt.wantError)
			}
			if err != nil {
				return
			}
			if got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
			if tt.wantHTTP && got.Transport != "http" {
				t.Errorf("transport = %q, want http", got.Transport)
			}
			if tt.target == "gemini" || tt.target == "antigravity" {
				if tt.name == "Gemini httpUrl" || tt.name == "Antigravity httpUrl" || tt.name == "Antigravity serverUrl" {
					if len(got.Headers) != 1 || got.Headers[0] != "X-Probe=literal" {
						t.Errorf("headers = %#v, want [X-Probe=literal]", got.Headers)
					}
				}
			}
			if tt.name == "wrong endpoint does not match selection" {
				expected := CatalogEntry{Transport: "http", URL: "https://example.invalid/mcp", Headers: []string{"X-Probe=literal"}}
				if equalOverwriteMCPDefinition(expected, got) {
					t.Fatal("wrong endpoint matched the selected HTTP definition")
				}
			}
		})
	}
}

func TestOverwriteJSONMCPDefinitionVerifiesNativeSSE(t *testing.T) {
	const endpoint = "https://example.invalid/events"
	for _, tt := range []struct {
		target, shape, endpointField, fields, transport string
		wantSSE                                         bool
	}{
		{target: "gemini", shape: "httpUrl", endpointField: "httpUrl", transport: "http"},
		{target: "gemini", shape: "url", endpointField: "url", transport: "sse", wantSSE: true},
		{target: "claude", shape: "explicit HTTP", endpointField: "url", fields: `"type":"http",`, transport: "http"},
		{target: "claude", shape: "explicit SSE", endpointField: "url", fields: `"type":"sse",`, transport: "sse", wantSSE: true},
		{target: "copilot", shape: "explicit HTTP", endpointField: "url", fields: `"type":"http",`, transport: "http"},
		{target: "copilot", shape: "explicit SSE", endpointField: "url", fields: `"type":"sse",`, transport: "sse", wantSSE: true},
		{target: "vscode", shape: "explicit HTTP", endpointField: "url", fields: `"type":"http",`, transport: "http"},
		{target: "vscode", shape: "explicit SSE", endpointField: "url", fields: `"type":"sse",`, transport: "sse", wantSSE: true},
		{target: "cursor", shape: "collapsed SSE", endpointField: "url", fields: `"type":"http",`, transport: "http", wantSSE: true},
		{target: "cursor", shape: "explicit SSE", endpointField: "url", fields: `"type":"sse",`, transport: "sse", wantSSE: true},
		{target: "kiro", shape: "collapsed SSE", endpointField: "url", transport: "http", wantSSE: true},
		{target: "kiro", shape: "explicit SSE", endpointField: "url", fields: `"type":"sse",`, transport: "sse", wantSSE: true},
		{target: "opencode", shape: "collapsed SSE", endpointField: "url", fields: `"type":"remote",`, transport: "http", wantSSE: true},
		{target: "opencode", shape: "legacy explicit SSE", endpointField: "url", fields: `"type":"remote","httpTransport":"sse",`, transport: "sse", wantSSE: true},
	} {
		t.Run(tt.target+"/"+tt.shape, func(t *testing.T) {
			raw := fmt.Sprintf(`{%s%q:%q,"headers":{"X-Probe":"literal"}}`, tt.fields, tt.endpointField, endpoint)
			actual, err := overwriteJSONMCPDefinition("sse-probe", json.RawMessage(raw), tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if actual.Transport != tt.transport {
				t.Errorf("transport = %q, want %q", actual.Transport, tt.transport)
			}
			expected := CatalogEntry{URL: endpoint, Headers: []string{"X-Probe=literal"}, Env: []string{"TOKEN=${TOKEN}"}}
			check := func(label, transport string, candidate CatalogEntry, want bool) {
				t.Helper()
				expected.Transport = transport
				if got := equalOverwriteMCPRuntimeDefinition(expected, candidate, tt.target); got != want {
					t.Errorf("%s with expected %s: comparison = %t, want %t", label, transport, got, want)
				}
			}
			check("native output", "http", actual, tt.transport == "http")
			check("native output without runtime environment", "sse", actual, tt.wantSSE)
			if tt.wantSSE {
				changed := actual
				changed.Env = []string{"TOKEN=${TOKEN}"}
				check("matching runtime environment", "sse", changed, true)
				changed.Env = []string{"TOKEN=changed"}
				check("changed runtime environment", "sse", changed, false)
				changed.Env = []string{"TOKEN=${TOKEN}", "EXTRA=unexpected"}
				check("unexpected runtime environment entry", "sse", changed, false)
				changed = actual
				changed.URL = "https://example.invalid/wrong"
				check("wrong URL", "sse", changed, false)
				changed = actual
				changed.Headers = []string{"X-Probe=changed"}
				check("wrong header", "sse", changed, false)
			}
		})
	}
}

func TestOverwriteJSONMCPDefinitionRequiresNativeSSEDiscriminator(t *testing.T) {
	for _, target := range []string{"claude", "copilot", "vscode"} {
		t.Run(target, func(t *testing.T) {
			expected := CatalogEntry{Transport: "sse", URL: "https://example.invalid/events"}
			for _, raw := range []json.RawMessage{
				json.RawMessage(`{"url":"https://example.invalid/events"}`),
				json.RawMessage(`{"transport":"sse","url":"https://example.invalid/events"}`),
				json.RawMessage(`{"type":"http","transport":"sse","url":"https://example.invalid/events"}`),
				json.RawMessage(`{"type":"http","httpTransport":"sse","url":"https://example.invalid/events"}`),
			} {
				actual, err := overwriteJSONMCPDefinition("sse-probe", raw, target)
				if err == nil && equalOverwriteMCPRuntimeDefinition(expected, actual, target) {
					t.Errorf("SSE matched a definition without type:sse: %s", raw)
				}
			}
		})
	}
}

func TestOverwriteJSONMCPDefinitionRejectsConflictingNativeTransport(t *testing.T) {
	const endpoint = "https://example.invalid/mcp"
	for _, target := range []string{"claude", "copilot", "vscode"} {
		t.Run(target, func(t *testing.T) {
			for _, tt := range []struct{ native, alias string }{{"sse", "http"}, {"http", "sse"}} {
				raw := fmt.Sprintf(`{"type":%q,"transport":%q,"url":%q}`, tt.native, tt.alias, endpoint)
				actual, err := overwriteJSONMCPDefinition("probe", json.RawMessage(raw), target)
				if err == nil {
					t.Errorf("conflicting fields in %s were not rejected; decoded as %q", raw, actual.Transport)
				}
			}
		})
	}
}

func TestOverwriteJSONMCPDefinitionRequiresOpenCodeRemoteType(t *testing.T) {
	for _, raw := range []string{
		`{"type":"http","url":"https://example.invalid/mcp"}`,
		`{"type":"http","url":"https://example.invalid/mcp","httpTransport":"sse"}`,
		`{"url":"https://example.invalid/mcp"}`,
	} {
		actual, err := overwriteJSONMCPDefinition("probe", json.RawMessage(raw), "opencode")
		if err == nil {
			t.Errorf("invalid OpenCode remote definition %s was not rejected; decoded as %q", raw, actual.Transport)
		}
	}
	actual, err := overwriteJSONMCPDefinition("probe", json.RawMessage(`{"type":"local","command":["/bin/true"],"httpTransport":"sse"}`), "opencode")
	if err != nil || actual.Transport != "stdio" {
		t.Errorf("legacy OpenCode SSE hint changed a local definition: got transport %q, error %v", actual.Transport, err)
	}
}

func TestEqualOverwriteMCPDefinitionKeepsSSETransportAndEnvironmentStrict(t *testing.T) {
	expected := CatalogEntry{Transport: "sse", URL: "https://example.invalid/events", Env: []string{"TOKEN=${TOKEN}"}}
	if !equalOverwriteMCPDefinition(expected, expected) {
		t.Fatal("exact SSE lock definition did not match")
	}
	collapsed := expected
	collapsed.Transport = "http"
	if equalOverwriteMCPDefinition(expected, collapsed) {
		t.Error("collapsed transport matched a selected SSE definition in the lock")
	}
	omitted := expected
	omitted.Env = nil
	if equalOverwriteMCPDefinition(expected, omitted) {
		t.Error("missing environment matched a selected SSE definition in the lock")
	}
	changed := expected
	changed.Env = []string{"TOKEN=changed"}
	if equalOverwriteMCPDefinition(expected, changed) {
		t.Error("changed environment matched a selected SSE definition in the lock")
	}
	for _, target := range []string{"antigravity", "codex", "unknown"} {
		if equalOverwriteMCPRuntimeDefinition(expected, collapsed, target) ||
			equalOverwriteMCPRuntimeDefinition(expected, omitted, target) {
			t.Errorf("%s accepted collapsed transport or omitted SSE environment", target)
		}
	}
}

package instill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveOverwriteTargets(t *testing.T) {
	tests := []struct {
		name     string
		declared []string
		dirs     []string
		want     string
		wantErr  string
	}{
		{name: "aliases and sorting", declared: []string{"agents", "agy", "claude"}, want: "antigravity,claude,copilot"},
		{name: "audited directory detection", dirs: []string{".github", ".grok"}, want: "copilot,grok-build"},
		{name: "audited detection wins over Pi fallback", dirs: []string{".pi", ".github"}, want: "copilot"},
		{name: "Pi fallback", dirs: []string{".pi"}, want: "agent-skills"},
		{name: "no project targets", wantErr: "run 'instill targets' first"},
		{name: "unsupported target", declared: []string{"intellij"}, wantErr: "unsupported project target"},
		{name: "all expansion", declared: []string{"all"}, want: "claude,codex,copilot,cursor,gemini,grok-build,kiro,opencode,windsurf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range test.dirs {
				if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := resolveOverwriteTargets(root, test.declared)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error=%v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != test.want {
				t.Fatalf("targets=%v, want %s", got, test.want)
			}
		})
	}
}

func TestPrepareProjectOverwriteMigratesLegacyPiTarget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	if err := os.Mkdir(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "apm.yml")
	if err := os.WriteFile(manifestPath, []byte("name: p\nversion: 1.0.0\ntargets: [pi]\ndependencies: {apm: [], mcp: []}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := Project{Root: root, ManifestPath: manifestPath}
	document, err := loadManifestDocument(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := document.projection
	manifest.Targets = []string{"pi"}

	plan, err := prepareProjectOverwrite(ProjectSelectionOptions{Project: project}, manifest, nil, document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.resolvedTargets, ",") != "agent-skills" {
		t.Fatalf("resolved targets = %v, want [agent-skills]", plan.resolvedTargets)
	}
}

func TestPrepareProjectOverwriteDetectsAuditedTargetBeforePiFallback(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{".pi", ".github"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(root, "apm.yml")
	contents := []byte("name: p\nversion: 1.0.0\ndependencies: {apm: [], mcp: []}\n")
	if err := os.WriteFile(manifestPath, contents, 0600); err != nil {
		t.Fatal(err)
	}
	project := Project{Root: root, ManifestPath: manifestPath}
	document, err := loadManifestDocument(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	plan, err := prepareProjectOverwrite(ProjectSelectionOptions{Project: project, LibraryPath: library}, document.projection, nil, document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.resolvedTargets, ",") != "copilot" {
		t.Fatalf("resolved targets = %v, want [copilot]", plan.resolvedTargets)
	}
}

func TestDetachOverwriteLockPreservesOtherLockFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apm.lock.yaml")
	before := []byte("lockfile_version: \"3\"\nmcp_target_servers:\n  windsurf: [old]\nmcp_servers: [old]\nmcp_configs: {old: {command: stale}}\nmcp_config_provenance: {old: stale}\nmcp_omissions: {old: stale}\ndependencies: [{name: pkg, resolved: abc}]\ndeployments:\n  - target: mcp\n    name: old\n  - target: codex\n    name: skill\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	file := &overwriteFile{path: path, before: before, mode: 0600}
	if err := detachOverwriteLock(file); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mcp_target_servers"]; !ok {
		t.Fatal("mcp_target_servers key was not retained")
	}
	if len(got["mcp_servers"].([]any)) != 0 || len(got["mcp_configs"].(map[string]any)) != 0 || len(got["mcp_config_provenance"].(map[string]any)) != 0 || len(got["mcp_omissions"].(map[string]any)) != 0 {
		t.Fatalf("stale MCP lock state was not cleared: %#v", got)
	}
	if got["dependencies"] == nil {
		t.Fatalf("unrelated fields lost: %#v", got)
	}
	deployments := got["deployments"].([]any)
	if len(deployments) != 1 || deployments[0].(map[string]any)["target"] != "codex" {
		t.Fatalf("deployments=%#v", deployments)
	}
}

func TestClearOverwriteDirectoryUnlinksSymlinkWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep")
	if err := os.WriteFile(outside, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	ns := filepath.Join(root, "namespace")
	if err := os.Mkdir(ns, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ns, "external")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Fatal(err)
	}
	if err := clearOverwriteDirectory(ns); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link remains: %v", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "sentinel" {
		t.Fatalf("outside sentinel changed: %q, %v", data, err)
	}
}
func TestClearManagedContextSectionPreservesOutsideBytes(t *testing.T) {
	before := []byte("authored prefix\n<!-- apm:start -->\nold generated\n<!-- apm:end -->\nauthored suffix\n")
	got, err := clearManagedContextSection(before, "<!-- apm:start -->", "<!-- apm:end -->")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("authored prefix\n<!-- apm:start -->\n<!-- apm:end -->\nauthored suffix\n")
	if string(got) != string(want) {
		t.Fatalf("managed section result = %q, want %q", got, want)
	}
	for _, malformed := range [][]byte{
		[]byte("<!-- apm:start -->\nmissing end"),
		[]byte("<!-- apm:start -->\na\n<!-- apm:end -->\n<!-- apm:end -->"),
		[]byte("<!-- apm:end -->\n<!-- apm:start -->"),
	} {
		if _, err := clearManagedContextSection(malformed, "<!-- apm:start -->", "<!-- apm:end -->"); err == nil {
			t.Fatalf("clearManagedContextSection(%q) succeeded, want malformed marker error", malformed)
		}
	}
}
func TestPrepareLockRejectsEscapingDeploymentPaths(t *testing.T) {
	tests := []struct{ name, lock string }{
		{name: "absolute deployed file", lock: "dependencies: [{name: x, deployed_files: [/tmp/global]}]"},
		{name: "parent traversal", lock: "dependencies: [{name: x, deployed_files: [../../outside]}]"},
		{name: "global deployment scope", lock: "deployments: [{target: codex, value: .codex/agents, scope: global}]"},
		{name: "absolute deployment value", lock: "deployments: [{target: codex, value: /outside, scope: project}]"},
		{name: "dependency scope type", lock: "dependencies: [{name: x, scope: [project]}]"},
		{name: "duplicate root key", lock: "dependencies: []\ndependencies: []"},
		{name: "anchor", lock: "x: &x []\ndependencies: *x"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "apm.lock.yaml"), []byte("lockfile_version: \"3\"\n"+test.lock), 0600); err != nil {
				t.Fatal(err)
			}
			plan := &projectOverwritePlan{root: root}
			if err := plan.prepareLock(); err == nil {
				t.Fatal("prepareLock succeeded for unsafe deployment")
			}
		})
	}
}
func TestPrepareConfigResetsRejectsMalformedNamespaces(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	tests := []struct{ name, path, contents string }{
		{name: "MCP namespace must be object", path: ".mcp.json", contents: `{"mcpServers":[]}`},
		{name: "Copilot enabled plugins must contain booleans", path: ".github/copilot/settings.json", contents: `{"enabledPlugins":{"plugin@apm":"false"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, test.path)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.contents), 0600); err != nil {
				t.Fatal(err)
			}
			if err := (&projectOverwritePlan{root: root}).prepareConfigResets(); err == nil {
				t.Fatal("prepareConfigResets succeeded for malformed namespace")
			}
		})
	}
}

func TestPrepareConfigResetsCopilotEnabledPluginsAsObject(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".github", "copilot", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	before := `{"enabledPlugins":{"plugin@apm":true},"other":7}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	plan := &projectOverwritePlan{root: root}
	if err := plan.prepareConfigResets(); err != nil {
		t.Fatal(err)
	}
	if len(plan.files) != 1 {
		t.Fatalf("reset files = %d, want one Copilot settings file", len(plan.files))
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(plan.files[0].after, &result); err != nil {
		t.Fatal(err)
	}
	var plugins map[string]bool
	if err := json.Unmarshal(result["enabledPlugins"], &plugins); err != nil {
		t.Fatalf("enabledPlugins = %s, want an object", result["enabledPlugins"])
	}
	if len(plugins) != 0 || string(result["other"]) != "7" {
		t.Fatalf("reset settings = %s, want empty enabledPlugins and preserved other value", plan.files[0].after)
	}
}

func TestPrepareLockRejectsUnsupportedSchema(t *testing.T) {
	tests := []struct {
		name string
		lock string
	}{
		{name: "unknown lockfile version", lock: `lockfile_version: "4"`},
		{name: "invalid MCP config map", lock: "lockfile_version: \"3\"\nmcp_configs: []"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "apm.lock.yaml"), []byte(test.lock), 0600); err != nil {
				t.Fatal(err)
			}
			if err := (&projectOverwritePlan{root: root}).prepareLock(); err == nil {
				t.Fatal("prepareLock succeeded for unsupported lock schema")
			}
		})
	}
}

func TestOverwriteApplyChecksLockSnapshotBeforeClearingNamespaces(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(namespace, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(namespace, "manual.md")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "apm.lock.yaml")
	if err := os.WriteFile(lockPath, []byte("lockfile_version: \"3\"\ndependencies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := snapshotOverwriteDirectory(namespace)
	if err != nil {
		t.Fatal(err)
	}
	plan := &projectOverwritePlan{root: root, directories: []overwriteDirectory{directory}}
	if err := plan.prepareLock(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("lockfile_version: \"3\"\ndependencies: [{name: changed}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err = withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return plan.apply(ctx, held)
	})
	if err == nil || !strings.Contains(ErrorMessage(err), "apm.lock.yaml changed during overwrite") {
		t.Fatalf("apply error = %v, want lock snapshot conflict", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("namespace changed before lock conflict: %q, %v", data, err)
	}
}

func TestCaptureOverwriteOpenCodeUsesEffectiveProjectLayer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"mcp":{"server":{"enabled":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.jsonc"), []byte(`{"mcp":{"server":{"enabled":false}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := captureOverwriteOpenCode(root)
	if err != nil {
		t.Fatal(err)
	}
	value, exists := state["server"]
	if !exists || !value.Present || string(value.Value) != "false" {
		t.Fatalf("effective project preference = %#v, want explicit false from later layer", value)
	}
}

func TestReconcileOverwritePiMCPConfigMigratesLegacyNamespace(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	path := piProjectMCPPath(root)
	before := `{"mcp-servers":{"keep":{"command":"old","args":[],"disabled":true,"custom":"preserve"},"stale":{"command":"stale","args":[]}},"_instillManagedServers":["keep","stale"]}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	previous, err := capturePiMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "keep", Transport: "stdio", Command: "new"}
	if err := reconcileOverwritePiMCPConfig(root, []CatalogEntry{entry}, []byte(before)); err != nil {
		t.Fatal(err)
	}
	if err := verifyOverwritePiMCPConfig(root, []CatalogEntry{entry}, previous); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, exists := object[piLegacyServersKey]; exists {
		t.Fatalf("legacy Pi namespace remains: %s", data)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(object[piMCPServersKey], &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("canonical servers = %s, want only selected server", object[piMCPServersKey])
	}
	var server map[string]json.RawMessage
	if err := json.Unmarshal(servers["keep"], &server); err != nil {
		t.Fatal(err)
	}
	if string(server["command"]) != `"new"` || string(server["disabled"]) != "true" || string(server["custom"]) != `"preserve"` {
		t.Fatalf("migrated server = %s, want updated definition and preserved user metadata/enabled state", servers["keep"])
	}
	var managed []string
	if err := json.Unmarshal(object[piManagedKey], &managed); err != nil || len(managed) != 1 || managed[0] != "keep" {
		t.Fatalf("managed names = %v, %v; want [keep]", managed, err)
	}
}

func TestCheckOverwritePathCanonicalLibraryAndSelectedSource(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(namespace, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(namespace, "selected", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("selected"), 0600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "library-link")
	if err := os.Symlink(filepath.Join(root, ".agents"), link); err != nil {
		t.Fatal(err)
	}
	if err := checkOverwritePath(root, namespace, link, ProjectSelection{}, nil); err == nil {
		t.Fatal("canonical Library overlap was accepted")
	}
	if err := checkOverwritePath(root, namespace, filepath.Join(root, "other-library"), ProjectSelection{}, []APMDependency{{Local: source}}); err == nil {
		t.Fatal("selected source outside Library was accepted")
	}
}

func TestOverwriteDirectorySnapshotDetectsConcurrentChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "namespace")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotOverwriteDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manual.md"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := snapshotOverwriteDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.existed != after.existed || string(before.before) == string(after.before) {
		t.Fatal("directory snapshot did not detect concurrent addition")
	}
}
func TestPrepareConfigResetsUsesPiExclusiveModeSemantics(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", " EXCLUSIVE ")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	err := (&projectOverwritePlan{root: root}).prepareConfigResets()
	if err == nil || !strings.Contains(err.Error(), "requires Pi project MCP configuration") {
		t.Fatalf("exclusive mode error = %v", err)
	}
}

func TestOverwriteApplyChecksPiSnapshotBeforeClearingNamespaces(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "apm.yml")
	manifestBytes := []byte("name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies: {apm: [], mcp: []}\n")
	if err := os.WriteFile(manifestPath, manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	project := Project{Root: root, ManifestPath: manifestPath}
	document, err := loadManifestDocument(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	piPath := piProjectMCPPath(root)
	if err := os.WriteFile(piPath, []byte(`{"mcpServers":{"manual":{"command":"old"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, ".agents", "skills", "manual", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	plan, err := prepareProjectOverwrite(ProjectSelectionOptions{Project: project, LibraryPath: library}, document.projection, nil, document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piPath, []byte(`{"mcpServers":{"manual":{"command":"changed"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	err = withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return plan.apply(ctx, held)
	})
	if err == nil || !strings.Contains(ErrorMessage(err), "Pi MCP configuration changed during overwrite") {
		t.Fatalf("apply error = %v, want Pi snapshot conflict", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("namespace changed before Pi snapshot conflict: %q, %v", data, err)
	}
}

func TestReconcileOverwritePiMCPConfigRejectsChangedSnapshot(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	path := piProjectMCPPath(root)
	before := []byte(`{"mcpServers":{"manual":{"command":"changed"}}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	err := reconcileOverwritePiMCPConfig(root, nil, []byte(`{"mcpServers":{"manual":{"command":"old"}}}`))
	if err == nil || !strings.Contains(ErrorMessage(err), "Pi MCP configuration changed during overwrite") {
		t.Fatalf("reconcile error = %v, want Pi snapshot conflict", err)
	}
	if data, readErr := os.ReadFile(path); readErr != nil || string(data) != string(before) {
		t.Fatalf("Pi config changed on snapshot conflict: %q, %v", data, readErr)
	}
}

func TestVerifyOverwriteMCPOutputsSkipsAPMOnlyAggregatesForPi(t *testing.T) {
	root := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "root-server", Transport: "stdio", Command: "/usr/bin/true"}
	lock := overwriteLockState{
		mcpServers:       []string{},
		mcpConfigs:       map[string]*yaml.Node{},
		mcpTargetServers: map[string][]string{},
	}
	if err := verifyOverwriteMCPOutputs(root, []string{"agent-skills"}, lock, []CatalogEntry{entry}, true); err != nil {
		t.Fatalf("Pi-only output verification = %v, want no unavailable APM service metadata requirement", err)
	}
	if err := verifyOverwriteMCPOutputs(root, []string{"agent-skills"}, lock, []CatalogEntry{entry}, false); err == nil || !strings.Contains(err.Error(), "lock MCP servers do not match") {
		t.Fatalf("non-Pi verification error = %v, want missing aggregate service lock rejection", err)
	}
}

func TestValidateOverwritePiConfigPathRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	path := piProjectMCPPath(root)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := validateOverwritePiConfigPath(root, path); err == nil {
		t.Fatal("symlinked Pi config path was accepted")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatalf("outside Pi target changed: %q, %v", data, err)
	}
}

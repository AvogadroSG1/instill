package instill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tomledit "github.com/smm-h/go-toml-edit"
)

func TestCodexMCPEnableRestoreInheritedAndDefault(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	projectConfig := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`[mcp_servers."quoted.name"]
command = "old"
enabled = true

[mcp_servers.omitted]
command = "old"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(`[mcp_servers.inherited]
command = "global"
enabled = false
`), 0o600); err != nil {
		t.Fatal(err)
	}

	rootState, merged, err := captureCodexMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rootState["quoted.name"].Present || rootState["quoted.name"].Value[0] != 't' {
		t.Fatalf("quoted root state = %#v", rootState["quoted.name"])
	}
	if !merged["inherited"].Present || merged["inherited"].Value[0] != 'f' {
		t.Fatalf("inherited state = %#v", merged["inherited"])
	}

	if err := os.WriteFile(projectConfig, []byte(`[mcp_servers."quoted.name"]
command = "new"
enabled = false

[mcp_servers.omitted]
command = "new"
enabled = true

[mcp_servers.inherited]
command = "new"
enabled = true

[mcp_servers.new]
command = "new"
enabled = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	def := new(false)
	if err := reconcileCodexMCPEnableState(root, mcpEnableSnapshot{CodexRoot: rootState, CodexMerged: merged}, mcpInstallPolicy{
		Catalog: map[string]*CatalogEntry{"new": {Name: "new", DefaultEnabled: def}},
	}, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(projectConfig)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`[mcp_servers."quoted.name"]`,
		`command = "new"`,
		`enabled = true`,
		`[mcp_servers.inherited]`,
		`enabled = false`,
		`[mcp_servers.new]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired Codex config missing %q:\n%s", want, text)
		}
	}
	omitted, err := tomledit.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if omitted.Has(codexEnablePath("omitted")) {
		t.Fatal("pre-existing omitted enabled member was not removed")
	}
	if got, err := omitted.GetBool(codexEnablePath("quoted.name")); err != nil || !got {
		t.Fatalf("quoted.name enabled = %v, %v; want true", got, err)
	}
	if got, err := omitted.GetBool(codexEnablePath("inherited")); err != nil || got {
		t.Fatalf("inherited enabled = %v, %v; want false", got, err)
	}
	if got, err := omitted.GetBool(codexEnablePath("new")); err != nil || got {
		t.Fatalf("new enabled = %v, %v; want false", got, err)
	}
}

func TestCodexMCPEnableNilDefaultLeavesAPMValue(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[mcp_servers.new]
command = "old"
enabled = false
`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := mcpEnableSnapshot{CodexRoot: map[string]mcpEnableValue{}, CodexMerged: map[string]mcpEnableValue{}}
	if err := reconcileCodexMCPEnableState(root, before, mcpInstallPolicy{Catalog: map[string]*CatalogEntry{"new": {Name: "new"}}}, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := tomledit.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := doc.GetBool(codexEnablePath("new")); err != nil || got {
		t.Fatalf("nil default changed APM value: %v, %v", got, err)
	}
}

func TestCodexMCPEnableRejectsMalformedStateBeforeMutation(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`[mcp_servers.bad]
enabled = "yes"
`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureCodexMCPEnableState(root); err == nil {
		t.Fatal("capture accepted non-boolean enabled member")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("malformed Codex config was modified")
	}
}

func TestCodexMCPEnableSharedSafeWrites(t *testing.T) {
	isolateMCPEnvironment(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	before := []byte("[mcp_servers.one]\nenabled = true\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mcp_servers.one]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPFile(path, before, []byte("[mcp_servers.one]\nenabled = false\n"), 0o600); err == nil || !strings.Contains(err.Error(), "MCP enable state changed before writing") {
		t.Fatalf("write conflict error = %v", err)
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readMCPFile(link, true); err == nil {
		t.Fatal("writable symlink was accepted")
	}
}

func TestCodexMCPEnablePreservesInlineConnectionFields(t *testing.T) {
	isolateMCPEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`mcp_servers = { "quoted.name" = { command = "old", enabled = true, extra = "keep" } }
`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	rootState, merged, err := captureCodexMCPEnableState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`mcp_servers = { "quoted.name" = { command = "new", enabled = false, extra = "keep" } }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileCodexMCPEnableState(root, mcpEnableSnapshot{CodexRoot: rootState, CodexMerged: merged}, mcpInstallPolicy{}, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := tomledit.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := doc.GetBool(codexEnablePath("quoted.name")); err != nil || !got {
		t.Fatalf("inline quoted.name enabled = %v, %v; want true", got, err)
	}
	if !strings.Contains(string(data), `command = "new"`) || !strings.Contains(string(data), `extra = "keep"`) {
		t.Fatalf("inline connection fields were not preserved:\n%s", data)
	}
}

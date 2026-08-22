package instill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestFixtureWritesYAMLDependencies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	manifest := APMManifest{
		Name:    "my-project",
		Version: "1.0.0",
		Dependencies: APMDependencies{
			APM: localDependencies("/library/skills/golang-testing"),
			MCP: []MCPDependency{{
				Name: "local-db", Transport: "stdio", Registry: false,
				Command: "sqlite-mcp", Args: []string{"--db", "dev.db"},
			}},
		},
	}

	writeAPMManifestForTest(t, path, manifest)
	data := readFile(t, path)
	requireContains(t, data, "name: my-project")
	requireContains(t, data, "version: 1.0.0")
	requireContains(t, data, "dependencies:")
	requireContains(t, data, "- /library/skills/golang-testing")
	requireContains(t, data, "name: local-db")
	requireContains(t, data, "transport: stdio")
	requireContains(t, data, "registry: false")
}

func TestReadAPMManifestPreservesOmittedAndFalseMCPRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte("dependencies:\n    mcp:\n        - name: registry-server\n        - name: local-server\n          registry: false\n"), 0o644))

	manifest, err := ReadAPMManifest(path)

	requireNoError(t, err)
	requireEqual(t, 2, len(manifest.Dependencies.MCP))
	if manifest.Dependencies.MCP[0].Registry != nil {
		t.Fatalf("first Registry = %v, want nil", manifest.Dependencies.MCP[0].Registry)
	}
	if manifest.Dependencies.MCP[1].Registry == nil {
		t.Fatal("second Registry = nil, want pointer to false")
	}
	requireEqual(t, false, manifest.Dependencies.MCP[1].Registry)
}

func TestReadAPMManifestProjectsUnmatchedMCPDependencyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte(`name: preservation
version: 1.0.0
dependencies:
  mcp:
    - name: io.example/custom
      registry: https://registry.example.test
      headers:
        Authorization: ${TOKEN}
      version: 2.4.1
      package: '@example/server'
      tools: [search, fetch]
      x-owner: platform
`), 0o644))

	manifest, err := ReadAPMManifest(path)
	requireNoError(t, err)
	requireEqual(t, "platform", manifest.Dependencies.MCP[0].Extra["x-owner"])

	data := readFile(t, path)
	requireContains(t, data, "registry: https://registry.example.test")
	requireContains(t, data, "Authorization: ${TOKEN}")
	requireContains(t, data, "version: 2.4.1")
	requireContains(t, data, "package: '@example/server'")
	requireContains(t, data, "tools: [search, fetch]")
	requireContains(t, data, "x-owner: platform")
}

func TestReadAPMManifestPreservesNameAndVersionFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte("name: test-project\nversion: 2.3.1\ndependencies:\n    apm:\n        - /lib/skills/docker\n"), 0o644))

	manifest, err := ReadAPMManifest(path)

	requireNoError(t, err)
	requireEqual(t, "test-project", manifest.Name)
	requireEqual(t, "2.3.1", manifest.Version)
	requireEqual(t, 1, len(manifest.Dependencies.APM))
}

func TestReadAPMManifestNormalizesMissingDependencies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	manifest, err := ReadAPMManifest(path)

	requireNoError(t, err)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	requireEqual(t, 0, len(manifest.Dependencies.MCP))
}

func TestReadAPMManifestReturnsExitGeneralOnMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte("dependencies: [\n"), 0o644))

	_, err := ReadAPMManifest(path)

	if err == nil {
		t.Fatal("ReadAPMManifest() error = nil, want malformed yaml error")
	}
	requireEqual(t, ExitGeneral, ExitCode(err))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	return string(data)
}

func requireContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("string %q does not contain %q", got, want)
	}
}

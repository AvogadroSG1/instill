package instill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const apmManifestFileName = "apm.yml"

type MCPDependency struct {
	Name    string   `yaml:"name,omitempty"`
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	Env     []string `yaml:"env,omitempty"`
	URL     string   `yaml:"url,omitempty"`
}

type APMDependencies struct {
	APM []string        `yaml:"apm,omitempty"`
	MCP []MCPDependency `yaml:"mcp,omitempty"`
}

type APMManifest struct {
	Name         string          `yaml:"name"`
	Dependencies APMDependencies `yaml:"dependencies"`
}

func ReadAPMManifest(path string) (APMManifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Manifest path is discovered under the selected project root.
	if err != nil {
		return APMManifest{}, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read manifest: %v", err))
	}

	var manifest APMManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return APMManifest{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
	}

	manifest.Dependencies.APM = normalizeStringSlice(manifest.Dependencies.APM)
	if manifest.Dependencies.MCP == nil {
		manifest.Dependencies.MCP = []MCPDependency{}
	}

	return manifest, nil
}

func WriteAPMManifestAtomic(path string, manifest APMManifest) error {
	manifest.Dependencies.APM = normalizeStringSlice(manifest.Dependencies.APM)
	if manifest.Dependencies.MCP == nil {
		manifest.Dependencies.MCP = []MCPDependency{}
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", err))
	}

	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write manifest: %v", err))
	}

	return nil
}

func ProjectAPMPath(root string) string {
	return filepath.Join(root, apmManifestFileName)
}

func normalizeStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	return normalized
}

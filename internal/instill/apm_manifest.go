package instill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const apmManifestFileName = "apm.yml"

type MCPDependency struct {
	Name      string            `yaml:"name,omitempty"`
	Transport string            `yaml:"transport,omitempty"`
	Registry  any               `yaml:"registry,omitempty"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Extra     map[string]any    `yaml:",inline"`
}

type APMDependencies struct {
	APM []APMDependency `yaml:"apm,omitempty"`
	MCP []MCPDependency `yaml:"mcp,omitempty"`
}

// APMDependency preserves either APM's legacy local path string or a Git source object.
type APMDependency struct {
	Local string
	Git   *GitDependency
}

type GitDependency struct {
	Repository string         `yaml:"git"`
	Path       string         `yaml:"path"`
	Ref        string         `yaml:"ref"`
	Extra      map[string]any `yaml:",inline"`
}

type APMManifest struct {
	Name         string          `yaml:"name"`
	Version      string          `yaml:"version"`
	Targets      []string        `yaml:"targets,omitempty"`
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

	manifest.Dependencies.APM = normalizeAPMDependencies(manifest.Dependencies.APM)
	if manifest.Dependencies.MCP == nil {
		manifest.Dependencies.MCP = []MCPDependency{}
	}

	return manifest, nil
}

func WriteAPMManifestAtomic(path string, manifest APMManifest) error {
	manifest.Dependencies.APM = normalizeAPMDependencies(manifest.Dependencies.APM)
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

func (d *APMDependency) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		d.Local = node.Value
		d.Git = nil
		return nil
	}
	var git GitDependency
	if err := node.Decode(&git); err != nil {
		return err
	}
	d.Local = ""
	d.Git = &git
	return nil
}

func (d APMDependency) MarshalYAML() (any, error) {
	if d.Git != nil {
		return d.Git, nil
	}
	return d.Local, nil
}

func normalizeAPMDependencies(values []APMDependency) []APMDependency {
	if values == nil {
		return []APMDependency{}
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]APMDependency, 0, len(values))
	for _, value := range values {
		key := value.identity()
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func localDependencies(values ...string) []APMDependency {
	dependencies := make([]APMDependency, 0, len(values))
	for _, value := range values {
		dependencies = append(dependencies, APMDependency{Local: value})
	}
	return dependencies
}

// LocalDependencies constructs legacy local-path dependencies.
func LocalDependencies(values ...string) []APMDependency { return localDependencies(values...) }

func (d APMDependency) identity() string {
	if d.Git != nil {
		return exactGitIdentity(d.Git.Repository, d.Git.Path, d.Git.Ref)
	}
	return "local:" + d.Local
}

func (d APMDependency) stableIdentity() string {
	if d.Git != nil {
		return stableGitIdentity(d.Git.Repository, d.Git.Path)
	}
	return "local:" + filepath.Clean(d.Local)
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

package instill

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const claudePluginRootPlaceholder = "${CLAUDE_PLUGIN_ROOT}"

// collectOverwriteMCPEntries builds the Pi MCP definition set from the selected
// root dependencies and directly selected packages. lockDependencies must be in
// fresh APM lock dependency order.
func collectOverwriteMCPEntries(projectRoot, libraryPath string, manifest APMManifest, selectedSkills, selectedPlugins, mcpCatalog []CatalogEntry, lockDependencies []apmLockDependency) ([]CatalogEntry, error) {
	selected := make([]CatalogEntry, 0, len(selectedSkills)+len(selectedPlugins))
	selected = append(selected, selectedSkills...)
	selected = append(selected, selectedPlugins...)
	selectedDeps := make(map[string]CatalogEntry)
	for _, dep := range manifest.Dependencies.APM {
		for _, entry := range selected {
			if dependencyMatchesSelectedPackage(libraryPath, dep, entry) {
				if previous, exists := selectedDeps[dep.identity()]; exists && previous.Type != entry.Type {
					return nil, fmt.Errorf("selected package source %q is ambiguous", dep.identity())
				}
				selectedDeps[dep.identity()] = entry
			}
		}
	}

	entriesByName := make(map[string]CatalogEntry, len(mcpCatalog))
	for _, entry := range mcpCatalog {
		if entry.Type == LibraryTypeMCP {
			entriesByName[entry.Name] = entry
		}
	}
	out := make([]CatalogEntry, 0, len(manifest.Dependencies.MCP))
	seen := make(map[string]struct{})
	add := func(entry CatalogEntry) {
		if _, exists := seen[entry.Name]; exists {
			return
		}
		seen[entry.Name] = struct{}{}
		entry.Type = LibraryTypeMCP
		out = append(out, entry)
	}
	for _, dep := range manifest.Dependencies.MCP {
		entry, ok := entriesByName[dep.Name]
		if !ok {
			return nil, fmt.Errorf("overwrite MCP %q is missing from the Library catalog", dep.Name)
		}
		add(entry)
	}

	seenSelected := make(map[string]bool, len(selectedDeps))
	for _, lockDep := range lockDependencies {
		var dep APMDependency
		matched := false
		for _, candidate := range manifest.Dependencies.APM {
			if candidate.Local != "" && lockDep.Source == "local" && sameLocalSource(candidate.Local, lockDep.LocalPath) {
				dep, matched = candidate, true
				break
			}
			if candidate.Git != nil && gitLockMatches(candidate, lockDep) {
				dep, matched = candidate, true
				break
			}
		}
		if !matched {
			continue
		}
		identity := dep.identity()
		entry, direct := selectedDeps[identity]
		if !direct {
			continue
		}
		seenSelected[identity] = true
		root, err := overwriteInstalledPackageRoot(projectRoot, dep, lockDep)
		if err != nil {
			return nil, fmt.Errorf("selected %s %q: %w", entry.Type, entry.Name, err)
		}
		manifestPath := filepath.Join(root, apmManifestFileName)
		if entry.Type == LibraryTypePlugin {
			if _, err := os.Stat(manifestPath); err != nil {
				return nil, fmt.Errorf("selected Plugin %q requires installed manifest %s: %w", entry.Name, manifestPath, err)
			}
		} else if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("cannot inspect selected Skill %q manifest %s: %w", entry.Name, manifestPath, err)
		}
		packageDocument, err := loadManifestDocument(manifestPath)
		if err != nil {
			return nil, newExitErrorWithCause(ExitCode(err), fmt.Sprintf("error: cannot read selected %s %q manifest %s: %s", entry.Type, entry.Name, manifestPath, ErrorMessage(err)), err)
		}
		packageMCP, err := packageMCPDependencies(packageDocument)
		if err != nil {
			return nil, fmt.Errorf("selected %s %q manifest %s: %w", entry.Type, entry.Name, manifestPath, err)
		}
		for _, dependency := range packageMCP {
			if _, alreadySelected := seen[dependency.Name]; alreadySelected {
				continue
			}
			if projectHasPi(projectRoot) {
				if err := validateOverwritePiPackageMCP(dependency); err != nil {
					return nil, fmt.Errorf("MCP %q cannot be rendered for Pi: %w", dependency.Name, err)
				}
			}
			derived, err := overwritePackageMCPEntry(dependency, root)
			if err != nil {
				return nil, fmt.Errorf("selected %s %q MCP %q: %w", entry.Type, entry.Name, dependency.Name, err)
			}
			add(derived)
		}
	}
	for identity, entry := range selectedDeps {
		if !seenSelected[identity] {
			return nil, fmt.Errorf("selected %s %q is absent from the fresh APM lock", entry.Type, entry.Name)
		}
	}
	return out, nil
}

func packageMCPDependencies(document *manifestDocument) ([]MCPDependency, error) {
	root, err := apmManifestMapping(document.document)
	if err != nil {
		return nil, err
	}
	dependencies := mappingValue(root, "dependencies")
	if dependencies == nil || dependencies.Tag == "!!null" {
		return nil, nil
	}
	if dependencies.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("dependencies must be a mapping")
	}
	mcp := mappingValue(dependencies, "mcp")
	if mcp == nil || mcp.Tag == "!!null" {
		return nil, nil
	}
	if mcp.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("dependencies.mcp must be a list")
	}
	result := make([]MCPDependency, 0, len(mcp.Content))
	for index, node := range mcp.Content {
		if err := validatePackageMCPDependencyNode(node); err != nil {
			return nil, fmt.Errorf("unsupported dependencies.mcp declaration at index %d: %w", index, err)
		}
		dependency, ok := projectMCPDependency(node)
		if !ok {
			return nil, fmt.Errorf("unsupported dependencies.mcp declaration at index %d; expected a mapping with a name", index)
		}
		result = append(result, dependency)
	}
	return result, nil
}
func validatePackageMCPDependencyNode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected an object")
	}
	for _, key := range []string{"name", "transport", "command", "url"} {
		value := mappingValue(node, key)
		if value == nil || value.Tag == "!!null" {
			continue
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return fmt.Errorf("%s must be a string", key)
		}
		if key == "name" && strings.TrimSpace(value.Value) == "" {
			return fmt.Errorf("name must be a non-empty string")
		}
	}
	if args := mappingValue(node, "args"); args != nil && args.Tag != "!!null" {
		if args.Kind != yaml.SequenceNode {
			return fmt.Errorf("args must be a list of strings")
		}
		for _, value := range args.Content {
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return fmt.Errorf("args must be a list of strings")
			}
		}
	}
	for _, key := range []string{"env", "headers"} {
		values := mappingValue(node, key)
		if values == nil || values.Tag == "!!null" {
			continue
		}
		if values.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be a mapping of strings", key)
		}
		for index := 0; index+1 < len(values.Content); index += 2 {
			entryKey, value := values.Content[index], values.Content[index+1]
			if entryKey.Kind != yaml.ScalarNode || entryKey.Tag != "!!str" || value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return fmt.Errorf("%s must be a mapping of strings", key)
			}
		}
	}
	if registry := mappingValue(node, "registry"); registry != nil && registry.Tag != "!!null" {
		if registry.Kind != yaml.ScalarNode || registry.Tag != "!!str" && registry.Tag != "!!bool" {
			return fmt.Errorf("registry must be a boolean or string")
		}
	}
	return nil
}

func gitLockMatches(dep APMDependency, lockDep apmLockDependency) bool {
	if dep.Git == nil || lockDep.Source == "local" || lockDep.Source != "" && lockDep.Source != "git" {
		return false
	}
	repository := normalizedGitRepositoryIdentity(dep.Git.Repository)
	if repository != normalizedGitLockRepository(lockDep) {
		return false
	}
	if lockDep.VirtualPath == "" {
		if lockDep.IsVirtual {
			return false
		}
		if normalizedGitPath(dep.Git.Path) == "." {
			return true
		}
		return filepath.Base(filepath.Clean(filepath.FromSlash(dep.Git.Path))) == lockDep.Name
	}
	return stableGitIdentity(repository, dep.Git.Path) ==
		stableGitIdentity(normalizedGitLockRepository(lockDep), lockDep.VirtualPath)
}

func normalizedGitLockRepository(lockDep apmLockDependency) string {
	repository := normalizedGitRepository(lockDep.RepoURL)
	host := normalizedGitRepository(lockDep.Host)
	if host != "" {
		if lockDep.Port > 0 {
			host = net.JoinHostPort(host, strconv.Itoa(lockDep.Port))
		}
		if !strings.HasPrefix(repository, host+"/") {
			repository = host + "/" + repository
		}
	}
	return normalizedGitRepositoryIdentity(repository)
}

func normalizedGitRepositoryIdentity(repository string) string {
	repository = normalizedGitRepository(repository)
	return strings.TrimPrefix(repository, "github.com/")
}

func normalizedGitRepository(repository string) string {
	repository = strings.TrimSpace(repository)
	switch {
	case strings.HasPrefix(repository, "https://"):
		repository = strings.TrimPrefix(repository, "https://")
	case strings.HasPrefix(repository, "http://"):
		repository = strings.TrimPrefix(repository, "http://")
	case strings.HasPrefix(repository, "ssh://git@"):
		repository = strings.TrimPrefix(repository, "ssh://git@")
	case strings.HasPrefix(repository, "ssh://"):
		repository = strings.TrimPrefix(repository, "ssh://")
	case strings.HasPrefix(repository, "git@"):
		repository = strings.TrimPrefix(repository, "git@")
		if host, path, ok := strings.Cut(repository, ":"); ok {
			repository = host + "/" + path
		}
	}
	repository = strings.TrimSuffix(strings.TrimSuffix(repository, "/"), ".git")
	return strings.ToLower(repository)
}

func dependencyMatchesSelectedPackage(libraryPath string, dep APMDependency, entry CatalogEntry) bool {
	if entry.Type == LibraryTypeSkill {
		return dep.identity() == skillDependencyFromCatalog(libraryPath, entry).identity()
	}
	if entry.Type == LibraryTypePlugin {
		return dep.identity() == pluginDependencyFromCatalog(libraryPath, entry).identity()
	}
	return false
}

func sameLocalSource(left, right string) bool {
	if strings.HasPrefix(left, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			left = filepath.Join(home, left[2:])
		}
	}
	if strings.HasPrefix(right, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			right = filepath.Join(home, right[2:])
		}
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func overwriteInstalledPackageRoot(projectRoot string, dep APMDependency, lockDep apmLockDependency) (string, error) {
	if dep.Local != "" {
		if lockDep.Source != "local" || !sameLocalSource(dep.Local, lockDep.LocalPath) {
			return "", fmt.Errorf("fresh lock does not match selected local source %q", dep.Local)
		}
		base := filepath.Base(filepath.Clean(dep.Local))
		if base == "." || base == string(filepath.Separator) || base == "" {
			return "", fmt.Errorf("invalid selected local source %q", dep.Local)
		}
		root := filepath.Join(projectRoot, "apm_modules", "_local", base)
		info, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("installed local slot %s: %w", root, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("installed local slot %s is not a directory", root)
		}
		return root, nil
	}
	if dep.Git == nil || !gitLockMatches(dep, lockDep) {
		return "", fmt.Errorf("fresh lock has no matching selected package source")
	}
	paths := apmPackageDirs(projectRoot, []APMDependency{dep})
	if len(paths) != 1 {
		return "", fmt.Errorf("git source %q is not a canonical materialization", dep.Git.Repository)
	}
	info, err := os.Stat(paths[0].dir)
	if err != nil {
		return "", fmt.Errorf("installed Git package %s: %w", paths[0].dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("installed Git package %s is not a directory", paths[0].dir)
	}
	return paths[0].dir, nil
}

func overwritePackageMCPEntry(dependency MCPDependency, installedRoot string) (CatalogEntry, error) {
	if strings.TrimSpace(dependency.Name) == "" {
		return CatalogEntry{}, fmt.Errorf("definition has an empty name")
	}
	if dependency.Registry != nil {
		registry, ok := dependency.Registry.(bool)
		if !ok || registry {
			return CatalogEntry{}, fmt.Errorf("registry-backed package MCP definitions cannot be projected from the selected package manifest")
		}
	}
	if strings.TrimSpace(dependency.Command) == "" && strings.TrimSpace(dependency.URL) == "" {
		return CatalogEntry{}, fmt.Errorf("package MCP definition has no command or URL")
	}
	value := map[string]any{}
	if dependency.Transport != "" {
		value["transport"] = dependency.Transport
	}
	if dependency.Command != "" {
		value["command"] = dependency.Command
	}
	if dependency.Args != nil {
		value["args"] = dependency.Args
	}
	if dependency.URL != "" {
		value["url"] = dependency.URL
	}
	if dependency.Env != nil {
		value["env"] = dependency.Env
	}
	for key, member := range dependency.Extra {
		value[key] = member
	}
	value = expandPluginRootStrings(value, installedRoot).(map[string]any)
	raw, err := json.Marshal(value)
	if err != nil {
		return CatalogEntry{}, fmt.Errorf("cannot encode normalized definition: %w", err)
	}
	var server mcpServer
	if err := json.Unmarshal(raw, &server); err != nil {
		return CatalogEntry{}, fmt.Errorf("invalid normalized definition: %w", err)
	}
	entry := catalogEntryFromMCPServer(dependency.Name, server, false)
	return entry, nil
}

func validateOverwritePiMCPEntry(entry CatalogEntry) error {
	switch entry.Transport {
	case "stdio":
		if strings.TrimSpace(entry.Command) == "" {
			return fmt.Errorf("stdio definition has no command")
		}
	case "http", "sse":
		if strings.TrimSpace(entry.URL) == "" {
			return fmt.Errorf("%s definition has no URL", entry.Transport)
		}
	default:
		return fmt.Errorf("cannot represent transport %q in Pi", entry.Transport)
	}
	return nil
}

func validateOverwritePiPackageMCP(dependency MCPDependency) error {
	for key := range dependency.Extra {
		switch key {
		case "headers", "type":
			continue
		default:
			return fmt.Errorf("package option %q is not representable in Pi configuration", key)
		}
	}
	return nil
}

func expandPluginRootStrings(value any, root string) any {
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, claudePluginRootPlaceholder, root)
	case []string:
		for i := range typed {
			typed[i] = strings.ReplaceAll(typed[i], claudePluginRootPlaceholder, root)
		}
	case []any:
		for i := range typed {
			typed[i] = expandPluginRootStrings(typed[i], root)
		}
	case map[string]string:
		for key, member := range typed {
			typed[key] = strings.ReplaceAll(member, claudePluginRootPlaceholder, root)
		}
	case map[string]any:
		for key, member := range typed {
			typed[key] = expandPluginRootStrings(member, root)
		}
	}
	return value
}

package instill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ImportOptions struct {
	Project     Project
	LibraryPath string
}

type ImportDirectoryOptions struct {
	LibraryPath string
	Path        string
}

func ImportOldInstill(opts ImportOptions) error {
	legacyManifestPath := ProjectManifestPath(opts.Project.Root)
	legacyManifest, err := ReadManifest(legacyManifestPath)
	if err != nil {
		return err
	}

	existing, err := LoadCatalog(opts.LibraryPath, LibraryTypeSkill)
	if err != nil {
		return err
	}
	entriesByName := make(map[string]CatalogEntry, len(existing))
	for _, entry := range existing {
		entriesByName[entry.Name] = entry
	}

	for _, skill := range legacyManifest.Skills {
		if _, ok := entriesByName[skill]; ok {
			continue
		}
		path, pathErr := importSkillCatalogPath(opts.LibraryPath, opts.Project, skill)
		if pathErr != nil {
			return pathErr
		}
		entry := CatalogEntry{
			Type: LibraryTypeSkill,
			Name: skill,
			Path: path,
		}
		if category := filepath.ToSlash(filepath.Dir(skill)); category != "." {
			entry.Category = category
		}
		existing = append(existing, entry)
		entriesByName[skill] = entry
	}
	if err := WriteCatalog(opts.LibraryPath, LibraryTypeSkill, existing); err != nil {
		return err
	}

	dependencies, err := resolveSkillDependencies(opts.LibraryPath, legacyManifest.Skills)
	if err != nil {
		return err
	}
	document, manifest, err := readAPMManifestDocument(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	manifest.Dependencies.APM = mergeAPMDependencies(manifest.Dependencies.APM, dependencies)
	if err := writeAPMManifestDocumentAtomic(opts.Project.ManifestPath, document, manifest.Dependencies, "apm"); err != nil {
		return err
	}

	for _, skill := range legacyManifest.Skills {
		if err := removeManagedSymlink(filepath.Join(opts.Project.Root, claudeDirName, skillsDirName, skillLinkName(skill))); err != nil {
			return err
		}
		if err := removeManagedSymlink(filepath.Join(opts.Project.Root, agentsDirName, skillsDirName, skillLinkName(skill))); err != nil {
			return err
		}
	}
	if err := removeManagedSettingsLocal(filepath.Join(opts.Project.Root, claudeDirName, settingsLocalFileName), legacyManifest.Skills); err != nil {
		return err
	}
	if err := os.Remove(legacyManifestPath); err != nil && !os.IsNotExist(err) {
		return NewExitError(ExitFilesystem, "error: cannot remove legacy manifest: "+err.Error())
	}
	return nil
}

func ImportGraft(opts ImportOptions) error {
	names, err := readGraftLock(filepath.Join(opts.Project.Root, "graft.lock"))
	if err != nil {
		return err
	}
	mcpPath := filepath.Join(opts.Project.Root, ".mcp.json")
	servers, err := readMCPJSON(mcpPath)
	if err != nil {
		return err
	}
	rawDocument, err := readMCPJSONRawDocument(mcpPath)
	if err != nil {
		return err
	}
	rawServers, err := rawMCPServers(rawDocument)
	if err != nil {
		return err
	}
	names, err = selectedGraftServers(names, rawServers)
	if err != nil {
		return err
	}
	if missing := missingGraftServers(names, rawServers); len(missing) > 0 {
		return NewExitError(ExitGeneral, "error: graft.lock references missing .mcp.json servers: "+strings.Join(missing, ", "))
	}

	existing, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	entriesByName := make(map[string]CatalogEntry, len(existing))
	for _, entry := range existing {
		entriesByName[entry.Name] = entry
	}

	document, manifest, err := readAPMManifestDocument(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	if err := ensureAPMManifestIdentity(document, opts.Project.Root); err != nil {
		return err
	}
	dependencies := make([]MCPDependency, 0, len(names))
	imported := make(map[string]struct{}, len(names))
	for _, name := range names {
		server, ok := servers[name]
		if !ok {
			continue
		}
		entry := catalogEntryFromMCPServer(name, server, false)
		if current, ok := entriesByName[name]; ok {
			entry.Description = current.Description
		}
		entriesByName[name] = entry
		if err := writeMCPConfigMarker(opts.LibraryPath, entry); err != nil {
			return err
		}
		dependencies = append(dependencies, MCPDependency{
			Name:    entry.Name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
			URL:     entry.URL,
		})
		imported[name] = struct{}{}
	}
	merged := make([]CatalogEntry, 0, len(entriesByName))
	for _, entry := range entriesByName {
		merged = append(merged, entry)
	}
	if err := WriteCatalog(opts.LibraryPath, LibraryTypeMCP, merged); err != nil {
		return err
	}
	manifest.Dependencies.MCP = mergeMCPDependencies(manifest.Dependencies.MCP, dependencies)
	if err := writeAPMManifestDocumentAtomic(opts.Project.ManifestPath, document, manifest.Dependencies, "mcp"); err != nil {
		return err
	}

	if err := removeImportedMCPServers(mcpPath, rawDocument, imported); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(opts.Project.Root, "graft.lock")); err != nil && !os.IsNotExist(err) {
		return NewExitError(ExitFilesystem, "error: cannot remove graft.lock: "+err.Error())
	}
	return nil
}

func ImportClaude(opts ImportOptions) error {
	configPath, err := claudeConfigPath()
	if err != nil {
		return err
	}
	servers, err := readClaudeServers(configPath)
	if err != nil {
		return err
	}

	existing, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	entriesByName := make(map[string]CatalogEntry, len(existing))
	for _, entry := range existing {
		entriesByName[entry.Name] = entry
	}

	for name, server := range servers {
		entry := catalogEntryFromMCPServer(name, server, true)
		if current, ok := entriesByName[name]; ok {
			entry.Description = current.Description
		} else {
			existing = append(existing, entry)
		}
		entriesByName[name] = entry
		if err := writeMCPConfigMarker(opts.LibraryPath, entry); err != nil {
			return err
		}
	}

	merged := make([]CatalogEntry, 0, len(entriesByName))
	for _, entry := range entriesByName {
		merged = append(merged, entry)
	}
	return WriteCatalog(opts.LibraryPath, LibraryTypeMCP, merged)
}

func ImportDirectory(opts ImportDirectoryOptions) error {
	source := opts.Path
	if strings.TrimSpace(source) == "" {
		source = opts.LibraryPath
	}
	if source != opts.LibraryPath {
		if err := importDirectoryContent(source, opts.LibraryPath); err != nil {
			return err
		}
	}
	return ScanLibrary(opts.LibraryPath, nil)
}

func removeImportedMCPServers(path string, document map[string]json.RawMessage, imported map[string]struct{}) error {
	if len(imported) == 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return NewExitError(ExitFilesystem, "error: cannot inspect .mcp.json: "+err.Error())
	}

	servers, err := rawMCPServers(document)
	if err != nil {
		return err
	}
	remaining := make(map[string]json.RawMessage, len(servers))
	for name, server := range servers {
		if _, ok := imported[name]; ok {
			continue
		}
		remaining[name] = server
	}
	if len(remaining) == 0 {
		delete(document, "mcpServers")
		if len(document) == 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return NewExitError(ExitFilesystem, "error: cannot remove .mcp.json: "+err.Error())
			}
			return nil
		}
	} else {
		raw, err := json.Marshal(remaining)
		if err != nil {
			return NewExitError(ExitGeneral, "error: cannot encode .mcp.json: "+err.Error())
		}
		document["mcpServers"] = raw
	}

	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot encode .mcp.json: "+err.Error())
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, info.Mode().Perm()); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write .mcp.json: "+err.Error())
	}
	return nil
}

func importDirectoryContent(source string, libraryPath string) error {
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		typ, ok := libraryTypeForMarker(d.Name())
		if !ok {
			return nil
		}

		contentDir := filepath.Dir(path)
		relative, err := filepath.Rel(source, contentDir)
		if err != nil {
			return err
		}
		if relative == "." {
			relative = filepath.Base(contentDir)
		}
		targetBase := filepath.Join(libraryPath, librarySubdirName(typ))
		return copyDirectoryTree(contentDir, filepath.Join(targetBase, relative))
	})
	if err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot import directory: %v", err))
	}
	return nil
}

func libraryTypeForMarker(name string) (LibraryType, bool) {
	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypeMCP, LibraryTypeInstruction, LibraryTypePrompt} {
		if name == catalogMarkerFileName(typ) {
			return typ, true
		}
	}
	return "", false
}

func librarySubdirName(typ LibraryType) string {
	switch typ {
	case LibraryTypeSkill:
		return "skills"
	case LibraryTypeMCP:
		return "mcp"
	case LibraryTypeInstruction:
		return "instructions"
	case LibraryTypePrompt:
		return "prompts"
	default:
		return ""
	}
}

func copyDirectoryTree(source string, target string) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(target, relative)

		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // Import copies user-selected local library content.
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return writeFileAtomic(targetPath, data, info.Mode().Perm())
	})
}

func readOrEmptyAPMManifest(path string) (APMManifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Manifest path is discovered under the selected project root.
	if err != nil {
		if os.IsNotExist(err) {
			return APMManifest{}, nil
		}
		return APMManifest{}, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read manifest: %v", err))
	}

	var manifest APMManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return APMManifest{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
	}
	normalizeAPMManifest(&manifest)
	return manifest, nil
}

func readAPMManifestDocument(path string) (*yaml.Node, APMManifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Manifest path is discovered under the selected project root.
	if err != nil {
		if os.IsNotExist(err) {
			manifest := APMManifest{}
			normalizeAPMManifest(&manifest)
			return emptyAPMManifestDocument(), manifest, nil
		}
		return nil, APMManifest{}, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read manifest: %v", err))
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, APMManifest{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
	}
	if document.Kind == 0 {
		document = *emptyAPMManifestDocument()
	}

	var manifest APMManifest
	if err := document.Decode(&manifest); err != nil {
		return nil, APMManifest{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
	}
	normalizeAPMManifest(&manifest)
	return &document, manifest, nil
}

func emptyAPMManifestDocument() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{
			{Kind: yaml.MappingNode},
		},
	}
}

func writeAPMManifestDocumentAtomic(path string, document *yaml.Node, dependencies APMDependencies, fields ...string) error {
	manifest := APMManifest{Dependencies: dependencies}
	normalizeAPMManifest(&manifest)
	dependencies = manifest.Dependencies
	mapping, err := apmManifestMapping(document)
	if err != nil {
		return err
	}
	dependenciesNode, err := ensureMappingNode(mapping, "dependencies")
	if err != nil {
		return err
	}
	for _, field := range fields {
		switch field {
		case "apm":
			node, encodeErr := yamlNode(dependencies.APM)
			if encodeErr != nil {
				return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", encodeErr))
			}
			setMappingValue(dependenciesNode, "apm", node)
		case "mcp":
			node, encodeErr := yamlNode(dependencies.MCP)
			if encodeErr != nil {
				return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", encodeErr))
			}
			setMappingValue(dependenciesNode, "mcp", node)
		}
	}

	data, err := yaml.Marshal(document)
	if err != nil {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", err))
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write manifest: %v", err))
	}
	return nil
}

func normalizeAPMManifest(manifest *APMManifest) {
	manifest.Dependencies.APM = normalizeStringSlice(manifest.Dependencies.APM)
	if manifest.Dependencies.MCP == nil {
		manifest.Dependencies.MCP = []MCPDependency{}
	}
}

func apmManifestMapping(document *yaml.Node) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode {
		return nil, NewExitError(ExitGeneral, "error: malformed manifest: expected document")
	}
	if len(document.Content) == 0 {
		document.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, NewExitError(ExitGeneral, "error: malformed manifest: expected mapping")
	}
	return document.Content[0], nil
}

func ensureAPMManifestIdentity(document *yaml.Node, root string) error {
	mapping, err := apmManifestMapping(document)
	if err != nil {
		return err
	}
	if mappingValue(mapping, "name") == nil {
		setMappingValue(mapping, "name", scalarNode(filepath.Base(root)))
	}
	if mappingValue(mapping, "version") == nil {
		setMappingValue(mapping, "version", scalarNode("0.1.0"))
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func ensureMappingNode(mapping *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		value := mapping.Content[i+1]
		if value.Kind != yaml.MappingNode {
			return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %s must be a mapping", key))
		}
		return value, nil
	}
	value := &yaml.Node{Kind: yaml.MappingNode}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
	return value, nil
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		mapping.Content[i+1] = value
		return
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func yamlNode(value any) (*yaml.Node, error) {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	return node, nil
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func mergeAPMDependencies(existing []string, imported []string) []string {
	return normalizeStringSlice(append(append([]string{}, existing...), imported...))
}

func mergeMCPDependencies(existing []MCPDependency, imported []MCPDependency) []MCPDependency {
	merged := append([]MCPDependency{}, existing...)
	positions := make(map[string]int, len(existing))
	for i, dependency := range merged {
		if strings.TrimSpace(dependency.Name) == "" {
			continue
		}
		positions[dependency.Name] = i
	}
	for _, dependency := range imported {
		if index, ok := positions[dependency.Name]; ok {
			merged[index] = dependency
			continue
		}
		positions[dependency.Name] = len(merged)
		merged = append(merged, dependency)
	}
	return merged
}

func importSkillCatalogPath(libraryPath string, project Project, skill string) (string, error) {
	skillRoot := filepath.Join(libraryPath, "skills", filepath.FromSlash(skill))
	if ok, err := hasSkillMarker(skillRoot); err == nil && ok {
		return filepath.ToSlash(filepath.Join(skill, "SKILL.md")), nil
	}

	legacyLink := filepath.Join(project.Root, claudeDirName, skillsDirName, skillLinkName(skill))
	target, err := os.Readlink(legacyLink)
	if err != nil {
		return "", NewExitError(ExitFilesystem, "error: cannot resolve legacy skill symlink: "+err.Error())
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(legacyLink), target)
	}
	base := filepath.Join(libraryPath, "skills")
	relative, err := filepath.Rel(base, filepath.Join(target, "SKILL.md"))
	if err != nil {
		return "", NewExitError(ExitFilesystem, "error: cannot map legacy skill into library: "+err.Error())
	}
	return filepath.ToSlash(relative), nil
}

func removeManagedSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return NewExitError(ExitFilesystem, "error: cannot inspect legacy artifact: "+err.Error())
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return NewExitError(ExitFilesystem, "error: cannot remove legacy artifact: "+err.Error())
	}
	return nil
}

func removeManagedSettingsLocal(path string, skills []string) error {
	settings, mode, _, err := readSettingsLocalTree(path)
	if err != nil {
		if strings.Contains(ErrorMessage(err), "cannot read settings.local.json") && os.IsNotExist(unwrapOSError(err)) {
			return nil
		}
		return err
	}
	if len(settings) == 0 {
		return nil
	}

	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		return nil
	}
	allow, err := reconcileAllowPermissions(permissions["allow"], skills, nil)
	if err != nil {
		return err
	}
	if len(allow) == 0 {
		delete(settings, "permissions")
	} else {
		permissions["allow"] = allow
	}

	if len(settings) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return NewExitError(ExitFilesystem, "error: cannot remove settings.local.json: "+err.Error())
		}
		return nil
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot encode settings.local.json: "+err.Error())
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, mode); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write settings.local.json: "+err.Error())
	}
	return nil
}

type graftLockMCP struct {
	Name string `yaml:"name"`
}

type graftLockFile struct {
	Servers []string       `yaml:"servers"`
	MCPs    []graftLockMCP `yaml:"mcps"`
}

func readGraftLock(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Graft import reads project-local migration input.
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot read graft.lock: "+err.Error())
	}
	var lock graftLockFile
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed graft.lock: %v", err))
	}
	names := append([]string{}, lock.Servers...)
	for _, mcp := range lock.MCPs {
		if strings.TrimSpace(mcp.Name) != "" {
			names = append(names, mcp.Name)
		}
	}
	return normalizeStringSlice(names), nil
}

type graftManagedMarker struct {
	Managed bool `json:"_graft_managed"`
}

func selectedGraftServers(locked []string, servers map[string]json.RawMessage) ([]string, error) {
	names := append([]string{}, locked...)
	for name, raw := range servers {
		var marker graftManagedMarker
		if err := json.Unmarshal(raw, &marker); err != nil {
			return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed .mcp.json server %q: %v", name, err))
		}
		if marker.Managed {
			names = append(names, name)
		}
	}
	names = normalizeStringSlice(names)
	sort.Strings(names)
	if len(names) == 0 {
		return nil, NewExitError(ExitGeneral, "error: no Graft-managed MCP servers found in graft.lock or .mcp.json")
	}
	return names, nil
}

type mcpJSONFile struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpJSONRawFile struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

type mcpServer struct {
	Type      string            `json:"type"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	URL       string            `json:"url"`
	Env       map[string]string `json:"env"`
}

func readMCPJSON(path string) (map[string]mcpServer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Import reads project-local migration input.
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot read .mcp.json: "+err.Error())
	}
	var config mcpJSONFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed .mcp.json: %v", err))
	}
	return config.MCPServers, nil
}

func readMCPJSONRaw(path string) (map[string]json.RawMessage, error) {
	document, err := readMCPJSONRawDocument(path)
	if err != nil {
		return nil, err
	}
	return rawMCPServers(document)
}

func readMCPJSONRawDocument(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Import reads project-local migration input.
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot read .mcp.json: "+err.Error())
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed .mcp.json: %v", err))
	}
	if document == nil {
		document = map[string]json.RawMessage{}
	}
	return document, nil
}

func rawMCPServers(document map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw, ok := document["mcpServers"]
	if !ok || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed .mcp.json: %v", err))
	}
	if servers == nil {
		return map[string]json.RawMessage{}, nil
	}
	return servers, nil
}

func missingGraftServers(names []string, servers map[string]json.RawMessage) []string {
	missing := make([]string, 0)
	for _, name := range names {
		if _, ok := servers[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

type claudeConfigFile struct {
	MCPServers map[string]mcpServer           `json:"mcpServers"`
	Projects   map[string]claudeProjectConfig `json:"projects"`
}

type claudeProjectConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

func claudeConfigPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", NewExitError(ExitEnvironment, "error: cannot resolve home directory: "+err.Error())
	}
	return filepath.Join(home, ".claude.json"), nil
}

func readClaudeServers(path string) (map[string]mcpServer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Import reads explicit user config for migration.
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot read Claude config: "+err.Error())
	}
	var config claudeConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed Claude config: %v", err))
	}

	servers := make(map[string]mcpServer)
	for name, server := range config.MCPServers {
		servers[name] = server
	}
	for _, project := range config.Projects {
		for name, server := range project.MCPServers {
			servers[name] = server
		}
	}
	return servers, nil
}

func catalogEntryFromMCPServer(name string, server mcpServer, redactEnv bool) CatalogEntry {
	entry := CatalogEntry{
		Type:    LibraryTypeMCP,
		Name:    name,
		Command: server.Command,
		Args:    append([]string{}, server.Args...),
		URL:     server.URL,
	}
	if strings.TrimSpace(server.Command) != "" {
		entry.Transport = "stdio"
	} else if strings.TrimSpace(server.URL) != "" {
		entry.Transport = strings.TrimSpace(server.Transport)
		if entry.Transport == "" {
			entry.Transport = strings.TrimSpace(server.Type)
		}
		if entry.Transport == "" {
			entry.Transport = "http"
		}
	}
	entry.Env = formatEnvList(server.Env, redactEnv)
	return entry
}

func writeMCPConfigMarker(libraryPath string, entry CatalogEntry) error {
	markerPath, err := mcpConfigMarkerPath(libraryPath, entry.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write mcp config: "+err.Error())
	}

	config := struct {
		Transport   string   `json:"transport"`
		Command     string   `json:"command,omitempty"`
		Args        []string `json:"args,omitempty"`
		URL         string   `json:"url,omitempty"`
		Env         []string `json:"env,omitempty"`
		Description string   `json:"description,omitempty"`
	}{
		Transport:   entry.Transport,
		Command:     entry.Command,
		Args:        append([]string{}, entry.Args...),
		URL:         entry.URL,
		Env:         append([]string{}, entry.Env...),
		Description: entry.Description,
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot encode mcp config: "+err.Error())
	}
	data = append(data, '\n')
	if err := writeFileAtomic(markerPath, data, 0o644); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write mcp config: "+err.Error())
	}
	return nil
}

func mcpConfigMarkerPath(libraryPath string, name string) (string, error) {
	localName, err := filepath.Localize(name)
	if err != nil {
		return "", NewExitError(ExitGeneral, "error: invalid mcp name: "+name)
	}
	return filepath.Join(libraryPath, "mcp", localName, "config.json"), nil
}

func formatEnvList(values map[string]string, redact bool) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if redact {
			value = "${" + key + "}"
		}
		env = append(env, key+"="+value)
	}
	return env
}

func unwrapOSError(err error) error {
	type unwrapper interface {
		Unwrap() error
	}
	current := err
	for current != nil {
		if _, ok := current.(*os.PathError); ok {
			return current
		}
		u, ok := current.(unwrapper)
		if !ok {
			return nil
		}
		current = u.Unwrap()
	}
	return nil
}

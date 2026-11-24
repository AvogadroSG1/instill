package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/smm-h/go-toml-edit"
	"gopkg.in/yaml.v3"
)

const overwriteAPMVersion = "0.35.0"

// ApplyProjectSelection replaces the complete typed project selection and reconciles project-local outputs.
// It requires APM 0.35.0 or later, resets managed target namespaces including manual entries, and never
// writes user-global MCP configuration. It rejects targets whose MCP integration requires global writes.
func ApplyProjectSelection(opts ProjectSelectionOptions) error {
	versionRunner := opts.Runner
	if versionRunner == nil {
		versionRunner = defaultCommandRunner
	}
	version, err := apmVersion(versionRunner)
	if err != nil {
		return wrapAPMEnvironmentError(err)
	}
	if !versionAtLeast(version, overwriteAPMVersion) {
		return NewExitError(ExitEnvironment, "error: pick --overwrite requires apm 0.35.0 or newer")
	}

	return withRootLocks(context.Background(), []string{opts.LibraryPath, opts.Project.Root}, func(ctx context.Context, held *heldLocks) error {
		return applyProjectSelectionLocked(ctx, held, opts)
	})
}

func applyProjectSelectionLocked(ctx context.Context, held *heldLocks, opts ProjectSelectionOptions) error {
	if err := held.requireContext(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, opts.Project.Root); err != nil {
		return err
	}

	selection, err := buildProjectSelectionPlan(opts)
	if err != nil {
		return err
	}
	document, err := loadManifestDocument(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	manifest := document.projection
	manifest.Targets, err = declaredOverwriteTargets(document)
	if err != nil {
		return err
	}
	manifest.Dependencies.APM = selection.APM
	manifest.Dependencies.MCP = selection.MCP

	reset, err := prepareProjectOverwrite(opts, manifest, selection.MCPCatalog, document)
	if err != nil {
		return err
	}
	manifest.Targets = reset.resolvedTargets
	if err := document.setTargets(manifest.Targets, false); err != nil {
		return err
	}
	if err := removeLegacyOverwriteTarget(document); err != nil {
		return err
	}
	if err := document.replaceAPM(selection.APM); err != nil {
		return err
	}
	if err := document.replaceMCP(selection.MCP); err != nil {
		return err
	}
	if err := document.repairIdentity(opts.Project.Root, false); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}
	for _, content := range selection.Content {
		if err := content.publish(); err != nil {
			return err
		}
	}
	if err := releaseMCPLibraryLock(ctx, held, opts.LibraryPath, opts.Project.Root); err != nil {
		return err
	}

	root := reset.projectRoot()
	if err := reset.apply(ctx, held); err != nil {
		return err
	}
	if err := ensureOverwriteTargetRoots(root, reset.resolvedTargets); err != nil {
		return err
	}
	if err := reset.detachLock(); err != nil {
		return err
	}

	installArgs := []string{"install", "--root", root}
	piOnly := isPiOnlyOverwriteTarget(reset.piConfigSnapshot != nil, reset.resolvedTargets)
	if piOnly && (len(manifest.Dependencies.APM) > 0 || len(manifest.Dependencies.MCP) > 0) {
		installArgs = append(installArgs, "--only", "apm")
	}
	if err := runOverwriteAPMCommand(opts.Runner, root, installArgs...); err != nil {
		return err
	}
	if err := runOverwriteAPMCommand(opts.Runner, root, "prune"); err != nil {
		return err
	}
	if err := runOverwriteAPMCommand(opts.Runner, root, "compile", "--clean", "--root", root); err != nil {
		return err
	}

	lock, err := readFreshOverwriteLock(root)
	if err != nil {
		return err
	}
	if len(manifest.Dependencies.APM) > 0 && !lock.exists {
		return NewExitError(ExitFilesystem, "error: APM install did not produce apm.lock.yaml for selected packages")
	}
	if err := verifyOverwriteLockDeployments(root, lock); err != nil {
		return err
	}

	selectedSkills, err := selectedOverwriteEntries(selection.Skills, opts.Selection.Skills, true)
	if err != nil {
		return err
	}
	selectedPlugins, err := selectedOverwriteEntries(selection.Plugins, opts.Selection.Plugins, false)
	if err != nil {
		return err
	}
	mcpEntries, err := collectOverwriteMCPEntries(root, opts.LibraryPath, manifest, selectedSkills, selectedPlugins, selection.MCPCatalog, lock.dependencies)
	if err != nil {
		return err
	}
	if projectHasPi(root) {
		for _, entry := range mcpEntries {
			if err := validateOverwritePiMCPEntry(entry); err != nil {
				return NewExitError(ExitGeneral, fmt.Sprintf("error: MCP %q cannot be rendered for Pi: %v", entry.Name, err))
			}
		}
	}
	if err := verifyOverwriteMCPOutputs(root, reset.resolvedTargets, lock, mcpEntries, piOnly); err != nil {
		return err
	}

	policy := newMCPInstallPolicy(mcpEntries, manifest.Dependencies.MCP, false)
	if err := reconcileOpenCodeMCPEnableState(root, reset.before, policy, true); err != nil {
		return err
	}
	if err := reconcileCodexMCPEnableState(root, reset.before, policy, true); err != nil {
		return err
	}
	if projectHasPi(root) {
		if reset.piConfigSnapshot == nil {
			return filesystemError("error: Pi project configuration appeared during overwrite")
		}
		if err := reconcileOverwritePiMCPConfig(root, mcpEntries, reset.piConfigSnapshot.before); err != nil {
			return err
		}
		if err := verifyOverwritePiMCPConfig(root, mcpEntries, reset.before.PiMerged); err != nil {
			return err
		}
	}

	var openCodeSources []openCodePluginSource
	openCodeActive := slices.Contains(reset.resolvedTargets, "opencode")
	if openCodeActive {
		openCodeSources = apmPackageDirs(root, manifest.Dependencies.APM)
		if _, err := reconcileOpenCodePlugins(root, openCodeSources); err != nil {
			return err
		}
	}
	if err := verifyOverwriteOpenCodePlugins(root, openCodeSources); err != nil {
		return err
	}

	return writeLine(opts.Stdout, "ok: replaced project selection and reconciled targets")
}

func declaredOverwriteTargets(document *manifestDocument) ([]string, error) {
	root, err := apmManifestMapping(document.document)
	if err != nil {
		return nil, err
	}
	if targets := mappingValue(root, "targets"); targets != nil {
		if targets.Kind != yaml.SequenceNode {
			return nil, malformedManifestError("targets", "must be a list of target strings")
		}
		if len(targets.Content) > 0 {
			values := make([]string, 0, len(targets.Content))
			for index, target := range targets.Content {
				if target.Kind != yaml.ScalarNode || target.Tag != "!!str" || strings.TrimSpace(target.Value) == "" {
					return nil, malformedManifestError(fmt.Sprintf("targets[%d]", index), "must be a non-empty target string")
				}
				values = append(values, target.Value)
			}
			return values, nil
		}
	}
	target := mappingValue(root, "target")
	if target == nil {
		return []string{}, nil
	}
	if target.Kind == yaml.ScalarNode && target.Tag == "!!str" {
		if strings.TrimSpace(target.Value) == "" {
			return nil, malformedManifestError("target", "must be a non-empty target string or a list of targets")
		}
		return []string{target.Value}, nil
	}
	if target.Kind != yaml.SequenceNode {
		return nil, malformedManifestError("target", "must be a target string or a list of targets")
	}
	targets := make([]string, 0, len(target.Content))
	for index, item := range target.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || strings.TrimSpace(item.Value) == "" {
			return nil, malformedManifestError(fmt.Sprintf("target[%d]", index), "must be a non-empty target string")
		}
		targets = append(targets, item.Value)
	}
	return targets, nil
}

func removeLegacyOverwriteTarget(document *manifestDocument) error {
	root, err := apmManifestMapping(document.document)
	if err != nil {
		return err
	}
	if mappingValue(root, "target") == nil {
		return nil
	}
	if err := document.checkMappingKeyMutation(root, "target", root, "target", "remove legacy target"); err != nil {
		return err
	}
	removeMappingValue(root, "target")
	document.dirty = true
	return nil
}

func runOverwriteAPMCommand(runner CommandRunner, root string, args ...string) error {
	var err error
	if runner == nil {
		err = runCommandInDir(root, "apm", args...)
	} else {
		err = runCommand(runner, "apm", args...)
	}
	if err != nil {
		return wrapCommandError("apm", err)
	}
	return nil
}

func overwriteTargetRootPaths(targets []string) []string {
	profiles := map[string][]string{
		"agent-skills": {".agents"}, "antigravity": {".agents", ".antigravity"}, "claude": {".claude"},
		"codex": {".codex", ".agents"}, "copilot": {".github", ".agents"}, "cursor": {".cursor", ".agents"},
		"gemini": {".gemini", ".agents"}, "grok-build": {".grok"}, "kiro": {".kiro"},
		"opencode": {".opencode", ".agents"}, "vscode": {".vscode", ".agents"},
	}
	roots := make(map[string]struct{})
	for _, target := range targets {
		for _, rel := range profiles[target] {
			roots[rel] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(roots))
	for rel := range roots {
		ordered = append(ordered, rel)
	}
	sort.Strings(ordered)
	return ordered
}

func validateOverwriteTargetRoots(root string, targets []string) error {
	for _, rel := range overwriteTargetRootPaths(targets) {
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return filesystemError("error: cannot inspect project target root: "+path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return filesystemError("error: refusing unsafe project target root: " + path)
		}
	}
	return nil
}

func ensureOverwriteTargetRoots(root string, targets []string) error {
	if err := validateOverwriteTargetRoots(root, targets); err != nil {
		return err
	}
	for _, rel := range overwriteTargetRootPaths(targets) {
		path := filepath.Join(root, rel)
		if err := os.Mkdir(path, 0o750); err != nil && !os.IsExist(err) {
			if !os.IsNotExist(err) {
				if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
					continue
				}
			}
			return filesystemError("error: cannot create project target root: "+path, err)
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return filesystemError("error: cannot safely create project target root: "+path, err)
		}
	}
	return nil
}

type overwriteLockState struct {
	exists           bool
	dependencies     []apmLockDependency
	dependencyFiles  [][]string
	mcpServers       []string
	mcpTargetServers map[string][]string
	mcpConfigs       map[string]*yaml.Node
	deploymentValues []string
}

func readFreshOverwriteLock(root string) (overwriteLockState, error) {
	state := overwriteLockState{
		dependencies:     []apmLockDependency{},
		dependencyFiles:  [][]string{},
		mcpServers:       []string{},
		mcpTargetServers: map[string][]string{},
		mcpConfigs:       map[string]*yaml.Node{},
		deploymentValues: []string{},
	}
	path := filepath.Join(root, "apm.lock.yaml")
	data, _, err := readMCPFile(path, true)
	if err != nil {
		return state, err
	}
	if data == nil {
		return state, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return state, NewExitError(ExitFilesystem, "error: malformed apm.lock.yaml: "+err.Error())
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure")
	}
	lockRoot := document.Content[0]
	if err := validateOverwriteLockNode(lockRoot); err != nil {
		return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure: "+err.Error())
	}
	if err := validateOverwriteLockSchema(lockRoot); err != nil {
		return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure: "+err.Error())
	}
	state.exists = true

	dependencies := mappingValue(lockRoot, "dependencies")
	if dependencies != nil {
		if dependencies.Kind != yaml.SequenceNode {
			return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml dependencies structure")
		}
		for _, node := range dependencies.Content {
			if node.Kind != yaml.MappingNode {
				return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml dependency structure")
			}
			if scope := mappingValue(node, "scope"); scope != nil &&
				(scope.Kind != yaml.ScalarNode || scope.Tag != "!!str" || strings.ToLower(strings.TrimSpace(scope.Value)) != "project") {
				return state, NewExitError(ExitFilesystem, "error: apm.lock.yaml contains an unsupported dependency scope")
			}
			var dependency apmLockDependency
			if err := node.Decode(&dependency); err != nil {
				return state, NewExitError(ExitFilesystem, "error: malformed apm.lock.yaml dependency: "+err.Error())
			}
			files := mappingValue(node, "deployed_files")
			var paths []string
			if files != nil {
				if files.Kind != yaml.SequenceNode {
					return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployed_files structure")
				}
				for _, file := range files.Content {
					if file.Kind != yaml.ScalarNode || file.Tag != "!!str" {
						return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployed_files path")
					}
					paths = append(paths, file.Value)
				}
			}
			state.dependencies = append(state.dependencies, dependency)
			state.dependencyFiles = append(state.dependencyFiles, paths)
		}
	}

	if err := decodeOverwriteLockStringList(lockRoot, "mcp_servers", &state.mcpServers); err != nil {
		return state, err
	}
	if err := decodeOverwriteLockStringMap(lockRoot, "mcp_target_servers", &state.mcpTargetServers); err != nil {
		return state, err
	}
	configs := mappingValue(lockRoot, "mcp_configs")
	if configs != nil && configs.Tag != "!!null" {
		if configs.Kind != yaml.MappingNode {
			return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml mcp_configs structure")
		}
		for index := 0; index+1 < len(configs.Content); index += 2 {
			key := configs.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml mcp_configs key")
			}
			state.mcpConfigs[key.Value] = configs.Content[index+1]
		}
	}
	deployments := mappingValue(lockRoot, "deployments")
	if deployments != nil {
		if deployments.Kind != yaml.SequenceNode {
			return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployments structure")
		}
		for _, row := range deployments.Content {
			if row.Kind != yaml.MappingNode {
				return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployment structure")
			}
			target := mappingValue(row, "target")
			if target == nil || target.Kind != yaml.ScalarNode || target.Tag != "!!str" {
				return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployment target")
			}
			if target.Value == "mcp" {
				continue
			}
			scope := mappingValue(row, "scope")
			if scope == nil || scope.Kind != yaml.ScalarNode || scope.Tag != "!!str" || strings.ToLower(strings.TrimSpace(scope.Value)) != "project" {
				return state, NewExitError(ExitFilesystem, "error: apm.lock.yaml contains a non-project deployment")
			}
			value := mappingValue(row, "value")
			if value == nil || value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return state, NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployment value")
			}
			state.deploymentValues = append(state.deploymentValues, value.Value)
		}
	}
	return state, nil
}

func decodeOverwriteLockStringMap(root *yaml.Node, key string, output *map[string][]string) error {
	node := mappingValue(root, key)
	if node == nil || node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml "+key+" structure")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		name, values := node.Content[index], node.Content[index+1]
		if name.Kind != yaml.ScalarNode || name.Tag != "!!str" || values.Kind != yaml.SequenceNode {
			return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml "+key+" entry")
		}
		servers := make([]string, 0, len(values.Content))
		seen := make(map[string]struct{}, len(values.Content))
		for _, value := range values.Content {
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
				return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml "+key+" server name")
			}
			if _, duplicate := seen[value.Value]; duplicate {
				return NewExitError(ExitFilesystem, "error: duplicate MCP server in apm.lock.yaml "+key)
			}
			seen[value.Value] = struct{}{}
			servers = append(servers, value.Value)
		}
		(*output)[name.Value] = servers
	}
	return nil
}

func decodeOverwriteLockStringList(root *yaml.Node, key string, output *[]string) error {
	node := mappingValue(root, key)
	if node == nil || node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml "+key+" structure")
	}
	seen := make(map[string]struct{}, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Value == "" {
			return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml "+key+" entry")
		}
		if _, duplicate := seen[item.Value]; duplicate {
			return NewExitError(ExitFilesystem, "error: duplicate MCP server in apm.lock.yaml "+key)
		}
		seen[item.Value] = struct{}{}
		*output = append(*output, item.Value)
	}
	return nil
}
func verifyOverwriteLockDeployments(root string, lock overwriteLockState) error {
	for index, files := range lock.dependencyFiles {
		for _, path := range files {
			if err := requireOverwriteOutput(root, path); err != nil {
				return NewExitError(ExitFilesystem, fmt.Sprintf("error: APM lock dependency %d is missing deployed output %q: %v", index, path, err))
			}
		}
	}
	for _, path := range lock.deploymentValues {
		if err := requireOverwriteOutput(root, path); err != nil {
			return NewExitError(ExitFilesystem, "error: APM lock deployment is missing project output "+path)
		}
	}
	return nil
}

func requireOverwriteOutput(root, relative string) error {
	if !projectLockPathInside(root, relative) {
		return fmt.Errorf("path is outside the Project")
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
		return err
	}
	return nil
}

func selectedOverwriteEntries(catalog []CatalogEntry, names []string, allowSuffix bool) ([]CatalogEntry, error) {
	byName := catalogEntriesByName(catalog)
	selected := make([]CatalogEntry, 0, len(names))
	for _, name := range normalizeSkills(names) {
		entry, ok := resolveCatalogEntryName(byName, name, allowSuffix)
		if !ok {
			return nil, unknownLibraryEntryError(string(catalogType(catalog)), name)
		}
		selected = append(selected, entry)
	}
	return selected, nil
}

func catalogType(entries []CatalogEntry) LibraryType {
	if len(entries) == 0 {
		return "package"
	}
	return entries[0].Type
}

type overwriteMCPRuntime struct {
	target string
	path   string
	key    string
}

var overwriteMCPRuntimes = []overwriteMCPRuntime{
	{target: "antigravity", path: ".agents/mcp_config.json", key: "mcpServers"},
	{target: "claude", path: ".mcp.json", key: "mcpServers"},
	{target: "codex", path: ".codex/config.toml", key: "mcp_servers"},
	{target: "copilot", path: ".github/mcp.json", key: "mcpServers"},
	{target: "cursor", path: ".cursor/mcp.json", key: "mcpServers"},
	{target: "gemini", path: ".gemini/settings.json", key: "mcpServers"},
	{target: "kiro", path: ".kiro/settings/mcp.json", key: "mcpServers"},
	{target: "opencode", path: "opencode.json", key: "mcp"},
	{target: "vscode", path: ".vscode/mcp.json", key: "servers"},
}

func verifyOverwriteMCPOutputs(root string, targets []string, lock overwriteLockState, entries []CatalogEntry, piOnly bool) error {
	expected := make([]string, 0, len(entries))
	for _, entry := range entries {
		expected = append(expected, entry.Name)
	}
	sort.Strings(expected)
	if !piOnly {
		if err := verifyOverwriteMCPConfigs(lock, entries, expected); err != nil {
			return err
		}
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[target] = struct{}{}
	}
	for _, runtime := range overwriteMCPRuntimes {
		want := []string{}
		_, active := targetSet[runtime.target]
		if active {
			want = expected
		}
		actualLock := lock.mcpTargetServers[runtime.target]
		if !equalStringSlices(sortedUniqueStrings(actualLock), want) {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: APM did not reconcile MCP servers for project target %s: got %v, want %v", runtime.target, actualLock, want))
		}
		actual, err := projectOverwriteMCPNames(root, runtime)
		if err != nil {
			return err
		}
		if !equalStringSlices(sortedUniqueStrings(actual), want) {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: project MCP configuration for %s does not match the selection: got %v, want %v", runtime.target, actual, want))
		}
		if active {
			definitions, err := projectOverwriteMCPDefinitions(root, runtime)
			if err != nil {
				return err
			}
			for _, expectedEntry := range entries {
				actualEntry, exists := definitions[expectedEntry.Name]
				if !exists || !equalOverwriteMCPRuntimeDefinition(expectedEntry, actualEntry, runtime.target) {
					return NewExitError(ExitGeneral, fmt.Sprintf("error: project MCP definition for %s does not match selected server %q", runtime.target, expectedEntry.Name))
				}
			}
		}
	}
	for _, target := range []string{"windsurf", "hermes"} {
		if len(lock.mcpTargetServers[target]) > 0 {
			return NewExitError(ExitGeneral, "error: APM unexpectedly reattached user-global MCP target "+target)
		}
	}
	return verifyOverwriteOpenCodeLayers(root)
}

func verifyOverwriteMCPConfigs(lock overwriteLockState, entries []CatalogEntry, expected []string) error {
	lockServers := sortedUniqueStrings(lock.mcpServers)
	if !equalStringSlices(lockServers, expected) {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: APM lock MCP servers do not match the selection: got %v, want %v", lockServers, expected))
	}
	configNames := make([]string, 0, len(lock.mcpConfigs))
	for name := range lock.mcpConfigs {
		configNames = append(configNames, name)
	}
	sort.Strings(configNames)
	if !equalStringSlices(configNames, expected) {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: APM lock MCP definitions do not match the selection: got %v, want %v", configNames, expected))
	}
	for _, expectedEntry := range entries {
		node := lock.mcpConfigs[expectedEntry.Name]
		if node == nil || node.Kind != yaml.MappingNode {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: APM lock is missing the MCP definition for %s", expectedEntry.Name))
		}
		var definition struct {
			Type      string            `yaml:"type"`
			Transport string            `yaml:"transport"`
			Command   string            `yaml:"command"`
			Args      []string          `yaml:"args"`
			URL       string            `yaml:"url"`
			Env       map[string]string `yaml:"env"`
			Headers   map[string]string `yaml:"headers"`
		}
		if err := node.Decode(&definition); err != nil {
			return NewExitError(ExitFilesystem, fmt.Sprintf("error: malformed APM lock MCP definition %s: %v", expectedEntry.Name, err))
		}
		actual := catalogEntryFromMCPServer(expectedEntry.Name, mcpServer{
			Type: definition.Type, Transport: definition.Transport, Command: definition.Command,
			Args: definition.Args, URL: definition.URL, Env: definition.Env, Headers: definition.Headers,
		}, false)
		if !equalOverwriteMCPDefinition(expectedEntry, actual) {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: APM lock MCP definition for %s does not match the selected definition", expectedEntry.Name))
		}
	}
	return nil
}

func equalOverwriteMCPDefinition(expected, actual CatalogEntry) bool {
	return expected.Transport == actual.Transport &&
		equalOverwriteMCPValue(expected.Command, actual.Command) &&
		equalOverwriteMCPValues(expected.Args, actual.Args) &&
		equalOverwriteMCPValue(expected.URL, actual.URL) &&
		equalOverwriteMCPValueMaps(expected.Env, actual.Env, false) &&
		equalOverwriteMCPValueMaps(expected.Headers, actual.Headers, true)
}

func equalOverwriteMCPRuntimeDefinition(expected, actual CatalogEntry, target string) bool {
	if (target == "codex" || target == "opencode") && (expected.Transport == "http" || expected.Transport == "sse") {
		expected.Env = nil
	}
	return equalOverwriteMCPDefinition(expected, actual)
}

var overwriteMCPEnvironmentReference = regexp.MustCompile(
	`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}|\{env:([A-Za-z_][A-Za-z0-9_]*)\}`,
)

func normalizeOverwriteMCPValue(value string) string {
	return overwriteMCPEnvironmentReference.ReplaceAllStringFunc(value, func(reference string) string {
		name := strings.TrimPrefix(reference, "${env:")
		if name == reference {
			name = strings.TrimPrefix(reference, "{env:")
		}
		name = strings.TrimSuffix(name, "}")
		return "${" + name + "}"
	})
}

func equalOverwriteMCPValue(expected, actual string) bool {
	return normalizeOverwriteMCPValue(expected) == normalizeOverwriteMCPValue(actual)
}

func equalOverwriteMCPValues(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if !equalOverwriteMCPValue(expected[index], actual[index]) {
			return false
		}
	}
	return true
}

func equalOverwriteMCPValueMaps(expected, actual []string, httpHeaders bool) bool {
	toMap := func(values []string) map[string]string {
		result := make(map[string]string, len(values))
		for _, pair := range values {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			if httpHeaders {
				key = strings.ToLower(key)
			}
			result[key] = normalizeOverwriteMCPValue(value)
		}
		return result
	}
	expectedValues, actualValues := toMap(expected), toMap(actual)
	if len(expectedValues) != len(actualValues) {
		return false
	}
	for key, value := range expectedValues {
		actualValue, exists := actualValues[key]
		if !exists || actualValue != value {
			return false
		}
	}
	return true
}

func sortedUniqueStrings(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func projectOverwriteMCPNames(root string, runtime overwriteMCPRuntime) ([]string, error) {
	path := filepath.Join(root, runtime.path)
	if runtime.target == "codex" {
		document, _, err := readCodexDocument(path, true)
		if err != nil {
			return nil, err
		}
		return codexDocumentNames(path, document)
	}
	data, _, err := readMCPFile(path, true)
	if err != nil {
		return nil, err
	}
	state, err := projectMCPJSONState(path, data, runtime.key, "enabled")
	if err != nil {
		return nil, err
	}
	if runtime.target == "opencode" {
		if err := validateOverwriteMCPEnableState(path, runtime.key, state); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(state))
	for name := range state {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
func projectOverwriteMCPDefinitions(root string, runtime overwriteMCPRuntime) (map[string]CatalogEntry, error) {
	path := filepath.Join(root, runtime.path)
	if runtime.target == "codex" {
		document, _, err := readCodexDocument(path, true)
		if err != nil {
			return nil, err
		}
		return codexOverwriteMCPDefinitions(path, document)
	}
	data, _, err := readMCPFile(path, true)
	if err != nil {
		return nil, err
	}
	_, object, err := parseMCPJSON(path, data)
	if err != nil {
		return nil, err
	}
	servers, err := mcpJSONObject(path, object[runtime.key], runtime.key)
	if err != nil {
		return nil, err
	}
	definitions := make(map[string]CatalogEntry, len(servers))
	for name, raw := range servers {
		entry, err := overwriteJSONMCPDefinition(name, raw, runtime.target)
		if err != nil {
			return nil, mcpConfigurationError(path, "server "+name+": "+err.Error())
		}
		definitions[name] = entry
	}
	return definitions, nil
}

func overwriteJSONMCPDefinition(name string, raw json.RawMessage, target string) (CatalogEntry, error) {
	var definition struct {
		Type          string            `json:"type"`
		Transport     string            `json:"transport"`
		Command       json.RawMessage   `json:"command"`
		Args          []string          `json:"args"`
		URL           string            `json:"url"`
		Env           map[string]string `json:"env"`
		Environment   map[string]string `json:"environment"`
		Headers       map[string]string `json:"headers"`
		HTTPHeaders   map[string]string `json:"http_headers"`
		HTTPTransport string            `json:"httpTransport"`
	}
	if err := json.Unmarshal(raw, &definition); err != nil {
		return CatalogEntry{}, fmt.Errorf("invalid MCP definition: %w", err)
	}
	command := ""
	args := definition.Args
	if len(definition.Command) > 0 {
		if bytes.TrimSpace(definition.Command)[0] == '[' {
			var parts []string
			if err := json.Unmarshal(definition.Command, &parts); err != nil || len(parts) == 0 {
				return CatalogEntry{}, fmt.Errorf("command array must contain a command")
			}
			command = parts[0]
			if len(parts) > 1 {
				if len(args) > 0 && !slices.Equal(args, parts[1:]) {
					return CatalogEntry{}, fmt.Errorf("command and args disagree")
				}
				args = parts[1:]
			}
		} else if err := json.Unmarshal(definition.Command, &command); err != nil {
			return CatalogEntry{}, fmt.Errorf("command must be a string or string array")
		}
	}
	env := definition.Env
	if env == nil {
		env = definition.Environment
	}
	headers := definition.Headers
	if headers == nil {
		headers = definition.HTTPHeaders
	}
	transport := definition.Transport
	if transport == "" {
		transport = definition.Type
	}
	if target == "opencode" {
		switch definition.Type {
		case "local":
			transport = "stdio"
		case "remote":
			if definition.Transport == "" {
				transport = "http"
			}
			if definition.HTTPTransport == "sse" {
				transport = "sse"
			}
		}
	}
	server := mcpServer{
		Type: definition.Type, Transport: transport, Command: command, Args: args,
		URL: definition.URL, Env: env, Headers: headers,
	}
	entry := catalogEntryFromMCPServer(name, server, false)
	if definition.HTTPTransport == "sse" {
		entry.Transport = "sse"
	}
	return entry, nil
}

func codexOverwriteMCPDefinitions(path string, document *tomledit.Document) (map[string]CatalogEntry, error) {
	definitions := make(map[string]CatalogEntry)
	if document == nil {
		return definitions, nil
	}
	entry, exists := document.Root().Get("mcp_servers")
	if !exists {
		return definitions, nil
	}
	servers, ok := entry.Record()
	if !ok {
		return nil, malformedCodex(path, "mcp_servers", fmt.Errorf("must be a table"))
	}
	for serverEntry := range servers.Entries() {
		name := serverEntry.Key()
		record, ok := serverEntry.Record()
		if !ok {
			return nil, malformedCodex(path, "mcp_servers."+name, fmt.Errorf("must be a table"))
		}
		command, err := tomlOverwriteString(path, record, "command")
		if err != nil {
			return nil, err
		}
		args, err := tomlOverwriteStringList(path, record, "args")
		if err != nil {
			return nil, err
		}
		url, err := tomlOverwriteString(path, record, "url")
		if err != nil {
			return nil, err
		}
		transport, err := tomlOverwriteString(path, record, "type")
		if err != nil {
			return nil, err
		}
		env, err := tomlOverwriteStringMap(path, record, "env")
		if err != nil {
			return nil, err
		}
		headers, err := tomlOverwriteStringMap(path, record, "http_headers")
		if err != nil {
			return nil, err
		}
		if len(headers) == 0 {
			headers, err = tomlOverwriteStringMap(path, record, "headers")
			if err != nil {
				return nil, err
			}
		}
		bearerTokenEnvVar, err := tomlOverwriteString(path, record, "bearer_token_env_var")
		if err != nil {
			return nil, err
		}
		envHTTPHeaders, err := tomlOverwriteStringMap(path, record, "env_http_headers")
		if err != nil {
			return nil, err
		}
		if bearerTokenEnvVar != "" || len(envHTTPHeaders) > 0 {
			if headers == nil {
				headers = make(map[string]string, len(envHTTPHeaders)+1)
			}
			if bearerTokenEnvVar != "" {
				headers["Authorization"] = "Bearer ${" + bearerTokenEnvVar + "}"
			}
			for header, variable := range envHTTPHeaders {
				headers[header] = "${" + variable + "}"
			}
		}
		entry := catalogEntryFromMCPServer(name, mcpServer{
			Type: transport, Transport: transport, Command: command, Args: args,
			URL: url, Env: env, Headers: headers,
		}, false)
		definitions[name] = entry
	}
	return definitions, nil
}

func tomlOverwriteString(path string, record *tomledit.Record, key string) (string, error) {
	entry, exists := record.Get(key)
	if !exists {
		return "", nil
	}
	node, ok := entry.Node()
	if !ok || node.Type() != tomledit.NodeString {
		return "", malformedCodex(path, "mcp_servers."+key, fmt.Errorf("must be a string"))
	}
	value, err := node.(tomledit.Scalar).AsString()
	if err != nil {
		return "", malformedCodex(path, "mcp_servers."+key, err)
	}
	return value, nil
}

func tomlOverwriteStringList(path string, record *tomledit.Record, key string) ([]string, error) {
	entry, exists := record.Get(key)
	if !exists {
		return nil, nil
	}
	node, ok := entry.Node()
	if !ok || node.Type() != tomledit.NodeArray {
		return nil, malformedCodex(path, "mcp_servers."+key, fmt.Errorf("must be an array of strings"))
	}
	array, ok := node.(*tomledit.ArrayNode)
	if !ok {
		return nil, malformedCodex(path, "mcp_servers."+key, fmt.Errorf("must be an array of strings"))
	}
	values := make([]string, 0, len(array.Elements()))
	for _, element := range array.Elements() {
		scalar, ok := element.(tomledit.Scalar)
		if !ok || element.Type() != tomledit.NodeString {
			return nil, malformedCodex(path, "mcp_servers."+key, fmt.Errorf("must be an array of strings"))
		}
		value, err := scalar.AsString()
		if err != nil {
			return nil, malformedCodex(path, "mcp_servers."+key, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func tomlOverwriteStringMap(path string, record *tomledit.Record, key string) (map[string]string, error) {
	entry, exists := record.Get(key)
	if !exists {
		return nil, nil
	}
	table, ok := entry.Record()
	if !ok {
		return nil, malformedCodex(path, "mcp_servers."+key, fmt.Errorf("must be a table of strings"))
	}
	values := make(map[string]string)
	for field := range table.Entries() {
		node, ok := field.Node()
		if !ok || node.Type() != tomledit.NodeString {
			return nil, malformedCodex(path, "mcp_servers."+key+"."+field.Key(), fmt.Errorf("must be a string"))
		}
		value, err := node.(tomledit.Scalar).AsString()
		if err != nil {
			return nil, malformedCodex(path, "mcp_servers."+key+"."+field.Key(), err)
		}
		values[field.Key()] = value
	}
	return values, nil
}

func codexDocumentNames(path string, document *tomledit.Document) ([]string, error) {
	state, err := codexDocumentEnableState(path, document)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(state))
	for name := range state {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func validateOverwriteMCPEnableState(path, namespace string, state map[string]mcpEnableValue) error {
	for name, value := range state {
		if !value.Present {
			continue
		}
		var enabled bool
		if err := json.Unmarshal(value.Value, &enabled); err != nil {
			return mcpConfigurationError(path, namespace+"."+name+".enabled must be a boolean")
		}
	}
	return nil
}

func verifyOverwriteOpenCodeLayers(root string) error {
	for _, rel := range []string{"opencode.jsonc", ".opencode/opencode.json", ".opencode/opencode.jsonc"} {
		path := filepath.Join(root, rel)
		data, _, err := readMCPFile(path, true)
		if err != nil {
			return err
		}
		state, err := projectMCPJSONState(path, data, "mcp", "enabled")
		if err != nil {
			return err
		}
		if len(state) != 0 {
			return NewExitError(ExitGeneral, "error: OpenCode project MCP output was not consolidated in opencode.json")
		}
	}
	return nil
}

func reconcileOverwritePiMCPConfig(root string, entries []CatalogEntry, expectedBefore []byte) error {
	path := piProjectMCPPath(root)
	if err := validateOverwritePiConfigPath(root, path); err != nil {
		return err
	}
	object, before, mode, err := piReadObject(path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, expectedBefore) {
		return filesystemError("error: Pi MCP configuration changed during overwrite: " + path)
	}
	_, servers, err := piServerMap(path, object)
	if err != nil {
		return err
	}
	document, _, err := parseMCPJSON(path, before)
	if err != nil {
		return err
	}
	changed := false
	patch := func(pointer string, raw json.RawMessage) error {
		edited, err := patchMCPMember(&document, pointer, raw)
		changed = changed || edited
		if err != nil {
			return piMalformed(path, err.Error())
		}
		return nil
	}
	mapPointer := "/" + mcpJSONPointer(piMCPServersKey)
	desired := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		desired[entry.Name] = entry
	}
	if len(desired) > 0 && document.Find(mapPointer) == nil {
		if err := patch(mapPointer, json.RawMessage("{}")); err != nil {
			return err
		}
	}
	if _, canonicalExists := object[piMCPServersKey]; canonicalExists {
		for name := range servers {
			if _, keep := desired[name]; keep {
				continue
			}
			if err := patch(mapPointer+"/"+mcpJSONPointer(name), nil); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw, exists := servers[name]
		pointer := mapPointer + "/" + mcpJSONPointer(name)
		entry := desired[name]
		if !exists {
			if err := patch(pointer, piRaw(piDefinition(&entry))); err != nil {
				return err
			}
			continue
		}
		if document.Find(pointer) == nil {
			if err := patch(pointer, raw); err != nil {
				return err
			}
		}
		current, err := mcpJSONObject(path, raw, "server "+name)
		if err != nil {
			return err
		}
		edited, err := piReconcileEntry(&document, pointer, current, &entry)
		changed = changed || edited
		if err != nil {
			return piMalformed(path, err.Error())
		}
	}
	if _, legacyExists := object[piLegacyServersKey]; legacyExists {
		if err := patch("/"+mcpJSONPointer(piLegacyServersKey), nil); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		if err := patch("/"+mcpJSONPointer(piManagedKey), nil); err != nil {
			return err
		}
	} else if err := patch("/"+mcpJSONPointer(piManagedKey), piRaw(names)); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return writeMCPFile(path, before, document.Pack(), mode)
}

func verifyOverwritePiMCPConfig(root string, entries []CatalogEntry, previous map[string]mcpEnableValue) error {
	path := piProjectMCPPath(root)
	object, _, _, err := piReadObject(path, true)
	if err != nil {
		return err
	}
	if _, legacyExists := object[piLegacyServersKey]; legacyExists {
		return NewExitError(ExitGeneral, "error: Pi MCP output still uses the legacy mcp-servers namespace")
	}
	_, servers, err := piServerMap(path, object)
	if err != nil {
		return err
	}
	expected := make([]string, 0, len(entries))
	byName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		expected = append(expected, entry.Name)
		byName[entry.Name] = entry
	}
	sort.Strings(expected)
	actual := make([]string, 0, len(servers))
	for name := range servers {
		actual = append(actual, name)
	}
	sort.Strings(actual)
	if !equalStringSlices(actual, expected) {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: Pi MCP configuration does not match the selection: got %v, want %v", actual, expected))
	}
	managed, err := piManagedNames(path, object)
	if err != nil {
		return err
	}
	if !equalStringSlices(sortedUniqueStrings(managed), expected) {
		return NewExitError(ExitGeneral, "error: Pi managed MCP names do not match the selection")
	}
	for _, name := range expected {
		server, err := mcpJSONObject(path, servers[name], "server "+name)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(server)
		if err != nil {
			return err
		}
		var native mcpServer
		if err := json.Unmarshal(raw, &native); err != nil {
			return piMalformed(path, "server "+name+": "+err.Error())
		}
		entry := catalogEntryFromMCPServer(name, native, false)
		if transport, ok := server["httpTransport"]; ok && string(transport) == `"sse"` {
			entry.Transport = "sse"
		}
		if !equalOverwriteMCPDefinition(byName[name], entry) {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: Pi MCP definition does not match selected server %q", name))
		}
		wantEnabled, existed := previous[name]
		if !existed && byName[name].DefaultEnabled != nil {
			disabled, _ := json.Marshal(!*byName[name].DefaultEnabled)
			wantEnabled = mcpEnableValue{Present: true, Value: disabled}
		}
		var actualDisabled json.RawMessage
		var nativeServer map[string]json.RawMessage
		if err := json.Unmarshal(servers[name], &nativeServer); err != nil {
			return piMalformed(path, "server "+name+": "+err.Error())
		}
		actualDisabled, exists := nativeServer["disabled"]
		if exists != wantEnabled.Present || exists && !bytesJSONEqual(actualDisabled, wantEnabled.Value) {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: Pi enabled preference changed for selected MCP %q", name))
		}
	}
	return nil
}

func verifyOverwriteOpenCodePlugins(root string, sources []openCodePluginSource) error {
	directory := filepath.Join(root, ".opencode", "plugins")
	expected := make(map[string]string)
	for _, source := range sources {
		pluginDir := filepath.Join(source.dir, "opencode", "plugins")
		entries, err := os.ReadDir(pluginDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return NewExitError(ExitFilesystem, "error: cannot inspect selected OpenCode plugins: "+err.Error())
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !isOpenCodePluginFile(entry.Name()) {
				continue
			}
			name := openCodeManagedPrefix + sanitizeContentName(source.name) + "-" + entry.Name()
			if _, duplicate := expected[name]; duplicate {
				return NewExitError(ExitGeneral, "error: opencode plugin name collision: "+name)
			}
			expected[name] = filepath.Join(pluginDir, entry.Name())
		}
	}
	actual, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		if len(expected) == 0 {
			return nil
		}
		return NewExitError(ExitFilesystem, "error: selected OpenCode plugin copies are missing")
	}
	if err != nil {
		return NewExitError(ExitFilesystem, "error: cannot inspect project OpenCode plugins: "+err.Error())
	}
	if len(actual) != len(expected) {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: project OpenCode plugin namespace has %d entries; want %d", len(actual), len(expected)))
	}
	for _, entry := range actual {
		if entry.IsDir() {
			return NewExitError(ExitGeneral, "error: unexpected entry in project OpenCode plugin namespace: "+entry.Name())
		}
		sourcePath, exists := expected[entry.Name()]
		if !exists {
			return NewExitError(ExitGeneral, "error: unexpected entry in project OpenCode plugin namespace: "+entry.Name())
		}
		same, err := sameFileContent(sourcePath, filepath.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		if !same {
			return NewExitError(ExitFilesystem, "error: project OpenCode plugin copy does not match its selected source: "+entry.Name())
		}
	}
	return nil
}

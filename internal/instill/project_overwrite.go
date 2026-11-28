package instill

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/smm-h/go-toml-edit"
	"gopkg.in/yaml.v3"
)

type overwriteFile struct {
	path          string
	before, after []byte
	mode          os.FileMode
	remove        bool
}

type overwriteDirectory struct {
	path    string
	before  []byte
	existed bool
}

type projectOverwritePlan struct {
	root             string
	before           mcpEnableSnapshot
	directories      []overwriteDirectory
	files            []overwriteFile
	lock             *overwriteFile
	resolvedTargets  []string
	piConfigSnapshot *overwriteFile
}

var overwriteTargetAliases = map[string]string{"agents": "copilot", "agy": "antigravity"}
var overwriteAllTargets = []string{"claude", "codex", "copilot", "cursor", "gemini", "grok-build", "kiro", "opencode", "windsurf"}
var overwriteDetection = []struct{ dir, target string }{
	{".antigravity", "antigravity"}, {".claude", "claude"}, {".codex", "codex"},
	{".cursor", "cursor"}, {".gemini", "gemini"}, {".hermes", "hermes"},
	{".kiro", "kiro"}, {".opencode", "opencode"}, {".windsurf", "windsurf"},
	{".github", "copilot"}, {".vscode", "vscode"}, {".grok", "grok-build"},
}
var overwritePrimitiveDirectories = map[string][]string{
	"agent-skills": {".agents/skills"}, "antigravity": {".agents/rules", ".agents/skills"},
	"claude": {".claude/rules", ".claude/agents", ".claude/commands", ".claude/skills", ".claude/hooks"},
	"codex":  {".codex/agents", ".agents/skills"}, "copilot": {".github/instructions", ".github/prompts", ".github/agents", ".github/hooks", ".github/extensions", ".agents/skills"}, "vscode": {".github/instructions", ".github/prompts", ".github/agents", ".github/hooks", ".github/extensions", ".agents/skills"},
	"cursor": {".cursor/rules", ".cursor/agents", ".cursor/commands", ".cursor/hooks", ".agents/skills"}, "gemini": {".gemini/commands", ".gemini/hooks", ".agents/skills"}, "grok-bot": {"agent-data/workflows"},
	"grok-build": {".grok/rules", ".grok/agents", ".grok/commands", ".grok/skills"}, "grok-cloud": {".grok/skills"}, "hermes": {".agents/skills"}, "openclaw": {".agents/skills"},
	"kiro": {".kiro/agents", ".kiro/steering", ".kiro/prompts", ".kiro/skills", ".kiro/hooks"}, "opencode": {".opencode/agents", ".opencode/commands", ".opencode/plugins", ".agents/skills"}, "windsurf": {".windsurf/rules", ".windsurf/workflows", ".windsurf/hooks", ".agents/skills"},
}
var overwriteLegacySkills = []string{".codex/skills", ".github/skills", ".cursor/skills", ".gemini/skills", ".opencode/skills", ".windsurf/skills"}

func prepareProjectOverwrite(opts ProjectSelectionOptions, manifest APMManifest, mcpCatalog []CatalogEntry, document *manifestDocument) (*projectOverwritePlan, error) {
	root, err := filepath.Abs(opts.Project.Root)
	if err != nil {
		return nil, filesystemError("error: cannot resolve project root", err)
	}
	root = filepath.Clean(root)
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, filesystemError("error: cannot inspect project root: "+root, err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, filesystemError("error: project root is not a real directory: " + root)
	}
	plan := &projectOverwritePlan{root: root}
	declaredTargets := manifest.Targets
	if len(declaredTargets) > 0 {
		declaredTargets = normalizeAPMProjectTargets(root, declaredTargets)
	}
	targets, err := resolveOverwriteTargets(root, declaredTargets)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if target == "antigravity" || target == "codex" {
			if err := validateOverwriteSSETarget(root, target, manifest.Dependencies); err != nil {
				return nil, err
			}
			break
		}
	}
	for _, target := range targets {
		if target == "windsurf" || target == "hermes" {
			return nil, NewExitError(ExitEnvironment, "error: pick --overwrite cannot reset user-global MCP targets; remove windsurf/hermes from project targets")
		}
	}
	plan.resolvedTargets = targets
	if err := validateOverwriteTargetRoots(root, targets); err != nil {
		return nil, err
	}
	codexPath := filepath.Join(root, ".codex", "config.toml")
	codexDoc, _, err := readCodexDocument(codexPath, true)
	if err != nil {
		return nil, err
	}
	codexProject, err := codexDocumentEnableState(codexPath, codexDoc)
	if err != nil {
		return nil, err
	}
	plan.before.CodexRoot = codexProject
	if projectHasPi(root) {
		piPath := piProjectMCPPath(root)
		if err := validateOverwritePiConfigPath(root, piPath); err != nil {
			return nil, err
		}
		piObject, piBefore, piMode, err := piReadObject(piPath, true)
		if err != nil {
			return nil, err
		}
		plan.piConfigSnapshot = &overwriteFile{path: piPath, before: piBefore, mode: piMode}
		if _, err := piManagedNames(piPath, piObject); err != nil {
			return nil, err
		}
		for _, key := range []string{piMCPServersKey, piLegacyServersKey} {
			raw, exists := piObject[key]
			if !exists {
				continue
			}
			servers, err := mcpJSONObject(piPath, raw, key)
			if err != nil {
				return nil, err
			}
			for name, server := range servers {
				if _, err := mcpJSONObject(piPath, server, key+"."+name); err != nil {
					return nil, err
				}
				if _, err := piEnableValue(piPath, server); err != nil {
					return nil, err
				}
			}
		}
		_, piServers, err := piServerMap(piPath, piObject)
		if err != nil {
			return nil, err
		}
		plan.before.PiMerged = make(map[string]mcpEnableValue, len(piServers))
		for name, raw := range piServers {
			value, err := piEnableValue(piPath, raw)
			if err != nil {
				return nil, err
			}
			if value.Present {
				var disabled bool
				if err := json.Unmarshal(value.Value, &disabled); err != nil {
					return nil, piMalformed(piPath, "server "+name+".disabled must be a boolean")
				}
			}
			plan.before.PiMerged[name] = value
		}
	}
	if projectHasPi(root) {
		catalog := catalogEntriesByName(mcpCatalog)
		for _, dependency := range manifest.Dependencies.MCP {
			entry, ok := catalog[dependency.Name]
			if !ok {
				return nil, NewExitError(ExitGeneral, "error: overwrite MCP "+dependency.Name+" is missing from the Library catalog")
			}
			if err := validateOverwritePiMCPEntry(entry); err != nil {
				return nil, NewExitError(ExitGeneral, "error: root MCP "+dependency.Name+": "+err.Error())
			}
		}
	}
	openCodeMerged, err := captureOverwriteOpenCode(root)
	if err != nil {
		return nil, err
	}
	plan.before.OpenCodeMerged = openCodeMerged
	dirs := make(map[string]struct{})
	// Clear every audited namespace, including outputs from formerly selected targets.
	for _, paths := range overwritePrimitiveDirectories {
		for _, rel := range paths {
			dirs[filepath.Clean(rel)] = struct{}{}
		}
	}
	for _, rel := range overwriteLegacySkills {
		dirs[rel] = struct{}{}
	}
	for rel := range dirs {
		path := filepath.Join(root, rel)
		if err := checkOverwritePath(root, path, opts.LibraryPath, opts.Selection, manifest.Dependencies.APM); err != nil {
			return nil, err
		}
		snapshot, err := snapshotOverwriteDirectory(path)
		if err != nil {
			return nil, err
		}
		plan.directories = append(plan.directories, snapshot)
	}
	for _, rel := range []string{".apm/instructions", ".apm/prompts"} {
		path := filepath.Join(root, rel)
		if err := checkOverwritePath(root, path, opts.LibraryPath, opts.Selection, manifest.Dependencies.APM); err != nil {
			return nil, err
		}
		for current := path; current != root && strings.HasPrefix(current, root+string(filepath.Separator)); current = filepath.Dir(current) {
			info, e := os.Lstat(current)
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, filesystemError("error: refusing symlinked overwrite parent: " + current)
			}
			if e != nil && !os.IsNotExist(e) {
				return nil, filesystemError("error: cannot inspect overwrite parent: "+current, e)
			}
		}
	}
	sort.Slice(plan.directories, func(i, j int) bool { return plan.directories[i].path < plan.directories[j].path })
	if err := plan.prepareConfigResets(); err != nil {
		return nil, err
	}
	if err := plan.prepareContextResets(document); err != nil {
		return nil, err
	}
	if err := plan.prepareLock(); err != nil {
		return nil, err
	}
	return plan, nil
}

func validateOverwriteSSETarget(root, target string, selected APMDependencies) error {
	unsupported := func(name, path string) error {
		message := fmt.Sprintf("error: pick --overwrite target %s does not support sse transport for MCP server %q", target, name)
		if path != "" {
			message += " declared in " + path
		}
		return NewExitError(ExitGeneral, message)
	}
	rootNames := make(map[string]struct{}, len(selected.MCP))
	for _, server := range selected.MCP {
		if server.Transport == "sse" {
			return unsupported(server.Name, "")
		}
		rootNames[server.Name] = struct{}{}
	}

	// Selected local skills and plugins can declare MCPs in their source manifests.
	for _, dependency := range selected.APM {
		if dependency.Local == "" {
			continue
		}
		source := filepath.FromSlash(dependency.Local)
		if !filepath.IsAbs(source) {
			source = expandPathLeadingHome(source)
			if !filepath.IsAbs(source) {
				source = filepath.Join(root, source)
			}
		}
		path := ProjectAPMPath(source)
		if _, statErr := os.Stat(path); statErr == nil {
			document, err := loadManifestDocument(path)
			if err != nil {
				return newExitErrorWithCause(ExitCode(err), fmt.Sprintf("error: cannot read selected package manifest %s: %s", path, ErrorMessage(err)), err)
			}
			servers, err := packageMCPDependencies(document)
			if err != nil {
				return newExitErrorWithCause(ExitGeneral, fmt.Sprintf("error: invalid MCP dependencies in selected package manifest %s: %v", path, err), err)
			}
			for _, server := range servers {
				if _, shadowed := rootNames[server.Name]; shadowed {
					continue
				}
				transport := server.Transport
				if transport == "" {
					transport, _ = server.Extra["type"].(string)
				}
				if strings.TrimSpace(transport) == "sse" {
					return unsupported(server.Name, path)
				}
			}
			continue
		} else if !os.IsNotExist(statErr) {
			return filesystemError("error: cannot inspect selected package manifest: "+path, statErr)
		}
		native, path, err := overwritePluginMCPServers(source)
		if err != nil {
			return err
		}
		var firstSSE string
		for name, raw := range native {
			if _, shadowed := rootNames[name]; shadowed || name == "" || firstSSE != "" && name >= firstSSE {
				continue
			}
			var server struct {
				Command string `json:"command"`
				URL     string `json:"url"`
				Type    string `json:"type"`
			}
			if json.Unmarshal(raw, &server) == nil && strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) != "" && strings.TrimSpace(server.Type) == "sse" {
				firstSSE = name
			}
		}
		if firstSSE != "" {
			return unsupported(firstSSE, path)
		}
	}
	return nil
}

func overwritePluginMCPServers(root string) (map[string]json.RawMessage, string, error) {
	// The flat manifest takes precedence over the nested marker in APM 0.39.0.
	hasMarker := false
	for _, marker := range []string{"plugin.json", ".claude-plugin/plugin.json", ".codex-plugin/plugin.json"} {
		path := filepath.Join(root, marker)
		manifest, err := readOverwritePluginMCPObject(root, path)
		if err != nil {
			return nil, "", err
		}
		if manifest == nil {
			continue
		}
		hasMarker = true
		if raw := manifest["mcpServers"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
			if raw[0] == '{' {
				servers, err := mcpJSONObject(path, raw, "mcpServers")
				return servers, path, err
			}
			// Explicit file references are not auto-discovered defaults.
			return nil, "", nil
		}
		break
	}
	if !hasMarker {
		return nil, "", nil
	}
	// APM uses the first valid nonempty default server map; an inline map bypasses it.
	for _, fallback := range []string{"mcp.json", ".mcp.json", filepath.Join(".github", ".mcp.json")} {
		path := filepath.Join(root, fallback)
		servers, err := readOverwritePluginMCPObject(root, path)
		if err != nil {
			if ExitCode(err) == ExitGeneral {
				// APM skips invalid optional MCP JSON and tries the next default.
				continue
			}
			return nil, "", err
		}
		if raw, wrapped := servers["mcpServers"]; wrapped {
			if len(raw) == 0 || raw[0] != '{' {
				continue
			}
			servers, err = mcpJSONObject(path, raw, "mcpServers")
			if err != nil {
				continue
			}
		} else {
			for _, server := range servers {
				if len(server) == 0 || server[0] != '{' {
					servers = nil
					break
				}
			}
		}
		if len(servers) > 0 {
			return servers, path, nil
		}
	}
	return nil, "", nil
}

func readOverwritePluginMCPObject(pkgRoot, path string) (map[string]json.RawMessage, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, filesystemError("error: cannot inspect selected package file: "+path, err)
	}
	target := path
	if info.Mode()&os.ModeSymlink != 0 {
		resolvedRoot, rootErr := filepath.EvalSymlinks(pkgRoot)
		if rootErr != nil {
			return nil, nil
		}
		resolvedTarget, linkErr := filepath.EvalSymlinks(path)
		if linkErr != nil || !isInOrUnderDir(resolvedRoot, resolvedTarget) {
			return nil, nil
		}
		targetInfo, statErr := os.Stat(resolvedTarget)
		if statErr != nil || !targetInfo.Mode().IsRegular() {
			return nil, nil
		}
		target = resolvedTarget
	} else if !info.Mode().IsRegular() {
		return nil, nil
	}
	data, err := os.ReadFile(target) //nolint:gosec // Inspect known MCP files contained in the selected package.
	if err != nil {
		return nil, filesystemError("error: cannot read selected package file: "+path, err)
	}
	return mcpJSONObject(path, data, "root")
}

func captureOverwriteOpenCode(root string) (map[string]mcpEnableValue, error) {
	paths := []string{"opencode.json", "opencode.jsonc", ".opencode/opencode.json", ".opencode/opencode.jsonc"}
	merged := make(map[string]mcpEnableValue)
	for _, rel := range paths {
		path := filepath.Join(root, rel)
		data, _, err := readMCPFile(path, true)
		if err != nil {
			return nil, err
		}
		if data == nil {
			continue
		}
		state, err := projectMCPJSONState(path, data, "mcp", "enabled")
		if err != nil {
			return nil, err
		}
		if err := validateOverwriteMCPEnableState(path, "mcp", state); err != nil {
			return nil, err
		}
		mergeMCPEnableValues(merged, state)
	}
	return merged, nil
}
func (p *projectOverwritePlan) projectRoot() string { return p.root }

func (p *projectOverwritePlan) detachLock() error {
	if p.lock == nil {
		return nil
	}
	return detachOverwriteLock(p.lock)
}

func resolveOverwriteTargets(root string, declared []string) ([]string, error) {
	values := append([]string(nil), declared...)
	if len(values) == 0 {
		for _, item := range overwriteDetection {
			path := filepath.Join(root, item.dir)
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, filesystemError("error: cannot inspect project target directory: "+path, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, filesystemError("error: project target path is not a real directory: " + path)
			}
			values = append(values, item.target)
		}
		if len(values) == 0 && projectHasPi(root) {
			values = []string{"agent-skills"}
		}
	}
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if alias := overwriteTargetAliases[value]; alias != "" {
			value = alias
		}
		if value == "all" {
			for _, t := range overwriteAllTargets {
				set[t] = struct{}{}
			}
			continue
		}
		switch value {
		case "claude", "codex", "copilot", "cursor", "gemini", "grok-build", "kiro", "opencode", "windsurf", "vscode", "antigravity", "agent-skills", "hermes":
		default:
			return nil, NewExitError(ExitEnvironment, "error: unsupported project target for pick --overwrite: "+value)
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for target := range set {
		out = append(out, target)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, NewExitError(ExitEnvironment, "error: pick --overwrite requires project targets; run 'instill targets' first")
	}
	return out, nil
}
func isPiOnlyOverwriteTarget(hasPi bool, targets []string) bool {
	return hasPi && len(targets) == 1 && targets[0] == "agent-skills"
}
func validateOverwritePiConfigPath(root, path string) error {
	directory := filepath.Join(root, ".pi")
	info, err := os.Lstat(directory)
	if err != nil {
		return filesystemError("error: cannot inspect project Pi config directory: "+directory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return filesystemError("error: project Pi config directory is not a real directory: " + directory)
	}
	info, err = os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return filesystemError("error: cannot inspect project Pi config: "+path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return filesystemError("error: project Pi config is not a regular file: " + path)
	}
	return nil
}

func checkOverwritePath(root, path, library string, selection ProjectSelection, dependencies []APMDependency) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rootInfo, rootErr := os.Lstat(root)
	if rootErr != nil {
		return filesystemError("error: cannot inspect project root: "+root, rootErr)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return filesystemError("error: refusing symlinked project root: " + root)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filesystemError("error: overwrite path escapes project: " + path)
	}
	for current := filepath.Dir(abs); current != root && strings.HasPrefix(current, root+string(filepath.Separator)); current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return filesystemError("error: refusing symlinked overwrite parent: " + current)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	resetPath, err := canonicalOverwritePath(abs)
	if err != nil {
		return err
	}
	sources := make([]string, 0, 1+len(dependencies))
	if library != "" {
		sources = append(sources, library)
		for _, selection := range []struct {
			dir   string
			names []string
		}{
			{"skills", selection.Skills}, {"plugins", selection.Plugins}, {"mcp", selection.MCPs},
			{"instructions", selection.Instructions}, {"prompts", selection.Prompts},
		} {
			for _, name := range selection.names {
				sources = append(sources, filepath.Join(library, selection.dir, filepath.FromSlash(name)))
			}
		}
	}
	for _, dependency := range dependencies {
		if dependency.Git != nil || dependency.Local == "" {
			continue
		}
		source := expandPathLeadingHome(dependency.Local)
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		sources = append(sources, source)
	}
	for _, source := range sources {
		canonicalSource, err := canonicalOverwritePath(source)
		if err != nil {
			return err
		}
		if overwritePathsOverlap(resetPath, canonicalSource) {
			return filesystemError("error: overwrite reset overlaps the Library or a selected source: " + path)
		}
	}
	return nil
}

func canonicalOverwritePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	candidate := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", filesystemError("error: cannot resolve overwrite source: "+path, resolveErr)
		}
		info, lstatErr := os.Lstat(candidate)
		if lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", filesystemError("error: cannot resolve symlinked overwrite source: "+path, resolveErr)
		}
		if lstatErr != nil && !os.IsNotExist(lstatErr) {
			return "", filesystemError("error: cannot inspect overwrite source: "+path, lstatErr)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", filesystemError("error: cannot resolve overwrite source: "+path, resolveErr)
		}
		suffix = append(suffix, filepath.Base(candidate))
		candidate = parent
	}
}

func overwritePathsOverlap(first, second string) bool {
	return first == second || strings.HasPrefix(first, second+string(filepath.Separator)) || strings.HasPrefix(second, first+string(filepath.Separator))
}

func (p *projectOverwritePlan) prepareConfigResets() error {
	jsonNamespaces := map[string][]string{".mcp.json": {"mcpServers"}, ".github/mcp.json": {"mcpServers"}, ".vscode/mcp.json": {"servers"}, ".cursor/mcp.json": {"mcpServers"}, ".gemini/settings.json": {"mcpServers", "hooks"}, ".agents/mcp_config.json": {"mcpServers"}, ".kiro/settings/mcp.json": {"mcpServers"}, "opencode.json": {"mcp", "plugins"}, "opencode.jsonc": {"mcp", "plugins"}, ".opencode/opencode.json": {"mcp", "plugins"}, ".opencode/opencode.jsonc": {"mcp", "plugins"}, ".claude/settings.json": {"hooks"}, ".cursor/hooks.json": {"hooks"}, ".codex/hooks.json": {"hooks"}, ".windsurf/hooks.json": {"hooks"}, ".agents/hooks.json": {"apm"}}
	if piExclusiveMode() {
		if info, err := os.Lstat(filepath.Join(p.root, ".pi")); err == nil && info.IsDir() {
			return NewExitError(ExitEnvironment, "error: pick --overwrite requires Pi project MCP configuration; exclusive mode ignores it")
		}
	}
	for rel, keys := range jsonNamespaces {
		path := filepath.Join(p.root, rel)
		data, mode, err := readMCPFile(path, true)
		if err != nil {
			return err
		}
		if data == nil {
			continue
		}
		value, _, err := parseMCPJSON(path, data)
		if err != nil {
			return err
		}
		next := value.Pack()
		for _, key := range keys {
			v, object, err := parseMCPJSON(path, next)
			if err != nil {
				return err
			}
			raw, exists := object[key]
			if !exists {
				continue
			}
			if key != "plugins" {
				if _, err := mcpJSONObject(path, raw, key); err != nil {
					return err
				}
				raw = []byte("{}")
			} else {
				var entries []json.RawMessage
				if err := json.Unmarshal(raw, &entries); err != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
					return mcpConfigurationError(path, "plugins must be an array")
				}
				raw = []byte("[]")
			}
			_, err = patchMCPMember(&v, "/"+mcpJSONPointer(key), raw)
			if err != nil {
				return mcpConfigurationError(path, err.Error())
			}
			next = v.Pack()
		}
		p.files = append(p.files, overwriteFile{path: path, before: data, after: next, mode: mode})
	}
	for _, rel := range []string{".github/copilot/settings.local.json", ".github/copilot/settings.json"} {
		path := filepath.Join(p.root, rel)
		data, mode, err := readMCPFile(path, true)
		if err != nil {
			return err
		}
		if data == nil {
			continue
		}
		value, object, err := parseMCPJSON(path, data)
		if err != nil {
			return err
		}
		if raw, exists := object["enabledPlugins"]; exists {
			var entries map[string]json.RawMessage
			if err := json.Unmarshal(raw, &entries); err != nil || entries == nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
				return mcpConfigurationError(path, "enabledPlugins must be an object of boolean values")
			}
			for name, rawEnabled := range entries {
				var enabled bool
				trimmed := bytes.TrimSpace(rawEnabled)
				if name == "" || (string(trimmed) != "true" && string(trimmed) != "false") || json.Unmarshal(rawEnabled, &enabled) != nil {
					return mcpConfigurationError(path, "enabledPlugins must be an object of boolean values")
				}
			}
			_, err = patchMCPMember(&value, "/enabledPlugins", []byte("{}"))
			if err != nil {
				return mcpConfigurationError(path, err.Error())
			}
		}
		if raw, exists := object["extraKnownMarketplaces"]; exists {
			marketplaces, err := mcpJSONObject(path, raw, "extraKnownMarketplaces")
			if err != nil {
				return err
			}
			if _, exists := marketplaces["apm"]; exists {
				_, err = patchMCPMember(&value, "/extraKnownMarketplaces/apm", nil)
				if err != nil {
					return mcpConfigurationError(path, err.Error())
				}
			}
		}
		p.files = append(p.files, overwriteFile{path: path, before: data, after: value.Pack(), mode: mode})
	}
	path := filepath.Join(p.root, ".codex", "config.toml")
	doc, before, mode, err := readCodexWritableDocument(path)
	if err != nil {
		return err
	}
	if doc != nil {
		entry, exists := doc.Root().Get("mcp_servers")
		if exists {
			record, ok := entry.Record()
			if !ok {
				return malformedCodex(path, "mcp_servers", fmt.Errorf("must be a table"))
			}
			var names []string
			for child := range record.Entries() {
				names = append(names, child.Key())
			}
			for _, name := range names {
				removePath := tomledit.JoinPath([]tomledit.PathSegment{{Kind: tomledit.SegmentKey, Key: "mcp_servers"}, {Kind: tomledit.SegmentKey, Key: name}})
				if err := doc.Delete(removePath); err != nil {
					return malformedCodex(path, "mcp_servers."+name, err)
				}
			}
		}
		p.files = append(p.files, overwriteFile{path: path, before: before, after: doc.Bytes(), mode: mode})
	}
	for _, rel := range []string{".agents/apm-hooks.json", ".claude/apm-hooks.json", ".cursor/apm-hooks.json", ".codex/apm-hooks.json", ".gemini/apm-hooks.json", ".windsurf/apm-hooks.json"} {
		path := filepath.Join(p.root, rel)
		data, mode, err := readMCPFile(path, true)
		if err != nil {
			return err
		}
		if data != nil {
			p.files = append(p.files, overwriteFile{path: path, before: data, mode: mode, remove: true})
		}
	}
	return nil
}

func (p *projectOverwritePlan) prepareContextResets(document *manifestDocument) error {
	agentsPath := filepath.Join(p.root, "AGENTS.md")
	startMarker, endMarker, managed, err := overwriteManagedMarkers(document)
	if err != nil {
		return err
	}
	if managed {
		data, mode, err := readMCPFile(agentsPath, true)
		if err != nil {
			return err
		}
		if data != nil {
			next, err := clearManagedContextSection(data, startMarker, endMarker)
			if err != nil {
				return NewExitError(ExitFilesystem, "error: malformed managed AGENTS.md section: "+err.Error())
			}
			p.files = append(p.files, overwriteFile{path: agentsPath, before: data, after: next, mode: mode})
		}
	}
	markers := map[string][]string{
		"CLAUDE.md": {"<!-- Generated by APM CLI -->"}, "GEMINI.md": {"<!-- Generated by APM CLI -->"},
		".github/copilot-instructions.md": {"<!-- Generated by APM CLI from .apm/ primitives -->"},
	}
	if !managed {
		markers["AGENTS.md"] = []string{"<!-- Generated by APM CLI from .apm/ primitives -->", "<!-- Generated by APM CLI from distributed .apm/ primitives -->"}
	}
	for rel, headers := range markers {
		path := filepath.Join(p.root, rel)
		data, mode, err := readMCPFile(path, true)
		if err != nil {
			return err
		}
		if data == nil {
			continue
		}
		lines := bytes.Split(data, []byte("\n"))
		generated := false
		for i := 0; i < len(lines) && i < 5; i++ {
			for _, header := range headers {
				if bytes.Equal(bytes.TrimSpace(lines[i]), []byte(header)) {
					generated = true
				}
			}
		}
		if generated {
			p.files = append(p.files, overwriteFile{path: path, before: data, mode: mode, remove: true})
		}
	}
	return nil
}

func overwriteManagedMarkers(document *manifestDocument) (string, string, bool, error) {
	root, err := apmManifestMapping(document.document)
	if err != nil {
		return "", "", false, err
	}
	compilation := mappingValue(root, "compilation")
	if compilation == nil {
		return "", "", false, nil
	}
	agents := mappingValue(compilation, "agents_md")
	if agents == nil {
		return "", "", false, nil
	}
	mode := mappingValue(agents, "mode")
	if mode == nil || mode.Value != "managed_section" {
		return "", "", false, nil
	}
	start, end := mappingValue(agents, "start_marker"), mappingValue(agents, "end_marker")
	if start == nil || end == nil || start.Kind != yaml.ScalarNode || end.Kind != yaml.ScalarNode || start.Value == "" || end.Value == "" {
		return "", "", false, NewExitError(ExitFilesystem, "error: managed AGENTS.md requires start_marker and end_marker")
	}
	return start.Value, end.Value, true, nil
}

func clearManagedContextSection(data []byte, start, end string) ([]byte, error) {
	startBytes, endBytes := []byte(start), []byte(end)
	if bytes.Count(data, startBytes) != 1 || bytes.Count(data, endBytes) != 1 {
		return nil, fmt.Errorf("expected exactly one configured marker pair")
	}
	startAt, endAt := bytes.Index(data, startBytes), bytes.Index(data, endBytes)
	if startAt >= endAt {
		return nil, fmt.Errorf("markers are missing or out of order")
	}
	startLineEnd := bytes.IndexByte(data[startAt+len(startBytes):], '\n')
	if startLineEnd < 0 {
		return nil, fmt.Errorf("start marker is not a complete line")
	}
	startLineEnd += startAt + len(startBytes) + 1
	endLineStart := bytes.LastIndex(data[:endAt], []byte("\n")) + 1
	if endLineStart < startLineEnd || !bytes.HasPrefix(data[endLineStart:], endBytes) {
		return nil, fmt.Errorf("markers are not on distinct lines")
	}
	next := make([]byte, 0, len(data)-(endLineStart-startLineEnd))
	next = append(next, data[:startLineEnd]...)
	next = append(next, data[endLineStart:]...)
	return next, nil
}

func (p *projectOverwritePlan) prepareLock() error {
	path := filepath.Join(p.root, "apm.lock.yaml")
	data, mode, err := readMCPFile(path, true)
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return NewExitError(ExitFilesystem, "error: malformed apm.lock.yaml: "+err.Error())
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure")
	}
	if err := validateOverwriteLockNode(doc.Content[0]); err != nil {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure: "+err.Error())
	}
	root := doc.Content[0]
	if err := validateOverwriteLockSchema(root); err != nil {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure: "+err.Error())
	}
	if deps := mappingValue(root, "dependencies"); deps != nil {
		if deps.Kind != yaml.SequenceNode {
			return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml dependencies structure")
		}
		for _, dep := range deps.Content {
			if dep.Kind != yaml.MappingNode {
				return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml dependency structure")
			}
			if scope := mappingValue(dep, "scope"); scope != nil {
				if scope.Kind != yaml.ScalarNode || scope.Tag != "!!str" || strings.ToLower(strings.TrimSpace(scope.Value)) != "project" {
					return NewExitError(ExitFilesystem, "error: apm.lock.yaml contains an unsupported dependency scope")
				}
			}
			files := mappingValue(dep, "deployed_files")
			if files == nil {
				continue
			}
			if files.Kind != yaml.SequenceNode {
				return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployed_files structure")
			}
			for _, item := range files.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || !projectLockPathInside(p.root, item.Value) {
					return NewExitError(ExitFilesystem, "error: apm.lock.yaml contains a deployment path outside the project")
				}
			}
		}
	}
	if deployments := mappingValue(root, "deployments"); deployments != nil {
		if deployments.Kind != yaml.SequenceNode {
			return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployments structure")
		}
		for _, row := range deployments.Content {
			if row.Kind != yaml.MappingNode {
				return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployment structure")
			}
			target, value, scope := mappingValue(row, "target"), mappingValue(row, "value"), mappingValue(row, "scope")
			if target == nil || target.Kind != yaml.ScalarNode || target.Tag != "!!str" || value == nil || value.Kind != yaml.ScalarNode || value.Tag != "!!str" || scope == nil || scope.Kind != yaml.ScalarNode || scope.Tag != "!!str" {
				return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployment target/value/scope")
			}
			if target.Value == "mcp" {
				continue
			}
			if strings.ToLower(strings.TrimSpace(scope.Value)) != "project" || !projectLockPathInside(p.root, value.Value) {
				return NewExitError(ExitFilesystem, "error: apm.lock.yaml contains a non-project deployment")
			}
			if legacyPath := mappingValue(row, "path"); legacyPath != nil && (legacyPath.Kind != yaml.ScalarNode || legacyPath.Tag != "!!str" || !projectLockPathInside(p.root, legacyPath.Value)) {
				return NewExitError(ExitFilesystem, "error: apm.lock.yaml contains an unsupported deployment path")
			}
		}
	}
	p.lock = &overwriteFile{path: path, before: data, mode: mode}
	return nil
}

func validateOverwriteLockSchema(root *yaml.Node) error {
	version := mappingValue(root, "lockfile_version")
	if version == nil || version.Kind != yaml.ScalarNode || version.Tag != "!!str" ||
		version.Value != "1" && version.Value != "2" && version.Value != "3" {
		return fmt.Errorf("lockfile_version must be a supported string")
	}
	for _, key := range []string{"dependencies", "deployments", "mcp_servers", "lsp_servers", "local_deployed_files"} {
		node := mappingValue(root, key)
		if node != nil && node.Tag != "!!null" && node.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s must be a list", key)
		}
	}
	for _, key := range []string{"mcp_configs", "mcp_target_servers", "mcp_config_provenance", "mcp_omissions", "lsp_configs", "lsp_target_servers", "lsp_config_provenance", "local_deployed_file_hashes"} {
		node := mappingValue(root, key)
		if node == nil || node.Tag == "!!null" {
			continue
		}
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be a mapping", key)
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			name := node.Content[index]
			if name.Kind != yaml.ScalarNode || name.Tag != "!!str" || name.Value == "" {
				return fmt.Errorf("%s keys must be non-empty strings", key)
			}
		}
	}
	for _, key := range []string{"mcp_target_servers", "lsp_target_servers"} {
		node := mappingValue(root, key)
		if node == nil || node.Tag == "!!null" {
			continue
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			values := node.Content[index+1]
			if values.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s values must be lists", key)
			}
			for _, value := range values.Content {
				if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
					return fmt.Errorf("%s entries must be non-empty strings", key)
				}
			}
		}
	}
	return nil
}

func validateOverwriteLockNode(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("anchors and aliases are not safe to mutate")
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("mapping keys must be strings")
			}
			if _, exists := seen[key.Value]; exists {
				return fmt.Errorf("duplicate mapping key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := validateOverwriteLockNode(key); err != nil {
				return err
			}
			if err := validateOverwriteLockNode(node.Content[index+1]); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := validateOverwriteLockNode(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func projectRelativeLockPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
func projectLockPathInside(root, path string) bool {
	if !projectRelativeLockPath(path) {
		return false
	}
	canonicalRoot, err := canonicalOverwritePath(root)
	if err != nil {
		return false
	}
	candidate, err := canonicalOverwritePath(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	return candidate == canonicalRoot || strings.HasPrefix(candidate, canonicalRoot+string(filepath.Separator))
}

func (p *projectOverwritePlan) apply(ctx context.Context, held *heldLocks) error {
	if held == nil {
		return filesystemError("error: project lock is required for overwrite reset")
	}
	if err := held.requireContext(ctx, p.root); err != nil {
		return err
	}
	if err := p.validateLockSnapshot(); err != nil {
		return err
	}
	if err := p.validatePiConfigSnapshot(); err != nil {
		return err
	}

	// Detect concurrent edits before the first destructive namespace mutation.
	for _, file := range p.files {
		current, _, err := readMCPFile(file.path, true)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, file.before) {
			return filesystemError("error: project configuration changed during overwrite: " + file.path)
		}
	}
	for _, directory := range p.directories {
		current, err := snapshotOverwriteDirectory(directory.path)
		if err != nil {
			return err
		}
		if current.existed != directory.existed || !bytes.Equal(current.before, directory.before) {
			return filesystemError("error: overwrite namespace changed during preparation: " + directory.path)
		}
	}
	for _, directory := range p.directories {
		if directory.existed {
			if err := clearOverwriteDirectory(directory.path); err != nil {
				return err
			}
		}
	}
	for _, file := range p.files {
		current, _, err := readMCPFile(file.path, true)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, file.before) {
			return filesystemError("error: project configuration changed during overwrite: " + file.path)
		}
		if file.remove {
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				return filesystemError("error: cannot remove project configuration: "+file.path, err)
			}
			continue
		}
		if err := writeMCPFile(file.path, file.before, file.after, file.mode); err != nil {
			return err
		}
	}
	return nil
}
func (p *projectOverwritePlan) validatePiConfigSnapshot() error {
	if p.piConfigSnapshot == nil {
		return nil
	}
	if err := validateOverwritePiConfigPath(p.root, p.piConfigSnapshot.path); err != nil {
		return err
	}
	current, _, err := readMCPFile(p.piConfigSnapshot.path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, p.piConfigSnapshot.before) {
		return filesystemError("error: Pi MCP configuration changed during overwrite: " + p.piConfigSnapshot.path)
	}
	return nil
}
func (p *projectOverwritePlan) validateLockSnapshot() error {
	if p.lock == nil {
		return nil
	}
	current, _, err := readMCPFile(p.lock.path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, p.lock.before) {
		return filesystemError("error: apm.lock.yaml changed during overwrite")
	}
	return nil
}

func snapshotOverwriteDirectory(path string) (overwriteDirectory, error) {
	snapshot := overwriteDirectory{path: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, filesystemError("error: cannot inspect overwrite namespace: "+path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return snapshot, filesystemError("error: refusing symlinked overwrite namespace: " + path)
	}
	if !info.IsDir() {
		return snapshot, filesystemError("error: overwrite namespace is not a directory: " + path)
	}
	hash := sha256.New()
	var visit func(string, string) error
	visit = func(current, relative string) error {
		entry, err := os.Lstat(current)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%o\x00%d\x00%d\x00", relative, entry.Mode(), entry.Size(), entry.ModTime().UnixNano())
		if entry.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			_, _ = hash.Write([]byte(target))
			return nil
		}
		if entry.IsDir() {
			children, err := os.ReadDir(current)
			if err != nil {
				return err
			}
			for _, child := range children {
				childRelative := filepath.Join(relative, child.Name())
				if err := visit(filepath.Join(current, child.Name()), childRelative); err != nil {
					return err
				}
			}
			return nil
		}
		if entry.Mode().IsRegular() {
			content, err := os.ReadFile(current) //nolint:gosec // current is a non-symlink regular file inside the locked project namespace.
			if err != nil {
				return err
			}
			_, _ = hash.Write(content)
		}
		return nil
	}
	if err := visit(path, "."); err != nil {
		return snapshot, filesystemError("error: cannot snapshot overwrite namespace: "+path, err)
	}
	snapshot.existed = true
	snapshot.before = hash.Sum(nil)
	return snapshot, nil
}
func clearOverwriteDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return filesystemError("error: cannot inspect overwrite namespace: "+path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return filesystemError("error: refusing unsafe overwrite namespace: " + path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return filesystemError("error: cannot read overwrite namespace: "+path, err)
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return filesystemError("error: cannot inspect overwrite entry: "+child, err)
		}
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := clearOverwriteDirectory(child); err != nil {
				return err
			}
			if err := os.Remove(child); err != nil {
				return filesystemError("error: cannot remove overwrite entry: "+child, err)
			}
		} else if err := os.Remove(child); err != nil {
			return filesystemError("error: cannot remove overwrite entry: "+child, err)
		}
	}
	return nil
}

func detachOverwriteLock(file *overwriteFile) error {
	current, _, err := readMCPFile(file.path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, file.before) {
		return filesystemError("error: apm.lock.yaml changed during overwrite")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(current, &doc); err != nil {
		return err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure")
	}
	if err := validateOverwriteLockNode(doc.Content[0]); err != nil {
		return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml structure: "+err.Error())
	}
	root := doc.Content[0]
	set := func(key string, value *yaml.Node) {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == key {
				root.Content[i+1] = value
				return
			}
		}
		root.Content = append(root.Content, scalarNode(key), value)
	}
	set("mcp_target_servers", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	set("mcp_servers", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
	set("mcp_configs", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	set("mcp_config_provenance", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	set("mcp_omissions", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	if deployments := mappingValue(root, "deployments"); deployments != nil {
		if deployments.Kind != yaml.SequenceNode {
			return NewExitError(ExitFilesystem, "error: unsupported apm.lock.yaml deployments structure")
		}
		kept := deployments.Content[:0]
		for _, row := range deployments.Content {
			target := mappingValue(row, "target")
			if target != nil && target.Value == "mcp" {
				continue
			}
			kept = append(kept, row)
		}
		deployments.Content = kept
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return err
	}
	if bytes.Equal(current, out.Bytes()) {
		return nil
	}
	return writeOverwriteLockAtomic(file.path, file.before, out.Bytes(), file.mode)
}
func writeOverwriteLockAtomic(path string, before, next []byte, mode os.FileMode) error {
	emitMutationTestEvent("overwrite-lock-before-write:" + path)
	current, _, err := readMCPFile(path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return filesystemError("error: apm.lock.yaml changed during overwrite")
	}
	return writeFileAtomic(path, next, mode)
}

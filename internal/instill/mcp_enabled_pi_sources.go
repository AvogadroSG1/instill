package instill

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/tailscale/hujson"
)

// This is a presence/disabled projection of pi-mcp-adapter 4.0.0's config,
// package and plugin loaders. Connection data never escapes into the snapshot.
type piSourceLayer struct {
	path   string
	object map[string]json.RawMessage
	values map[string]mcpEnableValue
}

func piExclusiveMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("PI_MCP_CONFIG_MODE")), "exclusive")
}

func piSourceIdentity(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved
	}
	return filepath.Clean(absolute)
}

func piSourceAgentDir(home string) (string, error) {
	configured := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
	if configured == "" {
		return filepath.Join(home, ".pi", "agent"), nil
	}
	if configured == "~" {
		return home, nil
	}
	if strings.HasPrefix(configured, "~/") {
		configured = filepath.Join(home, configured[2:])
	}
	absolute, err := filepath.Abs(configured)
	if err != nil {
		return "", NewExitError(ExitFilesystem, "error: cannot resolve Pi agent directory: "+configured+": "+err.Error())
	}
	return filepath.Clean(absolute), nil
}

func piLoadSourceLayer(root, path string) (piSourceLayer, error) {
	writable := filepath.Clean(path) == filepath.Clean(piProjectMCPPath(root)) && !piExclusiveMode()
	object, _, _, err := piReadObject(path, writable)
	if err != nil {
		return piSourceLayer{}, err
	}
	if writable {
		if _, err := piManagedNames(path, object); err != nil {
			return piSourceLayer{}, err
		}
	}
	_, servers, err := piServerMap(path, object)
	if err != nil {
		return piSourceLayer{}, err
	}
	values := make(map[string]mcpEnableValue, len(servers))
	for name, raw := range servers {
		value, err := piEnableValue(path, raw)
		if err != nil {
			return piSourceLayer{}, err
		}
		values[name] = value
	}
	return piSourceLayer{path: path, object: object, values: values}, nil
}

func piSourceSettings(layer piSourceLayer) (map[string]json.RawMessage, error) {
	return mcpJSONObject(layer.path, layer.object["settings"], "settings")
}

func piLoadSourceLayers(root, home, agentDir string) ([]piSourceLayer, error) {
	agentPath := filepath.Join(agentDir, piMCPConfigName)
	if piExclusiveMode() {
		layer, err := piLoadSourceLayer(root, agentPath)
		return []piSourceLayer{layer}, err
	}
	globalPaths := []string{filepath.Join(home, ".config", "mcp", "mcp.json"), filepath.Join(home, ".agents", "mcp.json"), filepath.Join(home, ".agents", "mcp", "mcp.json"), agentPath}
	layers := make([]piSourceLayer, 0, 12)
	reserved := map[string]bool{}
	var configured []string
	for _, path := range globalPaths {
		identity := piSourceIdentity(path)
		if reserved[identity] {
			continue
		}
		reserved[identity] = true
		layer, err := piLoadSourceLayer(root, path)
		if err != nil {
			return nil, err
		}
		settings, err := piSourceSettings(layer)
		if err != nil {
			return nil, err
		}
		if raw, exists := settings["ancestorConfigRoots"]; exists {
			if err := json.Unmarshal(raw, &configured); err != nil || configured == nil {
				return nil, piMalformed(path, "settings.ancestorConfigRoots must be an array of strings")
			}
		}
		layers = append(layers, layer)
	}
	projectPaths := []string{filepath.Join(root, ".mcp.json"), piProjectMCPPath(root)}
	for _, path := range projectPaths {
		reserved[piSourceIdentity(path)] = true
	}
	canonicalRoot := piSourceIdentity(root)
	canonicalHome := piSourceIdentity(home)
	boundary := ""
	for _, candidate := range configured {
		if strings.HasPrefix(candidate, "~/") {
			candidate = filepath.Join(home, candidate[2:])
		}
		if !filepath.IsAbs(candidate) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		if !isInOrUnderDir(canonicalHome, resolved) || !isInOrUnderDir(resolved, canonicalRoot) {
			continue
		}
		if len(resolved) > len(boundary) {
			boundary = resolved
		}
	}
	var ancestorPaths []string
	if boundary != "" {
		var dirs []string
		for dir := filepath.Dir(canonicalRoot); isInOrUnderDir(boundary, dir); dir = filepath.Dir(dir) {
			dirs = append(dirs, dir)
			if dir == boundary || filepath.Dir(dir) == dir {
				break
			}
		}
		for _, dir := range slices.Backward(dirs) {
			for _, path := range []string{filepath.Join(dir, ".mcp.json"), piProjectMCPPath(dir)} {
				identity := piSourceIdentity(path)
				if reserved[identity] {
					continue
				}
				info, err := os.Stat(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return nil, NewExitError(ExitFilesystem, "error: cannot inspect MCP configuration: "+path+": "+err.Error())
				}
				if info.IsDir() {
					return nil, piMalformed(path, "configuration must be a file")
				}
				for i, old := range ancestorPaths {
					if piSourceIdentity(old) == identity {
						ancestorPaths = append(ancestorPaths[:i], ancestorPaths[i+1:]...)
						break
					}
				}
				ancestorPaths = append(ancestorPaths, path)
			}
		}
	}
	for _, path := range append(ancestorPaths, projectPaths...) {
		// Project paths may be explicit aliases of a global source.
		if slices.Contains(globalPaths, path) {
			continue
		}
		layer, err := piLoadSourceLayer(root, path)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func piReadSourceJSON(path string) (map[string]json.RawMessage, error) {
	object, _, _, err := piReadObject(path, false)
	return object, err
}

var piImportKinds = []string{"cursor", "claude-code", "claude-desktop", "codex", "opencode", "windsurf", "vscode"}

func piImportCandidates(kind, root, home string) []string {
	switch kind {
	case "cursor":
		return []string{filepath.Join(home, ".cursor", "mcp.json")}
	case "claude-code":
		return []string{filepath.Join(home, ".claude", "mcp.json"), filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", "claude_desktop_config.json")}
	case "claude-desktop":
		return []string{filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")}
	case "codex":
		return []string{filepath.Join(home, ".codex", "config.toml"), filepath.Join(home, ".codex", "config.json")}
	case "windsurf":
		return []string{filepath.Join(home, ".windsurf", "mcp.json")}
	case "vscode":
		return []string{filepath.Join(root, ".vscode", "mcp.json")}
	case "opencode":
		project := filepath.Join(root, "opencode.json")
		ancestors := mcpAncestors(root, true)
		hasRepo := false
		if len(ancestors) > 0 {
			_, err := os.Stat(filepath.Join(ancestors[len(ancestors)-1], ".git"))
			hasRepo = err == nil
		}
		if hasRepo {
			for _, dir := range ancestors {
				candidate := filepath.Join(dir, "opencode.json")
				if _, err := os.Stat(candidate); err == nil || dir == ancestors[len(ancestors)-1] {
					project = candidate
					break
				}
			}
		}
		return []string{filepath.Join(home, ".config", "opencode", "opencode.json"), project}
	}
	return nil
}

func piImportedJSONMap(path, kind string, object map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	key := "mcpServers"
	switch kind {
	case "opencode":
		key = "mcp"
	case "codex":
		key = "mcp_servers"
	}
	raw, exists := object[key]
	if !exists && (kind == "cursor" || kind == "windsurf" || kind == "vscode") {
		raw = object["mcp-servers"]
	}
	if !exists && kind == "codex" {
		raw = object["mcpServers"]
	}
	servers, err := mcpJSONObject(path, raw, key)
	if err != nil {
		return nil, err
	}
	if kind == "opencode" && servers["servers"] != nil {
		nested, err := mcpJSONObject(path, servers["servers"], "mcp.servers")
		if err != nil {
			return nil, err
		}
		for name, value := range servers {
			if name != "servers" && name != "timeout" {
				if _, exists := nested[name]; !exists {
					nested[name] = value
				}
			}
		}
		servers = nested
	}
	return servers, nil
}

func piImportedTOMLState(path string, data []byte) (map[string]mcpEnableValue, error) {
	document, err := tomledit.Parse(data)
	if err != nil {
		return nil, malformedCodex(path, "document", err)
	}
	values := map[string]mcpEnableValue{}
	entry, exists := document.Root().Get("mcp_servers")
	if !exists {
		entry, exists = document.Root().Get("mcpServers")
	}
	if !exists {
		return values, nil
	}
	servers, ok := entry.Record()
	if !ok {
		return nil, piMalformed(path, "mcp_servers must be a table")
	}
	for entry := range servers.Entries() {
		server, ok := entry.Record()
		if !ok {
			return nil, piMalformed(path, "mcp_servers."+entry.Key()+" must be a table")
		}
		value := mcpEnableValue{}
		if disabled, exists := server.Get("disabled"); exists {
			node, ok := disabled.Node()
			if !ok {
				return nil, piMalformed(path, "disabled must be a value")
			}
			decoded, err := tomledit.DecodeNode[any](node)
			if err != nil {
				return nil, malformedCodex(path, "disabled", err)
			}
			raw, err := json.Marshal(*decoded)
			if err != nil {
				return nil, piMalformed(path, "cannot project disabled: "+err.Error())
			}
			value = mcpEnableValue{Present: true, Value: raw}
		}
		values[entry.Key()] = value
	}
	return values, nil
}

func piLoadImportedState(kind, root, home string) (map[string]mcpEnableValue, error) {
	if !slices.Contains(piImportKinds, kind) {
		return nil, piMalformed("imports", "unknown import kind: "+kind)
	}
	values := map[string]mcpEnableValue{}
	// OpenCode merges both candidates before filtering disabled/invalid entries.
	openCode := map[string]map[string]json.RawMessage{}
	for _, path := range piImportCandidates(kind, root, home) {
		data, _, err := readMCPFile(path, false)
		if err != nil {
			return nil, err
		}
		if data == nil {
			continue
		}
		if kind == "codex" && strings.HasSuffix(path, ".toml") {
			return piImportedTOMLState(path, data)
		}
		_, object, err := parseMCPJSON(path, data)
		if err != nil {
			return nil, err
		}
		servers, err := piImportedJSONMap(path, kind, object)
		if err != nil {
			return nil, err
		}
		for name, raw := range servers {
			server, err := mcpJSONObject(path, raw, "server "+name)
			if err != nil {
				return nil, err
			}
			if kind != "opencode" {
				value, err := piEnableValue(path, raw)
				if err != nil {
					return nil, err
				}
				values[name] = value
				continue
			}
			projected := openCode[name]
			if projected == nil {
				projected = map[string]json.RawMessage{}
			}
			if typ := server["type"]; typ != nil && string(typ) != string(projected["type"]) {
				delete(projected, "command")
				delete(projected, "url")
			}
			for _, key := range []string{"type", "command", "url", "enabled", "disabled"} {
				if value, exists := server[key]; exists {
					projected[key] = value
				}
			}
			openCode[name] = projected
		}
		if kind != "opencode" {
			return values, nil
		}
	}
	for name, server := range openCode {
		if bytesJSONEqual(server["enabled"], json.RawMessage("false")) || bytesJSONEqual(server["disabled"], json.RawMessage("true")) {
			continue
		}
		typ, _ := piSourceString(server["type"])
		valid := false
		switch typ {
		case "local":
			var command []string
			valid = json.Unmarshal(server["command"], &command) == nil && len(command) > 0
		case "remote":
			_, valid = piSourceString(server["url"])
		}
		if valid {
			values[name] = mcpEnableValue{}
		}
	}
	return values, nil
}

func bytesJSONEqual(left, right json.RawMessage) bool {
	return strings.TrimSpace(string(left)) == string(right)
}
func piSourceString(raw json.RawMessage) (string, bool) {
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err == nil
}

func piExpandedSourceValues(layer piSourceLayer, root, home string) (map[string]mcpEnableValue, error) {
	imported := map[string]mcpEnableValue{}
	if raw, exists := layer.object["imports"]; exists {
		var kinds []string
		if err := json.Unmarshal(raw, &kinds); err != nil || kinds == nil {
			return nil, piMalformed(layer.path, "imports must be an array of strings")
		}
		for _, kind := range kinds {
			values, err := piLoadImportedState(kind, root, home)
			if err != nil {
				return nil, err
			}
			for name, value := range values {
				if _, exists := imported[name]; !exists {
					imported[name] = value
				}
			}
		}
	}
	mergeMCPEnableValues(imported, layer.values)
	return imported, nil
}

func piNamespacePart(name, fallback string) string {
	var result strings.Builder
	previousInvalid := false
	for _, char := range name {
		valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-'
		if valid {
			result.WriteRune(char)
		} else if !previousInvalid {
			result.WriteByte('_')
		}
		previousInvalid = !valid
	}
	value := strings.Trim(result.String(), "_-")
	if value == "" {
		return fallback
	}
	return value
}

func piSourceObjectNames(raw json.RawMessage) []string {
	value, err := hujson.Parse(raw)
	if err != nil {
		return nil
	}
	object, ok := value.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(object.Members))
	for _, member := range object.Members {
		var name string
		if json.Unmarshal(member.Name.Pack(), &name) == nil {
			names = append(names, name)
		}
	}
	return names
}

func piContainedSourcePath(root, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if !isInOrUnderDir(filepath.Clean(root), filepath.Clean(path)) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	canonical := piSourceIdentity(root)
	return resolved, isInOrUnderDir(canonical, resolved)
}

func piResolvePackageSource(source, base string) string {
	if strings.HasPrefix(source, "npm:") {
		name := strings.TrimSpace(source[4:])
		start := 0
		if strings.HasPrefix(name, "@") {
			start = 1
		}
		if index := strings.Index(name[start:], "@"); index >= 0 {
			name = name[:start+index]
		}
		path := filepath.Join(base, "npm", "node_modules", filepath.FromSlash(name))
		if name == "" || !isInOrUnderDir(filepath.Join(base, "npm", "node_modules"), path) {
			return ""
		}
		return path
	}
	gitSource := source
	if strings.HasPrefix(source, "git:") {
		gitSource = strings.TrimSpace(source[4:])
	} else if !strings.Contains(source, "://") && !strings.HasPrefix(source, "git@") {
		gitSource = ""
	}
	if gitSource != "" {
		value := gitSource
		if strings.Contains(gitSource, "://") {
			parsed, err := url.Parse(gitSource)
			if err != nil || parsed.Hostname() == "" {
				return ""
			}
			value = parsed.Hostname() + parsed.Path
		} else if strings.HasPrefix(gitSource, "git@") {
			value = strings.Replace(gitSource[4:], ":", "/", 1)
		}
		slash := strings.Index(value, "/")
		if slash < 0 {
			return ""
		}
		if index := strings.Index(value[slash:], "@"); index >= 0 && slash+index < len(value)-1 {
			value = value[:slash+index]
		}
		value = strings.TrimSuffix(value, ".git")
		path := filepath.Join(base, "git", filepath.FromSlash(value))
		if strings.HasPrefix(value, "/") || !isInOrUnderDir(filepath.Join(base, "git"), path) {
			return ""
		}
		return path
	}
	if filepath.IsAbs(source) {
		return filepath.Clean(source)
	}
	return filepath.Join(base, source)
}

func piPackageSourceValues(root, agentDir string) (map[string]mcpEnableValue, error) {
	values := map[string]mcpEnableValue{}
	seenRoots := map[string]bool{}
	for _, settingsPath := range []string{filepath.Join(root, ".pi", "settings.json"), filepath.Join(agentDir, "settings.json")} {
		object, err := piReadSourceJSON(settingsPath)
		if err != nil {
			return nil, err
		}
		raw, exists := object["packages"]
		if !exists {
			continue
		}
		var packages []json.RawMessage
		if err := json.Unmarshal(raw, &packages); err != nil || packages == nil {
			return nil, piMalformed(settingsPath, "packages must be an array")
		}
		for _, raw := range packages {
			source, ok := piSourceString(raw)
			if !ok {
				entry, err := mcpJSONObject(settingsPath, raw, "package")
				if err != nil {
					return nil, err
				}
				source, ok = piSourceString(entry["source"])
			}
			if !ok || source == "" {
				return nil, piMalformed(settingsPath, "package entries must have a non-empty string source")
			}
			packageRoot := piResolvePackageSource(source, filepath.Dir(settingsPath))
			if packageRoot == "" || seenRoots[packageRoot] {
				continue
			}
			seenRoots[packageRoot] = true
			manifestPath := filepath.Join(packageRoot, "package.json")
			manifest, err := piReadSourceJSON(manifestPath)
			if err != nil {
				return nil, err
			}
			name, ok := piSourceString(manifest["name"])
			if !ok || name == "" {
				continue
			}
			pi, err := mcpJSONObject(manifestPath, manifest["pi"], "pi")
			if err != nil {
				return nil, err
			}
			pathsRaw, exists := pi["mcp"]
			if !exists {
				continue
			}
			var paths []string
			if path, ok := piSourceString(pathsRaw); ok {
				paths = []string{path}
			} else if json.Unmarshal(pathsRaw, &paths) != nil || paths == nil {
				continue
			}
			for _, path := range paths {
				configPath, contained := piContainedSourcePath(packageRoot, path)
				if !contained {
					continue
				}
				object, err := piReadSourceJSON(configPath)
				if err != nil {
					return nil, err
				}
				servers, err := mcpJSONObject(configPath, object["mcpServers"], "mcpServers")
				if err != nil {
					return nil, err
				}
				if object["mcpServers"] == nil {
					return nil, piMalformed(configPath, "mcpServers is required")
				}
				for _, serverName := range piSourceObjectNames(object["mcpServers"]) {
					nativeName := piNamespacePart(name, "package") + "__" + piNamespacePart(serverName, "server")
					if _, exists := values[nativeName]; exists {
						continue
					}
					value, err := piEnableValue(configPath, servers[serverName])
					if err != nil {
						return nil, err
					}
					values[nativeName] = value
				}
			}
		}
	}
	return values, nil
}

var piAgentPluginName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
var piClaudePluginName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func piPluginSourceRoot(source, root, home string) string {
	if source == "~" {
		return home
	}
	if strings.HasPrefix(source, "~/") {
		source = filepath.Join(home, source[2:])
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(root, source)
	}
	return piSourceIdentity(source)
}

func piOptionalStringFields(object map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if raw, exists := object[key]; exists {
			if _, ok := piSourceString(raw); !ok {
				return false
			}
		}
	}
	return true
}

func piPluginStringMap(path string, raw json.RawMessage, reserved bool, headers bool) bool {
	if raw == nil {
		return true
	}
	object, err := mcpJSONObject(path, raw, "string map")
	if err != nil {
		return false
	}
	seen := map[string]bool{}
	for key, raw := range object {
		value, ok := piSourceString(raw)
		if !ok {
			return false
		}
		if reserved && (key == "PLUGIN_ROOT" || key == "PLUGIN_DATA") {
			return false
		}
		if headers {
			normalized := strings.ToLower(key)
			if seen[normalized] || key == "" || strings.ContainsAny(key, " ()<>@,;:\\\"/[]?={}\t\r\n") || strings.ContainsAny(value, "\r\n\x00") {
				return false
			}
			seen[normalized] = true
		}
	}
	return true
}

func piAgentPluginServerValid(path, pluginRoot, agentDir, pluginName string, server map[string]json.RawMessage) bool {
	typ, _ := piSourceString(server["type"])
	allowed := []string{"type", "url", "headers"}
	if typ == "stdio" {
		allowed = []string{"type", "command", "args", "env", "cwd"}
	} else if typ != "streamable-http" && typ != "sse" {
		return false
	}
	for key := range server {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	if typ != "stdio" {
		value, ok := piSourceString(server["url"])
		if !ok || value == "" || strings.Contains(value, "${") || strings.Contains(value, "$env:") || strings.Contains(value, "{env:") {
			return false
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" || parsed.Fragment != "" {
			return false
		}
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			if parsed.User.Username() != "" || password != "" {
				return false
			}
		}
		if parsed.Scheme != "https" {
			if parsed.Scheme != "http" || !piPluginLoopbackHost(parsed.Hostname()) {
				return false
			}
		}
		return piPluginStringMap(path, server["headers"], false, true)
	}
	command, ok := piSourceString(server["command"])
	if !ok || command == "" {
		return false
	}
	if strings.HasPrefix(command, "./") {
		if _, ok := piContainedSourcePath(pluginRoot, command); !ok {
			return false
		}
	} else if strings.ContainsAny(command, "/\\") || strings.Contains(command, "${PLUGIN_ROOT}") || strings.Contains(command, "${PLUGIN_DATA}") {
		return false
	}
	if raw, exists := server["args"]; exists {
		var args []string
		if json.Unmarshal(raw, &args) != nil || args == nil {
			return false
		}
	}
	if !piPluginStringMap(path, server["env"], true, false) {
		return false
	}
	if raw, exists := server["cwd"]; exists {
		cwd, ok := piSourceString(raw)
		if !ok {
			return false
		}
		dataDir := filepath.Join(agentDir, "agent-plugin-data", pluginName)
		expanded := strings.ReplaceAll(strings.ReplaceAll(cwd, "${PLUGIN_ROOT}", pluginRoot), "${PLUGIN_DATA}", dataDir)
		if strings.HasPrefix(cwd, "./") || cwd == "${PLUGIN_ROOT}" || strings.HasPrefix(cwd, "${PLUGIN_ROOT}/") {
			if _, ok := piContainedSourcePath(pluginRoot, expanded); !ok {
				return false
			}
		} else if cwd == "${PLUGIN_DATA}" || strings.HasPrefix(cwd, "${PLUGIN_DATA}/") {
			if !isInOrUnderDir(dataDir, expanded) {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

func piAgentPluginSourceValues(root, home, agentDir string, pathsRaw json.RawMessage) (map[string]mcpEnableValue, error) {
	values := map[string]mcpEnableValue{}
	if pathsRaw == nil {
		return values, nil
	}
	var paths []string
	if json.Unmarshal(pathsRaw, &paths) != nil || paths == nil {
		return nil, piMalformed("settings.agentPluginPaths", "must be an array of strings")
	}
	for _, source := range paths {
		pluginRoot := piPluginSourceRoot(source, root, home)
		manifestPath, ok := piContainedSourcePath(pluginRoot, "plugin.json")
		if !ok {
			continue
		}
		manifest, err := piReadSourceJSON(manifestPath)
		if err != nil {
			return nil, err
		}
		schema, _ := piSourceString(manifest["$schema"])
		name, ok := piSourceString(manifest["name"])
		if schema != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json" || !ok || len(name) > 64 || !piAgentPluginName.MatchString(name) || strings.Contains(name, "--") || strings.Contains(name, "..") {
			continue
		}
		if !piOptionalStringFields(manifest, "version", "description", "homepage", "repository", "license") {
			continue
		}
		if raw, exists := manifest["keywords"]; exists {
			var keywords []string
			if json.Unmarshal(raw, &keywords) != nil || keywords == nil {
				continue
			}
		}
		if raw, exists := manifest["author"]; exists {
			author, err := mcpJSONObject(manifestPath, raw, "author")
			if err != nil {
				continue
			}
			valid := true
			for key, value := range author {
				if !slices.Contains([]string{"name", "email", "url"}, key) {
					valid = false
				}
				if _, ok := piSourceString(value); !ok {
					valid = false
				}
			}
			if !valid {
				continue
			}
		}
		configPath, ok := piContainedSourcePath(pluginRoot, "mcp.json")
		if !ok {
			continue
		}
		config, err := piReadSourceJSON(configPath)
		if err != nil {
			return nil, err
		}
		schema, _ = piSourceString(config["$schema"])
		if schema != "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json" {
			continue
		}
		valid := true
		for key := range config {
			if key != "$schema" && key != "mcpServers" {
				valid = false
			}
		}
		if !valid {
			continue
		}
		servers, err := mcpJSONObject(configPath, config["mcpServers"], "mcpServers")
		if err != nil {
			return nil, err
		}
		for _, serverName := range piSourceObjectNames(config["mcpServers"]) {
			server, err := mcpJSONObject(configPath, servers[serverName], "server")
			if err != nil {
				return nil, err
			}
			if !piAgentPluginServerValid(configPath, pluginRoot, agentDir, name, server) {
				continue
			}
			nativeName := piNamespacePart(name, "plugin") + "__" + piNamespacePart(serverName, "server")
			if _, exists := values[nativeName]; !exists {
				values[nativeName] = mcpEnableValue{}
			}
		}
	}
	return values, nil
}

func piClaudePluginSourceValues(root, home string, pluginsRaw json.RawMessage) (map[string]mcpEnableValue, error) {
	values := map[string]mcpEnableValue{}
	if pluginsRaw == nil {
		return values, nil
	}
	var plugins []map[string]json.RawMessage
	if json.Unmarshal(pluginsRaw, &plugins) != nil || plugins == nil {
		return nil, piMalformed("claudePlugins", "must be an array of objects")
	}
	roots := map[string]bool{}
	namespaces := map[string]bool{}
	for _, plugin := range plugins {
		source, ok := piSourceString(plugin["path"])
		if !ok || strings.TrimSpace(source) == "" || !bytesJSONEqual(plugin["mcp"], json.RawMessage("true")) {
			continue
		}
		pluginRoot := piPluginSourceRoot(source, root, home)
		if roots[pluginRoot] {
			continue
		}
		roots[pluginRoot] = true
		info, err := os.Stat(pluginRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, NewExitError(ExitFilesystem, "error: cannot inspect Pi Claude plugin: "+pluginRoot+": "+err.Error())
		}
		if !info.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
		data, _, err := readMCPFile(manifestPath, false)
		if err != nil {
			return nil, err
		}
		if data != nil {
			contained, ok := piContainedSourcePath(pluginRoot, manifestPath)
			if !ok {
				continue
			}
			manifest, err := piReadSourceJSON(contained)
			if err != nil {
				return nil, err
			}
			name, ok := piSourceString(manifest["name"])
			if !ok || !piClaudePluginName.MatchString(name) || !piOptionalStringFields(manifest, "$schema", "displayName", "version", "description", "homepage", "repository", "license") {
				continue
			}
		}
		configPath, ok := piContainedSourcePath(pluginRoot, ".mcp.json")
		if !ok {
			continue
		}
		object, err := piReadSourceJSON(configPath)
		if err != nil {
			return nil, err
		}
		servers, err := mcpJSONObject(configPath, object["mcpServers"], "mcpServers")
		if err != nil {
			return nil, err
		}
		for _, name := range piSourceObjectNames(object["mcpServers"]) {
			if strings.TrimSpace(name) == "" {
				continue
			}
			server, err := mcpJSONObject(configPath, servers[name], "server")
			if err != nil {
				return nil, err
			}
			transports := 0
			for _, key := range []string{"command", "url", "socket"} {
				if value, ok := piSourceString(server[key]); ok && strings.TrimSpace(value) != "" {
					transports++
				}
			}
			if transports != 1 {
				continue
			}
			namespace := strings.ReplaceAll(name, "-", "_")
			if namespaces[namespace] {
				continue
			}
			namespaces[namespace] = true
			value, err := piEnableValue(configPath, servers[name])
			if err != nil {
				return nil, err
			}
			values[name] = value
		}
	}
	return values, nil
}

func capturePiSources(root string) (map[string]mcpEnableValue, error) {
	if !projectHasPi(root) {
		return map[string]mcpEnableValue{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, NewExitError(ExitEnvironment, "error: cannot resolve Pi home directory: "+err.Error())
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot resolve Pi project root: "+root+": "+err.Error())
	}
	root = filepath.Clean(absolute)
	agentDir, err := piSourceAgentDir(home)
	if err != nil {
		return nil, err
	}
	layers, err := piLoadSourceLayers(root, home, agentDir)
	if err != nil {
		return nil, err
	}
	settings := map[string]json.RawMessage{}
	var claudePlugins json.RawMessage
	for _, layer := range layers {
		next, err := piSourceSettings(layer)
		if err != nil {
			return nil, err
		}
		for key, value := range next {
			settings[key] = value
		}
		if raw, exists := layer.object["claudePlugins"]; exists {
			claudePlugins = raw
		}
	}
	native := map[string]mcpEnableValue{}
	if !piExclusiveMode() && bytesJSONEqual(settings["hostConfigDiscovery"], json.RawMessage(`"on"`)) {
		for _, kind := range piImportKinds {
			values, err := piLoadImportedState(kind, root, home)
			if err != nil {
				return nil, err
			}
			mergeMCPEnableValues(native, values)
		}
	}
	for _, layer := range layers {
		expanded, err := piExpandedSourceValues(layer, root, home)
		if err != nil {
			return nil, err
		}
		mergeMCPEnableValues(native, expanded)
	}
	higher := map[string]mcpEnableValue{}
	if !piExclusiveMode() {
		packages, err := piPackageSourceValues(root, agentDir)
		if err != nil {
			return nil, err
		}
		plugins, err := piAgentPluginSourceValues(root, home, agentDir, settings["agentPluginPaths"])
		if err != nil {
			return nil, err
		}
		// Same-name package definitions are excluded, not field-merged, when an
		// Agent Plugin supplies that name.
		for name, value := range packages {
			if _, exists := plugins[name]; !exists {
				higher[name] = value
			}
		}
		mergeMCPEnableValues(higher, plugins)
	}
	mergeMCPEnableValues(higher, native)
	defaults, err := piClaudePluginSourceValues(root, home, claudePlugins)
	if err != nil {
		return nil, err
	}
	namespaces := map[string]string{}
	for name := range higher {
		namespaces[strings.ReplaceAll(name, "-", "_")] = name
	}
	for name := range defaults {
		if higherName, exists := namespaces[strings.ReplaceAll(name, "-", "_")]; exists && higherName != name {
			delete(defaults, name)
		}
	}
	mergeMCPEnableValues(defaults, higher)
	return defaults, nil
}

func piPluginLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address == netip.IPv6Loopback() || address.Is4() && address.As4()[0] == 127
	}
	// WHATWG URL parsing normalizes abbreviated, integer, octal and hexadecimal
	// IPv4 spellings before the provider checks its loopback hostname.
	parts := strings.Split(strings.TrimSuffix(host, "."), ".")
	if len(parts) > 4 {
		return false
	}
	var address uint64
	for i, part := range parts {
		if part == "" {
			return false
		}
		number, err := strconv.ParseUint(part, 0, 32)
		if err != nil {
			return false
		}
		if i == len(parts)-1 {
			bits := uint(8 * (5 - len(parts)))
			if number >= uint64(1)<<bits {
				return false
			}
			address |= number
		} else {
			if number > 255 {
				return false
			}
			address |= number << uint(8*(3-i))
		}
	}
	return address>>24 == 127
}

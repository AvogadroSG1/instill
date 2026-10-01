package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

type mcpInstallPolicy struct {
	Catalog      map[string]*CatalogEntry
	Dependencies []MCPDependency
	ManagePi     bool
	Targets      []string // Normalized APM-only view from the document-owning caller.
}

type mcpEnableValue struct {
	Present bool
	Value   json.RawMessage
}

type mcpEnableSnapshot struct {
	OpenCodeRoot   map[string]mcpEnableValue
	OpenCodeMerged map[string]mcpEnableValue
	CodexRoot      map[string]mcpEnableValue
	CodexMerged    map[string]mcpEnableValue
	ClaudeExisting map[string]struct{}
	PiMerged       map[string]mcpEnableValue
}

func newMCPInstallPolicy(catalog []CatalogEntry, dependencies []MCPDependency, managePi bool) mcpInstallPolicy {
	policy := mcpInstallPolicy{Catalog: make(map[string]*CatalogEntry, len(catalog)), Dependencies: dependencies, ManagePi: managePi}
	for i := range catalog {
		policy.Catalog[catalog[i].Name] = &catalog[i]
	}
	return policy
}

func captureMCPEnableState(root string) (mcpEnableSnapshot, error) {
	var before mcpEnableSnapshot
	var err error
	before.OpenCodeRoot, before.OpenCodeMerged, err = captureOpenCodeMCPEnableState(root)
	if err != nil {
		return before, err
	}
	before.CodexRoot, before.CodexMerged, err = captureCodexMCPEnableState(root)
	if err != nil {
		return before, err
	}
	before.ClaudeExisting, err = captureClaudeMCPEnableState(root)
	if err != nil {
		return before, err
	}
	before.PiMerged, err = capturePiMCPEnableState(root)
	return before, err
}

func reconcileMCPEnableState(ctx context.Context, held *heldLocks, root string, before mcpEnableSnapshot, policy mcpInstallPolicy, applyDefaults bool) error {
	if err := held.requireContext(ctx, root); err != nil {
		return err
	}
	openCodeErr := reconcileOpenCodeMCPEnableState(root, before, policy, applyDefaults)
	codexErr := reconcileCodexMCPEnableState(root, before, policy, applyDefaults)
	if openCodeErr != nil && codexErr != nil {
		return filesystemError("error: cannot repair MCP enable state", openCodeErr, codexErr)
	}
	if openCodeErr != nil {
		return openCodeErr
	}
	if codexErr != nil {
		return codexErr
	}
	if applyDefaults {
		return reconcileClaudeMCPEnableState(ctx, held, root, before, policy)
	}
	return nil
}

func mcpConfigurationError(path, detail string) error {
	return NewExitError(ExitGeneral, "error: malformed MCP configuration: "+path+": "+detail)
}

func checkMCPWritablePath(path string) error {
	for _, candidate := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return NewExitError(ExitFilesystem, "error: cannot inspect MCP configuration: "+path+": "+err.Error())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return NewExitError(ExitFilesystem, "error: refusing to replace symlinked MCP configuration: "+path)
		}
		if candidate != path && !info.IsDir() {
			return NewExitError(ExitFilesystem, "error: MCP configuration parent is not a directory: "+path)
		}
	}
	return nil
}

func readMCPFile(path string, writable bool) ([]byte, os.FileMode, error) {
	if writable {
		if err := checkMCPWritablePath(path); err != nil {
			return nil, 0, err
		}
	}
	mode := os.FileMode(0600)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, mode, nil
	}
	if err != nil {
		return nil, 0, NewExitError(ExitFilesystem, "error: cannot inspect MCP configuration: "+path+": "+err.Error())
	}
	mode = info.Mode().Perm()
	data, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return nil, 0, NewExitError(ExitFilesystem, "error: cannot read MCP configuration: "+path+": "+err.Error())
	}
	return data, mode, nil
}

const mcpJSONBOM = "\xef\xbb\xbf"

func writeMCPFile(path string, before, next []byte, mode os.FileMode) error {
	if bytes.HasPrefix(before, []byte(mcpJSONBOM)) && !bytes.HasPrefix(next, []byte(mcpJSONBOM)) {
		next = append([]byte(mcpJSONBOM), next...)
	}
	if bytes.Equal(before, next) {
		return nil
	}
	if err := checkMCPWritablePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot create MCP configuration directory: "+path+": "+err.Error())
	}
	emitMutationTestEvent("mcp-before-write:" + path)
	current, _, err := readMCPFile(path, true)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return NewExitError(ExitFilesystem, "error: MCP enable state changed before writing: "+path)
	}
	if err := writeFileAtomic(path, next, mode); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write MCP configuration: "+path+": "+err.Error())
	}
	return nil
}

func parseMCPJSON(path string, data []byte) (hujson.Value, map[string]json.RawMessage, error) {
	source := bytes.TrimPrefix(data, []byte(mcpJSONBOM))
	if data == nil {
		source = []byte("{}")
	}
	value, err := hujson.Parse(source)
	if err != nil {
		return value, nil, mcpConfigurationError(path, err.Error())
	}
	standard := value.Clone()
	standard.Standardize()
	object, err := mcpJSONObject(path, standard.Pack(), "root")
	return value, object, err
}

func mcpJSONObject(path string, raw json.RawMessage, label string) (map[string]json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if raw == nil {
		return object, nil
	}
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, mcpConfigurationError(path, label+" must be an object")
	}
	return object, nil
}

func mcpJSONPointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func patchMCPMember(value *hujson.Value, pointer string, raw json.RawMessage) (bool, error) {
	existing := value.Find(pointer)
	if existing == nil && raw == nil {
		return false, nil
	}
	if existing != nil && raw != nil {
		clone := existing.Clone()
		clone.Standardize()
		var current, next any
		if json.Unmarshal(clone.Pack(), &current) == nil && json.Unmarshal(raw, &next) == nil && reflect.DeepEqual(current, next) {
			return false, nil
		}
	}
	operation := struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value,omitempty"`
	}{Op: "add", Path: pointer, Value: raw}
	if raw == nil {
		operation.Op = "remove"
	} else if existing != nil {
		operation.Op = "replace"
	}
	patch, err := json.Marshal([]any{operation})
	if err != nil {
		return false, err
	}
	if err := value.Patch(patch); err != nil {
		return false, err
	}
	return true, nil
}

func mcpAncestors(root string, stopAtRepo bool) []string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for current := filepath.Clean(absolute); ; current = filepath.Dir(current) {
		dirs = append(dirs, current)
		if stopAtRepo {
			if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
				break
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return dirs
}

func mergeMCPEnableValues(dst, src map[string]mcpEnableValue) {
	for name, value := range src {
		previous, exists := dst[name]
		if !exists || value.Present {
			dst[name] = value
		} else {
			dst[name] = previous
		}
	}
}

func projectMCPJSONState(path string, data []byte, mapKey, flag string) (map[string]mcpEnableValue, error) {
	_, object, err := parseMCPJSON(path, data)
	if err != nil {
		return nil, err
	}
	servers, err := mcpJSONObject(path, object[mapKey], mapKey)
	if err != nil {
		return nil, err
	}
	values := make(map[string]mcpEnableValue, len(servers))
	for name, raw := range servers {
		entry, err := mcpJSONObject(path, raw, mapKey+"."+name)
		if err != nil {
			return nil, err
		}
		value, present := entry[flag]
		values[name] = mcpEnableValue{Present: present, Value: value}
	}
	return values, nil
}

func captureOpenCodeMCPEnableState(root string) (map[string]mcpEnableValue, map[string]mcpEnableValue, error) {
	rootPath := filepath.Join(root, "opencode.json")
	data, _, err := readMCPFile(rootPath, true)
	if err != nil {
		return nil, nil, err
	}
	rootValues, err := projectMCPJSONState(rootPath, data, "mcp", "enabled")
	if err != nil {
		return nil, nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, NewExitError(ExitEnvironment, "error: cannot resolve home directory: "+err.Error())
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	global := filepath.Join(configHome, "opencode")
	paths := []string{filepath.Join(global, "config.json"), filepath.Join(global, "opencode.json"), filepath.Join(global, "opencode.jsonc")}
	if custom := os.Getenv("OPENCODE_CONFIG"); custom != "" {
		paths = append(paths, custom)
	}
	disabled := strings.ToLower(strings.TrimSpace(os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG")))
	projectEnabled := disabled != "true" && disabled != "1"
	ancestors := mcpAncestors(root, true)
	if projectEnabled {
		for _, dir := range slices.Backward(ancestors) {
			paths = append(paths, filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc"))
		}
	}
	directories := []string{global}
	if projectEnabled {
		for _, dir := range ancestors {
			candidate := filepath.Join(dir, ".opencode")
			info, err := os.Stat(candidate)
			if err == nil && info.IsDir() {
				directories = append(directories, candidate)
			} else if err != nil && !os.IsNotExist(err) {
				return nil, nil, NewExitError(ExitFilesystem, "error: cannot inspect MCP configuration: "+candidate+": "+err.Error())
			}
		}
	}
	directories = append(directories, filepath.Join(home, ".opencode"))
	if custom := os.Getenv("OPENCODE_CONFIG_DIR"); custom != "" {
		directories = append(directories, custom)
	}
	seen := map[string]bool{}
	for _, dir := range directories {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if strings.HasSuffix(dir, ".opencode") || dir == os.Getenv("OPENCODE_CONFIG_DIR") {
			paths = append(paths, filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc"))
		}
	}
	merged := map[string]mcpEnableValue{}
	for _, path := range paths {
		data, _, err := readMCPFile(path, false)
		if err != nil {
			return nil, nil, err
		}
		state, err := projectMCPJSONState(path, data, "mcp", "enabled")
		if err != nil {
			return nil, nil, err
		}
		mergeMCPEnableValues(merged, state)
	}
	if content := os.Getenv("OPENCODE_CONFIG_CONTENT"); content != "" {
		state, err := projectMCPJSONState("OPENCODE_CONFIG_CONTENT", []byte(content), "mcp", "enabled")
		if err != nil {
			return nil, nil, err
		}
		mergeMCPEnableValues(merged, state)
	}
	return rootValues, merged, nil
}

func desiredMCPEnableValue(name string, rootValues, merged map[string]mcpEnableValue, policy mcpInstallPolicy, applyDefaults bool) (mcpEnableValue, bool) {
	if prior, exists := rootValues[name]; exists {
		return prior, true
	}
	if prior, exists := merged[name]; exists {
		return prior, true
	}
	if applyDefaults {
		if entry := policy.Catalog[name]; entry != nil && entry.DefaultEnabled != nil {
			value := json.RawMessage("false")
			if *entry.DefaultEnabled {
				value = json.RawMessage("true")
			}
			return mcpEnableValue{Present: true, Value: value}, true
		}
	}
	return mcpEnableValue{}, false
}

func reconcileOpenCodeMCPEnableState(root string, before mcpEnableSnapshot, policy mcpInstallPolicy, applyDefaults bool) error {
	path := filepath.Join(root, "opencode.json")
	data, mode, err := readMCPFile(path, true)
	if err != nil || data == nil {
		return err
	}
	value, object, err := parseMCPJSON(path, data)
	if err != nil {
		return err
	}
	servers, err := mcpJSONObject(path, object["mcp"], "mcp")
	if err != nil {
		return err
	}
	changed := false
	for name, raw := range servers {
		if _, err := mcpJSONObject(path, raw, "mcp."+name); err != nil {
			return err
		}
		desired, apply := desiredMCPEnableValue(name, before.OpenCodeRoot, before.OpenCodeMerged, policy, applyDefaults)
		if !apply {
			continue
		}
		var next json.RawMessage
		if desired.Present {
			next = desired.Value
		}
		edited, err := patchMCPMember(&value, "/mcp/"+mcpJSONPointer(name)+"/enabled", next)
		if err != nil {
			return mcpConfigurationError(path, err.Error())
		}
		changed = changed || edited
	}
	if !changed {
		return nil
	}
	return writeMCPFile(path, data, value.Pack(), mode)
}

func canonicalClaudeProjectRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(filepath.Clean(absolute))
	}
	if err != nil {
		return "", NewExitError(ExitFilesystem, "error: cannot resolve Claude project root: "+root+": "+err.Error())
	}
	return filepath.Clean(absolute), nil
}

func claudeApplicableState(path string, data []byte, absoluteRoot string) (map[string]struct{}, []string, error) {
	_, object, err := parseMCPJSON(path, data)
	if err != nil {
		return nil, nil, err
	}
	existing := map[string]struct{}{}
	global, err := mcpJSONObject(path, object["mcpServers"], "mcpServers")
	if err != nil {
		return nil, nil, err
	}
	for name := range global {
		existing[name] = struct{}{}
	}
	projects, err := mcpJSONObject(path, object["projects"], "projects")
	if err != nil {
		return nil, nil, err
	}
	project, err := mcpJSONObject(path, projects[absoluteRoot], "projects."+absoluteRoot)
	if err != nil {
		return nil, nil, err
	}
	local, err := mcpJSONObject(path, project["mcpServers"], "projects.mcpServers")
	if err != nil {
		return nil, nil, err
	}
	for name := range local {
		existing[name] = struct{}{}
	}
	disabled := []string{}
	if raw, ok := project["disabledMcpServers"]; ok {
		if err := json.Unmarshal(raw, &disabled); err != nil || disabled == nil {
			return nil, nil, mcpConfigurationError(path, "disabledMcpServers must be an array of strings")
		}
	}
	for _, name := range disabled {
		existing[name] = struct{}{}
	}
	return existing, disabled, nil
}

func captureClaudeMCPEnableState(root string) (map[string]struct{}, error) {
	absolute, err := canonicalClaudeProjectRoot(root)
	if err != nil {
		return nil, err
	}
	path, err := claudeConfigPath()
	if err != nil {
		return nil, err
	}
	data, _, err := readMCPFile(path, false)
	if err != nil {
		return nil, err
	}
	existing, _, err := claudeApplicableState(path, data, absolute)
	if err != nil {
		return nil, err
	}
	path = filepath.Join(root, ".mcp.json")
	data, _, err = readMCPFile(path, true)
	if err != nil {
		return nil, err
	}
	servers, err := projectMCPJSONState(path, data, "mcpServers", "enabled")
	if err != nil {
		return nil, err
	}
	for name := range servers {
		existing[name] = struct{}{}
	}
	return existing, nil
}

func reconcileClaudeMCPEnableState(ctx context.Context, held *heldLocks, root string, before mcpEnableSnapshot, policy mcpInstallPolicy) error {
	// APM may skip this client; only its emitted project definitions can get defaults.
	output := filepath.Join(root, ".mcp.json")
	data, _, err := readMCPFile(output, true)
	if err != nil || data == nil {
		return err
	}
	servers, err := projectMCPJSONState(output, data, "mcpServers", "enabled")
	if err != nil {
		return err
	}
	var names []string
	for name := range servers {
		entry := policy.Catalog[name]
		if _, exists := before.ClaudeExisting[name]; !exists && entry != nil && entry.DefaultEnabled != nil && !*entry.DefaultEnabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	absolute, err := canonicalClaudeProjectRoot(root)
	if err != nil {
		return err
	}
	path, err := claudeConfigPath()
	if err != nil {
		return err
	}
	if held.mcpClaudeStateRoot == "" {
		return filesystemError("error: required Claude state mutation lock is not held")
	}
	if err := held.requireContext(ctx, held.mcpClaudeStateRoot); err != nil {
		return err
	}
	if err := held.requireContext(ctx, held.mcpClaudeGuardRoot); err != nil {
		return err
	}
	data, mode, err := readMCPFile(path, true)
	if err != nil {
		return err
	}
	value, object, err := parseMCPJSON(path, data)
	if err != nil {
		return err
	}
	current, disabled, err := claudeApplicableState(path, data, absolute)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, exists := current[name]; !exists {
			disabled = append(disabled, name)
		}
	}
	if len(disabled) == 0 {
		return nil
	}
	projects, err := mcpJSONObject(path, object["projects"], "projects")
	if err != nil {
		return err
	}
	prefix := "/projects/" + mcpJSONPointer(absolute)
	if object["projects"] == nil {
		if _, err := patchMCPMember(&value, "/projects", json.RawMessage("{}")); err != nil {
			return mcpConfigurationError(path, err.Error())
		}
	}
	if projects[absolute] == nil {
		if _, err := patchMCPMember(&value, prefix, json.RawMessage("{}")); err != nil {
			return mcpConfigurationError(path, err.Error())
		}
	}
	raw, err := json.Marshal(disabled)
	if err != nil {
		return err
	}
	changed, err := patchMCPMember(&value, prefix+"/disabledMcpServers", raw)
	if err != nil {
		return mcpConfigurationError(path, err.Error())
	}
	if !changed {
		return nil
	}
	return writeMCPFile(path, data, value.Pack(), mode)
}

func claudeStateLockRoot(path string) (string, error) {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return "", NewExitError(ExitFilesystem, "error: Claude state parent is not a directory: "+dir)
			}
			return dir, nil
		}
		if !os.IsNotExist(err) {
			return "", NewExitError(ExitFilesystem, "error: cannot inspect Claude state directory: "+dir+": "+err.Error())
		}
		if filepath.Dir(dir) == dir {
			return "", NewExitError(ExitFilesystem, "error: cannot resolve Claude state lock root: "+path)
		}
	}
}

func withMCPMutationLocks(ctx context.Context, libraryPath, root string, fn func(context.Context, *heldLocks, []CatalogEntry) error) error {
	needsStateLock := false
	err := withRootLocks(ctx, []string{libraryPath, root}, func(ctx context.Context, held *heldLocks) error {
		catalog, err := LoadCatalog(libraryPath, LibraryTypeMCP)
		if err != nil {
			return err
		}
		// APM can materialize .claude after this acquisition. A false catalog
		// default therefore needs the state lock even before that directory exists.
		for i := range catalog {
			if catalog[i].DefaultEnabled != nil && !*catalog[i].DefaultEnabled {
				needsStateLock = true
				break
			}
		}
		if needsStateLock {
			return nil
		}
		return fn(ctx, held, catalog)
	})
	if err != nil || !needsStateLock {
		return err
	}
	path, err := claudeConfigPath()
	if err != nil {
		return err
	}
	if err := checkMCPWritablePath(path); err != nil {
		return err
	}
	guardRoot, err := os.UserHomeDir()
	if err != nil {
		return NewExitError(ExitEnvironment, "error: cannot resolve Claude state guard root: "+err.Error())
	}
	stateRoot, err := claudeStateLockRoot(path)
	if err != nil {
		return err
	}
	// The nearest existing parent may change when the first state write creates
	// private directories. HOME remains the common guard across that transition.
	return withRootLocks(ctx, []string{libraryPath, root, guardRoot, stateRoot}, func(ctx context.Context, held *heldLocks) error {
		held.mcpClaudeStateRoot = stateRoot
		held.mcpClaudeGuardRoot = guardRoot
		catalog, err := LoadCatalog(libraryPath, LibraryTypeMCP)
		if err != nil {
			return err
		}
		if err := checkMCPWritablePath(path); err != nil {
			return err
		}
		return fn(ctx, held, catalog)
	})
}

func releaseMCPLibraryLock(ctx context.Context, held *heldLocks, libraryPath, root string) error {
	paths, err := canonicalRoots([]string{libraryPath})
	if err != nil {
		return filesystemError("error: cannot resolve library lock", err)
	}
	protected, err := canonicalRoots([]string{root})
	if err != nil {
		return filesystemError("error: cannot resolve project lock", err)
	}
	if paths[0].key == protected[0].key {
		return nil
	}
	for _, stateRoot := range []string{held.mcpClaudeStateRoot, held.mcpClaudeGuardRoot} {
		if stateRoot == "" {
			continue
		}
		state, err := canonicalRoots([]string{stateRoot})
		if err != nil {
			return filesystemError("error: cannot resolve Claude state lock", err)
		}
		if paths[0].key == state[0].key {
			return nil
		}
	}
	return held.release(ctx, libraryPath)
}

func runAPMPruneWithPiLocked(ctx context.Context, held *heldLocks, runner CommandRunner, root string, policy mcpInstallPolicy) error {
	if err := held.requireContext(ctx, root); err != nil {
		return err
	}
	before, err := capturePiMCPEnableState(root)
	if err != nil {
		return err
	}
	if err := runAPMPruneLocked(ctx, held, runner, root); err != nil {
		return err
	}
	return reconcilePiMCPConfig(ctx, held, root, mcpEnableSnapshot{PiMerged: before}, policy, false)
}

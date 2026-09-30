package instill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/tailscale/hujson"
)

const (
	piMCPConfigName    = "mcp-adapter.json"
	piMCPServersKey    = "mcpServers"
	piLegacyServersKey = "mcp-servers"
	piManagedKey       = "_instillManagedServers"
)

func piProjectMCPPath(root string) string { return filepath.Join(root, ".pi", piMCPConfigName) }
func piMalformed(path, msg string) error {
	return NewExitError(ExitGeneral, fmt.Sprintf("error: malformed Pi MCP config %s: %s", path, msg))
}

func piReadObject(path string, writable bool) (map[string]json.RawMessage, []byte, os.FileMode, error) {
	data, mode, err := readMCPFile(path, writable)
	if err != nil {
		return nil, nil, 0, err
	}
	_, object, err := parseMCPJSON(path, data)
	return object, data, mode, err
}

func piServerMap(path string, object map[string]json.RawMessage) (string, map[string]json.RawMessage, error) {
	key := piMCPServersKey
	raw, exists := object[key]
	if !exists {
		if legacy, exists := object[piLegacyServersKey]; exists {
			key = piLegacyServersKey
			raw = legacy
		}
	}
	servers, err := mcpJSONObject(path, raw, key)
	return key, servers, err
}

func piEnableValue(path string, raw json.RawMessage) (mcpEnableValue, error) {
	object, err := mcpJSONObject(path, raw, "server")
	if err != nil {
		return mcpEnableValue{}, err
	}
	value, present := object["disabled"]
	return mcpEnableValue{Present: present, Value: value}, nil
}

func capturePiMCPEnableState(root string) (map[string]mcpEnableValue, error) {
	return capturePiSources(root)
}

func piDesiredServers(policy mcpInstallPolicy) map[string]*CatalogEntry {
	desired := make(map[string]*CatalogEntry, len(policy.Dependencies))
	for _, dependency := range policy.Dependencies {
		if entry := policy.Catalog[dependency.Name]; entry != nil && entry.Type == LibraryTypeMCP {
			desired[dependency.Name] = entry
		}
	}
	return desired
}

func validatePiMCPPolicy(root string, before mcpEnableSnapshot, policy mcpInstallPolicy) error {
	if !projectHasPi(root) || !piExclusiveMode() {
		return nil
	}
	for name := range piDesiredServers(policy) {
		if _, exists := before.PiMerged[name]; !exists {
			return NewExitError(ExitEnvironment, "error: Pi MCP defaults require project configuration; pi-mcp-adapter exclusive mode ignores .pi/mcp-adapter.json")
		}
	}
	return nil
}

func piRaw(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }

func piDefinition(entry *CatalogEntry) map[string]json.RawMessage {
	object := map[string]json.RawMessage{}
	if entry.Transport == "stdio" {
		object["command"] = piRaw(entry.Command)
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		object["args"] = piRaw(args)
		if len(entry.Env) > 0 {
			object["env"] = piRaw(mcpEnvironment(entry.Env))
		}
	} else {
		object["url"] = piRaw(entry.URL)
		if entry.Transport == "sse" {
			object["httpTransport"] = piRaw("sse")
		}
	}
	if entry.DefaultEnabled != nil {
		object["disabled"] = piRaw(!*entry.DefaultEnabled)
	}
	return object
}

func piReconcileEntry(document *hujson.Value, pointer string, current map[string]json.RawMessage, entry *CatalogEntry) (bool, error) {
	changed := false
	set := func(key string, raw json.RawMessage) error {
		edited, err := patchMCPMember(document, pointer+"/"+mcpJSONPointer(key), raw)
		changed = changed || edited
		return err
	}
	remove := func(keys ...string) error {
		for _, key := range keys {
			if err := set(key, nil); err != nil {
				return err
			}
		}
		return nil
	}
	if entry.Transport == "stdio" {
		if err := remove("url", "headers", "requestHeadersCommand", "caFile", "auth", "bearerToken", "bearerTokenEnv", "bearerTokenStore", "oauth", "httpTransport", "socket"); err != nil {
			return false, err
		}
		if err := set("command", piRaw(entry.Command)); err != nil {
			return false, err
		}
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		if err := set("args", piRaw(args)); err != nil {
			return false, err
		}
		var env json.RawMessage
		if len(entry.Env) > 0 {
			env = piRaw(mcpEnvironment(entry.Env))
		}
		if err := set("env", env); err != nil {
			return false, err
		}
	} else {
		if err := remove("command", "args", "env", "cwd", "pluginDataDir", "literalEnv", "inheritEnv", "socket"); err != nil {
			return false, err
		}
		oldURL, _ := piSourceString(current["url"])
		if oldURL != entry.URL {
			if err := remove("headers", "bearerToken", "bearerTokenEnv", "bearerTokenStore", "requestHeadersCommand", "caFile"); err != nil {
				return false, err
			}
			if !bytesJSONEqual(current["oauth"], json.RawMessage("false")) {
				if err := set("oauth", nil); err != nil {
					return false, err
				}
			}
		}
		if err := set("url", piRaw(entry.URL)); err != nil {
			return false, err
		}
		var transport json.RawMessage
		if entry.Transport == "sse" {
			transport = piRaw("sse")
		}
		if err := set("httpTransport", transport); err != nil {
			return false, err
		}
	}
	// disabled, directTools, lifecycle and every unrelated member are user-owned.
	return changed, nil
}

func piManagedNames(path string, object map[string]json.RawMessage) ([]string, error) {
	raw, exists := object[piManagedKey]
	if !exists {
		return nil, nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil || names == nil {
		return nil, piMalformed(path, piManagedKey+" must be an array of strings")
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			return nil, piMalformed(path, piManagedKey+" contains duplicate or empty names")
		}
		seen[name] = true
	}
	return names, nil
}

func reconcilePiMCPConfig(ctx context.Context, held *heldLocks, root string, before mcpEnableSnapshot, policy mcpInstallPolicy, allowCreate bool) error {
	if err := held.requireContext(ctx, root); err != nil {
		return err
	}
	if !projectHasPi(root) || piExclusiveMode() {
		return nil
	}
	path := piProjectMCPPath(root)
	object, data, mode, err := piReadObject(path, true)
	if err != nil {
		return err
	}
	managed, err := piManagedNames(path, object)
	if err != nil {
		return err
	}
	key, servers, err := piServerMap(path, object)
	if err != nil {
		return err
	}
	document, _, err := parseMCPJSON(path, data)
	if err != nil {
		return err
	}
	desired := piDesiredServers(policy)
	owned := make(map[string]bool, len(managed))
	for _, name := range managed {
		owned[name] = true
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
	mapPointer := "/" + mcpJSONPointer(key)
	for _, name := range managed {
		if _, selected := desired[name]; selected {
			continue
		}
		if _, exists := servers[name]; exists {
			if err := patch(mapPointer+"/"+mcpJSONPointer(name), nil); err != nil {
				return err
			}
		}
		delete(owned, name)
	}
	if allowCreate {
		names := make([]string, 0, len(desired))
		for name := range desired {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entry := desired[name]
			raw, exists := servers[name]
			pointer := mapPointer + "/" + mcpJSONPointer(name)
			if exists {
				if !owned[name] {
					continue
				} // Includes an unrecorded choice made during APM.
				current, err := mcpJSONObject(path, raw, "server "+name)
				if err != nil {
					return err
				}
				edited, err := piReconcileEntry(&document, pointer, current, entry)
				if err != nil {
					return piMalformed(path, err.Error())
				}
				changed = changed || edited
				continue
			}
			if _, existed := before.PiMerged[name]; existed && !owned[name] {
				continue
			}
			if document.Find(mapPointer) == nil {
				if err := patch(mapPointer, json.RawMessage("{}")); err != nil {
					return err
				}
			}
			if err := patch(pointer, piRaw(piDefinition(entry))); err != nil {
				return err
			}
			owned[name] = true
		}
	}
	final := make([]string, 0, len(owned))
	for name := range owned {
		final = append(final, name)
	}
	sort.Strings(final)
	if len(final) == 0 {
		if err := patch("/"+piManagedKey, nil); err != nil {
			return err
		}
	} else if !slices.Equal(final, managed) {
		if err := patch("/"+piManagedKey, piRaw(final)); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return writeMCPFile(path, data, document.Pack(), mode)
}

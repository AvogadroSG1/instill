package instill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/smm-h/go-toml-edit"
)

// captureCodexMCPEnableState reads the Codex user/project layers. The first
// result is the project's writable layer; the second is the effective
// read-only view, with the closest project layer taking precedence.
func captureCodexMCPEnableState(root string) (map[string]mcpEnableValue, map[string]mcpEnableValue, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, NewExitError(ExitFilesystem, "error: cannot resolve Codex project root: "+root+": "+err.Error())
	}
	root = filepath.Clean(absolute)
	projectPath := filepath.Join(root, ".codex", "config.toml")
	projectDoc, _, err := readCodexDocument(projectPath, true)
	if err != nil {
		return nil, nil, err
	}
	projectState, err := codexDocumentEnableState(projectPath, projectDoc)
	if err != nil {
		return nil, nil, err
	}

	merged := make(map[string]mcpEnableValue)
	layers, err := codexConfigLayers(root)
	if err != nil {
		return nil, nil, err
	}
	for _, path := range layers {
		// The project layer was already read with writable checks above. Read it
		// again here only when it is not the final layer; this keeps the layer
		// order explicit and avoids ever making a user file writable.
		var doc *tomledit.Document
		if filepath.Clean(path) == filepath.Clean(projectPath) {
			doc = projectDoc
		} else {
			doc, _, err = readCodexDocument(path, false)
			if err != nil {
				return nil, nil, err
			}
		}
		state, err := codexDocumentEnableState(path, doc)
		if err != nil {
			return nil, nil, err
		}
		mergeMCPEnableValues(merged, state)
	}
	return projectState, merged, nil
}

// reconcileCodexMCPEnableState restores existing Codex choices and applies a
// library default only to a server absent from every pre-install layer. It
// edits the project file emitted by APM, never the user-level file.
func reconcileCodexMCPEnableState(root string, before mcpEnableSnapshot, policy mcpInstallPolicy, applyDefaults bool) error {
	root = filepath.Clean(root)
	path := filepath.Join(root, ".codex", "config.toml")
	doc, original, mode, err := readCodexWritableDocument(path)
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}

	current, err := codexDocumentEnableState(path, doc)
	if err != nil {
		return err
	}
	changed := false
	for name := range current {
		pathEnabled := codexEnablePath(name)

		desired, haveDesired := desiredMCPEnableValue(name, before.CodexRoot, before.CodexMerged, policy, applyDefaults)
		if !haveDesired {
			continue
		}

		if desired.Present {
			value, err := codexEnableBool(desired)
			if err != nil {
				return malformedCodex(path, name+".enabled", err)
			}
			if currentValue := current[name]; currentValue.Present && bytes.Equal(currentValue.Value, desired.Value) {
				continue
			}
			if err := doc.Set(pathEnabled, value); err != nil {
				return malformedCodex(path, pathEnabled, err)
			}
			changed = true
			continue
		}
		if current[name].Present {
			if err := doc.Delete(pathEnabled); err != nil {
				return malformedCodex(path, pathEnabled, err)
			}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeMCPFile(path, original, doc.Bytes(), mode)
}

func readCodexWritableDocument(path string) (*tomledit.Document, []byte, os.FileMode, error) {
	data, mode, err := readMCPFile(path, true)
	if err != nil {
		return nil, nil, 0, err
	}
	if data == nil {
		return nil, nil, mode, nil
	}
	doc, err := tomledit.Parse(data)
	if err != nil {
		return nil, nil, 0, malformedCodex(path, "document", err)
	}
	return doc, data, mode, nil
}

func readCodexDocument(path string, writable bool) (*tomledit.Document, os.FileMode, error) {
	data, mode, err := readMCPFile(path, writable)
	if err != nil {
		return nil, mode, err
	}
	if data == nil {
		return nil, mode, nil
	}
	doc, err := tomledit.Parse(data)
	if err != nil {
		return nil, mode, malformedCodex(path, "document", err)
	}
	return doc, mode, nil
}

func codexDocumentEnableState(path string, doc *tomledit.Document) (map[string]mcpEnableValue, error) {
	out := make(map[string]mcpEnableValue)
	if doc == nil {
		return out, nil
	}
	entry, ok := doc.Root().Get("mcp_servers")
	if !ok {
		return out, nil
	}
	servers, ok := entry.Record()
	if !ok {
		return nil, malformedCodex(path, "mcp_servers", fmt.Errorf("must be a table"))
	}
	for serverEntry := range servers.Entries() {
		name := serverEntry.Key()
		server, ok := serverEntry.Record()
		if !ok {
			return nil, malformedCodex(path, "mcp_servers."+name, fmt.Errorf("must be a table"))
		}
		enabled, ok := server.Get("enabled")
		if !ok {
			out[name] = mcpEnableValue{}
			continue
		}
		if enabled.Kind() != tomledit.EntryValue {
			return nil, malformedCodex(path, "mcp_servers."+name+".enabled", fmt.Errorf("must be a boolean"))
		}
		node, ok := enabled.Node()
		if !ok || node.Type() != tomledit.NodeBoolean {
			return nil, malformedCodex(path, "mcp_servers."+name+".enabled", fmt.Errorf("must be a boolean"))
		}
		value, err := node.(tomledit.Scalar).AsBool()
		if err != nil {
			return nil, malformedCodex(path, "mcp_servers."+name+".enabled", err)
		}
		out[name] = mcpEnableValue{Present: true, Value: json.RawMessage(codexBoolJSON(value))}
	}
	return out, nil
}

func codexConfigLayers(root string) ([]string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, NewExitError(ExitEnvironment, "error: cannot resolve Codex home directory: "+err.Error())
		}
		codexHome = filepath.Join(home, ".codex")
	}
	layers := []string{filepath.Join(codexHome, "config.toml")}
	var projectLayers []string
	for current := filepath.Clean(root); ; current = filepath.Dir(current) {
		projectLayers = append(projectLayers, filepath.Join(current, ".codex", "config.toml"))
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for i := len(projectLayers) - 1; i >= 0; i-- {
		layers = append(layers, projectLayers[i])
	}
	return layers, nil
}

func codexEnablePath(name string) string {
	return tomledit.JoinPath([]tomledit.PathSegment{
		{Kind: tomledit.SegmentKey, Key: "mcp_servers"},
		{Kind: tomledit.SegmentKey, Key: name},
		{Kind: tomledit.SegmentKey, Key: "enabled"},
	})
}

func codexEnableBool(value mcpEnableValue) (bool, error) {
	if !value.Present {
		return false, fmt.Errorf("enabled member is absent")
	}
	var out bool
	if err := json.Unmarshal(value.Value, &out); err != nil {
		return false, err
	}
	return out, nil
}

func codexBoolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func malformedCodex(path, field string, err error) error {
	return NewExitError(ExitGeneral, fmt.Sprintf("error: malformed Codex config: %s: %s: %v", path, field, err))
}

package instill

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LibraryType string

const (
	LibraryTypeSkill       LibraryType = "skill"
	LibraryTypePlugin      LibraryType = "plugin"
	LibraryTypeMCP         LibraryType = "mcp"
	LibraryTypeInstruction LibraryType = "instruction"
	LibraryTypePrompt      LibraryType = "prompt"
)

type CatalogEntry struct {
	Type        LibraryType
	Name        string
	Category    string
	Path        string
	Transport   string
	Command     string
	Args        []string
	URL         string
	Env         []string
	ApplyTo     string
	Description string
}

func LoadCatalog(root string, typ LibraryType) ([]CatalogEntry, error) {
	path, headers, err := catalogFileSpec(root, typ)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // Catalog path is resolved under the selected library root.
	if err != nil {
		if os.IsNotExist(err) {
			return []CatalogEntry{}, nil
		}
		return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read catalog: %v", err))
	}

	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed catalog: %v", err))
	}
	if len(rows) == 0 {
		return []CatalogEntry{}, nil
	}
	if !equalStringSlices(rows[0], headers) {
		return nil, NewExitError(ExitGeneral, "error: malformed catalog: invalid header")
	}

	entries := make([]CatalogEntry, 0, max(0, len(rows)-1))
	for _, row := range rows[1:] {
		entry, err := parseCatalogRow(typ, row)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	sortCatalogEntries(entries)
	return entries, nil
}

func catalogExists(root string, typ LibraryType) (bool, error) {
	path, _, err := catalogFileSpec(root, typ)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read catalog: %v", err))
	}
	return !info.IsDir(), nil
}

func WriteCatalog(root string, typ LibraryType, entries []CatalogEntry) error {
	path, headers, err := catalogFileSpec(root, typ)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write catalog: %v", err))
	}

	normalized := append([]CatalogEntry{}, entries...)
	for i := range normalized {
		normalized[i].Type = typ
		if err := validateCatalogEntry(normalized[i]); err != nil {
			return err
		}
	}
	sortCatalogEntries(normalized)

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(headers); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write catalog: %v", err))
	}
	for _, entry := range normalized {
		if err := writer.Write(formatCatalogRow(entry)); err != nil {
			return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write catalog: %v", err))
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write catalog: %v", err))
	}

	if err := writeFileAtomic(path, buf.Bytes(), 0o644); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write catalog: %v", err))
	}
	return nil
}

func ScanLibrary(root string, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin, LibraryTypeMCP, LibraryTypeInstruction, LibraryTypePrompt} {
		existing, err := LoadCatalog(root, typ)
		if err != nil {
			return err
		}

		discovered, err := discoverCatalogEntries(root, typ)
		if err != nil {
			return err
		}

		existingByName := make(map[string]CatalogEntry, len(existing))
		existingByPath := make(map[string]CatalogEntry, len(existing))
		for _, entry := range existing {
			existingByName[entry.Name] = entry
			if strings.TrimSpace(entry.Path) != "" {
				existingByPath[entry.Path] = entry
			}
		}

		merged := make([]CatalogEntry, 0, len(discovered))
		preservedNames := make(map[string]struct{}, len(discovered))
		for _, entry := range discovered {
			current, ok := existingByName[entry.Name]
			if !ok && strings.TrimSpace(entry.Path) != "" {
				current, ok = existingByPath[entry.Path]
			}
			if ok {
				entry = mergeCatalogEntry(current, entry)
			}
			if err := validateCatalogEntry(entry); err != nil {
				if typ == LibraryTypeMCP && !ok {
					return NewExitError(ExitGeneral, fmt.Sprintf(
						"error: malformed mcp config: %s: %s",
						entry.Path,
						strings.TrimPrefix(ErrorMessage(err), "error: malformed catalog: "),
					))
				}
				return err
			}
			merged = append(merged, entry)
			preservedNames[entry.Name] = struct{}{}
		}

		for _, entry := range existing {
			if _, ok := preservedNames[entry.Name]; ok {
				continue
			}
			exists, err := catalogContentExists(root, entry)
			if err != nil {
				return err
			}
			if !exists {
				if err := writeLine(stdout, "removed: "+entry.Name+" (content not found)"); err != nil {
					return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
				}
			}
		}

		if err := WriteCatalog(root, typ, merged); err != nil {
			return err
		}
	}
	return nil
}

func AddCatalogEntry(root string, entry CatalogEntry) error {
	entries, err := LoadCatalog(root, entry.Type)
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	return WriteCatalog(root, entry.Type, entries)
}

func ShowCatalog(root string, typ LibraryType, filter string, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	entries, err := LoadCatalog(root, typ)
	if err != nil {
		return err
	}

	filter = strings.ToLower(filter)
	visible := 0
	for _, entry := range entries {
		if filter != "" && !strings.Contains(strings.ToLower(entry.Name), filter) {
			continue
		}
		if err := writeLine(stdout, entry.Name); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		visible++
	}

	return writeLine(stdout, fmt.Sprintf("%d entries", visible))
}

func catalogFileSpec(root string, typ LibraryType) (string, []string, error) {
	switch typ {
	case LibraryTypeSkill:
		return filepath.Join(root, "skills", "catalog.csv"), []string{"name", "category", "path", "description"}, nil
	case LibraryTypePlugin:
		return filepath.Join(root, "plugins", "catalog.csv"), []string{"name", "category", "path", "description"}, nil
	case LibraryTypeMCP:
		return filepath.Join(root, "mcp", "catalog.csv"), []string{"name", "transport", "command", "args", "url", "env", "description"}, nil
	case LibraryTypeInstruction:
		return filepath.Join(root, "instructions", "catalog.csv"), []string{"name", "apply_to", "path", "description"}, nil
	case LibraryTypePrompt:
		return filepath.Join(root, "prompts", "catalog.csv"), []string{"name", "path", "description"}, nil
	default:
		return "", nil, NewExitError(ExitGeneral, "error: invalid library type: "+string(typ))
	}
}

func parseCatalogRow(typ LibraryType, row []string) (CatalogEntry, error) {
	entry := CatalogEntry{Type: typ}
	switch typ {
	case LibraryTypeSkill, LibraryTypePlugin:
		if len(row) != 4 {
			return CatalogEntry{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed catalog: invalid %s row", typ))
		}
		entry.Name = row[0]
		entry.Category = row[1]
		entry.Path = row[2]
		entry.Description = row[3]
	case LibraryTypeMCP:
		if len(row) != 7 {
			return CatalogEntry{}, NewExitError(ExitGeneral, "error: malformed catalog: invalid mcp row")
		}
		entry.Name = row[0]
		entry.Transport = row[1]
		entry.Command = row[2]
		entry.Args = splitCSVList(row[3])
		entry.URL = row[4]
		entry.Env = splitCSVList(row[5])
		entry.Description = row[6]
	case LibraryTypeInstruction:
		if len(row) != 4 {
			return CatalogEntry{}, NewExitError(ExitGeneral, "error: malformed catalog: invalid instruction row")
		}
		entry.Name = row[0]
		entry.ApplyTo = row[1]
		entry.Path = row[2]
		entry.Description = row[3]
	case LibraryTypePrompt:
		if len(row) != 3 {
			return CatalogEntry{}, NewExitError(ExitGeneral, "error: malformed catalog: invalid prompt row")
		}
		entry.Name = row[0]
		entry.Path = row[1]
		entry.Description = row[2]
	default:
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: invalid library type: "+string(typ))
	}

	if err := validateCatalogEntry(entry); err != nil {
		return CatalogEntry{}, err
	}
	return entry, nil
}

func validateCatalogEntry(entry CatalogEntry) error {
	if strings.TrimSpace(entry.Name) == "" {
		return NewExitError(ExitGeneral, "error: malformed catalog: name is required")
	}

	switch entry.Type {
	case LibraryTypeSkill:
		if !IsValidSkillName(entry.Name) {
			return NewExitError(ExitGeneral, "error: malformed catalog: invalid skill name: "+entry.Name)
		}
		if strings.TrimSpace(entry.Path) == "" {
			return NewExitError(ExitGeneral, "error: malformed catalog: path is required")
		}
	case LibraryTypePlugin:
		if strings.TrimSpace(entry.Name) == "" {
			return NewExitError(ExitGeneral, "error: malformed catalog: name is required")
		}
		if strings.TrimSpace(entry.Path) == "" {
			return NewExitError(ExitGeneral, "error: malformed catalog: path is required")
		}
	case LibraryTypeMCP:
		switch entry.Transport {
		case "stdio":
			if strings.TrimSpace(entry.Command) == "" {
				return NewExitError(ExitGeneral, "error: malformed catalog: command is required for stdio transport")
			}
		case "http", "sse":
			if strings.TrimSpace(entry.URL) == "" {
				return NewExitError(ExitGeneral, "error: malformed catalog: url is required for "+entry.Transport+" transport")
			}
		default:
			return NewExitError(ExitGeneral, "error: malformed catalog: invalid transport: "+entry.Transport)
		}
	case LibraryTypeInstruction, LibraryTypePrompt:
		if strings.TrimSpace(entry.Path) == "" {
			return NewExitError(ExitGeneral, "error: malformed catalog: path is required")
		}
	default:
		return NewExitError(ExitGeneral, "error: invalid library type: "+string(entry.Type))
	}

	return nil
}

func formatCatalogRow(entry CatalogEntry) []string {
	switch entry.Type {
	case LibraryTypeSkill, LibraryTypePlugin:
		return []string{entry.Name, entry.Category, entry.Path, entry.Description}
	case LibraryTypeMCP:
		return []string{
			entry.Name,
			entry.Transport,
			entry.Command,
			strings.Join(entry.Args, ","),
			entry.URL,
			strings.Join(entry.Env, ","),
			entry.Description,
		}
	case LibraryTypeInstruction:
		return []string{entry.Name, entry.ApplyTo, entry.Path, entry.Description}
	case LibraryTypePrompt:
		return []string{entry.Name, entry.Path, entry.Description}
	default:
		return nil
	}
}

func splitCSVList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func sortCatalogEntries(entries []CatalogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
}

func discoverCatalogEntries(root string, typ LibraryType) ([]CatalogEntry, error) {
	base, _, err := catalogFileSpec(root, typ)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(base)
	marker := catalogMarkerFileName(typ)
	entries := make([]CatalogEntry, 0)

	if _, err := os.Stat(baseDir); err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read library: %v", err))
	}

	err = filepath.WalkDir(baseDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != marker {
			return nil
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		var name string
		if typ == LibraryTypePlugin {
			dir := filepath.Dir(path)
			baseName := filepath.Base(dir)
			if baseName == ".claude-plugin" || baseName == ".codex-plugin" {
				dir = filepath.Dir(dir)
			}
			relDir, err := filepath.Rel(baseDir, dir)
			if err != nil {
				return err
			}
			name = filepath.ToSlash(relDir)
			if name == "." {
				name = filepath.Base(dir)
			}
		} else {
			name = strings.TrimSuffix(relPath, "/"+marker)
		}
		entry := CatalogEntry{
			Type: typ,
			Name: name,
			Path: relPath,
		}
		if typ == LibraryTypeSkill || typ == LibraryTypePlugin {
			entry.Category = filepath.ToSlash(filepath.Dir(name))
			if entry.Category == "." {
				entry.Category = ""
			}
		}
		if typ == LibraryTypePlugin {
			if meta, err := loadPluginMetadata(path); err == nil && strings.TrimSpace(meta.Description) != "" {
				entry.Description = meta.Description
			}
		}
		if typ == LibraryTypeMCP {
			parsed, err := loadMCPConfig(path)
			if err != nil {
				return err
			}
			entry.Transport = parsed.Transport
			entry.Command = parsed.Command
			entry.Args = parsed.Args
			entry.URL = parsed.URL
			entry.Env = parsed.Env
			entry.Description = parsed.Description
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read library: %v", err))
	}

	sortCatalogEntries(entries)
	return entries, nil
}

func mergeCatalogEntry(existing CatalogEntry, discovered CatalogEntry) CatalogEntry {
	merged := existing
	merged.Type = discovered.Type
	if strings.TrimSpace(merged.Name) == "" {
		merged.Name = discovered.Name
	}
	if strings.TrimSpace(merged.Path) == "" || merged.Name == discovered.Name {
		merged.Path = discovered.Path
	}
	if strings.TrimSpace(merged.Category) == "" {
		merged.Category = discovered.Category
	}
	return merged
}

func catalogContentExists(root string, entry CatalogEntry) (bool, error) {
	path, err := catalogContentPath(root, entry)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read library: %v", err))
	}
	return !info.IsDir(), nil
}

func catalogContentPath(root string, entry CatalogEntry) (string, error) {
	path, _, err := catalogFileSpec(root, entry.Type)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), filepath.FromSlash(entry.Path)), nil
}

func catalogMarkerFileName(typ LibraryType) string {
	switch typ {
	case LibraryTypeSkill:
		return "SKILL.md"
	case LibraryTypePlugin:
		return "plugin.json"
	case LibraryTypeMCP:
		return "config.json"
	case LibraryTypeInstruction:
		return "INSTRUCTION.md"
	case LibraryTypePrompt:
		return "PROMPT.md"
	default:
		return ""
	}
}

type pluginMetadataFile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func loadPluginMetadata(path string) (pluginMetadataFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Scan reads marker files under the selected library root.
	if err != nil {
		return pluginMetadataFile{}, err
	}
	var meta pluginMetadataFile
	if len(bytes.TrimSpace(data)) == 0 {
		return meta, nil
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return pluginMetadataFile{}, nil
	}
	return meta, nil
}

type mcpConfigFile struct {
	Transport   string   `json:"transport"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	URL         string   `json:"url"`
	Env         []string `json:"env"`
	Description string   `json:"description"`
}

func loadMCPConfig(path string) (mcpConfigFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Scan reads marker files under the selected library root.
	if err != nil {
		return mcpConfigFile{}, err
	}
	var config mcpConfigFile
	if len(bytes.TrimSpace(data)) == 0 {
		return config, nil
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return mcpConfigFile{}, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed mcp config: %v", err))
	}
	return config, nil
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

package instill

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectSelection is the complete set of typed Library entries to retain in a project.
// Each field contains names from its corresponding Library catalog.
type ProjectSelection struct {
	Skills       []string
	Plugins      []string
	MCPs         []string
	Instructions []string
	Prompts      []string
}

// ProjectSelectionOptions supplies the project, Library, runner, and complete replacement selection.
type ProjectSelectionOptions struct {
	Project     Project
	LibraryPath string
	Selection   ProjectSelection
	Runner      CommandRunner
	Stdout      io.Writer
}

// projectContentFileSnapshot records one matching output without following symlinks.
type projectContentFileSnapshot struct {
	mode   os.FileMode
	data   []byte
	target string
}

// projectContentSnapshot holds source bytes and destination state captured under the relevant locks.
type projectContentSnapshot struct {
	typ          LibraryType
	root         string
	destinations map[string][]byte
	before       map[string]projectContentFileSnapshot
}

// projectSelectionPlan is the catalog-resolved selection used by overwrite orchestration.
type projectSelectionPlan struct {
	APM        []APMDependency
	MCP        []MCPDependency
	Skills     []CatalogEntry
	Plugins    []CatalogEntry
	MCPCatalog []CatalogEntry
	Content    []projectContentSnapshot
}

func buildProjectSelectionPlan(opts ProjectSelectionOptions) (*projectSelectionPlan, error) {
	skills, err := LoadCatalog(opts.LibraryPath, LibraryTypeSkill)
	if err != nil {
		return nil, err
	}
	plugins, err := LoadCatalog(opts.LibraryPath, LibraryTypePlugin)
	if err != nil {
		return nil, err
	}
	mcps, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return nil, err
	}
	instructions, err := LoadCatalog(opts.LibraryPath, LibraryTypeInstruction)
	if err != nil {
		return nil, err
	}
	prompts, err := LoadCatalog(opts.LibraryPath, LibraryTypePrompt)
	if err != nil {
		return nil, err
	}

	// Validate the entire selection before resolving any selected entries into output.
	for _, group := range []struct {
		typ     LibraryType
		names   []string
		entries []CatalogEntry
	}{
		{LibraryTypeSkill, opts.Selection.Skills, skills},
		{LibraryTypePlugin, opts.Selection.Plugins, plugins},
		{LibraryTypeMCP, opts.Selection.MCPs, mcps},
		{LibraryTypeInstruction, opts.Selection.Instructions, instructions},
		{LibraryTypePrompt, opts.Selection.Prompts, prompts},
	} {
		byName := catalogEntriesByName(group.entries)
		for _, name := range normalizeSkills(group.names) {
			_, ok := byName[name]
			if group.typ == LibraryTypeSkill {
				_, ok = resolveCatalogEntryName(byName, name, true)
			}
			if !ok {
				return nil, unknownLibraryEntryError(string(group.typ), name)
			}
		}
	}

	skillByName, pluginByName, mcpByName := catalogEntriesByName(skills), catalogEntriesByName(plugins), catalogEntriesByName(mcps)
	apm, err := applySkillPick(nil, opts.LibraryPath, skillByName, opts.Selection.Skills, nil)
	if err != nil {
		return nil, err
	}
	pluginsAPM, err := applyPluginPick(nil, opts.LibraryPath, pluginByName, opts.Selection.Plugins, nil)
	if err != nil {
		return nil, err
	}
	apm = append(apm, pluginsAPM...)
	apm = normalizeAPMDependencies(apm)
	mcp, err := applyMCPPick(nil, mcpByName, opts.Selection.MCPs, nil)
	if err != nil {
		return nil, err
	}
	sort.Slice(mcp, func(i, j int) bool { return mcp[i].Name < mcp[j].Name })

	content := make([]projectContentSnapshot, 0, 2)
	for _, group := range []struct {
		typ     LibraryType
		names   []string
		entries []CatalogEntry
	}{
		{LibraryTypeInstruction, opts.Selection.Instructions, instructions},
		{LibraryTypePrompt, opts.Selection.Prompts, prompts},
	} {
		snapshot, err := captureProjectContent(opts.Project.Root, opts.LibraryPath, group.typ, group.names, group.entries)
		if err != nil {
			return nil, err
		}
		content = append(content, snapshot)
	}
	return &projectSelectionPlan{
		APM: apm, MCP: mcp, Skills: skills, Plugins: plugins, MCPCatalog: mcps, Content: content,
	}, nil
}

func catalogEntriesByName(entries []CatalogEntry) map[string]CatalogEntry {
	byName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	return byName
}

func captureProjectContent(projectRoot, libraryPath string, typ LibraryType, names []string, entries []CatalogEntry) (projectContentSnapshot, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return projectContentSnapshot{}, NewExitError(ExitFilesystem, "error: cannot resolve project root: "+err.Error())
	}
	root = filepath.Clean(root)
	byName := catalogEntriesByName(entries)
	destinations := make(map[string][]byte, len(names))
	for _, name := range normalizeSkills(names) {
		entry, ok := byName[name]
		if !ok {
			return projectContentSnapshot{}, unknownLibraryEntryError(string(typ), name)
		}
		destination := projectContentPath(root, typ, entry.Name)
		if _, duplicate := destinations[destination]; duplicate {
			return projectContentSnapshot{}, NewExitError(ExitGeneral, fmt.Sprintf("error: selected %s names map to duplicate project destinations: %s", typ, destination))
		}
		data, err := os.ReadFile(filepath.Join(libraryPath, libraryTypeDir(typ), entry.Path)) //nolint:gosec // Catalog path is under the selected Library.
		if err != nil {
			return projectContentSnapshot{}, NewExitError(ExitFilesystem, "error: cannot read library content: "+err.Error())
		}
		destinations[destination] = data
	}
	before, err := snapshotProjectContentOutputs(root, typ)
	if err != nil {
		return projectContentSnapshot{}, err
	}
	return projectContentSnapshot{typ: typ, root: root, destinations: destinations, before: before}, nil
}

func snapshotProjectContentOutputs(root string, typ LibraryType) (map[string]projectContentFileSnapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot resolve project root: "+err.Error())
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, NewExitError(ExitFilesystem, "error: project content root is not a real directory: "+root)
	}
	directory := filepath.Dir(projectContentGlob(root, typ))
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, NewExitError(ExitFilesystem, "error: project content directory escapes project root")
	}
	for current := directory; current != root; current = filepath.Dir(current) {
		parent, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return nil, NewExitError(ExitFilesystem, "error: cannot inspect project content directory: "+current)
		}
		if parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir() {
			return nil, NewExitError(ExitFilesystem, "error: project content parent is not a real directory: "+current)
		}
	}
	matches, err := filepath.Glob(projectContentGlob(root, typ))
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot inspect project content: "+err.Error())
	}
	snapshot := make(map[string]projectContentFileSnapshot, len(matches))
	for _, path := range matches {
		entry, err := os.Lstat(path)
		if err != nil {
			return nil, NewExitError(ExitFilesystem, "error: cannot inspect project content: "+path)
		}
		file := projectContentFileSnapshot{mode: entry.Mode()}
		switch {
		case entry.Mode()&os.ModeSymlink != 0:
			file.target, err = os.Readlink(path)
		case entry.Mode().IsRegular():
			file.data, err = os.ReadFile(path) //nolint:gosec // path is a regular match under project root after parent symlinks are rejected.
		default:
			return nil, NewExitError(ExitFilesystem, "error: project content output is not a regular file: "+path)
		}
		if err != nil {
			return nil, NewExitError(ExitFilesystem, "error: cannot snapshot project content: "+path)
		}
		snapshot[filepath.Clean(path)] = file
	}
	return snapshot, nil
}

func equalProjectContentSnapshots(left, right map[string]projectContentFileSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for path, before := range left {
		after, exists := right[path]
		if !exists || before.mode != after.mode || before.target != after.target || !bytes.Equal(before.data, after.data) {
			return false
		}
	}
	return true
}

// publish applies captured bytes and removes every other copied file of this type.
func (s projectContentSnapshot) publish() error {
	current, err := snapshotProjectContentOutputs(s.root, s.typ)
	if err != nil {
		return err
	}
	if !equalProjectContentSnapshots(s.before, current) {
		return NewExitError(ExitFilesystem, "error: project content outputs changed during overwrite")
	}
	matches, err := filepath.Glob(projectContentGlob(s.root, s.typ))
	if err != nil {
		return NewExitError(ExitFilesystem, "error: cannot inspect project content: "+err.Error())
	}
	wanted := make(map[string]struct{}, len(s.destinations))
	for destination := range s.destinations {
		wanted[filepath.Clean(destination)] = struct{}{}
	}
	for _, path := range matches {
		if _, ok := wanted[filepath.Clean(path)]; !ok {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return NewExitError(ExitFilesystem, "error: cannot remove project content: "+err.Error())
			}
		}
	}
	for destination, data := range s.destinations {
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot create project content directory: "+err.Error())
		}
		if err := writeFileAtomic(destination, data, 0o644); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write project content: "+err.Error())
		}
	}
	return nil
}

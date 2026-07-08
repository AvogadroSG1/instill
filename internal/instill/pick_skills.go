package instill

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PickOptions configures additive and removal changes for one library type.
type PickOptions struct {
	Project     Project
	LibraryPath string
	Add         []string
	Remove      []string
	Type        LibraryType
	Runner      CommandRunner
	Stdout      io.Writer
}

// PickSkillsOptions configures manifest skill selection changes.
type PickSkillsOptions struct {
	Project     Project
	LibraryPath string
	Add         []string
	Remove      []string
	Stdout      io.Writer
}

// SkillSelectionOptions configures full manifest selection changes.
type SkillSelectionOptions struct {
	Project     Project
	LibraryPath string
	Skills      []string
	Runner      CommandRunner
	Stdout      io.Writer
}

// Pick applies additive and removal changes for a single library type.
func Pick(opts PickOptions) error {
	if len(opts.Add) == 0 && len(opts.Remove) == 0 {
		return NewExitError(ExitGeneral, "error: no items specified")
	}
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}

	manifest, err := ReadAPMManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	entries, err := LoadCatalog(opts.LibraryPath, opts.Type)
	if err != nil {
		return err
	}
	entriesByName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		entriesByName[entry.Name] = entry
	}

	switch opts.Type {
	case LibraryTypeSkill:
		manifest.Dependencies.APM, err = applySkillPick(manifest.Dependencies.APM, opts.LibraryPath, entriesByName, opts.Add, opts.Remove)
	case LibraryTypeMCP:
		manifest.Dependencies.MCP, err = applyMCPPick(manifest.Dependencies.MCP, entriesByName, opts.Add, opts.Remove)
	case LibraryTypeInstruction:
		err = applyContentPick(opts.Project.Root, opts.LibraryPath, entriesByName, opts.Add, opts.Remove, LibraryTypeInstruction)
	case LibraryTypePrompt:
		err = applyContentPick(opts.Project.Root, opts.LibraryPath, entriesByName, opts.Add, opts.Remove, LibraryTypePrompt)
	default:
		err = NewExitError(ExitGeneral, "error: invalid library type: "+string(opts.Type))
	}
	if err != nil {
		return err
	}
	if err := WriteAPMManifestAtomic(opts.Project.ManifestPath, manifest); err != nil {
		return err
	}

	added := len(normalizeSkills(opts.Add)) > 0
	removed := len(normalizeSkills(opts.Remove)) > 0
	if removed {
		if err := RunAPMPrune(opts.Runner, opts.Project.Root); err != nil {
			return err
		}
	}
	if added {
		return RunAPMInstall(opts.Runner, opts.Project.Root)
	}
	return nil
}

// PickSkills preserves the legacy skill-only entrypoint.
func PickSkills(opts PickSkillsOptions) error {
	return Pick(PickOptions{
		Project:     opts.Project,
		LibraryPath: opts.LibraryPath,
		Add:         opts.Add,
		Remove:      opts.Remove,
		Type:        LibraryTypeSkill,
		Stdout:      opts.Stdout,
	})
}

// ApplySkillSelection replaces the project skill dependency set.
func ApplySkillSelection(opts SkillSelectionOptions) error {
	manifest, err := ReadAPMManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	previous := append([]string{}, manifest.Dependencies.APM...)
	dependencies, err := resolveSkillDependencies(opts.LibraryPath, opts.Skills)
	if err != nil {
		return err
	}
	manifest.Dependencies.APM = dependencies
	if err := WriteAPMManifestAtomic(opts.Project.ManifestPath, manifest); err != nil {
		return err
	}
	if hasRemovedDependencies(previous, dependencies) {
		return RunAPMPrune(opts.Runner, opts.Project.Root)
	}
	return RunAPMInstall(opts.Runner, opts.Project.Root)
}

func hasRemovedDependencies(previous []string, next []string) bool {
	current := make(map[string]struct{}, len(next))
	for _, dependency := range next {
		current[dependency] = struct{}{}
	}
	for _, dependency := range previous {
		if _, ok := current[dependency]; !ok {
			return true
		}
	}
	return false
}

func applySkillPick(current []string, libraryPath string, entriesByName map[string]CatalogEntry, add []string, remove []string) ([]string, error) {
	byName := make(map[string]string, len(current))
	dependencyToName := make(map[string]string, len(entriesByName))
	for _, entry := range entriesByName {
		dependencyToName[filepath.Clean(skillDependencyPath(libraryPath, entry))] = entry.Name
	}
	for _, dependency := range current {
		name, ok := dependencyToName[filepath.Clean(dependency)]
		if !ok {
			name = skillDependencyName(libraryPath, dependency)
		}
		byName[name] = dependency
	}
	for _, name := range normalizeSkills(add) {
		entry, ok := entriesByName[name]
		if !ok {
			return nil, NewExitError(ExitGeneral, "error: unknown skill: "+name+" - run 'instill library show --type skill' to see available skills")
		}
		byName[name] = skillDependencyPath(libraryPath, entry)
	}
	for _, name := range normalizeSkills(remove) {
		if _, ok := byName[name]; !ok {
			if _, ok := entriesByName[name]; !ok {
				return nil, NewExitError(ExitGeneral, "error: unknown skill: "+name+" - run 'instill library show --type skill' to see available skills")
			}
		}
		delete(byName, name)
	}

	next := make([]string, 0, len(byName))
	for _, dependency := range byName {
		next = append(next, dependency)
	}
	return normalizeStringSlice(next), nil
}

func applyMCPPick(current []MCPDependency, entriesByName map[string]CatalogEntry, add []string, remove []string) ([]MCPDependency, error) {
	byName := make(map[string]MCPDependency, len(current))
	for _, dependency := range current {
		byName[dependency.Name] = dependency
	}
	for _, name := range normalizeSkills(add) {
		entry, ok := entriesByName[name]
		if !ok {
			return nil, NewExitError(ExitGeneral, "error: unknown mcp: "+name)
		}
		byName[name] = MCPDependency{
			Name:    entry.Name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
			URL:     entry.URL,
		}
	}
	for _, name := range normalizeSkills(remove) {
		if _, ok := byName[name]; !ok {
			if _, ok := entriesByName[name]; !ok {
				return nil, NewExitError(ExitGeneral, "error: unknown mcp: "+name)
			}
		}
		delete(byName, name)
	}

	next := make([]MCPDependency, 0, len(byName))
	for _, dependency := range byName {
		next = append(next, dependency)
	}
	return next, nil
}

func applyContentPick(projectRoot string, libraryPath string, entriesByName map[string]CatalogEntry, add []string, remove []string, typ LibraryType) error {
	for _, name := range normalizeSkills(add) {
		entry, ok := entriesByName[name]
		if !ok {
			return NewExitError(ExitGeneral, "error: unknown "+string(typ)+": "+name)
		}
		source := filepath.Join(libraryPath, libraryTypeDir(typ), entry.Path)
		destination := projectContentPath(projectRoot, typ, entry.Name)
		if err := copyFile(source, destination); err != nil {
			return err
		}
	}
	for _, name := range normalizeSkills(remove) {
		destination := projectContentPath(projectRoot, typ, name)
		if !projectContentExists(destination) {
			if _, ok := entriesByName[name]; !ok {
				return NewExitError(ExitGeneral, "error: unknown "+string(typ)+": "+name)
			}
		}
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return NewExitError(ExitFilesystem, "error: cannot remove project content: "+err.Error())
		}
	}
	return nil
}

func projectContentExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func resolveSkillDependencies(libraryPath string, names []string) ([]string, error) {
	entries, err := LoadCatalog(libraryPath, LibraryTypeSkill)
	if err != nil {
		return nil, err
	}
	entriesByName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		entriesByName[entry.Name] = entry
	}

	dependencies := make([]string, 0, len(names))
	for _, name := range normalizeSkills(names) {
		entry, ok := entriesByName[name]
		if !ok {
			return nil, NewExitError(ExitGeneral, "error: unknown skill: "+name+" - run 'instill library show --type skill' to see available skills")
		}
		dependencies = append(dependencies, skillDependencyPath(libraryPath, entry))
	}
	return normalizeStringSlice(dependencies), nil
}

func skillDependencyPath(libraryPath string, entry CatalogEntry) string {
	return filepath.Join(libraryPath, "skills", filepath.Dir(entry.Path))
}

func skillDependencyName(libraryPath string, dependency string) string {
	skillsRoot := filepath.Join(libraryPath, "skills")
	relative, err := filepath.Rel(skillsRoot, dependency)
	if err != nil {
		return filepath.Base(dependency)
	}
	relative = filepath.Clean(relative)
	if relative == "." || strings.HasPrefix(relative, "..") {
		return filepath.Base(dependency)
	}
	return filepath.ToSlash(relative)
}

func projectContentPath(projectRoot string, typ LibraryType, name string) string {
	switch typ {
	case LibraryTypeInstruction:
		return filepath.Join(projectRoot, ".apm", "instructions", sanitizeContentName(name)+".instructions.md")
	case LibraryTypePrompt:
		return filepath.Join(projectRoot, ".apm", "prompts", sanitizeContentName(name)+".prompt.md")
	default:
		return ""
	}
}

func sanitizeContentName(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}

func copyFile(source string, destination string) error {
	data, err := os.ReadFile(source) //nolint:gosec // Source path is resolved from the trusted library catalog.
	if err != nil {
		return NewExitError(ExitFilesystem, "error: cannot read library content: "+err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot create project content directory: "+err.Error())
	}
	if err := writeFileAtomic(destination, data, 0o644); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write project content: "+err.Error())
	}
	return nil
}

func libraryTypeDir(typ LibraryType) string {
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

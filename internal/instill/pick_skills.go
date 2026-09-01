package instill

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PickOptions configures additive and removal changes for one library type.
type PickOptions struct {
	Project         Project
	LibraryPath     string
	Add             []string
	Remove          []string
	Type            LibraryType
	Runner          CommandRunner
	Stdout          io.Writer
	manifestMetrics *manifestIOMetrics
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
	return withRootLocks(context.Background(), []string{opts.LibraryPath, opts.Project.Root}, func(ctx context.Context, held *heldLocks) error {
		return pickLocked(ctx, held, opts)
	})
}

func pickLocked(ctx context.Context, held *heldLocks, opts PickOptions) error {
	if err := held.requireContext(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, opts.Project.Root); err != nil {
		return err
	}
	skills, plugins, err := loadTypedPackageCatalogs(opts.LibraryPath)
	if err != nil {
		return err
	}
	var entries []CatalogEntry
	switch opts.Type {
	case LibraryTypeSkill:
		entries = skills
	case LibraryTypePlugin:
		entries = plugins
	default:
		entries, err = LoadCatalog(opts.LibraryPath, opts.Type)
		if err != nil {
			return err
		}
	}
	entriesByName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		entriesByName[entry.Name] = entry
	}
	if opts.Type == LibraryTypeInstruction || opts.Type == LibraryTypePrompt {
		if err := applyContentPick(opts.Project.Root, opts.LibraryPath, entriesByName, opts.Add, opts.Remove, opts.Type); err != nil {
			return err
		}
		if err := held.release(ctx, opts.LibraryPath); err != nil {
			return err
		}
		if len(normalizeSkills(opts.Add)) > 0 {
			if err := runAPMInstallLocked(ctx, held, opts.Runner, opts.Project.Root); err != nil {
				return err
			}
		}
		if len(normalizeSkills(opts.Remove)) > 0 {
			return runAPMPruneLocked(ctx, held, opts.Runner, opts.Project.Root)
		}
		return nil
	}
	document, err := loadManifestDocumentObserved(opts.Project.ManifestPath, opts.manifestMetrics)
	if err != nil {
		return err
	}
	manifest := document.projection
	previousAPMDependencies := manifest.Dependencies.APM

	switch opts.Type {
	case LibraryTypeSkill:
		manifest.Dependencies.APM, err = applySkillPick(manifest.Dependencies.APM, opts.LibraryPath, entriesByName, opts.Add, opts.Remove)
		if err == nil {
			catalogDependencies := make([]APMDependency, 0, len(entries))
			for _, entry := range entries {
				catalogDependencies = append(catalogDependencies, skillDependencyFromCatalog(opts.LibraryPath, entry))
			}
			ownership := ownershipForDependencies(catalogDependencies, []string{filepath.Join(opts.LibraryPath, "skills")})
			err = document.mutateAPM(manifest.Dependencies.APM, ownership, apmDependencyRelocations(previousAPMDependencies, opts.LibraryPath, skills, plugins))
		}
	case LibraryTypePlugin:
		manifest.Dependencies.APM, err = applyPluginPick(manifest.Dependencies.APM, opts.LibraryPath, entriesByName, opts.Add, opts.Remove)
		if err == nil {
			catalogDependencies := make([]APMDependency, 0, len(entries))
			for _, entry := range entries {
				catalogDependencies = append(catalogDependencies, pluginDependencyFromCatalog(opts.LibraryPath, entry))
			}
			ownership := ownershipForDependencies(catalogDependencies, []string{filepath.Join(opts.LibraryPath, "plugins")})
			err = document.mutateAPM(manifest.Dependencies.APM, ownership, apmDependencyRelocations(previousAPMDependencies, opts.LibraryPath, skills, plugins))
		}
	case LibraryTypeMCP:
		manifest.Dependencies.MCP, err = applyMCPPick(manifest.Dependencies.MCP, entriesByName, opts.Add, opts.Remove)
		if err == nil {
			err = document.mutateMCP(manifest.Dependencies.MCP, catalogEntryNamesSet(entries))
		}
	default:
		err = NewExitError(ExitGeneral, "error: invalid library type: "+string(opts.Type))
	}
	if err != nil {
		return err
	}
	if err := document.repairIdentity(opts.Project.Root, false); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}
	if err := held.release(ctx, opts.LibraryPath); err != nil {
		return err
	}

	added := len(normalizeSkills(opts.Add)) > 0
	removed := len(normalizeSkills(opts.Remove)) > 0
	if added {
		if err := runAPMInstallLocked(ctx, held, opts.Runner, opts.Project.Root); err != nil {
			return err
		}
	}
	if removed {
		return runAPMPruneLocked(ctx, held, opts.Runner, opts.Project.Root)
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
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	return withRootLocks(context.Background(), []string{opts.LibraryPath, opts.Project.Root}, func(ctx context.Context, held *heldLocks) error {
		return applySkillSelectionLocked(ctx, held, opts)
	})
}

func applySkillSelectionLocked(ctx context.Context, held *heldLocks, opts SkillSelectionOptions) error {
	if err := held.requireContext(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, opts.Project.Root); err != nil {
		return err
	}
	skills, _, err := loadTypedPackageCatalogs(opts.LibraryPath)
	if err != nil {
		return err
	}
	document, err := loadManifestDocument(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	manifest := document.projection
	previous := append([]APMDependency{}, manifest.Dependencies.APM...)
	entriesByName := make(map[string]CatalogEntry, len(skills))
	for _, entry := range skills {
		entriesByName[entry.Name] = entry
	}
	selected := make(map[string]struct{}, len(opts.Skills))
	for _, name := range normalizeSkills(opts.Skills) {
		selected[name] = struct{}{}
	}
	currentNames := ownedDependencyNames(previous, opts.LibraryPath, LibraryTypeSkill, skills)
	var remove []string
	for _, name := range currentNames {
		if _, ok := selected[name]; !ok {
			remove = append(remove, name)
		}
	}
	dependencies, err := applySkillPick(previous, opts.LibraryPath, entriesByName, opts.Skills, remove)
	if err != nil {
		return err
	}
	manifest.Dependencies.APM = dependencies
	catalogDependencies := make([]APMDependency, 0, len(skills))
	for _, entry := range skills {
		catalogDependencies = append(catalogDependencies, skillDependencyFromCatalog(opts.LibraryPath, entry))
	}
	ownership := ownershipForDependencies(catalogDependencies, []string{filepath.Join(opts.LibraryPath, "skills")})
	if err := document.mutateAPM(manifest.Dependencies.APM, ownership, apmDependencyRelocations(previous, opts.LibraryPath, skills, nil)); err != nil {
		return err
	}
	if err := document.repairIdentity(opts.Project.Root, false); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}
	if err := held.release(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if hasAddedDependencies(previous, dependencies) {
		if err := runAPMInstallLocked(ctx, held, opts.Runner, opts.Project.Root); err != nil {
			return err
		}
	}
	if hasRemovedDependencies(previous, dependencies) {
		return runAPMPruneLocked(ctx, held, opts.Runner, opts.Project.Root)
	}
	return nil
}

func hasAddedDependencies(previous []APMDependency, next []APMDependency) bool {
	return hasRemovedDependencies(next, previous)
}

func hasRemovedDependencies(previous []APMDependency, next []APMDependency) bool {
	current := make(map[string]struct{}, len(next))
	for _, dependency := range next {
		current[dependency.identity()] = struct{}{}
	}
	for _, dependency := range previous {
		if _, ok := current[dependency.identity()]; !ok {
			return true
		}
	}
	return false
}

func applySkillPick(current []APMDependency, libraryPath string, entriesByName map[string]CatalogEntry, add []string, remove []string) ([]APMDependency, error) {
	return applyTypedAPMPick(
		current, libraryPath, LibraryTypeSkill, entriesByName, add, remove,
		func(entry CatalogEntry) APMDependency { return skillDependencyFromCatalog(libraryPath, entry) },
		func(name string) (CatalogEntry, bool) {
			return resolveCatalogEntryName(entriesByName, name, true)
		},
		func(dependency APMDependency) string {
			if dependency.Git != nil {
				return dependency.identity()
			}
			return skillDependencyName(libraryPath, dependency.Local)
		},
	)
}

func applyPluginPick(current []APMDependency, libraryPath string, entriesByName map[string]CatalogEntry, add []string, remove []string) ([]APMDependency, error) {
	return applyTypedAPMPick(
		current, libraryPath, LibraryTypePlugin, entriesByName, add, remove,
		func(entry CatalogEntry) APMDependency { return pluginDependencyFromCatalog(libraryPath, entry) },
		func(name string) (CatalogEntry, bool) {
			return resolveCatalogEntryName(entriesByName, name, false)
		},
		func(dependency APMDependency) string {
			if dependency.Git != nil {
				return dependency.identity()
			}
			return pluginDependencyName(libraryPath, dependency.Local)
		},
	)
}

func applyTypedAPMPick(
	current []APMDependency,
	libraryPath string,
	typ LibraryType,
	entriesByName map[string]CatalogEntry,
	add, remove []string,
	dependencyFromCatalog func(CatalogEntry) APMDependency,
	entryForName func(string) (CatalogEntry, bool),
	unownedSortName func(APMDependency) string,
) ([]APMDependency, error) {
	dependencyToName := make(map[string]string, len(entriesByName))
	catalog := make([]CatalogEntry, 0, len(entriesByName))
	for _, entry := range entriesByName {
		dependencyToName[dependencyFromCatalog(entry).stableIdentity()] = entry.Name
		catalog = append(catalog, entry)
	}

	owned := make(map[string]APMDependency, len(entriesByName))
	passthrough := make([]APMDependency, 0, len(current))
	for _, dependency := range current {
		if name, ok := dependencyToName[dependency.stableIdentity()]; ok {
			owned[name] = dependency
			continue
		}
		if dependency.Git == nil {
			entry, ok := matchCatalogEntryForLocalDependency(libraryPath, typ, dependency.Local, catalog)
			if ok {
				owned[entry.Name] = dependencyFromCatalog(entry)
				continue
			}
			if isLocalDependencyForType(libraryPath, typ, dependency.Local) {
				owned[localDependencyName(libraryPath, typ, dependency.Local)] = dependency
				continue
			}
		}
		passthrough = append(passthrough, dependency)
	}
	for _, name := range normalizeSkills(add) {
		entry, ok := entryForName(name)
		if !ok {
			return nil, unknownLibraryEntryError(string(typ), name)
		}
		owned[entry.Name] = dependencyFromCatalog(entry)
	}
	for _, name := range normalizeSkills(remove) {
		entry, ok := entryForName(name)
		if !ok {
			if _, stale := owned[name]; stale {
				delete(owned, name)
				continue
			}
			return nil, unknownLibraryEntryError(string(typ), name)
		}
		delete(owned, entry.Name)
	}

	type keyedDependency struct {
		key        string
		dependency APMDependency
	}
	keyed := make([]keyedDependency, 0, len(owned)+len(passthrough))
	for name, dependency := range owned {
		keyed = append(keyed, keyedDependency{key: name, dependency: dependency})
	}
	for _, dependency := range passthrough {
		keyed = append(keyed, keyedDependency{key: unownedSortName(dependency), dependency: dependency})
	}
	sort.Slice(keyed, func(i, j int) bool {
		if keyed[i].key == keyed[j].key {
			return keyed[i].dependency.identity() < keyed[j].dependency.identity()
		}
		return keyed[i].key < keyed[j].key
	})
	next := make([]APMDependency, 0, len(keyed))
	for _, item := range keyed {
		next = append(next, item.dependency)
	}
	return normalizeAPMDependencies(next), nil
}

func resolveCatalogEntryName(entriesByName map[string]CatalogEntry, name string, allowSuffix bool) (CatalogEntry, bool) {
	if entry, ok := entriesByName[name]; ok {
		return entry, true
	}
	if !allowSuffix {
		return CatalogEntry{}, false
	}

	name = strings.Trim(filepath.ToSlash(filepath.Clean(name)), "/")
	if name == "." || name == "" {
		return CatalogEntry{}, false
	}
	suffix := "/" + name
	var matched CatalogEntry
	count := 0
	for _, entry := range entriesByName {
		if strings.HasSuffix(entry.Name, suffix) {
			matched = entry
			count++
		}
	}
	return matched, count == 1
}

func isLocalDependencyForType(libraryPath string, typ LibraryType, dependency string) bool {
	switch typ {
	case LibraryTypeSkill:
		return isUnderDir(filepath.Join(libraryPath, "skills"), dependency)
	case LibraryTypePlugin:
		return isUnderDir(filepath.Join(libraryPath, "plugins"), dependency)
	default:
		return false
	}
}

func localDependencyName(libraryPath string, typ LibraryType, dependency string) string {
	switch typ {
	case LibraryTypeSkill:
		return skillDependencyName(libraryPath, dependency)
	case LibraryTypePlugin:
		return pluginDependencyName(libraryPath, dependency)
	default:
		return dependency
	}
}

func unknownLibraryEntryError(typ, name string) error {
	return NewExitError(ExitGeneral, "error: unknown "+typ+": "+name+" - run 'instill library show --type "+typ+"' to see available "+typ+"s")
}

func pluginDependencyPath(libraryPath string, entry CatalogEntry) string {
	pluginDir := filepath.Dir(entry.Path)
	if filepath.Base(pluginDir) == ".claude-plugin" || filepath.Base(pluginDir) == ".codex-plugin" {
		pluginDir = filepath.Dir(pluginDir)
	}
	return filepath.Join(libraryPath, "plugins", filepath.FromSlash(pluginDir))
}

func pluginDependencyName(libraryPath string, dependency string) string {
	pluginsRoot := filepath.Join(libraryPath, "plugins")
	relative, err := filepath.Rel(pluginsRoot, dependency)
	if err != nil {
		return filepath.Base(dependency)
	}
	relative = filepath.Clean(relative)
	if relative == "." || strings.HasPrefix(relative, "..") {
		return filepath.Base(dependency)
	}
	return filepath.ToSlash(relative)
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
		byName[name] = mcpDependencyFromCatalog(entry)
	}
	for _, name := range normalizeSkills(remove) {
		if _, ok := entriesByName[name]; !ok {
			return nil, NewExitError(ExitGeneral, "error: unknown mcp: "+name)
		}
		delete(byName, name)
	}

	next := make([]MCPDependency, 0, len(byName))
	for _, dependency := range byName {
		next = append(next, dependency)
	}
	return next, nil
}

func mcpDependencyFromCatalog(entry CatalogEntry) MCPDependency {
	return MCPDependency{
		Name: entry.Name, Transport: entry.Transport, Registry: false,
		Command: entry.Command, Args: entry.Args, Env: mcpEnvironment(entry.Env), URL: entry.URL,
	}
}

func catalogEntryNamesSet(entries []CatalogEntry) map[string]struct{} {
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[entry.Name] = struct{}{}
	}
	return names
}

func mcpEnvironment(entries []string) map[string]string {
	environment := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			environment[key] = value
		}
	}
	return environment
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

func resolveSkillDependencies(libraryPath string, names []string) ([]APMDependency, error) {
	entries, err := LoadCatalog(libraryPath, LibraryTypeSkill)
	if err != nil {
		return nil, err
	}
	entriesByName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		entriesByName[entry.Name] = entry
	}

	dependencies := make([]APMDependency, 0, len(names))
	for _, name := range normalizeSkills(names) {
		entry, ok := resolveCatalogEntryName(entriesByName, name, true)
		if !ok {
			return nil, NewExitError(ExitGeneral, "error: unknown skill: "+name+" - run 'instill library show --type skill' to see available skills")
		}
		dependencies = append(dependencies, skillDependencyFromCatalog(libraryPath, entry))
	}
	return normalizeAPMDependencies(dependencies), nil
}

func skillDependencyFromCatalog(libraryPath string, entry CatalogEntry) APMDependency {
	if entry.Source == "git" {
		return APMDependency{Git: &GitDependency{Repository: entry.Repository, Path: remoteSkillPath(entry), Ref: entry.Ref}}
	}
	return APMDependency{Local: skillDependencyPath(libraryPath, entry)}
}

func pluginDependencyFromCatalog(libraryPath string, entry CatalogEntry) APMDependency {
	if entry.Source == "git" {
		return APMDependency{Git: &GitDependency{Repository: entry.Repository, Path: entry.Path, Ref: entry.Ref}}
	}
	return APMDependency{Local: pluginDependencyPath(libraryPath, entry)}
}

func loadTypedPackageCatalogs(libraryPath string) ([]CatalogEntry, []CatalogEntry, error) {
	skills, err := LoadCatalog(libraryPath, LibraryTypeSkill)
	if err != nil {
		return nil, nil, err
	}
	plugins, err := LoadCatalog(libraryPath, LibraryTypePlugin)
	if err != nil {
		return nil, nil, err
	}
	if err := validateTypedGitCatalogs(skills, plugins); err != nil {
		return nil, nil, err
	}
	return skills, plugins, nil
}

func ownedDependencyNames(dependencies []APMDependency, libraryPath string, typ LibraryType, entries []CatalogEntry) []string {
	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		dependency := skillDependencyFromCatalog(libraryPath, entry)
		if typ == LibraryTypePlugin {
			dependency = pluginDependencyFromCatalog(libraryPath, entry)
		}
		names[dependency.stableIdentity()] = entry.Name
	}
	var owned []string
	for _, dependency := range dependencies {
		if name, ok := names[dependency.stableIdentity()]; ok {
			owned = append(owned, name)
			continue
		}
		if dependency.Git == nil {
			if entry, ok := matchCatalogEntryForLocalDependency(libraryPath, typ, dependency.Local, entries); ok {
				owned = append(owned, entry.Name)
				continue
			}
			if typ == LibraryTypeSkill && isLocalDependencyForType(libraryPath, typ, dependency.Local) {
				owned = append(owned, skillDependencyName(libraryPath, dependency.Local))
			}
		}
	}
	return normalizeStringSlice(owned)
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
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
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
	case LibraryTypePlugin:
		return "plugins"
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

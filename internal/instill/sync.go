package instill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

type SyncOptions struct {
	Project         Project
	LibraryPath     string
	Runner          CommandRunner
	Stdout          io.Writer
	manifestMetrics *manifestIOMetrics
}

type StatusOptions struct {
	Project     Project
	LibraryPath string
	Runner      CommandRunner
	Stdout      io.Writer
}

func SyncProject(opts SyncOptions) error {
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	return withRootLocks(context.Background(), []string{opts.LibraryPath, opts.Project.Root}, func(ctx context.Context, held *heldLocks) error {
		return syncProjectLocked(ctx, held, opts)
	})
}

func syncProjectLocked(ctx context.Context, held *heldLocks, opts SyncOptions) error {
	if err := held.requireContext(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, opts.Project.Root); err != nil {
		return err
	}
	document, err := loadManifestDocumentObserved(opts.Project.ManifestPath, opts.manifestMetrics)
	if err != nil {
		return err
	}
	manifest := document.projection
	if err := document.setTargets(DetectHarnessTargets(opts.Project.Root), true); err != nil {
		return err
	}
	skillCatalog, pluginCatalog, err := loadTypedPackageCatalogs(opts.LibraryPath)
	if err != nil {
		return err
	}
	catalogDependencies := make([]APMDependency, 0, len(skillCatalog)+len(pluginCatalog))
	for _, entry := range skillCatalog {
		catalogDependencies = append(catalogDependencies, skillDependencyFromCatalog(opts.LibraryPath, entry))
	}
	for _, entry := range pluginCatalog {
		catalogDependencies = append(catalogDependencies, pluginDependencyFromCatalog(opts.LibraryPath, entry))
	}
	ownership := ownershipForDependencies(catalogDependencies, []string{
		filepath.Join(opts.LibraryPath, "skills"),
		filepath.Join(opts.LibraryPath, "plugins"),
	})
	if err := document.mutateAPM(manifest.Dependencies.APM, ownership); err != nil {
		return err
	}
	mcpCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	dependencies, changed := reconcileMCPDependencies(manifest.Dependencies.MCP, mcpCatalog)
	if changed {
		manifest.Dependencies.MCP = dependencies
	}
	if err := document.mutateMCP(dependencies, catalogEntryNamesSet(mcpCatalog)); err != nil {
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
	if err := runAPMInstallLocked(ctx, held, opts.Runner, opts.Project.Root); err != nil {
		return err
	}
	if err := runAPMCompileLocked(ctx, held, opts.Runner, opts.Project.Root); err != nil {
		return err
	}

	instructions, err := countProjectContent(filepath.Join(opts.Project.Root, ".apm", "instructions"), "*.instructions.md")
	if err != nil {
		return err
	}
	prompts, err := countProjectContent(filepath.Join(opts.Project.Root, ".apm", "prompts"), "*.prompt.md")
	if err != nil {
		return err
	}
	return writeLine(opts.Stdout, fmt.Sprintf(
		"ok: synced %d skills, %d plugins, %d mcp servers, %d instructions, %d prompts",
		len(ownedDependencyNames(manifest.Dependencies.APM, opts.LibraryPath, LibraryTypeSkill, skillCatalog)),
		len(ownedDependencyNames(manifest.Dependencies.APM, opts.LibraryPath, LibraryTypePlugin, pluginCatalog)),
		len(manifest.Dependencies.MCP),
		instructions,
		prompts,
	))
}

func reconcileMCPDependencies(current []MCPDependency, catalog []CatalogEntry) ([]MCPDependency, bool) {
	byName := make(map[string]CatalogEntry, len(catalog))
	for _, entry := range catalog {
		byName[entry.Name] = entry
	}
	next := append([]MCPDependency{}, current...)
	changed := false
	for i, dependency := range next {
		entry, ok := byName[dependency.Name]
		if !ok {
			continue
		}
		reconciled := mcpDependencyFromCatalog(entry)
		if reflect.DeepEqual(dependency, reconciled) {
			continue
		}
		next[i] = reconciled
		changed = true
	}
	return next, changed
}

func ProjectStatus(opts StatusOptions) error {
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	manifest, err := ReadAPMManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	skillCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeSkill)
	if err != nil {
		return err
	}
	pluginCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypePlugin)
	if err != nil {
		return err
	}
	if err := validateTypedGitCatalogs(skillCatalog, pluginCatalog); err != nil {
		return err
	}
	if err := reportSkillStatus(opts.Stdout, opts.LibraryPath, manifest.Dependencies.APM, skillCatalog); err != nil {
		return err
	}
	if err := reportPluginStatus(opts.Stdout, opts.LibraryPath, manifest.Dependencies.APM, pluginCatalog); err != nil {
		return err
	}
	mcpCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	if err := reportMCPStatus(opts.Stdout, manifest.Dependencies.MCP, mcpCatalog); err != nil {
		return err
	}

	lock, err := readAPMLock(filepath.Join(opts.Project.Root, "apm.lock.yaml"))
	if err != nil {
		return err
	}
	instructionCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeInstruction)
	if err != nil {
		return err
	}
	if err := reportContentStatus(opts.Stdout, opts.Project.Root, opts.LibraryPath, LibraryTypeInstruction, instructionCatalog, lock.Instructions); err != nil {
		return err
	}
	promptCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypePrompt)
	if err != nil {
		return err
	}
	return reportContentStatus(opts.Stdout, opts.Project.Root, opts.LibraryPath, LibraryTypePrompt, promptCatalog, lock.Prompts)
}

func countProjectContent(dir string, pattern string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return 0, NewExitError(ExitFilesystem, "error: cannot inspect project content: "+err.Error())
	}
	return len(matches), nil
}

type apmLock struct {
	Instructions []apmLockEntry `yaml:"instructions"`
	Prompts      []apmLockEntry `yaml:"prompts"`
}

type apmLockEntry struct {
	Name   string `yaml:"name"`
	SHA256 string `yaml:"sha256"`
}

func readAPMLock(path string) (apmLock, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Lock path is resolved inside the selected project.
	if err != nil {
		if os.IsNotExist(err) {
			return apmLock{}, nil
		}
		return apmLock{}, NewExitError(ExitFilesystem, "error: cannot read lockfile: "+err.Error())
	}
	var lock apmLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return apmLock{}, nil
	}
	return lock, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Hashing source files from the selected library is intentional.
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", NewExitError(ExitFilesystem, "error: cannot hash file: "+err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func reportSkillStatus(stdout io.Writer, libraryPath string, dependencies []APMDependency, catalog []CatalogEntry) error {
	librarySkills := make(map[string]CatalogEntry, len(catalog))
	dependencyToName := make(map[string]string, len(catalog))
	for _, entry := range catalog {
		librarySkills[entry.Name] = entry
		dependencyToName[skillDependencyFromCatalog(libraryPath, entry).stableIdentity()] = entry.Name
	}

	pluginsRoot := filepath.Join(libraryPath, "plugins")
	projectSkills := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		key := dependency.stableIdentity()
		name, ok := dependencyToName[key]
		if !ok {
			if dependency.Git != nil || isUnderDir(pluginsRoot, dependency.Local) {
				continue
			}
			name = skillDependencyName(libraryPath, dependency.Local)
		}
		projectSkills[name] = struct{}{}
		entry, inCatalog := librarySkills[name]
		if inCatalog && dependency.Git != nil && dependency.identity() != skillDependencyFromCatalog(libraryPath, entry).identity() {
			if err := writeLine(stdout, "update available: skill "+name); err != nil {
				return err
			}
		}
		if !inCatalog {
			if err := writeLine(stdout, "removed from library: skill "+name); err != nil {
				return err
			}
		}
	}
	for _, entry := range catalog {
		if _, ok := projectSkills[entry.Name]; ok {
			continue
		}
		if err := writeLine(stdout, "available in library: skill "+entry.Name); err != nil {
			return err
		}
	}
	return nil
}

func reportPluginStatus(stdout io.Writer, libraryPath string, dependencies []APMDependency, catalog []CatalogEntry) error {
	if len(catalog) == 0 {
		return nil
	}
	libraryPlugins := make(map[string]CatalogEntry, len(catalog))
	dependencyToName := make(map[string]string, len(catalog))
	for _, entry := range catalog {
		libraryPlugins[entry.Name] = entry
		dependencyToName[pluginDependencyFromCatalog(libraryPath, entry).stableIdentity()] = entry.Name
	}

	pluginsRoot := filepath.Join(libraryPath, "plugins")
	projectPlugins := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		name, ok := dependencyToName[dependency.stableIdentity()]
		if !ok {
			if dependency.Git != nil {
				continue
			}
			if !isUnderDir(pluginsRoot, dependency.Local) {
				continue
			}
			name = pluginDependencyName(libraryPath, dependency.Local)
		}
		projectPlugins[name] = struct{}{}
		entry, inCatalog := libraryPlugins[name]
		if inCatalog && dependency.Git != nil && dependency.identity() != pluginDependencyFromCatalog(libraryPath, entry).identity() {
			if err := writeLine(stdout, "update available: plugin "+name); err != nil {
				return err
			}
		}
		if !inCatalog {
			if err := writeLine(stdout, "removed from library: plugin "+name); err != nil {
				return err
			}
		}
	}
	for _, entry := range catalog {
		if _, ok := projectPlugins[entry.Name]; ok {
			continue
		}
		if err := writeLine(stdout, "available in library: plugin "+entry.Name); err != nil {
			return err
		}
	}
	return nil
}

func isUnderDir(parent string, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != "." && !strings.HasPrefix(rel, "..")
}

func reportMCPStatus(stdout io.Writer, dependencies []MCPDependency, catalog []CatalogEntry) error {
	libraryMCP := make(map[string]CatalogEntry, len(catalog))
	for _, entry := range catalog {
		libraryMCP[entry.Name] = entry
	}

	projectMCP := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		projectMCP[dependency.Name] = struct{}{}
		if _, ok := libraryMCP[dependency.Name]; !ok {
			if err := writeLine(stdout, "removed from library: mcp "+dependency.Name); err != nil {
				return err
			}
		}
	}
	for _, entry := range catalog {
		if _, ok := projectMCP[entry.Name]; ok {
			continue
		}
		if err := writeLine(stdout, "available in library: mcp "+entry.Name); err != nil {
			return err
		}
	}
	return nil
}

func reportContentStatus(stdout io.Writer, projectRoot string, libraryPath string, typ LibraryType, catalog []CatalogEntry, lockEntries []apmLockEntry) error {
	catalogByName := make(map[string]CatalogEntry, len(catalog))
	catalogBySanitizedName := make(map[string]string, len(catalog))
	for _, entry := range catalog {
		catalogByName[entry.Name] = entry
		catalogBySanitizedName[sanitizeContentName(entry.Name)] = entry.Name
	}

	projectContent, err := listProjectContentNames(projectRoot, typ, catalogBySanitizedName)
	if err != nil {
		return err
	}

	installedNames := make(map[string]struct{}, len(projectContent))
	for _, name := range projectContent {
		installedNames[name] = struct{}{}
		if _, ok := catalogByName[name]; !ok {
			if err := writeLine(stdout, "removed from library: "+string(typ)+" "+name); err != nil {
				return err
			}
		}
	}

	for _, entry := range catalog {
		if _, ok := installedNames[entry.Name]; ok {
			continue
		}
		if err := writeLine(stdout, "available in library: "+string(typ)+" "+entry.Name); err != nil {
			return err
		}
	}

	lockByName := make(map[string]apmLockEntry, len(lockEntries))
	for _, entry := range lockEntries {
		lockByName[entry.Name] = entry
	}
	for name := range installedNames {
		lockEntry, ok := lockByName[name]
		if !ok {
			continue
		}
		catalogEntry, ok := catalogByName[name]
		if !ok {
			continue
		}
		currentHash, hashErr := fileSHA256(filepath.Join(libraryPath, libraryTypeDir(typ), catalogEntry.Path))
		if hashErr != nil {
			return hashErr
		}
		if currentHash != "" && lockEntry.SHA256 != "" && currentHash != lockEntry.SHA256 {
			if err := writeLine(stdout, "hash mismatch: "+string(typ)+" "+name); err != nil {
				return err
			}
		}
	}
	return nil
}

func listProjectContentNames(projectRoot string, typ LibraryType, catalogBySanitizedName map[string]string) ([]string, error) {
	pattern := projectContentGlob(projectRoot, typ)
	if pattern == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot inspect project content: "+err.Error())
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		base := filepath.Base(match)
		suffix := projectContentSuffix(typ)
		if !strings.HasSuffix(base, suffix) {
			continue
		}
		sanitizedName := strings.TrimSuffix(base, suffix)
		if name, ok := catalogBySanitizedName[sanitizedName]; ok {
			names = append(names, name)
			continue
		}
		names = append(names, sanitizedName)
	}
	return names, nil
}

func projectContentGlob(projectRoot string, typ LibraryType) string {
	switch typ {
	case LibraryTypeInstruction:
		return filepath.Join(projectRoot, ".apm", "instructions", "*.instructions.md")
	case LibraryTypePrompt:
		return filepath.Join(projectRoot, ".apm", "prompts", "*.prompt.md")
	default:
		return ""
	}
}

func projectContentSuffix(typ LibraryType) string {
	switch typ {
	case LibraryTypeInstruction:
		return ".instructions.md"
	case LibraryTypePrompt:
		return ".prompt.md"
	default:
		return ""
	}
}

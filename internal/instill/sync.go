package instill

import (
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
	Project     Project
	LibraryPath string
	Runner      CommandRunner
	Stdout      io.Writer
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
	manifest, err := ReadAPMManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}
	if err := ensureTargets(opts.Project, &manifest); err != nil {
		return err
	}
	mcpCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	dependencies, changed := reconcileMCPDependencies(manifest.Dependencies.MCP, mcpCatalog)
	if changed {
		manifest.Dependencies.MCP = dependencies
		if err := WriteAPMManifestAtomic(opts.Project.ManifestPath, manifest); err != nil {
			return err
		}
	}
	if err := RunAPMInstall(opts.Runner, opts.Project.Root); err != nil {
		return err
	}
	if err := RunAPMCompile(opts.Runner, opts.Project.Root); err != nil {
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
		"ok: synced %d skills, %d mcp servers, %d instructions, %d prompts",
		len(manifest.Dependencies.APM),
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
	if err := reportSkillStatus(opts.Stdout, opts.LibraryPath, manifest.Dependencies.APM, skillCatalog); err != nil {
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

func ensureTargets(project Project, manifest *APMManifest) error {
	if len(manifest.Targets) > 0 {
		return nil
	}
	targets := DetectHarnessTargets(project.Root)
	if len(targets) == 0 {
		return nil
	}
	manifest.Targets = targets
	return WriteAPMManifestAtomic(project.ManifestPath, *manifest)
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

func reportSkillStatus(stdout io.Writer, libraryPath string, dependencies []string, catalog []CatalogEntry) error {
	librarySkills := make(map[string]CatalogEntry, len(catalog))
	dependencyToName := make(map[string]string, len(catalog))
	for _, entry := range catalog {
		librarySkills[entry.Name] = entry
		dependencyToName[filepath.Clean(skillDependencyPath(libraryPath, entry))] = entry.Name
	}

	projectSkills := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		name, ok := dependencyToName[filepath.Clean(dependency)]
		if !ok {
			name = skillDependencyName(libraryPath, dependency)
		}
		projectSkills[name] = struct{}{}
		if _, ok := librarySkills[name]; !ok {
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

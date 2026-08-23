package instill

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// InitProjectOptions configures project initialization.
type InitProjectOptions struct {
	Root          string
	LibraryPath   string
	Skills        []string
	Targets       []string
	TargetsSet    bool
	Force         bool
	Runner        CommandRunner
	Stdout        io.Writer
	SelectTargets func(detected []string) ([]string, error)
	SelectSkills  func() (InitialSkillSelectionPlan, bool, error)
}

// InitialSkillSelectionPlan describes the manifest-owned result of init selection.
type InitialSkillSelectionPlan struct {
	Skills []string
}

// InitProject initializes apm.yml for the selected project.
func InitProject(opts InitProjectOptions) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}
	project := Project{
		Root:             root,
		ManifestPath:     ProjectAPMPath(root),
		SymlinkDir:       filepath.Join(root, claudeDirName, skillsDirName),
		AgentsSymlinkDir: filepath.Join(root, agentsDirName, skillsDirName),
	}

	manifest := APMManifest{Name: filepath.Base(root), Version: "0.1.0"}
	targetsSelected := opts.TargetsSet || len(opts.Targets) > 0
	if opts.TargetsSet || len(opts.Targets) > 0 {
		manifest.Targets = normalizeStringSlice(opts.Targets)
	} else if opts.SelectTargets != nil {
		selected, err := opts.SelectTargets(DetectHarnessTargets(root))
		if err != nil {
			return err
		}
		manifest.Targets = normalizeStringSlice(selected)
		targetsSelected = true
	}

	skillNames := normalizeSkills(opts.Skills)
	if len(opts.Skills) == 0 && opts.SelectSkills != nil {
		plan, confirmed, selectErr := opts.SelectSkills()
		if selectErr != nil {
			return selectErr
		}
		if !confirmed {
			return NewExitError(ExitGeneral, "initialization cancelled")
		}
		skillNames = normalizeSkills(plan.Skills)
	}

	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	return withRootLocks(context.Background(), []string{opts.LibraryPath, root}, func(ctx context.Context, held *heldLocks) error {
		return initProjectLocked(ctx, held, opts, project, manifest, skillNames, targetsSelected)
	})
}

func initProjectLocked(
	ctx context.Context,
	held *heldLocks,
	opts InitProjectOptions,
	project Project,
	manifest APMManifest,
	skillNames []string,
	targetsSelected bool,
) error {
	if err := held.requireContext(ctx, opts.LibraryPath); err != nil {
		return err
	}
	if err := held.requireContext(ctx, project.Root); err != nil {
		return err
	}
	if !opts.Force {
		_, err := os.Stat(project.ManifestPath)
		if err == nil {
			return NewExitError(ExitGeneral, "error: manifest already exists; use --force to reinitialize")
		}
		if !os.IsNotExist(err) {
			return NewExitError(ExitFilesystem, "error: cannot inspect manifest: "+err.Error())
		}
	}
	if !targetsSelected {
		manifest.Targets = DetectHarnessTargets(project.Root)
	}
	dependencies, err := resolveSkillDependencies(opts.LibraryPath, skillNames)
	if err != nil {
		return err
	}
	manifest.Dependencies.APM = dependencies
	document, err := loadManifestDocument(project.ManifestPath)
	if err != nil {
		return err
	}
	skills, plugins, err := loadTypedPackageCatalogs(opts.LibraryPath)
	if err != nil {
		return err
	}
	catalogDependencies := make([]APMDependency, 0, len(skills)+len(plugins))
	for _, entry := range skills {
		catalogDependencies = append(catalogDependencies, skillDependencyFromCatalog(opts.LibraryPath, entry))
	}
	for _, entry := range plugins {
		catalogDependencies = append(catalogDependencies, pluginDependencyFromCatalog(opts.LibraryPath, entry))
	}
	ownership := ownershipForDependencies(catalogDependencies, []string{
		filepath.Join(opts.LibraryPath, "skills"),
		filepath.Join(opts.LibraryPath, "plugins"),
	})
	if _, err := document.dependencySequence("apm", true); err != nil {
		return err
	}
	if err := document.mutateAPM(manifest.Dependencies.APM, ownership); err != nil {
		return err
	}
	mcpCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	if _, err := document.dependencySequence("mcp", true); err != nil {
		return err
	}
	if err := document.mutateMCP([]MCPDependency{}, catalogEntryNamesSet(mcpCatalog)); err != nil {
		return err
	}
	if err := document.setTargets(manifest.Targets, false); err != nil {
		return err
	}
	if err := document.repairIdentity(project.Root, opts.Force); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}
	if err := held.release(ctx, opts.LibraryPath); err != nil {
		return err
	}

	if len(manifest.Dependencies.APM) == 0 {
		return nil
	}
	return runAPMInstallLocked(ctx, held, opts.Runner, project.Root)
}

func HasAPMManifest(root string) bool {
	_, err := ReadAPMManifest(ProjectAPMPath(root))
	return err == nil
}

package instill

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDistinctCatalogAddsPreserveBothEntries(t *testing.T) {
	root := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "base", Path: "base/SKILL.md"}},
	})
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, entry := range []CatalogEntry{
		{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"},
		{Type: LibraryTypeSkill, Name: "two", Path: "two/SKILL.md"},
	} {
		go func(entry CatalogEntry) {
			<-start
			errs <- AddCatalogEntry(root, entry)
		}(entry)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("AddCatalogEntry() error = %v", err)
		}
	}

	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	if got, want := catalogEntryNames(entries), []string{"base", "one", "two"}; !equalStringSlices(got, want) {
		t.Fatalf("catalog names = %v, want %v", got, want)
	}
}

func TestConcurrentCrossCatalogRegistrationAllowsOneOwner(t *testing.T) {
	root := t.TempDir()
	skillRunner := remoteRepoSkillRunner(remoteSkillSHA)
	pluginRunner := remotePluginRunner(
		t,
		remotePluginSHA,
		`{"plugins":[{"name":"plugin","source":"skills/repo"}]}`,
		`{"name":"plugin"}`,
	)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- AddRemoteSkill(context.Background(), root, "owner/repo", skillRunner)
	}()
	go func() {
		<-start
		errs <- AddRemotePlugin(context.Background(), root, "owner/repo", "plugin", pluginRunner)
	}()
	close(start)
	firstErr := <-errs
	secondErr := <-errs
	if (firstErr == nil) == (secondErr == nil) {
		t.Fatalf("registration errors = (%v, %v), want exactly one success", firstErr, secondErr)
	}
	skills, plugins, err := loadTypedPackageCatalogs(root)
	requireNoError(t, err)
	if len(skills)+len(plugins) != 1 {
		t.Fatalf("catalog owners = %d skills + %d plugins, want one", len(skills), len(plugins))
	}
}

func TestConcurrentAdditivePicksPreserveBothDependencies(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{
			{Type: LibraryTypeSkill, Name: "base", Path: "base/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "two", Path: "two/SKILL.md"},
		},
	})
	project := createAPMProject(t, APMManifest{
		Name:    "project",
		Version: "0.1.0",
		Dependencies: APMDependencies{APM: []APMDependency{{
			Local: filepath.Join(library, "skills", "base"),
		}}},
	})

	runConcurrentMutations(t,
		func() error { return Pick(concurrentPickOptions(project, library, "one")) },
		func() error { return Pick(concurrentPickOptions(project, library, "two")) },
	)

	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	want := localDependencies(
		filepath.Join(library, "skills", "base"),
		filepath.Join(library, "skills", "one"),
		filepath.Join(library, "skills", "two"),
	)
	assertDependencySet(t, manifest.Dependencies.APM, want)
}

func TestConcurrentPickAndTargetsPreserveBothChanges(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})

	runConcurrentMutations(t,
		func() error { return Pick(concurrentPickOptions(project, library, "one")) },
		func() error {
			return SetProjectTargets(SetTargetsOptions{Project: project, Targets: []string{"claude"}})
		},
	)

	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []string{"claude"}, manifest.Targets)
	requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
}

func TestConcurrentSyncAndPickCompleteInSerializableOrder(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0", Targets: []string{}})

	runConcurrentMutations(t,
		func() error { return Pick(concurrentPickOptions(project, library, "one")) },
		func() error {
			return SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: successfulAPMRunner})
		},
	)

	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
}

func TestHelperProcessesPreserveCatalogAndProjectFinalStates(t *testing.T) {
	t.Run("distinct catalog adds", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "base", Path: "base/SKILL.md"}},
		})
		runMutationProcesses(t,
			startMutationProcess(t, "catalog-add", "", library, "one"),
			startMutationProcess(t, "catalog-add", "", library, "two"),
		)
		entries, err := LoadCatalog(library, LibraryTypeSkill)
		requireNoError(t, err)
		requireEqual(t, []string{"base", "one", "two"}, catalogEntryNames(entries))
	})

	t.Run("cross catalog ownership", func(t *testing.T) {
		library := t.TempDir()
		runMutationProcesses(t,
			startMutationProcess(t, "remote-skill", "", library, ""),
			startMutationProcess(t, "remote-plugin", "", library, ""),
		)
		skills, plugins, err := loadTypedPackageCatalogs(library)
		if err != nil || len(skills)+len(plugins) != 1 {
			t.Fatalf("catalogs = %d skills + %d plugins, validation error = %v; want one owner and valid catalogs", len(skills), len(plugins), err)
		}
	})

	t.Run("additive picks", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{
			{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "two", Path: "two/SKILL.md"},
		}})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.WriteFile(project.ManifestPath, []byte(
			"name: project\nversion: 0.1.0\nx-review:\n  preserve: true\ndependencies:\n  apm: []\n  mcp: []\n",
		), 0o644))
		runMutationProcesses(t,
			startMutationProcess(t, "pick", project.Root, library, "one"),
			startMutationProcess(t, "pick", project.Root, library, "two"),
		)
		manifest, err := ReadAPMManifest(project.ManifestPath)
		requireNoError(t, err)
		assertDependencySet(t, manifest.Dependencies.APM, localDependencies(
			filepath.Join(library, "skills", "one"),
			filepath.Join(library, "skills", "two"),
		))
		if !strings.Contains(readFile(t, project.ManifestPath), "x-review:") {
			t.Fatal("additive Picks removed ADR 0005 unknown nodes")
		}
	})

	t.Run("pick and targets", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		runMutationProcesses(t,
			startMutationProcess(t, "pick", project.Root, library, "one"),
			startMutationProcess(t, "targets", project.Root, library, "claude"),
		)
		manifest, err := ReadAPMManifest(project.ManifestPath)
		requireNoError(t, err)
		requireEqual(t, []string{"claude"}, manifest.Targets)
		requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
	})

	t.Run("sync and pick", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0", Targets: []string{}})
		runMutationProcesses(t,
			startMutationProcess(t, "pick", project.Root, library, "one"),
			startMutationProcess(t, "sync", project.Root, library, ""),
		)
		manifest, err := ReadAPMManifest(project.ManifestPath)
		requireNoError(t, err)
		requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
		rendered := filepath.Join(project.Root, ".apm", "test-rendered-manifest")
		requireEqual(t, readFile(t, project.ManifestPath), readFile(t, rendered))
	})

	t.Run("directory imports", func(t *testing.T) {
		library := t.TempDir()
		one := filepath.Join(t.TempDir(), "one")
		two := filepath.Join(t.TempDir(), "two")
		requireNoError(t, os.MkdirAll(one, 0o750))
		requireNoError(t, os.MkdirAll(two, 0o750))
		requireNoError(t, os.WriteFile(filepath.Join(one, "SKILL.md"), []byte("# one"), 0o600))
		requireNoError(t, os.WriteFile(filepath.Join(two, "SKILL.md"), []byte("# two"), 0o600))
		runMutationProcesses(t,
			startMutationProcess(t, "import-dir", "", library, one),
			startMutationProcess(t, "import-dir", "", library, two),
		)
		entries, err := LoadCatalog(library, LibraryTypeSkill)
		requireNoError(t, err)
		requireEqual(t, []string{"one", "two"}, catalogEntryNames(entries))
	})

	t.Run("scan and categories", func(t *testing.T) {
		library := t.TempDir()
		writeTypedLibraryMarker(t, filepath.Join(library, "skills", "golang-testing", "SKILL.md"), "# test\n")
		runMutationProcesses(t,
			startMutationProcess(t, "scan", "", library, ""),
			startMutationProcess(t, "categorize", "", library, ""),
		)
		entries, err := LoadCatalog(library, LibraryTypeSkill)
		requireNoError(t, err)
		requireEqual(t, []string{"golang-testing"}, catalogEntryNames(entries))
		if !CategoryRegistryExists(library) {
			t.Fatal("category registry missing after concurrent scan/categorize")
		}
	})

	t.Run("init", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		projectRoot := t.TempDir()
		runMutationProcesses(t, startMutationProcess(t, "init", projectRoot, library, "one"))
		manifest, err := ReadAPMManifest(ProjectAPMPath(projectRoot))
		requireNoError(t, err)
		requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
	})

	t.Run("old instill import", func(t *testing.T) {
		library := t.TempDir()
		writeTypedLibraryMarker(t, filepath.Join(library, "skills", "one", "SKILL.md"), "# one\n")
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.MkdirAll(filepath.Join(project.Root, claudeDirName), 0o750))
		requireNoError(t, WriteManifestAtomic(ProjectManifestPath(project.Root), Manifest{Skills: []string{"one"}}))
		runMutationProcesses(t, startMutationProcess(t, "import-old", project.Root, library, ""))
		assertPathMissing(t, ProjectManifestPath(project.Root))
	})

	t.Run("graft import", func(t *testing.T) {
		library := t.TempDir()
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.WriteFile(filepath.Join(project.Root, "graft.lock"), []byte("servers:\n  - local\n"), 0o600))
		requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".mcp.json"), []byte(`{"mcpServers":{"local":{"command":"server"}}}`), 0o600))
		runMutationProcesses(t, startMutationProcess(t, "import-graft", project.Root, library, ""))
		assertPathMissing(t, filepath.Join(project.Root, "graft.lock"))
	})
}

func TestContendedMutationProcessProtocol(t *testing.T) {
	pair := func(t *testing.T, firstAction, secondAction, projectRoot, library, firstName, secondName string) ([]string, []string) {
		t.Helper()
		first := startControlledMutationProcess(t, firstAction, projectRoot, library, firstName, "dependent-read:", false)
		second := startControlledMutationProcess(t, secondAction, projectRoot, library, secondName, "", true)
		return runContendedMutationPair(t, first, second)
	}

	t.Run("distinct catalog adds", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "base", Path: "base/SKILL.md"}},
		})
		pair(t, "catalog-add", "catalog-add", "", library, "one", "two")
		entries, err := LoadCatalog(library, LibraryTypeSkill)
		requireNoError(t, err)
		requireEqual(t, []string{"base", "one", "two"}, catalogEntryNames(entries))
	})

	t.Run("cross catalog owner", func(t *testing.T) {
		library := t.TempDir()
		_, loserEvents := pair(t, "remote-skill", "remote-plugin", "", library, "", "")
		if !strings.Contains(strings.Join(loserEvents, "\n"), "loser-error:") ||
			!strings.Contains(strings.Join(loserEvents, "\n"), "skill repo") {
			t.Fatalf("plugin loser events = %v, want current skill owner", loserEvents)
		}
		skills, plugins, err := loadTypedPackageCatalogs(library)
		requireNoError(t, err)
		requireEqual(t, 1, len(skills))
		requireEqual(t, 0, len(plugins))
	})

	t.Run("additive picks preserve ADR5", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{
			{Type: LibraryTypeSkill, Name: "base", Path: "base/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "two", Path: "two/SKILL.md"},
		}})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.WriteFile(project.ManifestPath, []byte(
			"name: project\nversion: 0.1.0\nx-review:\n  preserve: true\ndependencies:\n  apm:\n    - "+filepath.Join(library, "skills", "base")+"\n  mcp: []\n",
		), 0o644))
		pair(t, "pick", "pick", project.Root, library, "one", "two")
		manifest, err := ReadAPMManifest(project.ManifestPath)
		requireNoError(t, err)
		assertDependencySet(t, manifest.Dependencies.APM, localDependencies(
			filepath.Join(library, "skills", "base"),
			filepath.Join(library, "skills", "one"),
			filepath.Join(library, "skills", "two"),
		))
		if !strings.Contains(readFile(t, project.ManifestPath), "x-review:") {
			t.Fatal("ADR 0005 unknown node was removed")
		}
	})

	t.Run("pick and targets", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		pair(t, "pick", "targets", project.Root, library, "one", "claude")
		manifest, err := ReadAPMManifest(project.ManifestPath)
		requireNoError(t, err)
		requireEqual(t, []string{"claude"}, manifest.Targets)
		requireEqual(t, localDependencies(filepath.Join(library, "skills", "one")), manifest.Dependencies.APM)
	})

	t.Run("sync and pick rendered final", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0", Targets: []string{}})
		pair(t, "sync", "pick", project.Root, library, "", "one")
		requireEqual(
			t,
			readFile(t, project.ManifestPath),
			readFile(t, filepath.Join(project.Root, ".apm", "test-rendered-manifest")),
		)
	})

	t.Run("scan and categories", func(t *testing.T) {
		library := t.TempDir()
		writeTypedLibraryMarker(t, filepath.Join(library, "skills", "golang-testing", "SKILL.md"), "# test\n")
		pair(t, "scan", "categorize", "", library, "", "")
		if !CategoryRegistryExists(library) {
			t.Fatal("category registry missing")
		}
	})

	t.Run("init and targets", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{
			skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		})
		projectRoot := t.TempDir()
		pair(t, "init", "targets", projectRoot, library, "one", "claude")
		manifest, err := ReadAPMManifest(ProjectAPMPath(projectRoot))
		requireNoError(t, err)
		requireEqual(t, []string{"claude"}, manifest.Targets)
	})

	t.Run("old instill import scope", func(t *testing.T) {
		library := t.TempDir()
		writeTypedLibraryMarker(t, filepath.Join(library, "skills", "one", "SKILL.md"), "# one\n")
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.MkdirAll(filepath.Join(project.Root, claudeDirName), 0o750))
		requireNoError(t, WriteManifestAtomic(ProjectManifestPath(project.Root), Manifest{Skills: []string{"one"}}))
		pair(t, "import-old", "catalog-add", project.Root, library, "", "two")
		assertPathMissing(t, ProjectManifestPath(project.Root))
	})

	t.Run("graft import scope", func(t *testing.T) {
		library := t.TempDir()
		project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
		requireNoError(t, os.WriteFile(filepath.Join(project.Root, "graft.lock"), []byte("servers:\n  - local\n"), 0o600))
		requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".mcp.json"), []byte(`{"mcpServers":{"local":{"command":"server"}}}`), 0o600))
		pair(t, "import-graft", "catalog-add", project.Root, library, "", "two")
		assertPathMissing(t, filepath.Join(project.Root, "graft.lock"))
	})
}

func TestInitTUISelectionDoesNotHoldRootLocksAcrossProcesses(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	projectRoot := t.TempDir()
	initProcess := startControlledMutationProcess(t, "init-tui", projectRoot, library, "one", "selection", false)
	initProcess.begin(t)
	initProcess.waitForEvent(t, "barrier:selection")

	probe := startRootLockProcess(t, library, projectRoot)
	probe.waitFor(t, "attempt")
	probe.waitFor(t, "acquired")
	probe.release(t)
	probe.wait(t)

	initProcess.proceed(t)
	initProcess.waitAndCollect(t)
}

func TestRemoteUpdatesRejectCrossProcessStaleResolution(t *testing.T) {
	tests := []struct {
		name          string
		initial       CatalogEntry
		updateAction  string
		mutateAction  string
		concurrentRef string
	}{
		{
			name: "skill",
			initial: CatalogEntry{
				Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git",
				Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA,
			},
			updateAction:  "update-skill",
			mutateAction:  "mutate-skill",
			concurrentRef: remotePluginSHA,
		},
		{
			name: "plugin",
			initial: CatalogEntry{
				Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git",
				Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA,
			},
			updateAction:  "update-plugin",
			mutateAction:  "mutate-plugin",
			concurrentRef: remoteSkillSHA,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			library := t.TempDir()
			requireNoError(t, WriteCatalog(library, test.initial.Type, []CatalogEntry{test.initial}))
			update := startControlledMutationProcess(t, test.updateAction, "", library, test.initial.Name, "remote-resolution", false)
			update.begin(t)
			update.waitForEvent(t, "barrier:remote-resolution")

			mutate := startMutationProcess(t, test.mutateAction, "", library, test.initial.Name)
			mutate.begin(t)
			mutateEvents := mutate.waitAndCollect(t)
			if lastEventIndex(mutateEvents, "event:lock-released:") < 0 {
				t.Fatalf("mutator events = %v, want committed release", mutateEvents)
			}

			update.proceed(t)
			updateEvents := update.waitAndCollect(t)
			joined := strings.Join(updateEvents, "\n")
			if !strings.Contains(joined, "event:revalidation:") || !strings.Contains(joined, "event:conflict-error:") {
				t.Fatalf("update events = %v, want locked revalidation conflict", updateEvents)
			}
			entries, err := LoadCatalog(library, test.initial.Type)
			requireNoError(t, err)
			requireEqual(t, test.concurrentRef, entries[0].Ref)
		})
	}
}

func TestMutationHelperProcess(t *testing.T) {
	action := os.Getenv("INSTILL_TEST_MUTATION")
	if action == "" {
		return
	}
	projectRoot := os.Getenv("INSTILL_TEST_PROJECT")
	library := os.Getenv("INSTILL_TEST_LIBRARY")
	name := os.Getenv("INSTILL_TEST_NAME")
	pauseEvent := os.Getenv("INSTILL_TEST_PAUSE_EVENT")
	pauseContended := os.Getenv("INSTILL_TEST_PAUSE_CONTENDED") == "true"
	paused := false
	mutationTestEventHook = func(event string) {
		fmt.Println("event:" + event)
		shouldPause := !paused && pauseEvent != "" && strings.HasPrefix(event, pauseEvent)
		shouldPause = shouldPause || !paused && pauseContended && strings.HasPrefix(event, "lock-contended:")
		if shouldPause {
			paused = true
			fmt.Println("barrier:" + event)
			if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
	fmt.Println("ready")
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	project := Project{
		Root:             projectRoot,
		ManifestPath:     ProjectAPMPath(projectRoot),
		SymlinkDir:       filepath.Join(projectRoot, claudeDirName, skillsDirName),
		AgentsSymlinkDir: filepath.Join(projectRoot, agentsDirName, skillsDirName),
	}
	var err error
	switch action {
	case "catalog-add":
		err = AddCatalogEntry(library, CatalogEntry{Type: LibraryTypeSkill, Name: name, Path: name + "/SKILL.md"})
	case "pick":
		opts := concurrentPickOptions(project, library, name)
		opts.Runner = renderingAPMRunner(projectRoot)
		err = Pick(opts)
	case "targets":
		err = SetProjectTargets(SetTargetsOptions{Project: project, Targets: []string{name}})
	case "sync":
		err = SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: renderingAPMRunner(projectRoot)})
	case "remote-skill":
		err = AddRemoteSkill(context.Background(), library, "owner/repo", remoteRepoSkillRunner(remoteSkillSHA))
	case "remote-plugin":
		err = AddRemotePlugin(
			context.Background(),
			library,
			"owner/repo",
			"plugin",
			remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"skills/repo"}]}`, `{"name":"plugin"}`),
		)
	case "import-dir":
		err = ImportDirectory(ImportDirectoryOptions{LibraryPath: library, Path: name})
	case "scan":
		err = ScanLibrary(library, nil)
	case "categorize":
		err = CategorizeLibrary(CategorizeOptions{LibraryPath: library})
	case "hooks":
		err = AddHooks(project, nil)
	case "init":
		err = InitProject(InitProjectOptions{Root: projectRoot, LibraryPath: library, Skills: []string{name}, Runner: renderingAPMRunner(projectRoot)})
	case "init-tui":
		err = InitProject(InitProjectOptions{
			Root:        projectRoot,
			LibraryPath: library,
			Runner:      renderingAPMRunner(projectRoot),
			SelectSkills: func() (InitialSkillSelectionPlan, bool, error) {
				emitMutationTestEvent("selection")
				return InitialSkillSelectionPlan{Skills: []string{name}}, true, nil
			},
		})
	case "import-old":
		err = ImportOldInstill(ImportOptions{Project: project, LibraryPath: library})
	case "import-graft":
		err = ImportGraft(ImportOptions{Project: project, LibraryPath: library})
	case "update-skill":
		skillBase := remoteSkillRunner(t, refreshedRemoteSkillSHA)
		err = UpdateRemoteSkill(context.Background(), library, name, func(command string, args ...string) ([]byte, error) {
			if command == "git" && len(args) > 0 && args[0] == "ls-remote" {
				emitMutationTestEvent("remote-resolution:skill")
			}
			return skillBase(command, args...)
		})
	case "mutate-skill":
		skillEntries, skillLoadErr := LoadCatalog(library, LibraryTypeSkill)
		if skillLoadErr != nil {
			err = skillLoadErr
			break
		}
		skillEntries[0].Ref = remotePluginSHA
		err = WriteCatalog(library, LibraryTypeSkill, skillEntries)
	case "update-plugin":
		pluginBase := remotePluginRunner(
			t,
			refreshedRemotePluginSHA,
			`{"plugins":[{"name":"plugin","source":"plugin"}]}`,
			`{"name":"plugin"}`,
		)
		err = UpdateRemotePlugin(context.Background(), library, name, func(command string, args ...string) ([]byte, error) {
			if command == "git" && len(args) > 0 && args[0] == "ls-remote" {
				emitMutationTestEvent("remote-resolution:plugin")
			}
			return pluginBase(command, args...)
		})
	case "mutate-plugin":
		pluginEntries, pluginLoadErr := LoadCatalog(library, LibraryTypePlugin)
		if pluginLoadErr != nil {
			err = pluginLoadErr
			break
		}
		pluginEntries[0].Ref = remoteSkillSHA
		err = WriteCatalog(library, LibraryTypePlugin, pluginEntries)
	default:
		err = errors.New("unknown mutation action: " + action)
	}
	if (action == "remote-skill" || action == "remote-plugin") && err != nil && strings.Contains(err.Error(), "owned") {
		emitMutationTestEvent("loser-error:" + err.Error())
		err = nil
	}
	if (action == "update-skill" || action == "update-plugin") && err != nil && strings.Contains(err.Error(), "concurrent catalog change") {
		emitMutationTestEvent("conflict-error:" + err.Error())
		err = nil
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type mutationProcess struct {
	action  string
	command *exec.Cmd
	stdin   io.WriteCloser
	lines   <-chan string
	stderr  *strings.Builder
	history []string
}

func startMutationProcess(t *testing.T, action string, projectRoot string, library string, name string) *mutationProcess {
	return startControlledMutationProcess(t, action, projectRoot, library, name, "", false)
}

func startControlledMutationProcess(
	t *testing.T,
	action string,
	projectRoot string,
	library string,
	name string,
	pauseEvent string,
	pauseContended bool,
) *mutationProcess {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestMutationHelperProcess$")
	command.Env = append(
		os.Environ(),
		"INSTILL_TEST_MUTATION="+action,
		"INSTILL_TEST_PROJECT="+projectRoot,
		"INSTILL_TEST_LIBRARY="+library,
		"INSTILL_TEST_NAME="+name,
		"INSTILL_TEST_PAUSE_EVENT="+pauseEvent,
		fmt.Sprintf("INSTILL_TEST_PAUSE_CONTENDED=%t", pauseContended),
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	stderr := &strings.Builder{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	lines := make(chan string, 128)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return &mutationProcess{action: action, command: command, stdin: stdin, lines: lines, stderr: stderr}
}

func (p *mutationProcess) waitForEvent(t *testing.T, prefix string) string {
	t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			t.Fatalf("%s helper output closed waiting for %q: %s", p.action, prefix, p.stderr.String())
		}
		if !strings.HasPrefix(line, prefix) {
			p.history = append(p.history, line)
			return p.waitForEvent(t, prefix)
		}
		p.history = append(p.history, line)
		return line
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s helper event %q", p.action, prefix)
		return ""
	}
}

func (p *mutationProcess) begin(t *testing.T) {
	t.Helper()
	p.waitForEvent(t, "ready")
	if _, err := p.stdin.Write([]byte{'s'}); err != nil {
		t.Fatalf("%s start Write() error = %v", p.action, err)
	}
}

func (p *mutationProcess) proceed(t *testing.T) {
	t.Helper()
	if _, err := p.stdin.Write([]byte{'c'}); err != nil {
		t.Fatalf("%s proceed Write() error = %v", p.action, err)
	}
}

func (p *mutationProcess) waitAndCollect(t *testing.T) []string {
	t.Helper()
	if err := p.command.Wait(); err != nil {
		t.Fatalf("%s helper Wait() error = %v: %s", p.action, err, p.stderr.String())
	}
	_ = p.stdin.Close()
	var events []string
	events = append(events, p.history...)
	for line := range p.lines {
		events = append(events, line)
	}
	return events
}

func runContendedMutationPair(t *testing.T, first *mutationProcess, second *mutationProcess) ([]string, []string) {
	t.Helper()
	first.begin(t)
	first.waitForEvent(t, "barrier:dependent-read:")
	second.begin(t)
	second.waitForEvent(t, "event:lock-contended:")
	second.waitForEvent(t, "barrier:lock-contended:")
	for _, event := range second.history {
		if strings.HasPrefix(event, "event:lock-set-acquired") || strings.HasPrefix(event, "event:first-write:") {
			t.Fatalf("second %s entered mutation before first release: %v", second.action, second.history)
		}
	}

	first.proceed(t)
	firstEvents := first.waitAndCollect(t)
	if lastEventIndex(firstEvents, "event:lock-released:") < 0 {
		t.Fatalf("first %s events = %v, want release before second acquisition", first.action, firstEvents)
	}
	second.proceed(t)
	secondEvents := second.waitAndCollect(t)
	if lastEventIndex(secondEvents, "event:lock-set-acquired") < 0 {
		t.Fatalf("second %s events = %v, want acquisition after first release", second.action, secondEvents)
	}
	return firstEvents, secondEvents
}

func runMutationProcesses(t *testing.T, processes ...*mutationProcess) {
	t.Helper()
	for _, process := range processes {
		select {
		case line := <-process.lines:
			if line != "ready" {
				t.Fatalf("helper output = %q, want ready", line)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for mutation helper readiness")
		}
	}
	for _, process := range processes {
		if _, err := process.stdin.Write([]byte{'x'}); err != nil {
			t.Fatalf("helper release Write() error = %v", err)
		}
		_ = process.stdin.Close()
	}
	for _, process := range processes {
		if err := process.command.Wait(); err != nil {
			t.Fatalf("mutation helper Wait() error = %v: %s", err, process.stderr.String())
		}
		var events []string
		for line := range process.lines {
			events = append(events, line)
		}
		joined := strings.Join(events, "\n")
		for _, required := range []string{"event:lock-request:", "event:lock-acquired:", "event:dependent-read:", "event:lock-released:"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("%s helper events = %q, want %q", process.action, joined, required)
			}
		}
		if process.action != "remote-skill" && process.action != "remote-plugin" && !strings.Contains(joined, "event:first-write:") {
			t.Fatalf("%s helper events = %q, want first-write", process.action, joined)
		}
		if (process.action == "remote-skill" || process.action == "remote-plugin") &&
			!strings.Contains(joined, "event:first-write:") && !strings.Contains(joined, "event:loser-error:") {
			t.Fatalf("%s helper events = %q, want winner write or actionable loser error", process.action, joined)
		}
		acquired := lastEventIndex(events, "event:lock-acquired:")
		dependentRead := eventIndexAfter(events, "event:dependent-read:", acquired)
		released := eventIndexAfter(events, "event:lock-released:", acquired)
		if acquired < 0 || dependentRead < 0 || released < dependentRead {
			t.Fatalf("%s helper event order = %v, want acquisition then dependent read then release", process.action, events)
		}
		if process.action != "remote-skill" && process.action != "remote-plugin" {
			firstWrite := eventIndexAfter(events, "event:first-write:", acquired)
			if firstWrite < 0 || released < firstWrite {
				t.Fatalf("%s helper event order = %v, want first write inside lock interval", process.action, events)
			}
		}
	}
}

func lastEventIndex(events []string, prefix string) int {
	index := -1
	for i, event := range events {
		if strings.HasPrefix(event, prefix) {
			index = i
		}
	}
	return index
}

func eventIndexAfter(events []string, prefix string, after int) int {
	for i := after + 1; i < len(events); i++ {
		if strings.HasPrefix(events[i], prefix) {
			return i
		}
	}
	return -1
}

func TestLibraryEarlyReleaseFailurePreventsAPM(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
	canonical, err := canonicalRoots([]string{library, project.Root})
	requireNoError(t, err)
	libraryIndex := 0
	for index, root := range canonical {
		if root.path == library {
			libraryIndex = index
		}
	}
	provider := &recordingLockProvider{unlockErr: map[int]error{libraryIndex: errors.New("release failed")}}
	apmStarted := false
	opts := concurrentPickOptions(project, library, "one")
	opts.Runner = func(name string, args ...string) ([]byte, error) {
		apmStarted = true
		return nil, nil
	}
	err = withRootLocksUsing(context.Background(), []string{library, project.Root}, time.Second, provider, func(ctx context.Context, held *heldLocks) error {
		return pickLocked(ctx, held, opts)
	})
	if err == nil || ExitCode(err) != ExitFilesystem {
		t.Fatalf("pickLocked() error = %v, want filesystem release failure", err)
	}
	if apmStarted {
		t.Fatal("APM started after Library early-release failure")
	}
}

func TestPickReleasesLibraryButRetainsProjectDuringAPM(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	project := createAPMProject(t, APMManifest{Name: "project", Version: "0.1.0"})
	apmStarted := make(chan struct{})
	releaseAPM := make(chan struct{})
	runner := func(name string, args ...string) ([]byte, error) {
		if name == "apm" && len(args) == 1 && args[0] == "--version" {
			return []byte("apm 0.28.0"), nil
		}
		if err := os.WriteFile(filepath.Join(project.Root, "apm-first-mutation"), []byte("install"), 0o600); err != nil {
			return nil, err
		}
		close(apmStarted)
		<-releaseAPM
		return nil, nil
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Pick(PickOptions{
			Project:     project,
			LibraryPath: library,
			Type:        LibraryTypeSkill,
			Add:         []string{"one"},
			Runner:      runner,
		})
	}()
	<-apmStarted
	requireEqual(t, "install", readFile(t, filepath.Join(project.Root, "apm-first-mutation")))

	libraryMutation := startMutationProcess(t, "catalog-add", "", library, "two")
	runMutationProcesses(t, libraryMutation)
	projectWaiter := startRootLockWaiterProcess(t, project.Root)
	projectWaiter.waitFor(t, "attempt")
	projectWaiter.waitFor(t, "contended")
	projectWaiter.checkBlocked(t)

	close(releaseAPM)
	if err := <-errCh; err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	projectWaiter.waitFor(t, "acquired")
	projectWaiter.release(t)
	projectWaiter.wait(t)
	entries, err := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []string{"one", "two"}, catalogEntryNames(entries))
}

func TestInitSelectionCompletesBeforeRootLocks(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
	})
	projectRoot := t.TempDir()
	selectionAcquiredLocks := false
	err := InitProject(InitProjectOptions{
		Root:        projectRoot,
		LibraryPath: library,
		Runner:      successfulAPMRunner,
		SelectSkills: func() (InitialSkillSelectionPlan, bool, error) {
			err := withRootLocks(context.Background(), []string{library, projectRoot}, func(context.Context, *heldLocks) error {
				selectionAcquiredLocks = true
				return nil
			})
			return InitialSkillSelectionPlan{Skills: []string{"one"}}, true, err
		},
	})
	requireNoError(t, err)
	if !selectionAcquiredLocks {
		t.Fatal("selection could not acquire roots before Init mutation locking")
	}
}

func concurrentPickOptions(project Project, library string, name string) PickOptions {
	return PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{name},
		Runner:      successfulAPMRunner,
	}
}

func successfulAPMRunner(name string, args ...string) ([]byte, error) {
	if name == "apm" && len(args) == 1 && args[0] == "--version" {
		return []byte("apm 0.28.0"), nil
	}
	if name != "apm" {
		return nil, errors.New("unexpected command: " + name)
	}
	return nil, nil
}

func renderingAPMRunner(projectRoot string) CommandRunner {
	return func(name string, args ...string) ([]byte, error) {
		if name == "apm" && len(args) == 1 && args[0] == "--version" {
			return []byte("apm 0.28.0"), nil
		}
		if name != "apm" {
			return nil, errors.New("unexpected command: " + name)
		}
		manifest, err := os.ReadFile(ProjectAPMPath(projectRoot))
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(projectRoot, ".apm")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
		emitMutationTestEvent("first-write:" + filepath.Join(dir, "test-rendered-manifest"))
		return nil, os.WriteFile(filepath.Join(dir, "test-rendered-manifest"), manifest, 0o600)
	}
}

func remoteRepoSkillRunner(sha string) CommandRunner {
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/repo.git HEAD":
			return []byte("ref: refs/heads/main\tHEAD\n" + sha + "\tHEAD\n"), nil
		case strings.HasPrefix(command, "git init "):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/repo.git"):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " ls-tree "+sha+" -- skills/repo/SKILL.md"):
			return []byte("100644 blob abc\tskills/repo/SKILL.md\n"), nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " show "+sha+":skills/repo/SKILL.md"):
			return []byte("# repo"), nil
		default:
			return nil, errors.New("unexpected command: " + command)
		}
	}
}

func runConcurrentMutations(t *testing.T, mutations ...func() error) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, len(mutations))
	var ready sync.WaitGroup
	ready.Add(len(mutations))
	for _, mutation := range mutations {
		go func(mutation func() error) {
			ready.Done()
			<-start
			errs <- mutation()
		}(mutation)
	}
	ready.Wait()
	close(start)
	for range mutations {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent mutation error = %v", err)
		}
	}
}

func assertDependencySet(t *testing.T, got []APMDependency, want []APMDependency) {
	t.Helper()
	gotSet := make(map[string]struct{}, len(got))
	for _, dependency := range got {
		gotSet[dependency.identity()] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, dependency := range want {
		wantSet[dependency.identity()] = struct{}{}
	}
	requireEqual(t, wantSet, gotSet)
}

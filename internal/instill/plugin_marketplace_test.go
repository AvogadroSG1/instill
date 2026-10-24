package instill

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mattpocockMarketplaceFiles() map[string]string {
	return map[string]string{
		".claude-plugin/marketplace.json": `{"name":"mattpocock","plugins":[{"name":"mattpocock-skills","source":"./","description":"d"}]}`,
		".claude-plugin/plugin.json":      `{"name":"mattpocock-skills"}`,
		"skills/tdd/SKILL.md":             "# tdd\n",
	}
}

func marketplaceRunner(t *testing.T, sha string, files map[string]string) CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/repo.git HEAD":
			return []byte(sha + "\tHEAD\n"), nil
		case strings.Contains(command, " init "),
			strings.Contains(command, " remote add origin "),
			strings.HasSuffix(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, " checkout --force --detach "+sha):
			workTree := ""
			for _, arg := range args {
				if strings.HasPrefix(arg, "--work-tree=") {
					workTree = strings.TrimPrefix(arg, "--work-tree=")
				}
			}
			for rel, content := range files {
				path := filepath.Join(workTree, filepath.FromSlash(rel))
				requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				requireNoError(t, os.WriteFile(path, []byte(content), 0o644))
			}
			return nil, nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}
}

func marketplaceCatalogBytes(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "plugins", "catalog.csv"))
	if os.IsNotExist(err) {
		return nil
	}
	requireNoError(t, err)
	return data
}

func requireNoMarketplaceStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "plugins", pluginMarketplacesDir))
	if os.IsNotExist(err) {
		return
	}
	requireNoError(t, err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") || strings.HasSuffix(entry.Name(), ".previous") {
			t.Fatalf("leftover marketplace entry %q", entry.Name())
		}
	}
}

func requireMarketplaceError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

func TestAddPluginMarketplaceDownloadsTreeAndCatalogsPlugins(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	requireNoError(t, AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, mattpocockMarketplaceFiles()), &stdout))

	rows, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypePlugin,
		Name:        "mattpocock/mattpocock-skills",
		Category:    "mattpocock",
		Path:        "marketplaces/mattpocock/.claude-plugin/plugin.json",
		Source:      "marketplace",
		Repository:  "https://github.com/owner/repo.git",
		Ref:         otherRemoteSHA,
		Description: "d",
	}}, rows)

	_, err = os.Stat(filepath.Join(root, "plugins", "marketplaces", "mattpocock", "skills", "tdd", "SKILL.md"))
	requireNoError(t, err)
	assertPathMissing(t, filepath.Join(root, "plugins", "marketplaces", "mattpocock", ".git"))

	entries, err := os.ReadDir(filepath.Join(root, "plugins", "marketplaces"))
	requireNoError(t, err)
	requireEqual(t, 1, len(entries))
	requireEqual(t, "mattpocock", entries[0].Name())

	requireEqual(t, "added plugin marketplace mattpocock from owner/repo at 111111111111: 1 plugins\n", stdout.String())
}

func TestAddPluginMarketplaceSkipsUninstallablePlugins(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".claude-plugin/marketplace.json": `{"name":"mkt","plugins":[` +
			`{"name":"a","source":"./plugins/a"},` +
			`{"name":"b","source":{"source":"github","repo":"x/y"}},` +
			`{"name":"c","source":"./plugins/missing"},` +
			`{"name":"d","source":"./plugins/d"},` +
			`{"name":"e","source":"../escape"}]}`,
		"plugins/a/.claude-plugin/plugin.json": `{"name":"a"}`,
		"plugins/d/README.md":                  "readme\n",
	}
	var stdout bytes.Buffer
	requireNoError(t, AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, files), &stdout))

	rows, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, 1, len(rows))
	requireEqual(t, "mkt/a", rows[0].Name)
	requireEqual(t, "marketplaces/mkt/plugins/a/.claude-plugin/plugin.json", rows[0].Path)

	for _, want := range []string{
		"skipped: b (external source)",
		"skipped: c (source not found)",
		"skipped: d (no plugin manifest)",
		"skipped: e (invalid source path)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want containing %q", stdout.String(), want)
		}
	}
}

func TestAddPluginMarketplaceRejectsWithoutMutation(t *testing.T) {
	valid := func(marketplace string, plugins string) map[string]string {
		return map[string]string{
			".claude-plugin/marketplace.json":      `{"name":"` + marketplace + `","plugins":[` + plugins + `]}`,
			"plugins/a/.claude-plugin/plugin.json": `{"name":"a"}`,
		}
	}
	const onePlugin = `{"name":"a","source":"./plugins/a"}`
	tests := []struct {
		name    string
		files   map[string]string
		setup   func(t *testing.T, root string)
		wantErr string
	}{
		{
			name:    "missing marketplace json",
			files:   map[string]string{"skills/tdd/SKILL.md": "x"},
			wantErr: "remote marketplace metadata is missing",
		},
		{
			name:    "malformed json",
			files:   map[string]string{".claude-plugin/marketplace.json": "{"},
			wantErr: "malformed remote marketplace metadata",
		},
		{
			name:    "invalid name",
			files:   valid("", onePlugin),
			wantErr: "remote marketplace name is missing or invalid",
		},
		{
			name:    "duplicate plugin names",
			files:   valid("mkt", onePlugin+`,`+onePlugin),
			wantErr: "duplicate remote marketplace plugin name",
		},
		{
			name:    "plugin name containing slash",
			files:   valid("mkt", `{"name":"a/b","source":"./plugins/a"}`),
			wantErr: "remote marketplace plugin name is invalid",
		},
		{
			name:    "zero installable plugins",
			files:   valid("mkt", `{"name":"a","source":"./plugins/missing"}`),
			wantErr: "remote marketplace has no installable plugins",
		},
		{
			name:  "marketplace already added",
			files: valid("mkt", onePlugin),
			setup: func(t *testing.T, root string) {
				requireNoError(t, AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, valid("mkt", onePlugin)), io.Discard))
			},
			wantErr: "library update plugin-marketplace",
		},
		{
			name:  "existing local row with same name",
			files: valid("mattpocock", `{"name":"mattpocock-skills","source":"./plugins/a"}`),
			setup: func(t *testing.T, root string) {
				requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{{
					Type: LibraryTypePlugin, Name: "mattpocock/mattpocock-skills", Path: "mattpocock-skills/.claude-plugin/plugin.json",
				}}))
			},
			wantErr: "error: plugin already exists: mattpocock/mattpocock-skills",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.setup != nil {
				test.setup(t, root)
			}
			before := marketplaceCatalogBytes(t, root)
			err := AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, test.files), io.Discard)
			requireMarketplaceError(t, err, test.wantErr)
			if string(marketplaceCatalogBytes(t, root)) != string(before) {
				t.Fatalf("catalog changed after rejected add")
			}
			requireNoMarketplaceStaging(t, root)
		})
	}
}

func TestUpdatePluginMarketplaceReplacesTreeAndRows(t *testing.T) {
	root := t.TempDir()
	addFiles := map[string]string{
		".claude-plugin/marketplace.json":      `{"name":"mkt","plugins":[{"name":"a","source":"./plugins/a"},{"name":"b","source":"./plugins/b"}]}`,
		"plugins/a/.claude-plugin/plugin.json": `{"name":"a"}`,
		"plugins/a/skills/x/SKILL.md":          "old\n",
		"plugins/b/.claude-plugin/plugin.json": `{"name":"b"}`,
	}
	requireNoError(t, AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, addFiles), io.Discard))

	updateFiles := map[string]string{
		".claude-plugin/marketplace.json":      `{"name":"mkt","plugins":[{"name":"a","source":"./plugins/a"},{"name":"c","source":"./plugins/c"}]}`,
		"plugins/a/.claude-plugin/plugin.json": `{"name":"a"}`,
		"plugins/a/skills/x/SKILL.md":          "new\n",
		"plugins/c/.claude-plugin/plugin.json": `{"name":"c"}`,
	}
	var stdout bytes.Buffer
	requireNoError(t, UpdatePluginMarketplace(context.Background(), root, "mkt", marketplaceRunner(t, otherRefreshedRemoteSHA, updateFiles), &stdout))

	rows, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, 2, len(rows))
	requireEqual(t, "mkt/a", rows[0].Name)
	requireEqual(t, "mkt/c", rows[1].Name)
	for _, row := range rows {
		requireEqual(t, otherRefreshedRemoteSHA, row.Ref)
	}

	requireEqual(t, "new\n", readFile(t, filepath.Join(root, "plugins", "marketplaces", "mkt", "plugins", "a", "skills", "x", "SKILL.md")))
	requireNoMarketplaceStaging(t, root)
	requireEqual(t, "updated plugin marketplace mkt to 222222222222: 2 plugins\n", stdout.String())

	failing := func(string, ...string) ([]byte, error) { return nil, errors.New("offline") }
	beforeCatalog := marketplaceCatalogBytes(t, root)
	requireMarketplaceError(t, UpdatePluginMarketplace(context.Background(), root, "mkt", failing, io.Discard), "offline")
	if string(marketplaceCatalogBytes(t, root)) != string(beforeCatalog) {
		t.Fatalf("catalog changed after failed refresh")
	}
	requireEqual(t, "new\n", readFile(t, filepath.Join(root, "plugins", "marketplaces", "mkt", "plugins", "a", "skills", "x", "SKILL.md")))

	requireMarketplaceError(t, UpdatePluginMarketplace(context.Background(), root, "nope", marketplaceRunner(t, otherRefreshedRemoteSHA, updateFiles), io.Discard), "unknown plugin marketplace: nope")
}

func TestPickInstallsMarketplacePluginFromLibraryCopy(t *testing.T) {
	library := t.TempDir()
	requireNoError(t, AddPluginMarketplace(context.Background(), library, "owner/repo", marketplaceRunner(t, otherRemoteSHA, mattpocockMarketplaceFiles()), io.Discard))

	project := createAPMProject(t, APMManifest{})
	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Add: []string{"mattpocock/mattpocock-skills"}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{{Local: filepath.Join(library, "plugins", "marketplaces", "mattpocock")}}, manifest.Dependencies.APM)
}

func TestScanPreservesMarketplacePlugins(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, AddPluginMarketplace(context.Background(), root, "owner/repo", marketplaceRunner(t, otherRemoteSHA, mattpocockMarketplaceFiles()), io.Discard))
	before, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)

	var out bytes.Buffer
	requireNoError(t, ScanLibraryType(root, LibraryTypePlugin, &out))
	after, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, before, after)
	requireEqual(t, "", out.String())

	requireNoError(t, os.RemoveAll(filepath.Join(root, "plugins", "marketplaces", "mattpocock")))
	out.Reset()
	requireNoError(t, ScanLibraryType(root, LibraryTypePlugin, &out))
	rows, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, 0, len(rows))
	requireContains(t, out.String(), "removed: mattpocock/mattpocock-skills (content not found)")
}

func TestPluginCatalogRejectsGitPinnedRows(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "plugins"), 0o755))
	csv := "name,category,path,source,repository,ref,description\n" +
		"mkt/plugin,mkt,plugin,git,https://github.com/owner/repo.git," + otherRemoteSHA + ",d\n"
	requireNoError(t, os.WriteFile(filepath.Join(root, "plugins", "catalog.csv"), []byte(csv), 0o644))

	_, err := LoadCatalog(root, LibraryTypePlugin)
	requireMarketplaceError(t, err, "no longer supported")
}

func TestPickTypeStatesShowMarketplaceAsPluginCategory(t *testing.T) {
	library := t.TempDir()
	requireNoError(t, AddPluginMarketplace(context.Background(), library, "owner/repo", marketplaceRunner(t, otherRemoteSHA, mattpocockMarketplaceFiles()), io.Discard))
	requireNoError(t, os.MkdirAll(filepath.Join(library, "plugins", "local", ".claude-plugin"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(library, "plugins", "local", ".claude-plugin", "plugin.json"), []byte(`{"name":"local"}`), 0o644))
	var out bytes.Buffer
	requireNoError(t, ScanLibraryType(library, LibraryTypePlugin, &out))

	project := createAPMProject(t, APMManifest{})
	states, err := loadPickTypeStates(project, library)
	requireNoError(t, err)
	model := newPickPickerModel(states, LibraryTypePlugin).itemModel()
	requireEqual(t, []string{"All", "mattpocock"}, model.categories)
	requireEqual(t, []string{"mattpocock/mattpocock-skills"}, model.tree.immediateSkills([]string{"mattpocock"}))
}

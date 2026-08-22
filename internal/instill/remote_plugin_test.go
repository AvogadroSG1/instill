package instill

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const remotePluginSHA = "1111111111111111111111111111111111111111"
const refreshedRemotePluginSHA = "2222222222222222222222222222222222222222"

func TestAddRemotePluginInfersSingletonFromMarketplace(t *testing.T) {
	root := t.TempDir()
	runner := remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"impeccable","description":"Design tools","category":"design","source":"plugins/impeccable"}]}`, `{"name":"impeccable"}`)

	err := AddRemotePlugin(root, "Owner/Repo", "", runner)

	requireNoError(t, err)
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type: LibraryTypePlugin, Name: "impeccable", Category: "design", Path: "plugins/impeccable",
		Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA, Description: "Design tools",
	}}, entries)
}

func TestAddRemotePluginNormalizesOptionalGitSuffix(t *testing.T) {
	for _, repository := range []string{"owner/repo", "owner/repo.git"} {
		t.Run(repository, func(t *testing.T) {
			root := t.TempDir()
			err := AddRemotePlugin(root, repository, "", remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, `{"name":"plugin"}`))
			requireNoError(t, err)
			entries, loadErr := LoadCatalog(root, LibraryTypePlugin)
			requireNoError(t, loadErr)
			requireEqual(t, "https://github.com/owner/repo.git", entries[0].Repository)
		})
	}
}

func TestAddRemotePluginCanonicalizesGitHubRepositoryCase(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, AddRemotePlugin(root, "OWNER/Repo", "", remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"Plugin"}]}`, `{"name":"plugin"}`)))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, "https://github.com/owner/repo.git", entries[0].Repository)
	requireEqual(t, "git:https://github.com/owner/repo.git:Plugin", stableCatalogGitIdentity(entries[0]))
}

func TestAddRemotePluginSelectsNamedPluginFromMultiPluginMarketplace(t *testing.T) {
	root := t.TempDir()
	marketplace := `{"plugins":[{"name":"beta","source":"plugins/beta"},{"name":"alpha","source":"plugins/alpha"}]}`
	requireNoError(t, AddRemotePlugin(root, "owner/repo", "alpha", remotePluginRunner(t, remotePluginSHA, marketplace, `{"name":"alpha"}`)))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, "alpha", entries[0].Name)
}

func TestAddRemotePluginRequiresNameForMultiPluginMarketplace(t *testing.T) {
	root := t.TempDir()
	marketplace := `{"plugins":[{"name":"beta","source":"beta"},{"name":"alpha","source":"alpha"}]}`
	err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, marketplace, ""))
	if err == nil || !strings.Contains(ErrorMessage(err), "alpha, beta") {
		t.Fatalf("AddRemotePlugin() error = %v, want sorted available names", err)
	}
	assertPathMissing(t, filepath.Join(root, "plugins", "catalog.csv"))
}

func TestAddRemotePluginRejectsUnknownMarketplaceNameWithoutMutation(t *testing.T) {
	for _, marketplace := range []string{
		`{"plugins":[{"name":"alpha","source":"alpha"}]}`,
		`{"plugins":[{"name":"beta","source":"beta"},{"name":"alpha","source":"alpha"}]}`,
	} {
		root := t.TempDir()
		original := pluginCatalogBytes(t, root)
		err := AddRemotePlugin(root, "owner/repo", "missing", remotePluginRunner(t, remotePluginSHA, marketplace, ""))
		if err == nil || !strings.Contains(ErrorMessage(err), "alpha") {
			t.Fatalf("AddRemotePlugin() error = %v, want available name", err)
		}
		requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
	}
}

func TestAddRemotePluginRejectsUnsafeNamesWithoutMutation(t *testing.T) {
	for _, name := range []string{" plugin", "plugin\nspoof", "plugin\x1b[31m"} {
		root := t.TempDir()
		original := pluginCatalogBytes(t, root)
		marketplace, err := json.Marshal(pluginMarketplace{Plugins: []marketplacePlugin{{Name: name, Source: "plugin"}}})
		requireNoError(t, err)
		if err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, string(marketplace), "")); err == nil {
			t.Fatalf("AddRemotePlugin() error = nil for name %q", name)
		}
		requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
	}
}

func TestAddRemotePluginRejectsDuplicateMarketplaceNamesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	original := pluginCatalogBytes(t, root)
	err := AddRemotePlugin(root, "owner/repo", "same", remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"same","source":"one"},{"name":"same","source":"two"}]}`, ""))
	if err == nil {
		t.Fatal("AddRemotePlugin() error = nil, want duplicate name error")
	}
	requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
}

func TestAddRemotePluginRejectsMalformedOrEmptyMarketplaceWithoutMutation(t *testing.T) {
	for _, marketplace := range []string{"{", `{"plugins":[]}`} {
		root := t.TempDir()
		original := pluginCatalogBytes(t, root)
		err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, marketplace, ""))
		if err == nil {
			t.Fatalf("AddRemotePlugin() error = nil for %q", marketplace)
		}
		requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
	}
}

func TestAddRemotePluginRejectsOversizedMetadataBeforeReading(t *testing.T) {
	for _, oversizedPath := range []string{".claude-plugin/marketplace.json", "plugin/.claude-plugin/plugin.json"} {
		t.Run(oversizedPath, func(t *testing.T) {
			root := t.TempDir()
			original := pluginCatalogBytes(t, root)
			base := remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, `{"name":"plugin"}`)
			showCalled := false
			runner := func(name string, args ...string) ([]byte, error) {
				command := name + " " + strings.Join(args, " ")
				if strings.Contains(command, " cat-file -s "+remotePluginSHA+":"+oversizedPath) {
					return []byte(strconv.FormatInt(maxRemoteMetadataBytes+1, 10)), nil
				}
				if strings.Contains(command, " show "+remotePluginSHA+":"+oversizedPath) {
					showCalled = true
				}
				return base(name, args...)
			}

			if err := AddRemotePlugin(root, "owner/repo", "", runner); err == nil {
				t.Fatal("AddRemotePlugin() error = nil, want size limit error")
			}
			if showCalled {
				t.Fatal("AddRemotePlugin() read oversized metadata")
			}
			requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
		})
	}
}

func TestAddRemotePluginRejectsUnsafeSourcePathsWithoutMutation(t *testing.T) {
	paths := []string{"", "/plugin", "https://example.test/plugin", "git@github.com:owner/repo", `plugin\child`, "plugin\x00child", "plugin/../child"}
	for _, source := range paths {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			original := pluginCatalogBytes(t, root)
			marketplace := `{"plugins":[{"name":"plugin","source":"` + source + `"}]}`
			if source == "plugin\x00child" {
				marketplace = "{\"plugins\":[{\"name\":\"plugin\",\"source\":\"plugin\\u0000child\"}]}"
			}
			err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, marketplace, ""))
			if err == nil {
				t.Fatalf("AddRemotePlugin() error = nil for source %q", source)
			}
			requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
		})
	}
}

func TestAddRemotePluginRejectsSymlinkedSourceWithoutMutation(t *testing.T) {
	root := t.TempDir()
	original := pluginCatalogBytes(t, root)
	runner := remotePluginRunnerWithModes(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, `{"name":"plugin"}`, "120000", "100644")
	if err := AddRemotePlugin(root, "owner/repo", "", runner); err == nil {
		t.Fatal("AddRemotePlugin() error = nil, want symlink rejection")
	}
	requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
}

func TestAddRemotePluginRejectsMissingOrInvalidPluginManifestWithoutMutation(t *testing.T) {
	for _, manifest := range []string{"", "{", `{"name":"other"}`} {
		root := t.TempDir()
		original := pluginCatalogBytes(t, root)
		err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, manifest))
		if err == nil {
			t.Fatalf("AddRemotePlugin() error = nil for manifest %q", manifest)
		}
		requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
	}
}

func TestRemotePluginCatalogRejectsNonImmutableRef(t *testing.T) {
	err := WriteCatalog(t.TempDir(), LibraryTypePlugin, []CatalogEntry{{
		Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git",
		Repository: "https://github.com/owner/repo.git", Ref: "main",
	}})
	if err == nil {
		t.Fatal("WriteCatalog() error = nil, want immutable ref error")
	}
}

func TestScanLibraryPreservesRemotePluginWithoutLocalMarker(t *testing.T) {
	root := t.TempDir()
	remote := remotePluginCatalogEntry(remotePluginSHA)
	requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{remote}))
	var stdout bytes.Buffer
	requireNoError(t, ScanLibraryType(root, LibraryTypePlugin, &stdout))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{remote}, entries)
	requireEqual(t, "", stdout.String())
}

func TestUpdateRemotePluginRefreshesSHAAndPreservesCuratedMetadata(t *testing.T) {
	root := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Category: "curated", Description: "curated description", Path: "plugin", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA}
	requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{entry}))

	requireNoError(t, UpdateRemotePlugin(root, "plugin", remotePluginRunner(t, refreshedRemotePluginSHA, `{"plugins":[{"name":"plugin","category":"new","description":"new","source":"plugin"}]}`, `{"name":"plugin"}`)))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, "curated", entries[0].Category)
	requireEqual(t, "curated description", entries[0].Description)
	requireEqual(t, refreshedRemotePluginSHA, entries[0].Ref)
}

func TestUpdateRemotePluginRejectsPackagePathChangeWithoutMutation(t *testing.T) {
	root := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA}
	requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{entry}))
	original := readFile(t, filepath.Join(root, "plugins", "catalog.csv"))
	err := UpdateRemotePlugin(root, "plugin", remotePluginRunner(t, refreshedRemotePluginSHA, `{"plugins":[{"name":"plugin","source":"moved"}]}`, `{"name":"plugin"}`))
	if err == nil {
		t.Fatal("UpdateRemotePlugin() error = nil, want path change error")
	}
	requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
}

func TestUpdateRemotePluginFailurePreservesCatalogByteForByte(t *testing.T) {
	tests := []struct {
		name   string
		runner CommandRunner
	}{
		{name: "repository unavailable", runner: func(string, ...string) ([]byte, error) { return nil, errors.New("unavailable") }},
		{name: "malformed marketplace", runner: remotePluginRunner(t, refreshedRemotePluginSHA, "{", "")},
		{name: "invalid manifest", runner: remotePluginRunner(t, refreshedRemotePluginSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, "{")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			entry := CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA}
			requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{entry}))
			original := readFile(t, filepath.Join(root, "plugins", "catalog.csv"))
			if err := UpdateRemotePlugin(root, "plugin", tt.runner); err == nil {
				t.Fatal("UpdateRemotePlugin() error = nil, want update failure")
			}
			requireEqual(t, original, readFile(t, filepath.Join(root, "plugins", "catalog.csv")))
		})
	}
}

func TestRemotePluginIdentityIgnoresRefButExactIdentityIncludesRef(t *testing.T) {
	one := CatalogEntry{Source: "git", Repository: "HTTPS://GITHUB.COM/Owner/Repo.git", Path: "plugin/./child", Ref: remotePluginSHA}
	two := one
	two.Ref = refreshedRemotePluginSHA
	requireEqual(t, stableCatalogGitIdentity(one), stableCatalogGitIdentity(two))
	if exactCatalogGitIdentity(one) == exactCatalogGitIdentity(two) {
		t.Fatal("exactCatalogGitIdentity() ignored ref")
	}
}

func TestPluginCatalogReadsLegacyFourColumnRows(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugins", "catalog.csv")
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	requireNoError(t, os.WriteFile(path, []byte("name,category,path,description\nplugin,tools,plugin/plugin.json,legacy\n"), 0o644))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, "plugin", entries[0].Name)
}

func TestPluginCatalogMigratesLegacyRowsOnlyOnWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugins", "catalog.csv")
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	original := "name,category,path,description\nplugin,tools,plugin/plugin.json,legacy\n"
	requireNoError(t, os.WriteFile(path, []byte(original), 0o644))
	entries, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, original, readFile(t, path))
	requireNoError(t, WriteCatalog(root, LibraryTypePlugin, entries))
	requireContains(t, readFile(t, path), "name,category,path,source,repository,ref,description")
}

func pluginCatalogBytes(t *testing.T, root string) string {
	t.Helper()
	requireNoError(t, WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{{Type: LibraryTypePlugin, Name: "local", Path: "local/plugin.json"}}))
	return readFile(t, filepath.Join(root, "plugins", "catalog.csv"))
}

func remotePluginRunner(t *testing.T, sha, marketplace, manifest string) CommandRunner {
	t.Helper()
	return remotePluginRunnerWithModes(t, sha, marketplace, manifest, "040000", "100644")
}

func remotePluginRunnerWithModes(t *testing.T, sha, marketplace, manifest, sourceMode, manifestMode string) CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/repo.git HEAD":
			return []byte(sha + "\tHEAD\n"), nil
		case strings.Contains(command, " init ") || strings.HasSuffix(command, " init"):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/repo.git"):
			return nil, nil
		case strings.Contains(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, " ls-tree "+sha+" -- .claude-plugin/marketplace.json"):
			return []byte("100644 blob abc\t.claude-plugin/marketplace.json\n"), nil
		case strings.Contains(command, " cat-file -s "+sha+":.claude-plugin/marketplace.json"):
			return []byte(strconv.Itoa(len(marketplace))), nil
		case strings.Contains(command, " show "+sha+":.claude-plugin/marketplace.json"):
			return []byte(marketplace), nil
		case strings.Contains(command, " ls-tree "+sha+" -- ") && strings.HasSuffix(command, "/.claude-plugin/plugin.json"):
			if manifest == "" {
				return nil, errors.New("missing")
			}
			path := command[strings.LastIndex(command, " -- ")+4:]
			return []byte(manifestMode + " blob def\t" + path + "\n"), nil
		case strings.Contains(command, " cat-file -s "+sha+":"):
			return []byte(strconv.Itoa(len(manifest))), nil
		case strings.Contains(command, " ls-tree "+sha+" -- "):
			path := command[strings.LastIndex(command, " -- ")+4:]
			kind := "tree"
			if sourceMode == "120000" {
				kind = "blob"
			}
			return []byte(sourceMode + " " + kind + " def\t" + path + "\n"), nil
		case strings.Contains(command, " show "+sha+":"):
			return []byte(manifest), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}
}

package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
)

const (
	maxRemoteMetadataBytes int64 = 1 << 20
	maxMarketplacePlugins        = 256
	maxRemotePluginName          = 128
)

type pluginMarketplace struct {
	Plugins []marketplacePlugin `json:"plugins"`
}

type marketplacePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
}

func AddRemotePlugin(ctx context.Context, root, repository, name string, runner CommandRunner) error {
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	entry, err := resolveRemotePlugin(boundedCtx, repository, name, runner)
	if err != nil {
		return err
	}
	// Deterministically classify an already-done bounded context as ExitGeneral
	// before entering withRootLocks, rather than letting it fall into ADR-0006
	// lock-acquisition classification (ADR 0007).
	if err := boundedContextError(boundedCtx, entry.Repository); err != nil {
		return err
	}
	return withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
		if err := boundedContextError(ctx, entry.Repository); err != nil {
			return err
		}
		skills, plugins, err := loadTypedPackageCatalogs(root)
		if err != nil {
			return err
		}
		for _, existing := range plugins {
			if existing.Name == entry.Name {
				return NewExitError(ExitGeneral, "error: plugin already exists: "+entry.Name)
			}
			if existing.Source == "git" && stableCatalogGitIdentity(existing) == stableCatalogGitIdentity(entry) {
				return NewExitError(ExitGeneral, "error: remote package already exists: "+existing.Name)
			}
		}
		if err := validateTypedGitCatalogs(skills, append(plugins, entry)); err != nil {
			return err
		}
		return writeCatalogLocked(ctx, held, root, LibraryTypePlugin, append(plugins, entry))
	})
}

func UpdateRemotePlugin(ctx context.Context, root, name string, runner CommandRunner) error {
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	plugins, err := LoadCatalog(root, LibraryTypePlugin)
	if err != nil {
		return err
	}
	for _, entry := range plugins {
		if entry.Name != name {
			continue
		}
		if entry.Source != "git" {
			return NewExitError(ExitGeneral, "error: plugin is not remotely sourced: "+name)
		}
		repository := strings.TrimSuffix(strings.TrimPrefix(entry.Repository, "https://github.com/"), ".git")
		updated, err := resolveRemotePlugin(boundedCtx, repository, name, runner)
		if err != nil {
			return err
		}
		// Deterministically classify an already-done bounded context as
		// ExitGeneral before entering withRootLocks (ADR 0007).
		if err := boundedContextError(boundedCtx, updated.Repository); err != nil {
			return err
		}
		snapshot := entry
		return withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
			emitMutationTestEvent("revalidation:remote-plugin:" + name)
			if err := boundedContextError(ctx, updated.Repository); err != nil {
				return err
			}
			lockedPlugins, err := LoadCatalog(root, LibraryTypePlugin)
			if err != nil {
				return err
			}
			lockedIndex := -1
			for index, locked := range lockedPlugins {
				if locked.Name == name {
					lockedIndex = index
					if !catalogRowSnapshotEqual(snapshot, locked) {
						return concurrentCatalogConflict("plugin", name)
					}
					updated.Category = locked.Category
					updated.Description = locked.Description
					break
				}
			}
			if lockedIndex < 0 {
				return concurrentCatalogConflict("plugin", name)
			}
			if stableCatalogGitIdentity(updated) != stableCatalogGitIdentity(snapshot) {
				return NewExitError(ExitGeneral, "error: remote plugin package path changed: "+snapshot.Path+" -> "+updated.Path)
			}
			if exactCatalogGitIdentity(updated) == exactCatalogGitIdentity(lockedPlugins[lockedIndex]) {
				return nil
			}
			if err := rejectCrossCatalogGitIdentityLocked(ctx, held, root, LibraryTypePlugin, updated); err != nil {
				return err
			}
			lockedPlugins[lockedIndex] = updated
			return writeCatalogLocked(ctx, held, root, LibraryTypePlugin, lockedPlugins)
		})
	}
	return NewExitError(ExitGeneral, "error: unknown plugin: "+name)
}

func resolveRemotePlugin(ctx context.Context, repository, requestedName string, runner CommandRunner) (CatalogEntry, error) {
	snapshot, url, err := openGitSnapshot(ctx, repository, runner)
	if err != nil {
		return CatalogEntry{}, err
	}
	defer snapshot.close()

	data, err := snapshot.regularFile(".claude-plugin/marketplace.json", maxRemoteMetadataBytes)
	if err != nil {
		return CatalogEntry{}, err
	}
	var marketplace pluginMarketplace
	if len(bytes.TrimSpace(data)) == 0 || json.Unmarshal(data, &marketplace) != nil {
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: malformed remote marketplace metadata")
	}
	if len(marketplace.Plugins) > maxMarketplacePlugins {
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: remote marketplace contains too many plugins")
	}
	selected, err := selectMarketplacePlugin(marketplace.Plugins, requestedName)
	if err != nil {
		return CatalogEntry{}, err
	}
	selected.Source, err = validateRemotePluginSource(selected.Source)
	if err != nil {
		return CatalogEntry{}, err
	}
	if err := snapshot.requireTree(selected.Source); err != nil {
		return CatalogEntry{}, err
	}
	manifestPath := selected.Source + "/.claude-plugin/plugin.json"
	manifestData, err := snapshot.regularFile(manifestPath, maxRemoteMetadataBytes)
	if err != nil {
		return CatalogEntry{}, err
	}
	var manifest pluginMetadataFile
	if json.Unmarshal(manifestData, &manifest) != nil {
		return CatalogEntry{}, NewExitError(ExitGeneral, "error: malformed remote plugin manifest: "+manifestPath)
	}
	if manifest.Name != selected.Name {
		return CatalogEntry{}, NewExitError(ExitGeneral, fmt.Sprintf("error: remote plugin manifest name %q does not match marketplace name %q", manifest.Name, selected.Name))
	}
	return CatalogEntry{
		Type: LibraryTypePlugin, Name: selected.Name, Category: selected.Category, Path: selected.Source,
		Source: "git", Repository: url, Ref: snapshot.sha, Description: selected.Description,
	}, nil
}

func selectMarketplacePlugin(plugins []marketplacePlugin, requestedName string) (marketplacePlugin, error) {
	if len(plugins) == 0 {
		return marketplacePlugin{}, NewExitError(ExitGeneral, "error: remote marketplace contains no plugins")
	}
	names := make([]string, 0, len(plugins))
	seen := make(map[string]struct{}, len(plugins))
	for _, plugin := range plugins {
		if !validRemotePluginName(plugin.Name) {
			return marketplacePlugin{}, NewExitError(ExitGeneral, "error: remote marketplace plugin name is invalid")
		}
		if _, ok := seen[plugin.Name]; ok {
			return marketplacePlugin{}, NewExitError(ExitGeneral, "error: duplicate remote marketplace plugin name: "+plugin.Name)
		}
		seen[plugin.Name] = struct{}{}
		names = append(names, plugin.Name)
	}
	sort.Strings(names)
	if requestedName == "" {
		if len(plugins) == 1 {
			return plugins[0], nil
		}
		return marketplacePlugin{}, availablePluginNamesError("plugin name is required", names)
	}
	for _, plugin := range plugins {
		if plugin.Name == requestedName {
			return plugin, nil
		}
	}
	return marketplacePlugin{}, availablePluginNamesError("unknown plugin: "+requestedName, names)
}

func availablePluginNamesError(message string, names []string) error {
	return NewExitError(ExitGeneral, "error: "+message+"; available plugins: "+strings.Join(names, ", "))
}

func validRemotePluginName(name string) bool {
	if name == "" || len(name) > maxRemotePluginName || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func validateRemotePluginSource(source string) (string, error) {
	if source == "" || strings.HasPrefix(source, "/") || strings.Contains(source, ":") || strings.Contains(source, "\\") || strings.ContainsRune(source, '\x00') {
		return "", NewExitError(ExitGeneral, "error: remote plugin source must be a repository-local slash path")
	}
	for _, segment := range strings.Split(source, "/") {
		if segment == ".." {
			return "", NewExitError(ExitGeneral, "error: remote plugin source must not contain ..")
		}
	}
	normalized := path.Clean(source)
	if normalized == "." || strings.HasPrefix(normalized, "../") {
		return "", NewExitError(ExitGeneral, "error: remote plugin source must be a repository-local slash path")
	}
	return normalized, nil
}

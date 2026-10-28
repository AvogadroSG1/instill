package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const (
	pluginMarketplaceSource           = "marketplace"
	pluginMarketplacesDir             = "marketplaces"
	maxMarketplaceMetadataBytes int64 = 1 << 20
	maxMarketplacePlugins             = 256
	maxMarketplaceNameLength          = 128
)

var marketplaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// PluginMarketplaceFormat identifies the publisher format used to discover plugins.
type PluginMarketplaceFormat string

// PluginMarketplaceFormatClaude selects Claude marketplace metadata.
const PluginMarketplaceFormatClaude PluginMarketplaceFormat = "claude"

// PluginMarketplaceFormatCodex selects Codex marketplace metadata.
const PluginMarketplaceFormatCodex PluginMarketplaceFormat = "codex"

// PluginMarketplaceOptions configures one repository-backed marketplace import.
// Format may be empty when the repository contains exactly one supported index.
type PluginMarketplaceOptions struct {
	// LibraryPath is the root of the configured Library.
	LibraryPath string
	// Repository is a GitHub owner/repo identifier.
	Repository string
	// Format selects an index explicitly; empty detects the sole supported index.
	Format PluginMarketplaceFormat
	// Runner executes the Git operations used to resolve the repository snapshot.
	Runner CommandRunner
	// Stdout receives the import report.
	Stdout io.Writer
}

type marketplaceFile struct {
	Name    string                  `json:"name"`
	Plugins []marketplaceFilePlugin `json:"plugins"`
}

type marketplaceFilePlugin struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Source      json.RawMessage `json:"source"`
}

type codexMarketplaceSource struct {
	Type string `json:"source"`
	Path string `json:"path"`
}

type pluginMarketplaceSourceRef struct {
	repository string
	format     PluginMarketplaceFormat
}

// marketplaceDownload is a downloaded marketplace tree staged under the
// library. staging is an absolute path the caller must move or remove.
type marketplaceDownload struct {
	name, url, sha, staging string
	format                  PluginMarketplaceFormat
	entries                 []CatalogEntry
	skipped                 []string
}

// AddPluginMarketplace downloads the default-branch commit of one GitHub plugin
// marketplace and catalogs its installable repository-local plugins.
func AddPluginMarketplace(ctx context.Context, options PluginMarketplaceOptions) error {
	root := options.LibraryPath
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	dl, err := downloadPluginMarketplace(
		boundedCtx,
		root,
		pluginMarketplaceSourceRef{repository: options.Repository, format: options.Format},
		options.Runner,
	)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dl.staging) }()
	// Deterministically classify an already-done bounded context as ExitGeneral
	// before entering withRootLocks (ADR 0007).
	if err := boundedContextError(boundedCtx, dl.url); err != nil {
		return err
	}
	err = withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
		if err := boundedContextError(ctx, dl.url); err != nil {
			return err
		}
		plugins, err := LoadCatalog(root, LibraryTypePlugin)
		if err != nil {
			return err
		}
		if len(marketplaceRows(plugins, dl.name)) > 0 {
			return NewExitError(ExitGeneral, "error: plugin marketplace already exists: "+dl.name+"; run 'instill library update plugin-marketplace "+dl.name+"'")
		}
		if existing := marketplaceRowsForRepository(plugins, dl.url); len(existing) > 0 {
			return NewExitError(ExitGeneral, "error: plugin marketplace repository already exists: "+ownerRepoFromURL(dl.url))
		}
		dest := filepath.Join(root, "plugins", pluginMarketplacesDir, dl.name)
		if _, err := os.Lstat(dest); err == nil {
			return NewExitError(ExitGeneral, "error: plugin marketplace directory already exists: "+dest)
		}
		if err := requireNoPluginNameCollision(plugins, dl.entries); err != nil {
			return err
		}
		if err := os.Rename(dl.staging, dest); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot install plugin marketplace: "+err.Error())
		}
		if err := writeCatalogLocked(ctx, held, root, LibraryTypePlugin, append(plugins, dl.entries...)); err != nil {
			_ = os.RemoveAll(dest)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeMarketplaceReport(options.Stdout, append(dl.skipped, fmt.Sprintf("added plugin marketplace %s from %s at %s: %d plugins", dl.name, ownerRepoFromURL(dl.url), dl.sha[:12], len(dl.entries))))
}

// UpdatePluginMarketplace replaces a downloaded marketplace tree and its catalog
// rows with the latest default-branch commit of the repository it came from.
func UpdatePluginMarketplace(ctx context.Context, root, name string, runner CommandRunner, stdout io.Writer) error {
	plugins, err := LoadCatalog(root, LibraryTypePlugin)
	if err != nil {
		return err
	}
	current := marketplaceRows(plugins, name)
	if len(current) == 0 {
		return NewExitError(ExitGeneral, "error: unknown plugin marketplace: "+name)
	}
	source := pluginMarketplaceSourceRef{
		repository: ownerRepoFromURL(current[0].Repository),
		format:     current[0].MarketplaceFormat,
	}
	boundedCtx, cancel := context.WithTimeout(ctx, remoteGitTimeout)
	defer cancel()
	dl, err := downloadPluginMarketplace(boundedCtx, root, source, runner)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dl.staging) }()
	if dl.name != name {
		return NewExitError(ExitGeneral, "error: plugin marketplace name changed from "+name+" to "+dl.name+"; remove it from the catalog and add it again")
	}
	if err := boundedContextError(boundedCtx, dl.url); err != nil {
		return err
	}
	dest := filepath.Join(root, "plugins", pluginMarketplacesDir, name)
	previous := dl.staging + ".previous"
	err = withRootLocks(boundedCtx, []string{root}, func(ctx context.Context, held *heldLocks) error {
		if err := boundedContextError(ctx, dl.url); err != nil {
			return err
		}
		locked, err := LoadCatalog(root, LibraryTypePlugin)
		if err != nil {
			return err
		}
		lockedCurrent := marketplaceRows(locked, name)
		if len(lockedCurrent) == 0 {
			return concurrentCatalogConflict("plugin marketplace", name)
		}
		for _, row := range lockedCurrent {
			if row.Repository != current[0].Repository || row.MarketplaceFormat != current[0].MarketplaceFormat {
				return concurrentCatalogConflict("plugin marketplace", name)
			}
		}
		others := make([]CatalogEntry, 0, len(locked))
		for _, entry := range locked {
			if entry.Source != pluginMarketplaceSource || entry.Category != name {
				others = append(others, entry)
			}
		}
		if err := requireNoPluginNameCollision(others, dl.entries); err != nil {
			return err
		}
		hadDest := false
		if _, err := os.Lstat(dest); err == nil {
			if err := os.Rename(dest, previous); err != nil {
				return NewExitError(ExitFilesystem, "error: cannot replace plugin marketplace: "+err.Error())
			}
			hadDest = true
		}
		if err := os.Rename(dl.staging, dest); err != nil {
			if hadDest {
				_ = os.Rename(previous, dest)
			}
			return NewExitError(ExitFilesystem, "error: cannot install plugin marketplace: "+err.Error())
		}
		if err := writeCatalogLocked(ctx, held, root, LibraryTypePlugin, append(others, dl.entries...)); err != nil {
			_ = os.RemoveAll(dest)
			if hadDest {
				_ = os.Rename(previous, dest)
			}
			return err
		}
		if hadDest {
			_ = os.RemoveAll(previous)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeMarketplaceReport(stdout, append(dl.skipped, fmt.Sprintf("updated plugin marketplace %s to %s: %d plugins", name, dl.sha[:12], len(dl.entries))))
}

// downloadPluginMarketplace checks out the default-branch commit of repository
// into a staging directory under the library and catalogs its plugins. On
// error the staging directory is removed; on success the caller owns it.
func downloadPluginMarketplace(ctx context.Context, root string, source pluginMarketplaceSourceRef, runner CommandRunner) (*marketplaceDownload, error) {
	if source.format != "" && source.format != PluginMarketplaceFormatClaude && source.format != PluginMarketplaceFormatCodex {
		return nil, NewExitError(ExitGeneral, "error: unsupported plugin marketplace format: "+string(source.format))
	}
	snapshot, url, err := openGitSnapshot(ctx, source.repository, runner)
	if err != nil {
		return nil, err
	}
	defer snapshot.close()

	parent, err := filepath.Abs(filepath.Join(root, "plugins", pluginMarketplacesDir))
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot create plugin marketplace directory: "+err.Error())
	}
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot create plugin marketplace directory: "+err.Error())
	}
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot create plugin marketplace staging directory: "+err.Error())
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := snapshot.checkoutTo(staging); err != nil {
		return nil, err
	}

	format, metadataPath, err := locateMarketplaceMetadata(staging, source.format)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(metadataPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, NewExitError(ExitGeneral, "error: remote marketplace metadata is not a regular file: "+filepath.ToSlash(strings.TrimPrefix(metadataPath, staging+string(filepath.Separator))))
	}
	if info.Size() > maxMarketplaceMetadataBytes {
		return nil, NewExitError(ExitGeneral, "error: remote marketplace metadata exceeds size limit")
	}
	data, err := os.ReadFile(metadataPath) //nolint:gosec // Path is inside the staging directory created above.
	if err != nil {
		return nil, NewExitError(ExitGeneral, "error: malformed remote marketplace metadata")
	}
	var mf marketplaceFile
	if len(bytes.TrimSpace(data)) == 0 || json.Unmarshal(data, &mf) != nil {
		return nil, NewExitError(ExitGeneral, "error: malformed remote marketplace metadata")
	}
	if !validMarketplaceName(mf.Name) {
		return nil, NewExitError(ExitGeneral, "error: remote marketplace name is missing or invalid")
	}
	if len(mf.Plugins) > maxMarketplacePlugins {
		return nil, NewExitError(ExitGeneral, "error: remote marketplace contains too many plugins")
	}
	seen := make(map[string]struct{}, len(mf.Plugins))
	for _, plugin := range mf.Plugins {
		if !validMarketplacePluginName(plugin.Name) {
			return nil, NewExitError(ExitGeneral, "error: remote marketplace plugin name is invalid")
		}
		if _, ok := seen[plugin.Name]; ok {
			return nil, NewExitError(ExitGeneral, "error: duplicate remote marketplace plugin name: "+plugin.Name)
		}
		seen[plugin.Name] = struct{}{}
	}

	stagingReal, err := filepath.EvalSymlinks(staging)
	if err != nil {
		return nil, NewExitError(ExitFilesystem, "error: cannot resolve plugin marketplace staging directory: "+err.Error())
	}
	dl := &marketplaceDownload{name: mf.Name, url: url, sha: snapshot.sha, staging: staging, format: format}
	for _, plugin := range mf.Plugins {
		entry, reason := dl.resolvePlugin(stagingReal, plugin)
		if reason != "" {
			dl.skipped = append(dl.skipped, fmt.Sprintf("skipped: %s (%s)", plugin.Name, reason))
			continue
		}
		dl.entries = append(dl.entries, entry)
	}
	if len(dl.entries) == 0 {
		return nil, NewExitError(ExitGeneral, "error: remote marketplace has no installable plugins")
	}
	committed = true
	return dl, nil
}

// resolvePlugin maps one marketplace plugin to a catalog row, or returns the
// reason it cannot be installed from the marketplace tree.
func (dl *marketplaceDownload) resolvePlugin(stagingReal string, plugin marketplaceFilePlugin) (CatalogEntry, string) {
	source, reason := marketplacePluginSourcePath(plugin.Source, dl.format)
	if reason != "" {
		return CatalogEntry{}, reason
	}
	rel, err := normalizeMarketplaceSource(source)
	if err != nil {
		return CatalogEntry{}, "invalid source path"
	}
	dir := filepath.Join(dl.staging, filepath.FromSlash(rel))
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return CatalogEntry{}, "source not found"
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil || !isInOrUnderDir(stagingReal, realDir) {
		return CatalogEntry{}, "source outside marketplace"
	}
	marker, reason := marketplacePluginManifest(dir, dl.format, plugin.Name)
	if reason != "" {
		return CatalogEntry{}, reason
	}
	markerRel, err := filepath.Rel(dir, marker)
	if err != nil {
		return CatalogEntry{}, "no plugin manifest"
	}
	description := plugin.Description
	if strings.TrimSpace(description) == "" {
		if meta, err := loadPluginMetadata(marker); err == nil {
			description = meta.Description
		}
	}
	return CatalogEntry{
		Type:              LibraryTypePlugin,
		Name:              dl.name + "/" + plugin.Name,
		Category:          dl.name,
		Path:              path.Join(pluginMarketplacesDir, dl.name, rel, filepath.ToSlash(markerRel)),
		Source:            pluginMarketplaceSource,
		Repository:        dl.url,
		Ref:               dl.sha,
		Description:       description,
		MarketplaceFormat: dl.format,
	}, ""
}

func locateMarketplaceMetadata(staging string, requested PluginMarketplaceFormat) (PluginMarketplaceFormat, string, error) {
	type candidate struct {
		format PluginMarketplaceFormat
		path   string
		rel    string
		info   os.FileInfo
	}

	candidates := make([]candidate, 0, 2)
	for _, format := range []PluginMarketplaceFormat{PluginMarketplaceFormatClaude, PluginMarketplaceFormatCodex} {
		rel := marketplaceMetadataPath(format)
		path := filepath.Join(staging, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", NewExitError(ExitFilesystem, "error: cannot inspect remote marketplace metadata: "+err.Error())
		}
		candidates = append(candidates, candidate{format: format, path: path, rel: rel, info: info})
	}

	if requested != "" {
		for _, candidate := range candidates {
			if candidate.format == requested {
				if !candidate.info.Mode().IsRegular() {
					return "", "", NewExitError(ExitGeneral, "error: remote marketplace metadata is not a regular file: "+candidate.rel)
				}
				return candidate.format, candidate.path, nil
			}
		}
		return "", "", NewExitError(ExitGeneral, "error: remote marketplace metadata is missing: "+marketplaceMetadataPath(requested))
	}

	if len(candidates) == 0 {
		return "", "", NewExitError(ExitGeneral, "error: remote marketplace metadata is missing: expected .claude-plugin/marketplace.json or .agents/plugins/marketplace.json")
	}
	if len(candidates) > 1 {
		return "", "", NewExitError(ExitGeneral, "error: remote marketplace format is ambiguous; pass --format claude or --format codex")
	}
	if !candidates[0].info.Mode().IsRegular() {
		return "", "", NewExitError(ExitGeneral, "error: remote marketplace metadata is not a regular file: "+candidates[0].rel)
	}
	return candidates[0].format, candidates[0].path, nil
}

func marketplaceMetadataPath(format PluginMarketplaceFormat) string {
	switch format {
	case PluginMarketplaceFormatClaude:
		return ".claude-plugin/marketplace.json"
	case PluginMarketplaceFormatCodex:
		return ".agents/plugins/marketplace.json"
	default:
		return ""
	}
}

func marketplacePluginSourcePath(raw json.RawMessage, format PluginMarketplaceFormat) (string, string) {
	if len(raw) == 0 {
		return "", "invalid source path"
	}
	switch format {
	case PluginMarketplaceFormatClaude:
		var source string
		if err := json.Unmarshal(raw, &source); err != nil {
			return "", "external source"
		}
		return source, ""
	case PluginMarketplaceFormatCodex:
		var source codexMarketplaceSource
		if err := json.Unmarshal(raw, &source); err != nil || source.Type == "" {
			return "", "invalid source path"
		}
		if source.Type != "local" {
			return "", "external source"
		}
		return source.Path, ""
	default:
		return "", "invalid source path"
	}
}

func marketplacePluginManifest(dir string, format PluginMarketplaceFormat, pluginName string) (string, string) {
	switch format {
	case PluginMarketplaceFormatClaude:
		marker := filepath.Join(dir, ".claude-plugin", "plugin.json")
		info, err := os.Lstat(marker)
		if err != nil || !info.Mode().IsRegular() {
			return "", "no plugin manifest"
		}
		return marker, ""
	case PluginMarketplaceFormatCodex:
		portablePath := filepath.Join(dir, "plugin.json")
		compatPath := filepath.Join(dir, ".codex-plugin", "plugin.json")
		portable, hasPortable, reason := readCodexPluginManifest(portablePath)
		if reason != "" {
			return "", reason
		}
		compat, hasCompat, reason := readCodexPluginManifest(compatPath)
		if reason != "" {
			return "", reason
		}
		if !hasPortable && !hasCompat {
			return "", "no plugin manifest"
		}
		if hasPortable && hasCompat && portable.Name != compat.Name {
			return "", "conflicting plugin manifests"
		}
		manifest := portable
		marker := portablePath
		if !hasPortable {
			manifest = compat
			marker = compatPath
		}
		if manifest.Name != pluginName {
			return "", "plugin manifest name mismatch"
		}
		return marker, ""
	default:
		return "", "no plugin manifest"
	}
}

func readCodexPluginManifest(path string) (pluginMetadataFile, bool, string) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return pluginMetadataFile{}, false, ""
	}
	if err != nil || !info.Mode().IsRegular() {
		return pluginMetadataFile{}, true, "invalid plugin manifest"
	}
	data, err := os.ReadFile(path) //nolint:gosec // Path is inside the checked-out repository staging tree.
	if err != nil {
		return pluginMetadataFile{}, true, "invalid plugin manifest"
	}
	var manifest pluginMetadataFile
	if json.Unmarshal(data, &manifest) != nil || strings.TrimSpace(manifest.Name) == "" {
		return pluginMetadataFile{}, true, "invalid plugin manifest"
	}
	return manifest, true, ""
}
func marketplaceRows(entries []CatalogEntry, name string) []CatalogEntry {
	var rows []CatalogEntry
	for _, entry := range entries {
		if entry.Source == pluginMarketplaceSource && entry.Category == name {
			rows = append(rows, entry)
		}
	}
	return rows
}

func marketplaceRowsForRepository(entries []CatalogEntry, repository string) []CatalogEntry {
	var rows []CatalogEntry
	for _, entry := range entries {
		if entry.Source == pluginMarketplaceSource && entry.Repository == repository {
			rows = append(rows, entry)
		}
	}
	return rows
}

func requireNoPluginNameCollision(existing, entries []CatalogEntry) error {
	names := make(map[string]struct{}, len(existing))
	for _, entry := range existing {
		names[entry.Name] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := names[entry.Name]; ok {
			return NewExitError(ExitGeneral, "error: plugin already exists: "+entry.Name)
		}
	}
	return nil
}

func ownerRepoFromURL(url string) string {
	return strings.TrimSuffix(strings.TrimPrefix(url, "https://github.com/"), ".git")
}

func writeMarketplaceReport(stdout io.Writer, lines []string) error {
	for _, line := range lines {
		if err := writeLine(stdout, line); err != nil {
			return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
	}
	return nil
}

// validMarketplacePluginName reports whether name is usable as a marketplace
// plugin name. Names containing "/" are rejected because the picker derives
// categories from "/" separated names.
func validMarketplacePluginName(name string) bool {
	if name == "" || len(name) > maxMarketplaceNameLength || strings.TrimSpace(name) != name || strings.Contains(name, "/") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func validMarketplaceName(name string) bool {
	return len(name) <= maxMarketplaceNameLength && marketplaceNamePattern.MatchString(name)
}

func normalizeMarketplaceSource(source string) (string, error) {
	if source == "." || source == "./" {
		return ".", nil
	}
	return normalizeRepositoryRelativePath(source)
}

func normalizeRepositoryRelativePath(source string) (string, error) {
	if source == "" || strings.HasPrefix(source, "/") || strings.Contains(source, ":") || strings.Contains(source, "\\") || strings.ContainsRune(source, '\x00') {
		return "", NewExitError(ExitGeneral, "error: marketplace plugin source must be a repository-local slash path")
	}
	for _, segment := range strings.Split(source, "/") {
		if segment == ".." {
			return "", NewExitError(ExitGeneral, "error: marketplace plugin source must not contain ..")
		}
	}
	normalized := path.Clean(source)
	if normalized == "." || strings.HasPrefix(normalized, "../") {
		return "", NewExitError(ExitGeneral, "error: marketplace plugin source must be a repository-local slash path")
	}
	return normalized, nil
}

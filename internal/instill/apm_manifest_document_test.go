package instill

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestMutateAPMPreservesCommentsOnRelocatedLocalDependency(t *testing.T) {
	libraryPath := t.TempDir()
	oldPath := filepath.Join(libraryPath, "skills", "gws-skills", "gws-gmail-read")
	canonicalPath := filepath.Join(libraryPath, "skills", "productivity", "gws-skills", "gws-gmail-read")
	path := writeManifestFixture(t, fmt.Sprintf(`name: project
version: 1.0.0
dependencies:
  apm:
    - &relocated '%s' # relocated skill
    - owner/remote#main
`, oldPath))

	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	sequence, err := document.dependencySequence("apm", false)
	requireNoError(t, err)
	original := sequence.Content[0]
	wanted := localDependencies(canonicalPath)
	ownership := ownershipForDependencies(wanted, []string{filepath.Join(libraryPath, "skills")})
	requireNoError(t, document.mutateAPM(wanted, ownership, map[string]string{oldPath: canonicalPath}))

	if sequence.Content[0] != original {
		t.Fatal("relocated local dependency node was replaced")
	}
	requireEqual(t, canonicalPath, original.Value)
	requireEqual(t, yaml.SingleQuotedStyle, original.Style)
	requireEqual(t, "relocated", original.Anchor)
	requireEqual(t, "# relocated skill", original.LineComment)
	requireEqual(t, "owner/remote#main", sequence.Content[1].Value)
	requireNoError(t, document.write())

	after := mustManifestNode(t, path)
	root, _ := apmManifestMapping(after)
	item := mappingValue(mappingValue(root, "dependencies"), "apm").Content[0]
	requireEqual(t, canonicalPath, item.Value)
	requireEqual(t, yaml.SingleQuotedStyle, item.Style)
	requireEqual(t, "relocated", item.Anchor)
	requireEqual(t, "# relocated skill", item.LineComment)
}

func TestMutateAPMRelocatesRootLocalDependency(t *testing.T) {
	libraryPath := t.TempDir()
	oldPath := filepath.Join(libraryPath, "skills")
	canonicalPath := filepath.Join(libraryPath, "skills", "productivity", "root-skill")
	path := writeManifestFixture(t, fmt.Sprintf("dependencies:\n  apm: [%q]\n", oldPath))

	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	wanted := localDependencies(canonicalPath)
	ownership := ownershipForDependencies(wanted, []string{filepath.Join(libraryPath, "skills")})
	requireNoError(t, document.mutateAPM(wanted, ownership, map[string]string{oldPath: canonicalPath}))

	sequence, err := document.dependencySequence("apm", false)
	requireNoError(t, err)
	requireEqual(t, 1, len(sequence.Content))
	requireEqual(t, canonicalPath, sequence.Content[0].Value)
}

func TestManifestMutationPreservesUnknownTopLevelAndDependencyNodes(t *testing.T) {
	path := writeManifestFixture(t, `name: project
version: 1.0.0
x-flow: {tagged: !custom value, list: [one, two]}
dependencies:
  lsp: [{name: gopls, x: true}]
  apm:
    - owner/remote#main
    - {marketplace: private, name: plugin, x: keep}
  mcp:
    - io.example/server
`)
	before := mustManifestNode(t, path)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	requireNoError(t, document.repairIdentity(filepath.Dir(path), false))
	requireNoError(t, document.write())

	after := mustManifestNode(t, path)
	assertNodeGraphEqualExcept(t, before, after, map[string]struct{}{"targets": {}})
	root, _ := apmManifestMapping(after)
	requireNodeValue(t, mappingValue(root, "x-flow"), "tagged", "value")
	dependencies := mappingValue(root, "dependencies")
	if mappingValue(dependencies, "lsp") == nil || len(mappingValue(dependencies, "apm").Content) != 2 || len(mappingValue(dependencies, "mcp").Content) != 1 {
		t.Fatalf("unknown dependency nodes were not preserved: %s", readFile(t, path))
	}
}

func TestManifestMutationPreservesCommentsStylesAnchorsAndAliases(t *testing.T) {
	path := writeManifestFixture(t, `# head
name: 'project' # identity
version: 1.0.0
x-shared: &shared {quoted: "value"} # anchor line
x-alias: *shared
dependencies: {apm: [], mcp: []} # flow
`)
	before := mustManifestNode(t, path)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"claude"}, false))
	requireNoError(t, document.write())
	after := mustManifestNode(t, path)

	beforeRoot, _ := apmManifestMapping(before)
	afterRoot, _ := apmManifestMapping(after)
	for _, key := range []string{"name", "x-shared", "x-alias", "dependencies"} {
		assertNodeSemantics(t, mappingValue(beforeRoot, key), mappingValue(afterRoot, key))
	}
	if mappingValue(afterRoot, "x-alias").Alias != mappingValue(afterRoot, "x-shared") {
		t.Fatal("alias target was not retained")
	}
}

func TestManifestMutationFailsWhenAnchoredNodeCannotBeSafelyChanged(t *testing.T) {
	path := writeManifestFixture(t, `name: project
version: 1.0.0
dependencies:
  apm:
    - &package {git: https://example.test/repo.git, path: skill, ref: old}
x-copy: *package
`)
	before := readFile(t, path)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	wanted := []APMDependency{{Git: &GitDependency{Repository: "https://example.test/repo.git", Path: "skill", Ref: "new"}}}
	err = document.mutateAPM(wanted, ownershipForDependencies(wanted, nil), nil)
	if err == nil {
		t.Fatal("mutateAPM() error = nil, want cross-boundary alias failure")
	}
	for _, text := range []string{"dependencies.apm.ref", "package", "update Git ref", "explicit value"} {
		requireContains(t, ErrorMessage(err), text)
	}
	requireEqual(t, before, readFile(t, path))
}

func TestManifestProjectionDoesNotControlSerialization(t *testing.T) {
	path := writeManifestFixture(t, "name: project\nversion: 1.0.0\nx-owned-by-user: {keep: true}\ndependencies: {apm: [], mcp: []}\n")
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	document.projection = APMManifest{}
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	requireNoError(t, document.write())
	requireContains(t, readFile(t, path), "x-owned-by-user:")
}

func TestManifestMutationReusesMatchingGitNode(t *testing.T) {
	path := writeManifestFixture(t, `name: project
version: 1.0.0
dependencies:
  apm:
    - &package
      git: https://example.test/repo.git
      path: skill
      ref: 'old' # package
      x-owner: user
`)
	before := mustManifestNode(t, path)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	sequence, _ := document.dependencySequence("apm", false)
	original := sequence.Content[0]
	wanted := []APMDependency{{Git: &GitDependency{Repository: "https://example.test/repo.git", Path: "skill", Ref: "new"}}}
	requireNoError(t, document.mutateAPM(wanted, ownershipForDependencies(wanted, nil), nil))
	if sequence.Content[0] != original {
		t.Fatal("matching Git dependency node was replaced")
	}
	requireEqual(t, "user", mappingValue(original, "x-owner").Value)
	requireEqual(t, "package", original.Anchor)
	requireEqual(t, yaml.SingleQuotedStyle, mappingValue(original, "ref").Style)
	requireNoError(t, document.write())
	after := mustManifestNode(t, path)
	assertNodeGraphEqualExcept(t, before, after, map[string]struct{}{"dependencies.apm[0].ref": {}})
	afterRoot, _ := apmManifestMapping(after)
	requireEqual(t, "package", mappingValue(mappingValue(afterRoot, "dependencies"), "apm").Content[0].Anchor)
}

func TestManifestMutationReusesMatchingMCPNode(t *testing.T) {
	path := writeManifestFixture(t, "name: project\nversion: 1.0.0\ndependencies:\n  mcp:\n    - {name: server, registry: true, command: old, x-owner: user}\n")
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	sequence, _ := document.dependencySequence("mcp", false)
	original := sequence.Content[0]
	wanted := []MCPDependency{{Name: "server", Registry: false, Command: "new"}}
	requireNoError(t, document.mutateMCP(wanted, dependencyNames(wanted)))
	if sequence.Content[0] != original {
		t.Fatal("matching MCP dependency node was replaced")
	}
	requireEqual(t, "user", mappingValue(original, "x-owner").Value)
	requireEqual(t, "false", mappingValue(original, "registry").Value)
	requireNoError(t, document.write())
	after := mustManifestNode(t, path)
	root, _ := apmManifestMapping(after)
	item := mappingValue(mappingValue(root, "dependencies"), "mcp").Content[0]
	requireEqual(t, "new", mappingValue(item, "command").Value)
	requireEqual(t, "false", mappingValue(item, "registry").Value)
	requireEqual(t, "user", mappingValue(item, "x-owner").Value)
}

func TestManifestMCPRegistryOwnership(t *testing.T) {
	path := writeManifestFixture(t, `name: project
version: 1.0.0
dependencies:
  mcp:
    - {name: owned-absent}
    - {name: owned-true, registry: true}
    - {name: unmatched, registry: true}
    - {name: unmatched-absent}
    - io.example/opaque
`)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	wanted := []MCPDependency{{Name: "owned-absent"}, {Name: "owned-true"}, {Name: "unmatched", Registry: true}, {Name: "unmatched-absent"}}
	requireNoError(t, document.mutateMCP(wanted, map[string]struct{}{"owned-absent": {}, "owned-true": {}}))
	sequence, _ := document.dependencySequence("mcp", false)
	requireEqual(t, "false", mappingValue(sequence.Content[0], "registry").Value)
	requireEqual(t, "false", mappingValue(sequence.Content[1], "registry").Value)
	requireEqual(t, "true", mappingValue(sequence.Content[2], "registry").Value)
	if mappingValue(sequence.Content[3], "registry") != nil {
		t.Fatal("unmatched dependency gained registry field")
	}
	requireNoError(t, document.write())
	after := mustManifestNode(t, path)
	root, _ := apmManifestMapping(after)
	sequence = mappingValue(mappingValue(root, "dependencies"), "mcp")
	requireEqual(t, "false", mappingValue(sequence.Content[0], "registry").Value)
	requireEqual(t, "false", mappingValue(sequence.Content[1], "registry").Value)
	requireEqual(t, "true", mappingValue(sequence.Content[2], "registry").Value)
	if mappingValue(sequence.Content[3], "registry") != nil {
		t.Fatal("unmatched dependency gained registry field after reparse")
	}
}

func TestManifestMutationPreservesOpaqueUnsupportedDependencies(t *testing.T) {
	path := writeManifestFixture(t, "name: project\nversion: 1.0.0\ndependencies:\n  apm: [owner/repo#main, {registry: private, id: package}]\n  mcp: [io.example/server, {x-custom: true}]\n")
	before := mustManifestNode(t, path)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	requireNoError(t, document.write())
	after := mustManifestNode(t, path)
	beforeRoot, _ := apmManifestMapping(before)
	afterRoot, _ := apmManifestMapping(after)
	assertNodeSemantics(t, mappingValue(mappingValue(beforeRoot, "dependencies"), "apm"), mappingValue(mappingValue(afterRoot, "dependencies"), "apm"))
	assertNodeSemantics(t, mappingValue(mappingValue(beforeRoot, "dependencies"), "mcp"), mappingValue(mappingValue(afterRoot, "dependencies"), "mcp"))
}

func TestManifestMutationNeverOwnsTaggedOrRemoteShorthandScalars(t *testing.T) {
	path := writeManifestFixture(t, `name: project
version: 1.0.0
dependencies:
  apm:
    - !custom /library/skills/tagged
    - owner/repository#main
    - /library/skills/owned
`)
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	ownership := ownershipForDependencies(nil, []string{"/library/skills"})
	requireNoError(t, document.mutateAPM(nil, ownership, nil))
	requireNoError(t, document.write())

	after := mustManifestNode(t, path)
	root, _ := apmManifestMapping(after)
	sequence := mappingValue(mappingValue(root, "dependencies"), "apm")
	if len(sequence.Content) != 2 {
		t.Fatalf("dependencies.apm length = %d, want tagged and remote opaque entries", len(sequence.Content))
	}
	requireEqual(t, "!custom", sequence.Content[0].Tag)
	requireEqual(t, "/library/skills/tagged", sequence.Content[0].Value)
	requireEqual(t, "owner/repository#main", sequence.Content[1].Value)
}

func TestManifestClassifiesSupportedAndOpaqueDependencyShapes(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		supported bool
		malformed bool
	}{
		{name: "local scalar", yaml: "/library/skills/one", supported: true},
		{name: "remote shorthand scalar", yaml: "owner/repo#main"},
		{name: "tagged local-looking scalar", yaml: "!custom /library/skills/one"},
		{name: "Git without ref", yaml: "{git: repo, path: skill}", supported: true},
		{name: "marketplace mapping", yaml: "{marketplace: store, name: one}"},
		{name: "malformed Git", yaml: "{git: repo}", malformed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var node yaml.Node
			requireNoError(t, yaml.Unmarshal([]byte(test.yaml), &node))
			ownership := ownershipForDependencies(nil, []string{"/library/skills"})
			_, supported, malformed := classifyAPMNode(node.Content[0], ownership)
			requireEqual(t, test.supported, supported)
			requireEqual(t, test.malformed, malformed)
		})
	}

	ownership := ownershipForDependencies(nil, []string{"/library/skills"})
	if !ownership.ownsLocal("/library/skills/one") {
		t.Fatal("catalog-root local path was not classified as owned")
	}
	if ownership.ownsLocal("owner/repo#main") {
		t.Fatal("remote shorthand scalar was classified as owned")
	}

	mcpTests := []struct {
		name      string
		yaml      string
		supported bool
	}{
		{name: "supported mapping", yaml: "{name: server}", supported: true},
		{name: "shorthand scalar", yaml: "io.example/server"},
		{name: "opaque mapping", yaml: "{registry: custom}"},
		{name: "empty name", yaml: "{name: ''}"},
	}
	for _, test := range mcpTests {
		t.Run("MCP "+test.name, func(t *testing.T) {
			var node yaml.Node
			requireNoError(t, yaml.Unmarshal([]byte(test.yaml), &node))
			_, supported := projectMCPDependency(node.Content[0])
			requireEqual(t, test.supported, supported)
		})
	}
}

func TestManifestMutationRejectsAmbiguousSupportedIdentityWithoutWrite(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		mcp  bool
	}{
		{name: "duplicate Git", yaml: "name: p\nversion: 1.0.0\ndependencies:\n  apm: [{git: repo, path: one}, {git: repo, path: one}]\n"},
		{name: "malformed Git", yaml: "name: p\nversion: 1.0.0\ndependencies:\n  apm: [{git: repo}]\n"},
		{name: "duplicate MCP", yaml: "name: p\nversion: 1.0.0\ndependencies:\n  mcp: [{name: one}, {name: one}]\n", mcp: true},
		{name: "malformed MCP", yaml: "name: p\nversion: 1.0.0\ndependencies:\n  mcp: [{name: []}]\n", mcp: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeManifestFixture(t, test.yaml)
			before := readFile(t, path)
			document, err := loadManifestDocument(path)
			requireNoError(t, err)
			if test.mcp {
				err = document.mutateMCP(nil, map[string]struct{}{})
			} else {
				err = document.mutateAPM(nil, ownershipForDependencies(nil, nil), nil)
			}
			if err == nil {
				t.Fatal("mutation error = nil, want ambiguity error")
			}
			requireEqual(t, before, readFile(t, path))
		})
	}
}

func TestManifestAliasSafetyUsesOwnedSubtreeBoundary(t *testing.T) {
	t.Run("internal alias permits scalar mutation", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n    - {name: one, command: &cmd old, x-copy: *cmd}\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		requireNoError(t, document.mutateMCP([]MCPDependency{{Name: "one", Command: "new"}}, map[string]struct{}{"one": {}}))
		requireNoError(t, document.write())
		after := mustManifestNode(t, path)
		root, _ := apmManifestMapping(after)
		item := mappingValue(mappingValue(root, "dependencies"), "mcp").Content[0]
		if mappingValue(item, "x-copy").Alias != mappingValue(item, "command") {
			t.Fatal("internal alias did not target the updated anchored scalar after reparse")
		}
		requireEqual(t, "new", mappingValue(item, "x-copy").Alias.Value)
	})
	t.Run("anchored ancestor with external alias fails", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies: &deps\n  apm:\n    - {git: repo, path: skill, ref: old}\nx-dependencies: *deps\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		wanted := []APMDependency{{Git: &GitDependency{Repository: "repo", Path: "skill", Ref: "new"}}}
		err = document.mutateAPM(wanted, ownershipForDependencies(wanted, nil), nil)
		if err == nil {
			t.Fatal("mutation error = nil, want ancestor anchor failure")
		}
		requireContains(t, ErrorMessage(err), "deps")
	})
	t.Run("anchored mapping key with external alias fails", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies:\n  apm:\n    - {git: repo, path: skill, &refkey ref: old}\nx-key: *refkey\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		wanted := []APMDependency{{Git: &GitDependency{Repository: "repo", Path: "skill", Ref: "new"}}}
		err = document.mutateAPM(wanted, ownershipForDependencies(wanted, nil), nil)
		if err == nil {
			t.Fatal("mutation error = nil, want anchored key failure")
		}
		requireContains(t, ErrorMessage(err), "refkey")
	})
	t.Run("missing field addition checks anchored mapping ancestors", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n    - &server {name: one, x-owner: user}\nx-server: *server\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		err = document.mutateMCP([]MCPDependency{{Name: "one"}}, map[string]struct{}{"one": {}})
		if err == nil {
			t.Fatal("mutation error = nil, want missing-field ancestor anchor failure")
		}
		requireContains(t, ErrorMessage(err), "server")
	})
	t.Run("field removal checks anchored key", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n    - {name: one, &commandkey command: old}\nx-key: *commandkey\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		err = document.mutateMCP([]MCPDependency{{Name: "one"}}, map[string]struct{}{"one": {}})
		if err == nil {
			t.Fatal("mutation error = nil, want anchored removal key failure")
		}
		requireContains(t, ErrorMessage(err), "commandkey")
	})
	t.Run("direct alias fails", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\nx-command: &cmd old\ndependencies:\n  mcp:\n    - {name: one, command: *cmd}\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		err = document.mutateMCP([]MCPDependency{{Name: "one", Command: "new"}}, map[string]struct{}{"one": {}})
		if err == nil {
			t.Fatal("mutation error = nil, want direct alias failure")
		}
	})
	t.Run("anchored removal with a reference fails", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n    - {name: one, command: &cmd old, x-copy: *cmd}\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		err = document.mutateMCP(nil, map[string]struct{}{"one": {}})
		if err == nil {
			t.Fatal("mutation error = nil, want anchored removal failure")
		}
	})
	t.Run("untouched external anchor survives", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\nx-value: &value keep\nx-copy: *value\n")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		requireNoError(t, document.setTargets([]string{"codex"}, false))
		requireNoError(t, document.write())
		after := mustManifestNode(t, path)
		root, _ := apmManifestMapping(after)
		if mappingValue(root, "x-copy").Alias != mappingValue(root, "x-value") {
			t.Fatal("untouched external alias target changed")
		}
	})
}

func TestManifestMutationRepairsMissingIdentityInSameWrite(t *testing.T) {
	path := writeManifestFixture(t, "dependencies: {apm: [], mcp: []}\n")
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	requireNoError(t, document.repairIdentity(filepath.Dir(path), false))
	requireNoError(t, document.write())
	manifest, err := ReadAPMManifest(path)
	requireNoError(t, err)
	requireEqual(t, filepath.Base(filepath.Dir(path)), manifest.Name)
	requireEqual(t, "0.1.0", manifest.Version)
}

func TestManifestMutationRejectsInvalidPresentIdentity(t *testing.T) {
	for _, value := range []string{"''", "null", "[]"} {
		t.Run(value, func(t *testing.T) {
			path := writeManifestFixture(t, "name: "+value+"\nversion: 1.0.0\n")
			document, err := loadManifestDocument(path)
			requireNoError(t, err)
			err = document.repairIdentity(filepath.Dir(path), false)
			if err == nil {
				t.Fatal("repairIdentity() error = nil, want invalid identity error")
			}
		})
	}
}

func TestManifestTargetOwnershipAndExplicitEmptySemantics(t *testing.T) {
	t.Run("sync initializes absent plural targets", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{})
		root := t.TempDir()
		requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
		path := ProjectAPMPath(root)
		requireNoError(t, os.WriteFile(path, []byte("name: p\nversion: 1.0.0\ndependencies: {apm: [], mcp: []}\n"), 0o644))
		project := Project{Root: root, ManifestPath: path}
		requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard()}))
		manifest, err := ReadAPMManifest(path)
		requireNoError(t, err)
		requireEqual(t, []string{"codex"}, manifest.Targets)
	})
	t.Run("sync preserves explicit empty plural targets", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{})
		root := t.TempDir()
		requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
		path := ProjectAPMPath(root)
		original := "name: p\nversion: 1.0.0\ntargets: []\ndependencies: {apm: [], mcp: []}\n"
		requireNoError(t, os.WriteFile(path, []byte(original), 0o644))
		project := Project{Root: root, ManifestPath: path}
		assertManifestUnchangedBy(t, path, func() error {
			return SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard()})
		})
	})
	t.Run("singular target is preserved while absent plural initializes", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{})
		root := t.TempDir()
		requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
		path := ProjectAPMPath(root)
		requireNoError(t, os.WriteFile(path, []byte("name: p\nversion: 1.0.0\ntarget: claude\ndependencies: {apm: [], mcp: []}\n"), 0o644))
		project := Project{Root: root, ManifestPath: path}
		requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard()}))
		node := mustManifestNode(t, path)
		mapping, _ := apmManifestMapping(node)
		requireEqual(t, "claude", mappingValue(mapping, "target").Value)
		manifest, err := ReadAPMManifest(path)
		requireNoError(t, err)
		requireEqual(t, []string{"codex"}, manifest.Targets)
	})
	t.Run("both singular and explicit empty plural preserve plural", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{})
		root := t.TempDir()
		requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
		path := ProjectAPMPath(root)
		original := "name: p\nversion: 1.0.0\ntarget: claude\ntargets: []\ndependencies: {apm: [], mcp: []}\n"
		requireNoError(t, os.WriteFile(path, []byte(original), 0o644))
		project := Project{Root: root, ManifestPath: path}
		assertManifestUnchangedBy(t, path, func() error {
			return SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard()})
		})
	})
	t.Run("SetProjectTargets writes explicit empty plural", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ntarget: claude\ntargets: [codex]\n")
		project := Project{Root: filepath.Dir(path), ManifestPath: path}
		requireNoError(t, SetProjectTargets(SetTargetsOptions{Project: project, Targets: []string{}}))
		node := mustManifestNode(t, path)
		mapping, _ := apmManifestMapping(node)
		requireEqual(t, "claude", mappingValue(mapping, "target").Value)
		requireEqual(t, yaml.SequenceNode, mappingValue(mapping, "targets").Kind)
		requireEqual(t, 0, len(mappingValue(mapping, "targets").Content))
	})
}

func TestManifestNoOpAndIdentityRepairTruthTable(t *testing.T) {
	t.Run("unchanged targets with identity do not write", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\ntargets: [codex]\n")
		project := Project{Root: filepath.Dir(path), ManifestPath: path}
		assertManifestUnchangedBy(t, path, func() error {
			return SetProjectTargets(SetTargetsOptions{Project: project, Targets: []string{"codex"}})
		})
	})
	t.Run("unchanged targets with missing identity repair", func(t *testing.T) {
		path := writeManifestFixture(t, "targets: [codex]\n")
		project := Project{Root: filepath.Dir(path), ManifestPath: path}
		requireNoError(t, SetProjectTargets(SetTargetsOptions{Project: project, Targets: []string{"codex"}}))
		requireContains(t, readFile(t, path), "version: 0.1.0")
	})
	t.Run("repeated package add and absent remove do not write", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "docker", Path: "docker/SKILL.md"}}})
		dependency := filepath.Join(library, "skills", "docker")
		project := createAPMProject(t, APMManifest{Name: "project", Version: "1.0.0", Dependencies: APMDependencies{APM: localDependencies(dependency)}})
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{"docker"}, Runner: recordingRunner(nil, nil)})
		})
		requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Remove: []string{"docker"}, Runner: recordingRunner(nil, nil)}))
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Remove: []string{"docker"}, Runner: recordingRunner(nil, nil)})
		})
	})
	t.Run("repeated package add repairs missing identity", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "docker", Path: "docker/SKILL.md"}}})
		path := writeManifestFixture(t, "dependencies:\n  apm:\n    - "+filepath.Join(library, "skills", "docker")+"\n")
		project := Project{Root: filepath.Dir(path), ManifestPath: path}
		metrics := &manifestIOMetrics{}
		assertSingleManifestRepairBy(t, path, metrics, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{"docker"}, Runner: recordingRunner(nil, nil), manifestMetrics: metrics})
		})
	})
	t.Run("Plugin repeated add and remove", func(t *testing.T) {
		entry := CatalogEntry{Type: LibraryTypePlugin, Name: "example", Path: "example/.claude-plugin/plugin.json"}
		library := createCatalogLibrary(t, catalogLibrarySeed{plugins: []CatalogEntry{entry}})
		dependency := pluginDependencyFromCatalog(library, entry)
		project := createAPMProject(t, APMManifest{Name: "project", Version: "1.0.0", Dependencies: APMDependencies{APM: []APMDependency{dependency}}})
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Add: []string{"example"}, Runner: recordingRunner(nil, nil)})
		})
		requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Remove: []string{"example"}, Runner: recordingRunner(nil, nil)}))
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Remove: []string{"example"}, Runner: recordingRunner(nil, nil)})
		})
	})
	t.Run("repeated MCP add does not write and missing identity repairs", func(t *testing.T) {
		entry := CatalogEntry{Type: LibraryTypeMCP, Name: "server", Transport: "stdio", Command: "server-mcp"}
		library := createCatalogLibrary(t, catalogLibrarySeed{mcp: []CatalogEntry{entry}})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "1.0.0", Dependencies: APMDependencies{MCP: []MCPDependency{mcpDependencyFromCatalog(entry)}}})
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeMCP, Add: []string{"server"}, Runner: recordingRunner(nil, nil)})
		})
		path := writeManifestFixture(t, "dependencies:\n  mcp:\n    - {name: server, transport: stdio, registry: false, command: server-mcp}\n")
		missingIdentity := Project{Root: filepath.Dir(path), ManifestPath: path}
		metrics := &manifestIOMetrics{}
		assertSingleManifestRepairBy(t, path, metrics, func() error {
			return Pick(PickOptions{Project: missingIdentity, LibraryPath: library, Type: LibraryTypeMCP, Add: []string{"server"}, Runner: recordingRunner(nil, nil), manifestMetrics: metrics})
		})
	})
	t.Run("MCP absent and repeated remove", func(t *testing.T) {
		entry := CatalogEntry{Type: LibraryTypeMCP, Name: "server", Transport: "stdio", Command: "server-mcp"}
		library := createCatalogLibrary(t, catalogLibrarySeed{mcp: []CatalogEntry{entry}})
		project := createAPMProject(t, APMManifest{Name: "project", Version: "1.0.0"})
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeMCP, Remove: []string{"server"}, Runner: recordingRunner(nil, nil)})
		})
		requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeMCP, Add: []string{"server"}, Runner: recordingRunner(nil, nil)}))
		requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeMCP, Remove: []string{"server"}, Runner: recordingRunner(nil, nil)}))
		assertManifestUnchangedBy(t, project.ManifestPath, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeMCP, Remove: []string{"server"}, Runner: recordingRunner(nil, nil)})
		})
	})
	t.Run("absent removal with missing identity repairs", func(t *testing.T) {
		library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "docker", Path: "docker/SKILL.md"}}})
		path := writeManifestFixture(t, "dependencies: {apm: [], mcp: []}\n")
		project := Project{Root: filepath.Dir(path), ManifestPath: path}
		metrics := &manifestIOMetrics{}
		assertSingleManifestRepairBy(t, path, metrics, func() error {
			return Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Remove: []string{"docker"}, Runner: recordingRunner(nil, nil), manifestMetrics: metrics})
		})
	})
	t.Run("already merged old import does not write and missing identity repairs", func(t *testing.T) {
		for _, identity := range []bool{true, false} {
			t.Run(fmt.Sprintf("identity=%t", identity), func(t *testing.T) {
				library := createTypedLibrary(t)
				root := t.TempDir()
				requireNoError(t, os.MkdirAll(filepath.Dir(ProjectManifestPath(root)), 0o755))
				requireNoError(t, WriteManifestAtomic(ProjectManifestPath(root), Manifest{Skills: []string{"cloud/azure/azure-cli"}}))
				dependency := filepath.Join(library, "skills", "cloud", "azure", "azure-cli")
				prefix := ""
				if identity {
					prefix = "name: project\nversion: 1.0.0\n"
				}
				path := ProjectAPMPath(root)
				requireNoError(t, os.WriteFile(path, []byte(prefix+"dependencies:\n  apm:\n    - "+dependency+"\n"), 0o644))
				project := Project{Root: root, ManifestPath: path}
				metrics := &manifestIOMetrics{}
				operation := func() error {
					return ImportOldInstill(ImportOptions{Project: project, LibraryPath: library, manifestMetrics: metrics})
				}
				if identity {
					assertManifestUnchangedBy(t, path, operation)
					requireEqual(t, 0, metrics.rawDigestRereads)
					requireEqual(t, 0, metrics.atomicReplacements)
				} else {
					assertSingleManifestRepairBy(t, path, metrics, operation)
				}
			})
		}
	})
	t.Run("already merged Graft import with and without identity", func(t *testing.T) {
		for _, identity := range []bool{true, false} {
			t.Run(fmt.Sprintf("identity=%t", identity), func(t *testing.T) {
				library := t.TempDir()
				root := t.TempDir()
				requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - server\n"), 0o644))
				requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"server":{"command":"server-mcp"}}}`), 0o644))
				prefix := ""
				if identity {
					prefix = "name: project\nversion: 1.0.0\n"
				}
				path := ProjectAPMPath(root)
				requireNoError(t, os.WriteFile(path, []byte(prefix+"dependencies:\n  mcp:\n    - {name: server, transport: stdio, registry: false, command: server-mcp}\n"), 0o644))
				project := Project{Root: root, ManifestPath: path}
				metrics := &manifestIOMetrics{}
				operation := func() error {
					return ImportGraft(ImportOptions{Project: project, LibraryPath: library, manifestMetrics: metrics})
				}
				if identity {
					assertManifestUnchangedBy(t, path, operation)
					requireEqual(t, 0, metrics.rawDigestRereads)
					requireEqual(t, 0, metrics.atomicReplacements)
				} else {
					assertSingleManifestRepairBy(t, path, metrics, operation)
				}
			})
		}
	})
}

func TestInstructionAndPromptOperationsDoNotWriteManifest(t *testing.T) {
	for _, typ := range []LibraryType{LibraryTypeInstruction, LibraryTypePrompt} {
		t.Run(string(typ), func(t *testing.T) {
			entry := CatalogEntry{Type: typ, Name: "content"}
			seed := catalogLibrarySeed{}
			if typ == LibraryTypeInstruction {
				entry.Path = "content/INSTRUCTION.md"
				seed.instructions = []CatalogEntry{entry}
			} else {
				entry.Path = "content/PROMPT.md"
				seed.prompts = []CatalogEntry{entry}
			}
			library := createCatalogLibrary(t, seed)
			path := writeManifestFixture(t, "dependencies: {}\n")
			project := Project{Root: filepath.Dir(path), ManifestPath: path}
			assertManifestUnchangedBy(t, path, func() error {
				return Pick(PickOptions{Project: project, LibraryPath: library, Type: typ, Add: []string{"content"}, Runner: recordingRunner(nil, nil)})
			})
			assertManifestUnchangedBy(t, path, func() error {
				return ProjectStatus(StatusOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard()})
			})
			assertManifestUnchangedBy(t, path, func() error {
				return Pick(PickOptions{Project: project, LibraryPath: library, Type: typ, Remove: []string{"content"}, Runner: recordingRunner(nil, nil)})
			})
		})
	}
}

func assertManifestUnchangedBy(t *testing.T, path string, operation func() error) {
	t.Helper()
	beforeBytes := readFile(t, path)
	beforeInfo, err := os.Stat(path)
	requireNoError(t, err)
	time.Sleep(10 * time.Millisecond)
	requireNoError(t, operation())
	afterInfo, err := os.Stat(path)
	requireNoError(t, err)
	requireEqual(t, beforeBytes, readFile(t, path))
	requireEqual(t, beforeInfo.ModTime(), afterInfo.ModTime())
}

func assertSingleManifestRepairBy(t *testing.T, path string, metrics *manifestIOMetrics, operation func() error) {
	t.Helper()
	beforeBytes := readFile(t, path)
	beforeInfo, err := os.Stat(path)
	requireNoError(t, err)
	time.Sleep(10 * time.Millisecond)
	requireNoError(t, operation())
	afterInfo, err := os.Stat(path)
	requireNoError(t, err)
	if beforeBytes == readFile(t, path) {
		t.Fatal("manifest bytes unchanged, want identity repair")
	}
	if beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("manifest modification time unchanged, want one repair write")
	}
	requireEqual(t, 1, metrics.rawDigestRereads)
	requireEqual(t, 1, metrics.atomicReplacements)
	requireContains(t, readFile(t, path), "version: 0.1.0")
}

func TestManifestWritePreservesOriginalFileMode(t *testing.T) {
	path := writeManifestFixture(t, "name: p\nversion: 1.0.0\n")
	requireNoError(t, os.Chmod(path, 0o600))
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	requireNoError(t, document.write())
	info, err := os.Stat(path)
	requireNoError(t, err)
	requireEqual(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestManifestNewFileModeHonorsProcessUmask(t *testing.T) {
	const helperEnv = "INSTILL_UMASK_TEST_HELPER"
	if os.Getenv(helperEnv) == "1" {
		if !setTestUmask(0o077) {
			return
		}
		path := os.Getenv("INSTILL_UMASK_TEST_PATH")
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		requireNoError(t, document.setTargets([]string{"codex"}, false))
		requireNoError(t, document.repairIdentity(filepath.Dir(path), false))
		requireNoError(t, document.write())
		return
	}

	path := filepath.Join(t.TempDir(), "apm.yml")
	command := exec.Command(os.Args[0], "-test.run=^TestManifestNewFileModeHonorsProcessUmask$") //nolint:gosec // The isolated test process is the current test binary.
	command.Env = append(os.Environ(), helperEnv+"=1", "INSTILL_UMASK_TEST_PATH="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("umask helper error = %v, output = %s", err, output)
	}
	info, err := os.Stat(path)
	requireNoError(t, err)
	requireEqual(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestManifestWriteRejectsDigestConflict(t *testing.T) {
	path := writeManifestFixture(t, "name: p\nversion: 1.0.0\n")
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	requireNoError(t, document.setTargets([]string{"codex"}, false))
	external := "name: externally-edited\nversion: 2.0.0\n"
	requireNoError(t, os.WriteFile(path, []byte(external), 0o644))
	err = document.write()
	if err == nil {
		t.Fatal("write() error = nil, want digest conflict")
	}
	requireContains(t, ErrorMessage(err), "changed after Instill read it")
	requireContains(t, ErrorMessage(err), "retry")
	requireEqual(t, external, readFile(t, path))
}

func TestManifestWriteIsAtomicOnEncodeOrFilesystemFailure(t *testing.T) {
	t.Run("encode failure", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\n")
		before := readFile(t, path)
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		document.dirty = true
		document.document.Content[0].Content = append(document.document.Content[0].Content, scalarNode("invalid"), &yaml.Node{Kind: yaml.AliasNode})
		err = document.write()
		if err == nil {
			t.Fatal("write() error = nil, want encoding failure")
		}
		requireEqual(t, before, readFile(t, path))
	})
	t.Run("filesystem replacement failure", func(t *testing.T) {
		path := writeManifestFixture(t, "name: p\nversion: 1.0.0\n")
		before := readFile(t, path)
		document, err := loadManifestDocument(path)
		requireNoError(t, err)
		requireNoError(t, document.setTargets([]string{"codex"}, false))
		document.atomicWrite = func(string, []byte, os.FileMode) error {
			return errors.New("injected filesystem failure")
		}
		err = document.write()
		if err == nil {
			t.Fatal("write() error = nil, want filesystem failure")
		}
		requireContains(t, ErrorMessage(err), "injected filesystem failure")
		requireEqual(t, before, readFile(t, path))
	})
}

func writeManifestFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func mustManifestNode(t *testing.T, path string) *yaml.Node {
	t.Helper()
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	var document yaml.Node
	requireNoError(t, yaml.Unmarshal(data, &document))
	return &document
}

func requireNodeValue(t *testing.T, mapping *yaml.Node, key string, want string) {
	t.Helper()
	node := mappingValue(mapping, key)
	if node == nil || node.Value != want {
		t.Fatalf("mapping value %q = %#v, want %q", key, node, want)
	}
}

func assertNodeSemantics(t *testing.T, before *yaml.Node, after *yaml.Node) {
	t.Helper()
	if before == nil || after == nil {
		t.Fatalf("node mismatch: before=%#v after=%#v", before, after)
	}
	if before.Kind != after.Kind || before.Tag != after.Tag || before.Value != after.Value || before.Style != after.Style || before.Anchor != after.Anchor || before.HeadComment != after.HeadComment || before.LineComment != after.LineComment || before.FootComment != after.FootComment || len(before.Content) != len(after.Content) {
		t.Fatalf("node semantics changed:\nbefore=%#v\nafter=%#v", before, after)
	}
	for i := range before.Content {
		assertNodeSemantics(t, before.Content[i], after.Content[i])
	}
	if before.Kind == yaml.AliasNode && (after.Alias == nil || before.Alias.Value != after.Alias.Value || before.Alias.Anchor != after.Alias.Anchor) {
		t.Fatalf("alias semantics changed: before=%#v after=%#v", before.Alias, after.Alias)
	}
}

func assertNodeGraphEqualExcept(t *testing.T, before *yaml.Node, after *yaml.Node, excluded map[string]struct{}) {
	t.Helper()
	assertNodeGraphPathEqual(t, before, after, "", excluded)
	requireEqual(t, aliasTargetPaths(before, excluded), aliasTargetPaths(after, excluded))
}

func aliasTargetPaths(root *yaml.Node, excluded map[string]struct{}) map[string]string {
	paths := make(map[*yaml.Node]string)
	indexNodePaths(root, "", paths)
	aliases := make(map[string]string)
	for node, path := range paths {
		if node.Kind != yaml.AliasNode || node.Alias == nil || pathExcluded(path, excluded) {
			continue
		}
		aliases[path] = paths[node.Alias]
	}
	return aliases
}

func indexNodePaths(node *yaml.Node, path string, paths map[*yaml.Node]string) {
	paths[node] = path
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			childPath := joinYAMLPath(path, node.Content[index].Value)
			indexNodePaths(node.Content[index], childPath+".@key", paths)
			indexNodePaths(node.Content[index+1], childPath, paths)
		}
		return
	}
	for index, child := range node.Content {
		childPath := path
		if node.Kind == yaml.SequenceNode {
			childPath = fmt.Sprintf("%s[%d]", path, index)
		}
		indexNodePaths(child, childPath, paths)
	}
}

func pathExcluded(path string, excluded map[string]struct{}) bool {
	for excludedPath := range excluded {
		if path == excludedPath || strings.HasPrefix(path, excludedPath+".") || strings.HasPrefix(path, excludedPath+"[") {
			return true
		}
	}
	return false
}

func assertNodeGraphPathEqual(t *testing.T, before *yaml.Node, after *yaml.Node, path string, excluded map[string]struct{}) {
	t.Helper()
	if _, ok := excluded[path]; ok {
		return
	}
	if before == nil || after == nil {
		t.Fatalf("node graph differs at %s: before=%#v after=%#v", path, before, after)
	}
	if before.Kind != after.Kind || before.Tag != after.Tag || before.Value != after.Value || before.Style != after.Style || before.Anchor != after.Anchor || before.HeadComment != after.HeadComment || before.LineComment != after.LineComment || before.FootComment != after.FootComment {
		t.Fatalf("node graph metadata differs at %s:\nbefore=%#v\nafter=%#v", path, before, after)
	}
	if before.Kind == yaml.MappingNode {
		beforePairs := mappingPairsExcept(before, path, excluded)
		afterPairs := mappingPairsExcept(after, path, excluded)
		if len(beforePairs) != len(afterPairs) {
			t.Fatalf("mapping pair count differs at %s: before=%d after=%d", path, len(beforePairs), len(afterPairs))
		}
		for index := range beforePairs {
			childPath := joinYAMLPath(path, beforePairs[index][0].Value)
			assertNodeGraphPathEqual(t, beforePairs[index][0], afterPairs[index][0], childPath+".@key", excluded)
			assertNodeGraphPathEqual(t, beforePairs[index][1], afterPairs[index][1], childPath, excluded)
		}
		return
	}
	if len(before.Content) != len(after.Content) {
		t.Fatalf("node graph child count differs at %s: before=%d after=%d", path, len(before.Content), len(after.Content))
	}
	for index := range before.Content {
		childPath := path
		if before.Kind == yaml.SequenceNode {
			childPath = fmt.Sprintf("%s[%d]", path, index)
		}
		assertNodeGraphPathEqual(t, before.Content[index], after.Content[index], childPath, excluded)
	}
	if before.Kind == yaml.AliasNode {
		if before.Alias == nil || after.Alias == nil || before.Alias.Anchor != after.Alias.Anchor || before.Alias.Value != after.Alias.Value {
			t.Fatalf("alias target differs at %s: before=%#v after=%#v", path, before.Alias, after.Alias)
		}
	}
}

func mappingPairsExcept(mapping *yaml.Node, path string, excluded map[string]struct{}) [][2]*yaml.Node {
	pairs := make([][2]*yaml.Node, 0, len(mapping.Content)/2)
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		childPath := joinYAMLPath(path, mapping.Content[index].Value)
		if _, ok := excluded[childPath]; ok {
			continue
		}
		pairs = append(pairs, [2]*yaml.Node{mapping.Content[index], mapping.Content[index+1]})
	}
	return pairs
}

func joinYAMLPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func TestManifestDocumentErrorsNameManifestPath(t *testing.T) {
	path := writeManifestFixture(t, "name: p\nversion: 1.0.0\n")
	document, err := loadManifestDocument(path)
	requireNoError(t, err)
	document.dirty = true
	requireNoError(t, os.Remove(path))
	err = document.write()
	if err == nil || !strings.Contains(ErrorMessage(err), "apm.yml") {
		t.Fatalf("write() error = %v, want named manifest conflict", err)
	}
}

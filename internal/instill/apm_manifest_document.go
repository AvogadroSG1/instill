package instill

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type manifestDocument struct {
	path        string
	document    *yaml.Node
	projection  APMManifest
	digest      [sha256.Size]byte
	mode        os.FileMode
	existed     bool
	dirty       bool
	parents     map[*yaml.Node]*yaml.Node
	aliases     map[*yaml.Node][]*yaml.Node
	metrics     *manifestIOMetrics
	atomicWrite func(string, []byte, os.FileMode) error
}

type manifestIOMetrics struct {
	authoritativeLoads  int
	authoritativeParses int
	rawDigestRereads    int
	atomicReplacements  int
	events              []string
}

type apmMutationOwnership struct {
	localRoots       []string
	localIdentities  map[string]struct{}
	stableIdentities map[string]struct{}
}

func ownershipForDependencies(dependencies []APMDependency, localRoots []string) apmMutationOwnership {
	ownership := apmMutationOwnership{
		localRoots:       append([]string{}, localRoots...),
		localIdentities:  make(map[string]struct{}),
		stableIdentities: make(map[string]struct{}),
	}
	for _, dependency := range dependencies {
		if dependency.Git != nil {
			ownership.stableIdentities[dependency.stableIdentity()] = struct{}{}
			continue
		}
		ownership.localIdentities[filepath.Clean(dependency.Local)] = struct{}{}
	}
	return ownership
}

func dependencyNames(dependencies []MCPDependency) map[string]struct{} {
	names := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		names[dependency.Name] = struct{}{}
	}
	return names
}

func loadManifestDocument(path string) (*manifestDocument, error) {
	return loadManifestDocumentObserved(path, nil)
}

func loadManifestDocumentObserved(path string, metrics *manifestIOMetrics) (*manifestDocument, error) {
	emitMutationTestEvent("dependent-read:manifest:" + path)
	if metrics != nil {
		metrics.authoritativeLoads++
		metrics.events = append(metrics.events, "authoritative-load")
	}
	data, err := os.ReadFile(path) //nolint:gosec // The selected project owns the manifest path.
	existed := err == nil
	mode := os.FileMode(0o644)
	if err != nil && !os.IsNotExist(err) {
		return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot read manifest: %v", err))
	}
	if existed {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot inspect manifest: %v", statErr))
		}
		mode = info.Mode().Perm()
	}

	document := emptyAPMManifestDocument()
	if existed {
		if metrics != nil {
			metrics.authoritativeParses++
			metrics.events = append(metrics.events, "authoritative-parse")
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(document); err != nil {
			return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
		}
		var extra yaml.Node
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, NewExitError(ExitGeneral, "error: malformed manifest: expected exactly one YAML document")
			}
			return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %v", err))
		}
	}
	if _, err := apmManifestMapping(document); err != nil {
		return nil, err
	}

	d := &manifestDocument{
		path:     path,
		document: document,
		digest:   sha256.Sum256(data),
		mode:     mode,
		existed:  existed,
		metrics:  metrics,
	}
	d.reindex()
	d.projection = projectManifest(document)
	return d, nil
}

func projectManifest(document *yaml.Node) APMManifest {
	manifest := APMManifest{}
	root, err := apmManifestMapping(document)
	if err != nil {
		return manifest
	}
	decodeScalar(mappingValue(root, "name"), &manifest.Name)
	decodeScalar(mappingValue(root, "version"), &manifest.Version)
	decodeStringSequence(mappingValue(root, "targets"), &manifest.Targets)

	dependencies := mappingValue(root, "dependencies")
	if dependencies == nil || dependencies.Kind != yaml.MappingNode {
		normalizeAPMManifest(&manifest)
		return manifest
	}
	if apm := mappingValue(dependencies, "apm"); apm != nil && apm.Kind == yaml.SequenceNode {
		for _, node := range apm.Content {
			if dependency, ok := projectAPMDependency(node); ok {
				manifest.Dependencies.APM = append(manifest.Dependencies.APM, dependency)
			}
		}
	}
	if mcp := mappingValue(dependencies, "mcp"); mcp != nil && mcp.Kind == yaml.SequenceNode {
		for _, node := range mcp.Content {
			if dependency, ok := projectMCPDependency(node); ok {
				manifest.Dependencies.MCP = append(manifest.Dependencies.MCP, dependency)
			}
		}
	}
	normalizeAPMManifest(&manifest)
	return manifest
}

func projectAPMDependency(node *yaml.Node) (APMDependency, bool) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && isInstillReadableLocalPath(node.Value) {
		return APMDependency{Local: node.Value}, true
	}
	if node.Kind != yaml.MappingNode || mappingValue(node, "git") == nil {
		return APMDependency{}, false
	}
	git := mappingValue(node, "git")
	path := mappingValue(node, "path")
	ref := mappingValue(node, "ref")
	if !nonEmptyScalar(git) || !nonEmptyScalar(path) || ref != nil && ref.Kind != yaml.ScalarNode {
		return APMDependency{}, false
	}
	dependency := GitDependency{Repository: git.Value, Path: path.Value}
	if ref != nil {
		dependency.Ref = ref.Value
	}
	dependency.Extra = projectExtraFields(node, map[string]struct{}{"git": {}, "path": {}, "ref": {}})
	return APMDependency{Git: &dependency}, true
}

func isInstillReadableLocalPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "~/") {
		return true
	}
	if len(value) >= 3 && value[1] == ':' && (value[2] == '/' || value[2] == '\\') {
		first := value[0]
		return first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
	}
	return false
}

func projectMCPDependency(node *yaml.Node) (MCPDependency, bool) {
	if node.Kind != yaml.MappingNode {
		return MCPDependency{}, false
	}
	name := mappingValue(node, "name")
	if !nonEmptyScalar(name) {
		return MCPDependency{}, false
	}
	dependency := MCPDependency{Name: name.Value}
	decodeScalar(mappingValue(node, "transport"), &dependency.Transport)
	decodeAny(mappingValue(node, "registry"), &dependency.Registry)
	decodeScalar(mappingValue(node, "command"), &dependency.Command)
	decodeStringSequence(mappingValue(node, "args"), &dependency.Args)
	decodeStringMap(mappingValue(node, "env"), &dependency.Env)
	decodeScalar(mappingValue(node, "url"), &dependency.URL)
	dependency.Extra = projectExtraFields(node, map[string]struct{}{
		"name": {}, "transport": {}, "registry": {}, "command": {}, "args": {}, "env": {}, "url": {},
	})
	return dependency, true
}

func projectExtraFields(node *yaml.Node, owned map[string]struct{}) map[string]any {
	extra := make(map[string]any)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := owned[key]; ok {
			continue
		}
		var value any
		if node.Content[i+1].Decode(&value) == nil {
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func decodeScalar(node *yaml.Node, destination *string) {
	if node != nil && node.Kind == yaml.ScalarNode && node.Tag != "!!null" {
		*destination = node.Value
	}
}

func decodeStringSequence(node *yaml.Node, destination *[]string) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return
		}
		values = append(values, item.Value)
	}
	*destination = values
}

func decodeStringMap(node *yaml.Node, destination *map[string]string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	values := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind != yaml.ScalarNode || node.Content[i+1].Kind != yaml.ScalarNode {
			return
		}
		values[node.Content[i].Value] = node.Content[i+1].Value
	}
	*destination = values
}

func decodeAny(node *yaml.Node, destination *any) {
	if node == nil {
		return
	}
	var value any
	if node.Decode(&value) == nil {
		*destination = value
	}
}

func (d *manifestDocument) repairIdentity(root string, force bool) error {
	mapping, _ := apmManifestMapping(d.document)
	values := []struct {
		key   string
		value string
	}{
		{key: "name", value: filepath.Base(root)},
		{key: "version", value: "0.1.0"},
	}
	for _, identity := range values {
		current := mappingValue(mapping, identity.key)
		if current == nil {
			if err := d.checkMutation(mapping, mapping, identity.key, "add identity"); err != nil {
				return err
			}
			setMappingValue(mapping, identity.key, scalarNode(identity.value))
			d.dirty = true
			continue
		}
		if !force {
			if !nonEmptyScalar(current) {
				return malformedManifestError(identity.key, "must be a non-empty scalar; correct the value and retry")
			}
			continue
		}
		if current.Kind == yaml.AliasNode {
			return d.unsafeMutationError(current, current, identity.key, "replace identity", "replace the alias with an explicit scalar")
		}
		if current.Kind != yaml.ScalarNode || current.Value != identity.value || current.Tag != "!!str" {
			if err := d.checkMappingKeyMutation(mapping, identity.key, mapping, identity.key, "replace identity"); err != nil {
				return err
			}
			if err := d.checkMutation(current, current, identity.key, "replace identity"); err != nil {
				return err
			}
			current.Kind = yaml.ScalarNode
			current.Tag = "!!str"
			current.Value = identity.value
			current.Content = nil
			d.dirty = true
		}
	}
	return nil
}

func (d *manifestDocument) setTargets(targets []string, onlyIfAbsent bool) error {
	mapping, _ := apmManifestMapping(d.document)
	current := mappingValue(mapping, "targets")
	if current != nil && onlyIfAbsent {
		return nil
	}
	next, _ := yamlNode(normalizeStringSlice(targets))
	if current == nil {
		if err := d.checkMutation(mapping, mapping, "targets", "add targets"); err != nil {
			return err
		}
		setMappingValue(mapping, "targets", next)
		d.dirty = true
		return nil
	}
	if nodeValueEqual(current, next) {
		return nil
	}
	if err := d.checkMappingKeyMutation(mapping, "targets", mapping, "targets", "set targets"); err != nil {
		return err
	}
	if err := d.checkMutation(current, current, "targets", "set targets"); err != nil {
		return err
	}
	copyNodeValue(current, next)
	d.dirty = true
	return nil
}

func (d *manifestDocument) mutateAPM(desired []APMDependency, ownership apmMutationOwnership, relocations map[string]string) error {
	sequence, err := d.dependencySequence("apm", len(desired) > 0)
	if err != nil || sequence == nil {
		return err
	}
	type currentDependency struct {
		node       *yaml.Node
		dependency APMDependency
		supported  bool
	}
	current := make([]currentDependency, 0, len(sequence.Content))
	stable := make(map[string]*yaml.Node)
	for index, node := range sequence.Content {
		dependency, supported, malformed := classifyAPMNode(node, ownership)
		if malformed {
			return malformedManifestError(fmt.Sprintf("dependencies.apm[%d]", index), "contains an invalid Git dependency; provide non-empty scalar git and path values and an optional scalar ref")
		}
		if supported && dependency.Git != nil {
			key := dependency.stableIdentity()
			if _, duplicate := stable[key]; duplicate {
				return malformedManifestError("dependencies.apm", "contains duplicate Git identities; remove the duplicate and retry")
			}
			stable[key] = node
		}
		current = append(current, currentDependency{node: node, dependency: dependency, supported: supported})
	}

	desiredByStable := make(map[string]APMDependency, len(desired))
	desiredLocals := make(map[string]APMDependency, len(desired))
	for _, dependency := range desired {
		if dependency.Git != nil {
			desiredByStable[dependency.stableIdentity()] = dependency
			continue
		}
		desiredLocals[filepath.Clean(dependency.Local)] = dependency
	}

	nextContent := make([]*yaml.Node, 0, len(sequence.Content)+len(desired))
	matched := make(map[string]struct{}, len(desired))
	sequenceChecked := false
	checkSequenceMutation := func(change string) error {
		if sequenceChecked {
			return nil
		}
		d.reindex()
		parent := d.parents[sequence]
		if err := d.checkMappingKeyMutation(parent, "apm", sequence, "dependencies.apm", change); err != nil {
			return err
		}
		if err := d.checkMutation(sequence, sequence, "dependencies.apm", change); err != nil {
			return err
		}
		sequenceChecked = true
		return nil
	}
	for _, item := range current {
		if !item.supported {
			nextContent = append(nextContent, item.node)
			continue
		}
		if item.dependency.Git == nil {
			key := filepath.Clean(item.dependency.Local)
			wanted, ok := desiredLocals[key]
			relocated := false
			if !ok {
				if canonicalPath, hasRelocation := relocations[key]; hasRelocation {
					wanted, ok = desiredLocals[filepath.Clean(canonicalPath)]
					relocated = ok
				}
			}
			if ok {
				canonicalKey := filepath.Clean(wanted.Local)
				matchKey := "local:" + canonicalKey
				if _, duplicate := matched[matchKey]; duplicate {
					if err := d.checkRemoval(item.node, item.node, "dependencies.apm", "remove duplicate local dependency"); err != nil {
						return err
					}
					if err := checkSequenceMutation("remove duplicate local dependency"); err != nil {
						return err
					}
					d.dirty = true
					continue
				}
				if relocated {
					if err := d.checkMutation(item.node, item.node, "dependencies.apm", "relocate local dependency"); err != nil {
						return err
					}
					item.node.Value = wanted.Local
					d.dirty = true
				}
				matched[matchKey] = struct{}{}
				nextContent = append(nextContent, item.node)
				continue
			}
			if !ownership.ownsLocal(item.dependency.Local) {
				nextContent = append(nextContent, item.node)
				continue
			}
			if err := d.checkRemoval(item.node, item.node, "dependencies.apm", "remove local dependency"); err != nil {
				return err
			}
			if err := checkSequenceMutation("remove local dependency"); err != nil {
				return err
			}
			d.dirty = true
			continue
		}

		key := item.dependency.stableIdentity()
		wanted, ok := desiredByStable[key]
		if !ok {
			if _, owned := ownership.stableIdentities[key]; !owned {
				nextContent = append(nextContent, item.node)
				continue
			}
			if err := d.checkRemoval(item.node, item.node, "dependencies.apm", "remove Git dependency"); err != nil {
				return err
			}
			if err := checkSequenceMutation("remove Git dependency"); err != nil {
				return err
			}
			d.dirty = true
			continue
		}
		matched[key] = struct{}{}
		if err := d.reconcileGitNode(item.node, wanted.Git); err != nil {
			return err
		}
		nextContent = append(nextContent, item.node)
	}

	for _, dependency := range desired {
		key := "local:" + filepath.Clean(dependency.Local)
		owned := ownership.ownsLocal(dependency.Local)
		if dependency.Git != nil {
			key = dependency.stableIdentity()
			_, owned = ownership.stableIdentities[key]
		}
		if _, ok := matched[key]; ok || !owned {
			continue
		}
		node, encodeErr := yamlNode(dependency)
		if encodeErr != nil {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest dependency: %v", encodeErr))
		}
		if err := checkSequenceMutation("add dependency"); err != nil {
			return err
		}
		nextContent = append(nextContent, node)
		d.dirty = true
	}
	sequence.Content = nextContent
	return nil
}

func (o apmMutationOwnership) ownsLocal(path string) bool {
	if _, ok := o.localIdentities[filepath.Clean(path)]; ok {
		return true
	}
	for _, root := range o.localRoots {
		if isInOrUnderDir(root, path) {
			return true
		}
	}
	return false
}

func classifyAPMNode(node *yaml.Node, ownership apmMutationOwnership) (APMDependency, bool, bool) {
	if node.Kind == yaml.ScalarNode {
		if node.Tag != "!!str" || strings.TrimSpace(node.Value) == "" || !ownership.ownsLocal(node.Value) {
			return APMDependency{}, false, false
		}
		return APMDependency{Local: node.Value}, true, false
	}
	if node.Kind != yaml.MappingNode || mappingValue(node, "git") == nil {
		return APMDependency{}, false, false
	}
	dependency, ok := projectAPMDependency(node)
	return dependency, ok, !ok
}

func (d *manifestDocument) reconcileGitNode(node *yaml.Node, wanted *GitDependency) error {
	ref := mappingValue(node, "ref")
	if wanted.Ref == "" {
		if ref == nil {
			return nil
		}
		if err := d.checkMappingFieldRemoval(node, "ref", node, "dependencies.apm.ref", "remove Git ref"); err != nil {
			return err
		}
		removeMappingValue(node, "ref")
		d.dirty = true
		return nil
	}
	if ref == nil {
		if err := d.checkMutation(node, node, "dependencies.apm.ref", "add Git ref"); err != nil {
			return err
		}
		setMappingValue(node, "ref", scalarNode(wanted.Ref))
		d.dirty = true
		return nil
	}
	if ref.Value == wanted.Ref && ref.Kind == yaml.ScalarNode {
		return nil
	}
	if err := d.checkMutation(ref, node, "dependencies.apm.ref", "update Git ref"); err != nil {
		return err
	}
	if err := d.checkMappingKeyMutation(node, "ref", node, "dependencies.apm.ref", "update Git ref"); err != nil {
		return err
	}
	ref.Kind = yaml.ScalarNode
	ref.Tag = "!!str"
	ref.Value = wanted.Ref
	ref.Content = nil
	d.dirty = true
	return nil
}

func (d *manifestDocument) mutateMCP(desired []MCPDependency, ownedNames map[string]struct{}) error {
	sequence, err := d.dependencySequence("mcp", len(desired) > 0)
	if err != nil || sequence == nil {
		return err
	}
	desiredByName := make(map[string]MCPDependency, len(desired))
	for _, dependency := range desired {
		desiredByName[dependency.Name] = dependency
	}
	seen := make(map[string]struct{})
	for index, node := range sequence.Content {
		if node.Kind != yaml.MappingNode {
			continue
		}
		name := mappingValue(node, "name")
		if name == nil {
			continue
		}
		if !nonEmptyScalar(name) {
			return malformedManifestError(fmt.Sprintf("dependencies.mcp[%d].name", index), "must be a non-empty scalar; correct or remove it and retry")
		}
		if _, duplicate := seen[name.Value]; duplicate {
			return malformedManifestError("dependencies.mcp", "contains duplicate MCP names; remove the duplicate and retry")
		}
		seen[name.Value] = struct{}{}
	}

	nextContent := make([]*yaml.Node, 0, len(sequence.Content)+len(desired))
	matched := make(map[string]struct{}, len(desired))
	sequenceChecked := false
	checkSequenceMutation := func(change string) error {
		if sequenceChecked {
			return nil
		}
		d.reindex()
		parent := d.parents[sequence]
		if err := d.checkMappingKeyMutation(parent, "mcp", sequence, "dependencies.mcp", change); err != nil {
			return err
		}
		if err := d.checkMutation(sequence, sequence, "dependencies.mcp", change); err != nil {
			return err
		}
		sequenceChecked = true
		return nil
	}
	for _, node := range sequence.Content {
		dependency, supported := projectMCPDependency(node)
		if !supported {
			nextContent = append(nextContent, node)
			continue
		}
		wanted, wantedExists := desiredByName[dependency.Name]
		_, owned := ownedNames[dependency.Name]
		if !wantedExists {
			if !owned {
				nextContent = append(nextContent, node)
				continue
			}
			if err := d.checkRemoval(node, node, "dependencies.mcp", "remove MCP dependency"); err != nil {
				return err
			}
			if err := checkSequenceMutation("remove MCP dependency"); err != nil {
				return err
			}
			d.dirty = true
			continue
		}
		matched[dependency.Name] = struct{}{}
		if owned {
			if err := d.reconcileMCPNode(node, wanted); err != nil {
				return err
			}
		}
		nextContent = append(nextContent, node)
	}
	for _, dependency := range desired {
		if _, ok := matched[dependency.Name]; ok {
			continue
		}
		if _, owned := ownedNames[dependency.Name]; !owned {
			continue
		}
		node, encodeErr := yamlNode(dependency)
		if encodeErr != nil {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode MCP dependency: %v", encodeErr))
		}
		if err := checkSequenceMutation("add MCP dependency"); err != nil {
			return err
		}
		nextContent = append(nextContent, node)
		d.dirty = true
	}
	sequence.Content = nextContent
	return nil
}

func (d *manifestDocument) reconcileMCPNode(node *yaml.Node, dependency MCPDependency) error {
	owned := []struct {
		key     string
		value   any
		present bool
	}{
		{key: "name", value: dependency.Name, present: dependency.Name != ""},
		{key: "transport", value: dependency.Transport, present: dependency.Transport != ""},
		{key: "registry", value: false, present: true},
		{key: "command", value: dependency.Command, present: dependency.Command != ""},
		{key: "args", value: dependency.Args, present: len(dependency.Args) > 0},
		{key: "env", value: dependency.Env, present: len(dependency.Env) > 0},
		{key: "url", value: dependency.URL, present: dependency.URL != ""},
	}
	for _, field := range owned {
		current := mappingValue(node, field.key)
		if !field.present {
			if current == nil {
				continue
			}
			if err := d.checkMappingFieldRemoval(node, field.key, node, "dependencies.mcp."+field.key, "remove MCP field"); err != nil {
				return err
			}
			removeMappingValue(node, field.key)
			d.dirty = true
			continue
		}
		next, encodeErr := yamlNode(field.value)
		if encodeErr != nil {
			return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode MCP field %s: %v", field.key, encodeErr))
		}
		if current == nil {
			if err := d.checkMutation(node, node, "dependencies.mcp."+field.key, "add MCP field"); err != nil {
				return err
			}
			setMappingValue(node, field.key, next)
			d.dirty = true
			continue
		}
		if nodeValueEqual(current, next) {
			continue
		}
		if err := d.checkMappingKeyMutation(node, field.key, node, "dependencies.mcp."+field.key, "update MCP field"); err != nil {
			return err
		}
		if err := d.checkMutation(current, node, "dependencies.mcp."+field.key, "update MCP field"); err != nil {
			return err
		}
		copyNodeValue(current, next)
		d.dirty = true
	}
	return nil
}

func (d *manifestDocument) dependencySequence(key string, create bool) (*yaml.Node, error) {
	root, _ := apmManifestMapping(d.document)
	dependencies := mappingValue(root, "dependencies")
	if dependencies == nil {
		if !create {
			return nil, nil
		}
		if err := d.checkMutation(root, root, "dependencies", "add dependencies mapping"); err != nil {
			return nil, err
		}
		dependencies = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(root, "dependencies", dependencies)
		d.dirty = true
	}
	if dependencies.Kind != yaml.MappingNode {
		return nil, malformedManifestError("dependencies", "must be a mapping; correct it and retry")
	}
	sequence := mappingValue(dependencies, key)
	if sequence == nil {
		if !create {
			return nil, nil
		}
		if err := d.checkMutation(dependencies, dependencies, "dependencies."+key, "add dependency sequence"); err != nil {
			return nil, err
		}
		sequence = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(dependencies, key, sequence)
		d.dirty = true
	}
	if sequence.Kind != yaml.SequenceNode {
		return nil, malformedManifestError("dependencies."+key, "must be a sequence; correct it and retry")
	}
	return sequence, nil
}

func (d *manifestDocument) write() error {
	if !d.dirty {
		return nil
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(d.document); err != nil {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", err))
	}
	if err := encoder.Close(); err != nil {
		return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot encode manifest: %v", err))
	}

	current, err := os.ReadFile(d.path) //nolint:gosec // Raw reread checks the selected manifest before replacement.
	if d.metrics != nil {
		d.metrics.rawDigestRereads++
		d.metrics.events = append(d.metrics.events, "raw-digest-reread")
	}
	conflict := d.existed && err != nil || !d.existed && err == nil
	if err != nil && !os.IsNotExist(err) {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot re-read manifest before writing: %v", err))
	}
	if !conflict && d.existed {
		conflict = sha256.Sum256(current) != d.digest
	}
	if conflict {
		return NewExitError(ExitFilesystem, "error: apm.yml changed after Instill read it; review the concurrent edit and retry")
	}
	writer := d.atomicWrite
	if writer == nil {
		writer = writeFileAtomic
		if !d.existed {
			writer = writeNewFileAtomic
		}
	}
	emitMutationTestEvent("first-write:" + d.path)
	if err := writer(d.path, encoded.Bytes(), d.mode); err != nil {
		return NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write manifest: %v", err))
	}
	if d.metrics != nil {
		d.metrics.atomicReplacements++
		d.metrics.events = append(d.metrics.events, "atomic-replacement")
	}
	d.dirty = false
	return nil
}

func (d *manifestDocument) reindex() {
	d.parents = make(map[*yaml.Node]*yaml.Node)
	d.aliases = make(map[*yaml.Node][]*yaml.Node)
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.AliasNode && node.Alias != nil {
			d.aliases[node.Alias] = append(d.aliases[node.Alias], node)
		}
		for _, child := range node.Content {
			d.parents[child] = node
			walk(child)
		}
	}
	walk(d.document)
}

func (d *manifestDocument) checkMutation(target *yaml.Node, ownedRoot *yaml.Node, path string, change string) error {
	d.reindex()
	if target.Kind == yaml.AliasNode {
		return d.unsafeMutationError(target, ownedRoot, path, change, "replace the alias with an explicit value")
	}
	for node := target; node != nil; node = d.parents[node] {
		if node.Anchor != "" {
			for _, alias := range d.aliases[node] {
				if !nodeContains(ownedRoot, alias) {
					return d.unsafeMutationError(node, ownedRoot, path, change, "remove the anchor or replace the external alias with an explicit value")
				}
			}
		}
	}
	return nil
}

func (d *manifestDocument) checkMappingFieldRemoval(mapping *yaml.Node, key string, ownedRoot *yaml.Node, path string, change string) error {
	keyNode, valueNode := mappingEntry(mapping, key)
	if keyNode == nil {
		return nil
	}
	if err := d.checkRemoval(keyNode, ownedRoot, path, change); err != nil {
		return err
	}
	return d.checkRemoval(valueNode, ownedRoot, path, change)
}

func (d *manifestDocument) checkMappingKeyMutation(mapping *yaml.Node, key string, ownedRoot *yaml.Node, path string, change string) error {
	keyNode, _ := mappingEntry(mapping, key)
	if keyNode == nil {
		return nil
	}
	return d.checkMutation(keyNode, ownedRoot, path, change)
}

func (d *manifestDocument) checkRemoval(target *yaml.Node, ownedRoot *yaml.Node, path string, change string) error {
	if err := d.checkMutation(target, ownedRoot, path, change); err != nil {
		return err
	}
	d.reindex()
	var anchoredWithAlias *yaml.Node
	walkNode(target, func(node *yaml.Node) {
		if anchoredWithAlias == nil && node.Anchor != "" && len(d.aliases[node]) > 0 {
			anchoredWithAlias = node
		}
	})
	if anchoredWithAlias != nil {
		return d.unsafeMutationError(anchoredWithAlias, ownedRoot, path, change, "remove references to the anchor before retrying")
	}
	return nil
}

func (d *manifestDocument) unsafeMutationError(node *yaml.Node, _ *yaml.Node, path string, change string, remediation string) error {
	anchor := ""
	if node.Anchor != "" {
		anchor = fmt.Sprintf(" anchor %q", node.Anchor)
	}
	return NewExitError(ExitGeneral, fmt.Sprintf("error: cannot safely %s at %s in %s:%s %s", change, path, d.path, anchor, remediation))
}

func walkNode(node *yaml.Node, visit func(*yaml.Node)) {
	visit(node)
	for _, child := range node.Content {
		walkNode(child, visit)
	}
}

func nodeContains(root *yaml.Node, target *yaml.Node) bool {
	found := false
	walkNode(root, func(node *yaml.Node) {
		if node == target {
			found = true
		}
	})
	return found
}

func nodeValueEqual(left *yaml.Node, right *yaml.Node) bool {
	if left.Kind != right.Kind || left.Tag != right.Tag || left.Value != right.Value || len(left.Content) != len(right.Content) {
		return false
	}
	for index := range left.Content {
		if !nodeValueEqual(left.Content[index], right.Content[index]) {
			return false
		}
	}
	if left.Kind != yaml.AliasNode {
		return true
	}
	return left.Alias != nil && right.Alias != nil && left.Alias.Anchor == right.Alias.Anchor
}

func copyNodeValue(destination *yaml.Node, source *yaml.Node) {
	head, line, foot := destination.HeadComment, destination.LineComment, destination.FootComment
	style, anchor := destination.Style, destination.Anchor
	*destination = *source
	destination.HeadComment, destination.LineComment, destination.FootComment = head, line, foot
	destination.Style, destination.Anchor = style, anchor
}

func nonEmptyScalar(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag != "!!null" && strings.TrimSpace(node.Value) != ""
}

func malformedManifestError(path string, remediation string) error {
	return NewExitError(ExitGeneral, fmt.Sprintf("error: malformed manifest: %s %s", path, remediation))
}

func emptyAPMManifestDocument() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}},
	}
}

func apmManifestMapping(document *yaml.Node) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, NewExitError(ExitGeneral, "error: malformed manifest: expected exactly one YAML document with a mapping root")
	}
	return document.Content[0], nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	_, value := mappingEntry(mapping, key)
	return value
}

func mappingEntry(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mappingKeyMatches(mapping.Content[i], key) {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

func mappingKeyMatches(node *yaml.Node, key string) bool {
	if node.Kind == yaml.ScalarNode {
		return node.Value == key
	}
	return node.Kind == yaml.AliasNode && node.Alias != nil && node.Alias.Kind == yaml.ScalarNode && node.Alias.Value == key
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mappingKeyMatches(mapping.Content[i], key) {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func removeMappingValue(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mappingKeyMatches(mapping.Content[i], key) {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

func yamlNode(value any) (*yaml.Node, error) {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	return node, nil
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

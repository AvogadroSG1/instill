package instill

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	skillPickerPageSize             = 15
	skillPickerAllCategory          = "All"
	skillPickerUnclassifiedCategory = "Unclassified"
)

type skillPickerPane int

const (
	skillPickerCategoriesPane skillPickerPane = iota
	skillPickerSkillsPane
)

// PickTUIOptions configures the interactive typed library picker.
type PickTUIOptions struct {
	Project     Project
	LibraryPath string
	InitialType LibraryType
	Stdin       *os.File
	Stdout      io.Writer
	Stderr      io.Writer
	Runner      CommandRunner
}

// PickSkillsTUIOptions configures the interactive skill picker.
type PickSkillsTUIOptions struct {
	Project      Project
	LibraryPath  string
	Runner       CommandRunner
	Stdin        *os.File
	Stdout       io.Writer
	Stderr       io.Writer
	IsTTY        func(*os.File) bool
	SelectSkills func(available []string, selected []string, stdin *os.File, output io.Writer) ([]string, bool, error)
}

// RunPickTUI lets a user choose project library entries interactively.
func RunPickTUI(opts PickTUIOptions) error {
	if opts.Stdin == nil || !IsTerminal(opts.Stdin) {
		return NewExitError(ExitEnvironment, "error: pick TUI requires a terminal")
	}
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}

	states, err := loadPickTypeStates(opts.Project, opts.LibraryPath)
	if err != nil {
		return err
	}

	output := opts.Stderr
	if output == nil {
		output = io.Discard
	}

	typ, selected, confirmed, err := runPickPickerProgram(states, opts.InitialType, opts.Stdin, output)
	if err != nil {
		return NewExitError(ExitGeneral, "error: pick TUI failed: "+err.Error())
	}
	if !confirmed {
		return nil
	}

	state, ok := pickStateByType(states, typ)
	if !ok {
		return NewExitError(ExitGeneral, "error: invalid library type: "+string(typ))
	}
	selected = preserveHiddenSelections(state.available, state.selected, selected)
	add, remove := selectionDiff(state.selected, selected)
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	return Pick(PickOptions{
		Project:     opts.Project,
		LibraryPath: opts.LibraryPath,
		Add:         add,
		Remove:      remove,
		Type:        typ,
		Runner:      opts.Runner,
		Stdout:      opts.Stdout,
	})
}

// RunPickSkillsTUI lets a user choose project skills interactively.
func RunPickSkillsTUI(opts PickSkillsTUIOptions) error {
	isTTY := opts.IsTTY
	if isTTY == nil {
		isTTY = IsTerminal
	}
	if opts.Stdin == nil || !isTTY(opts.Stdin) {
		return NewExitError(ExitEnvironment, "error: pick-skills TUI requires a terminal")
	}
	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}

	librarySkills, err := ListLibrarySkills(opts.LibraryPath, opts.Stderr)
	if err != nil {
		return err
	}
	selectedSkills, err := currentProjectSkills(opts.Project, opts.LibraryPath)
	if err != nil {
		return err
	}

	output := opts.Stderr
	if output == nil {
		output = io.Discard
	}

	selectSkills := opts.SelectSkills
	if selectSkills == nil {
		selectSkills = runSkillPickerProgram
	}
	selected, confirmed, err := selectSkills(librarySkills, selectedSkills, opts.Stdin, output)
	if err != nil {
		return NewExitError(ExitGeneral, "error: pick-skills TUI failed: "+err.Error())
	}
	if !confirmed {
		return nil
	}
	return ApplySkillSelection(SkillSelectionOptions{
		Project:     opts.Project,
		LibraryPath: opts.LibraryPath,
		Skills:      selected,
		Runner:      opts.Runner,
		Stdout:      opts.Stdout,
	})
}

type pickTypeState struct {
	typ       LibraryType
	available []string
	selected  []string
}

type pickPickerModel struct {
	types      []pickTypeState
	typeCursor int
	active     bool
	items      skillPickerModel
	confirmed  bool
	cancelled  bool
}

func runSkillPickerProgram(available []string, selected []string, stdin *os.File, output io.Writer) ([]string, bool, error) {
	program := tea.NewProgram(
		newSkillPickerModel(available, selected),
		tea.WithInput(stdin),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return nil, false, err
	}

	model, ok := finalModel.(skillPickerModel)
	if !ok || !model.confirmed {
		return nil, false, nil
	}
	return model.selectedSkills(), true, nil
}

func runPickPickerProgram(states []pickTypeState, initialType LibraryType, stdin *os.File, output io.Writer) (LibraryType, []string, bool, error) {
	program := tea.NewProgram(
		newPickPickerModel(states, initialType),
		tea.WithInput(stdin),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return "", nil, false, err
	}

	model, ok := finalModel.(pickPickerModel)
	if !ok || !model.confirmed {
		return "", nil, false, nil
	}
	return model.currentType(), model.items.selectedSkills(), true, nil
}

func loadPickTypeStates(project Project, libraryPath string) ([]pickTypeState, error) {
	manifest, err := ReadAPMManifest(project.ManifestPath)
	if err != nil {
		return nil, err
	}

	states := make([]pickTypeState, 0, len(pickLibraryTypes()))
	for _, typ := range pickLibraryTypes() {
		entries, err := LoadCatalog(libraryPath, typ)
		if err != nil {
			return nil, err
		}
		available := catalogEntryNames(entries)
		selected, err := currentProjectTypeSelection(project, libraryPath, typ, manifest, entries)
		if err != nil {
			return nil, err
		}
		states = append(states, pickTypeState{
			typ:       typ,
			available: available,
			selected:  selected,
		})
	}
	return states, nil
}

func currentProjectTypeSelection(project Project, libraryPath string, typ LibraryType, manifest APMManifest, entries []CatalogEntry) ([]string, error) {
	switch typ {
	case LibraryTypeSkill:
		namesByDependency := make(map[string]string, len(entries))
		for _, entry := range entries {
			namesByDependency[skillDependencyFromCatalog(libraryPath, entry).identity()] = entry.Name
		}

		selected := make([]string, 0, len(manifest.Dependencies.APM))
		for _, dependency := range manifest.Dependencies.APM {
			if name, ok := namesByDependency[dependency.identity()]; ok {
				selected = append(selected, name)
			}
		}
		return normalizeStringSlice(selected), nil
	case LibraryTypePlugin:
		namesByDependency := make(map[string]string, len(entries))
		for _, entry := range entries {
			namesByDependency[pluginDependencyPath(libraryPath, entry)] = entry.Name
		}

		selected := make([]string, 0, len(manifest.Dependencies.APM))
		for _, dependency := range manifest.Dependencies.APM {
			if dependency.Git != nil {
				continue
			}
			if name, ok := namesByDependency[dependency.Local]; ok {
				selected = append(selected, name)
			}
		}
		return normalizeStringSlice(selected), nil
	case LibraryTypeMCP:
		selected := make([]string, 0, len(manifest.Dependencies.MCP))
		for _, dependency := range manifest.Dependencies.MCP {
			selected = append(selected, dependency.Name)
		}
		return normalizeStringSlice(selected), nil
	case LibraryTypeInstruction, LibraryTypePrompt:
		namesBySanitized := make(map[string]string, len(entries))
		for _, entry := range entries {
			namesBySanitized[sanitizeContentName(entry.Name)] = entry.Name
		}
		return listProjectContentNames(project.Root, typ, namesBySanitized)
	default:
		return nil, NewExitError(ExitGeneral, "error: invalid library type: "+string(typ))
	}
}

func currentProjectSkills(project Project, libraryPath string) ([]string, error) {
	manifest, err := ReadAPMManifest(project.ManifestPath)
	if err != nil {
		return nil, err
	}
	entries, err := LoadCatalog(libraryPath, LibraryTypeSkill)
	if err != nil {
		return nil, err
	}

	namesByDependency := make(map[string]string, len(entries))
	for _, entry := range entries {
		namesByDependency[skillDependencyFromCatalog(libraryPath, entry).identity()] = entry.Name
	}

	selected := make([]string, 0, len(manifest.Dependencies.APM))
	for _, dependency := range manifest.Dependencies.APM {
		if name, ok := namesByDependency[dependency.identity()]; ok {
			selected = append(selected, name)
		}
	}
	return normalizeStringSlice(selected), nil
}

func newPickPickerModel(states []pickTypeState, initialType LibraryType) pickPickerModel {
	normalized := normalizePickTypeStates(states)
	cursor := pickTypeIndex(normalized, initialType)
	model := pickPickerModel{
		types:      normalized,
		typeCursor: cursor,
	}
	model.items = model.itemModel()
	return model
}

func normalizePickTypeStates(states []pickTypeState) []pickTypeState {
	byType := make(map[LibraryType]pickTypeState, len(states))
	for _, state := range states {
		state.available = normalizeStringSlice(state.available)
		state.selected = normalizeStringSlice(state.selected)
		byType[state.typ] = state
	}

	normalized := make([]pickTypeState, 0, len(pickLibraryTypes()))
	for _, typ := range pickLibraryTypes() {
		state := byType[typ]
		state.typ = typ
		normalized = append(normalized, state)
	}
	return normalized
}

func (m pickPickerModel) Init() tea.Cmd {
	return nil
}

func (m pickPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.active {
		switch key.Type {
		case tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyRunes:
			if !m.items.searchMode && key.String() == "a" {
				m.confirmed = true
				return m, tea.Quit
			}
		}
		updated, cmd := m.items.Update(msg)
		m.items = updated.(skillPickerModel)
		if m.items.confirmed {
			m.confirmed = true
		}
		if m.items.cancelled {
			m.cancelled = true
		}
		return m, cmd
	}

	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyEnter, tea.KeyRight:
		m.active = true
		m.items = m.itemModel()
	case tea.KeyUp:
		m.moveType(-1)
	case tea.KeyDown:
		m.moveType(1)
	case tea.KeyRunes:
		switch key.String() {
		case "q":
			m.cancelled = true
			return m, tea.Quit
		case "j":
			m.moveType(1)
		case "k":
			m.moveType(-1)
		case "s":
			m.focusType(LibraryTypeSkill)
		case "l":
			m.focusType(LibraryTypePlugin)
		case "m":
			m.focusType(LibraryTypeMCP)
		case "i":
			m.focusType(LibraryTypeInstruction)
		case "p":
			m.focusType(LibraryTypePrompt)
		}
	}

	return m, nil
}

func (m pickPickerModel) View() string {
	if m.active {
		var builder strings.Builder
		builder.WriteString(pickTypeLabel(m.currentType()) + "\n")
		builder.WriteString(m.items.View())
		builder.WriteString("a applies selection\n")
		return builder.String()
	}

	var builder strings.Builder
	builder.WriteString("Pick library type\n")
	for i, state := range m.types {
		prefix := "  "
		if i == m.typeCursor {
			prefix = "▶ "
		}
		_, _ = fmt.Fprintf(&builder, "%s%s (%d available, %d installed)\n", prefix, pickTypeLabel(state.typ), len(state.available), len(state.selected))
	}
	builder.WriteString("Enter selects type, s/m/i/p jumps, q/Esc cancels\n")
	return builder.String()
}

func (m *pickPickerModel) moveType(delta int) {
	m.typeCursor += delta
	if m.typeCursor < 0 {
		m.typeCursor = 0
	}
	if m.typeCursor >= len(m.types) {
		m.typeCursor = len(m.types) - 1
	}
}

func (m *pickPickerModel) focusType(typ LibraryType) {
	m.typeCursor = pickTypeIndex(m.types, typ)
}

func (m pickPickerModel) currentType() LibraryType {
	if m.typeCursor < 0 || m.typeCursor >= len(m.types) {
		return LibraryTypeSkill
	}
	return m.types[m.typeCursor].typ
}

func (m pickPickerModel) currentState() pickTypeState {
	if m.typeCursor < 0 || m.typeCursor >= len(m.types) {
		return pickTypeState{typ: LibraryTypeSkill}
	}
	return m.types[m.typeCursor]
}

func (m pickPickerModel) itemModel() skillPickerModel {
	state := m.currentState()
	return newSkillPickerModel(state.available, state.selected)
}

func pickTypeIndex(states []pickTypeState, typ LibraryType) int {
	for i, state := range states {
		if state.typ == typ {
			return i
		}
	}
	return 0
}

func pickStateByType(states []pickTypeState, typ LibraryType) (pickTypeState, bool) {
	for _, state := range states {
		if state.typ == typ {
			return state, true
		}
	}
	return pickTypeState{}, false
}

func preserveHiddenSelections(available []string, previous []string, selected []string) []string {
	availableSet := make(map[string]struct{}, len(available))
	for _, name := range normalizeStringSlice(available) {
		availableSet[name] = struct{}{}
	}
	next := append([]string{}, selected...)
	for _, name := range normalizeStringSlice(previous) {
		if _, visible := availableSet[name]; visible {
			continue
		}
		next = append(next, name)
	}
	return normalizeStringSlice(next)
}

func selectionDiff(previous []string, next []string) ([]string, []string) {
	previousSet := make(map[string]struct{}, len(previous))
	for _, name := range normalizeStringSlice(previous) {
		previousSet[name] = struct{}{}
	}
	nextSet := make(map[string]struct{}, len(next))
	for _, name := range normalizeStringSlice(next) {
		nextSet[name] = struct{}{}
	}

	add := make([]string, 0)
	for name := range nextSet {
		if _, ok := previousSet[name]; !ok {
			add = append(add, name)
		}
	}
	remove := make([]string, 0)
	for name := range previousSet {
		if _, ok := nextSet[name]; !ok {
			remove = append(remove, name)
		}
	}
	return normalizeStringSlice(add), normalizeStringSlice(remove)
}

func catalogEntryNames(entries []CatalogEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return normalizeStringSlice(names)
}

func pickLibraryTypes() []LibraryType {
	return []LibraryType{LibraryTypeSkill, LibraryTypePlugin, LibraryTypeMCP, LibraryTypeInstruction, LibraryTypePrompt}
}

func pickTypeLabel(typ LibraryType) string {
	switch typ {
	case LibraryTypeSkill:
		return "Skills"
	case LibraryTypePlugin:
		return "Plugins"
	case LibraryTypeMCP:
		return "MCP Servers"
	case LibraryTypeInstruction:
		return "Instructions"
	case LibraryTypePrompt:
		return "Prompts"
	default:
		return string(typ)
	}
}

type skillPickerModel struct {
	skills         []string
	selected       map[string]bool
	categories     []string
	tree           *categoryNode
	categoryPath   []string
	categoryCursor int
	skillCursor    int
	searchCursor   int
	focusedPane    skillPickerPane
	filter         string
	searchMode     bool
	confirmed      bool
	cancelled      bool
}

// categoryNode is a node in the tree derived from skill-name path segments.
// children maps a subcategory segment to its node; skills holds the full names
// of skills that live directly at this node's level.
type categoryNode struct {
	children map[string]*categoryNode
	skills   []string
}

// buildCategoryTree groups skill names by their path segments. A skill
// "a/b/leaf" registers "leaf" as an immediate skill of node a/b and ensures
// category nodes a and a/b exist. A flat skill "docker" becomes an immediate
// skill of the root (surfaced only under "All").
func buildCategoryTree(skills []string) *categoryNode {
	root := &categoryNode{children: map[string]*categoryNode{}}
	for _, skill := range skills {
		segs := strings.Split(skill, "/")
		node := root
		for _, seg := range segs[:len(segs)-1] {
			child, ok := node.children[seg]
			if !ok {
				child = &categoryNode{children: map[string]*categoryNode{}}
				node.children[seg] = child
			}
			node = child
		}
		node.skills = append(node.skills, skill)
	}
	return root
}

func (n *categoryNode) nodeAt(path []string) *categoryNode {
	cur := n
	for _, seg := range path {
		next, ok := cur.children[seg]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// subcategoryNames returns the sorted child-category names at path.
func (n *categoryNode) subcategoryNames(path []string) []string {
	node := n.nodeAt(path)
	if node == nil {
		return nil
	}
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// immediateSkills returns the sorted skill names that live directly at path.
func (n *categoryNode) immediateSkills(path []string) []string {
	node := n.nodeAt(path)
	if node == nil {
		return nil
	}
	out := append([]string{}, node.skills...)
	sort.Strings(out)
	return out
}

func newSkillPickerModel(skills []string, selected []string) skillPickerModel {
	available := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		available[skill] = struct{}{}
	}
	selection := make(map[string]bool, len(selected))
	for _, skill := range selected {
		if _, ok := available[skill]; ok {
			selection[skill] = true
		}
	}
	tree := buildCategoryTree(skills)
	return skillPickerModel{
		skills:      append([]string{}, skills...),
		selected:    selection,
		tree:        tree,
		categories:  categoryPaneEntries(tree.subcategoryNames(nil)),
		focusedPane: skillPickerSkillsPane,
	}
}

func categoryPaneEntries(categories []string) []string {
	entries := make([]string, 0, len(categories)+1)
	entries = append(entries, skillPickerAllCategory)
	if len(categories) == 0 {
		return entries
	}
	return append(entries, categories...)
}

func (m skillPickerModel) Init() tea.Cmd {
	return nil
}

func (m skillPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyEsc:
		if m.searchMode {
			m.searchMode = false
			m.filter = ""
			m.searchCursor = 0
			return m, nil
		}
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyEnter:
		m.confirmed = true
		return m, tea.Quit
	case tea.KeyLeft:
		if m.searchMode {
			break
		}
		if m.focusedPane == skillPickerCategoriesPane && len(m.categoryPath) > 0 {
			m.categoryPath = m.categoryPath[:len(m.categoryPath)-1]
			m.categories = m.categoriesForPath()
			m.categoryCursor = 0
			m.skillCursor = 0
			break
		}
		m.focusedPane = skillPickerCategoriesPane
	case tea.KeyRight:
		if m.searchMode {
			break
		}
		if m.focusedPane == skillPickerCategoriesPane && m.selectedCategoryHasChildren() {
			m.categoryPath = append(m.categoryPath, m.selectedCategory())
			m.categories = m.categoriesForPath()
			m.categoryCursor = 0
			m.skillCursor = 0
			break
		}
		m.focusedPane = skillPickerSkillsPane
	case tea.KeyUp:
		m.move(-1)
	case tea.KeyDown:
		m.move(1)
	case tea.KeyBackspace:
		if m.searchMode && m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.clampCursor()
		}
	case tea.KeySpace:
		m.toggleCurrent()
	case tea.KeyRunes:
		switch key.String() {
		case "/":
			if m.searchMode {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.searchMode = true
			m.filter = ""
			m.searchCursor = 0
		case "q":
			if m.searchMode {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.cancelled = true
			return m, tea.Quit
		case "j":
			if m.searchMode {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.move(1)
		case "k":
			if m.searchMode {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.move(-1)
		default:
			if m.searchMode {
				m.filter += key.String()
				m.clampCursor()
			}
		}
	}

	return m, nil
}

func (m skillPickerModel) View() string {
	visible := m.visibleSkills()
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "Select skills (%d selected)\n", len(m.selectedSkills()))
	if m.searchMode {
		builder.WriteString("/" + m.filter + "\n")
		for _, line := range m.searchPaneLines(visible) {
			builder.WriteString(line + "\n")
		}
		builder.WriteString("Enter confirms, space toggles, Esc returns to browse\n")
		return builder.String()
	}
	if breadcrumb := m.categoryBreadcrumb(); breadcrumb != "" {
		builder.WriteString(breadcrumb + "\n")
	}

	categoryLines := m.categoryPaneLines()
	skillLines := m.skillPaneLines(visible)
	maxLines := len(categoryLines)
	if len(skillLines) > maxLines {
		maxLines = len(skillLines)
	}

	builder.WriteString("Categories                 Skills\n")
	for i := range maxLines {
		category := ""
		if i < len(categoryLines) {
			category = categoryLines[i]
		}
		skill := ""
		if i < len(skillLines) {
			skill = skillLines[i]
		}
		_, _ = fmt.Fprintf(&builder, "%-26s %s\n", category, skill)
	}
	builder.WriteString("Left/right changes pane, enter confirms, space toggles, / searches, q/Esc cancels\n")
	return builder.String()
}

func (m *skillPickerModel) move(delta int) {
	if m.searchMode {
		visible := m.visibleSkills()
		if len(visible) == 0 {
			m.searchCursor = 0
			return
		}
		m.searchCursor += delta
		m.clampCursor()
		return
	}

	if m.focusedPane == skillPickerCategoriesPane {
		previous := m.categoryCursor
		m.categoryCursor += delta
		if m.categoryCursor < 0 {
			m.categoryCursor = 0
		}
		if m.categoryCursor >= len(m.categories) {
			m.categoryCursor = len(m.categories) - 1
		}
		if m.categoryCursor != previous {
			m.skillCursor = 0
		}
		return
	}

	visible := m.visibleSkills()
	if len(visible) == 0 {
		m.skillCursor = 0
		return
	}
	m.skillCursor += delta
	m.clampCursor()
}

func (m *skillPickerModel) clampCursor() {
	visible := m.visibleSkills()
	if len(visible) == 0 {
		if m.searchMode {
			m.searchCursor = 0
			return
		}
		m.skillCursor = 0
		return
	}
	if m.searchMode {
		if m.searchCursor < 0 {
			m.searchCursor = 0
		}
		if m.searchCursor >= len(visible) {
			m.searchCursor = len(visible) - 1
		}
		return
	}
	if m.skillCursor < 0 {
		m.skillCursor = 0
	}
	if m.skillCursor >= len(visible) {
		m.skillCursor = len(visible) - 1
	}
}

func (m *skillPickerModel) toggleCurrent() {
	if !m.searchMode && m.focusedPane != skillPickerSkillsPane {
		return
	}
	visible := m.visibleSkills()
	if len(visible) == 0 {
		return
	}
	cursor := m.skillCursor
	if m.searchMode {
		cursor = m.searchCursor
	}
	skill := visible[cursor]
	m.selected[skill] = !m.selected[skill]
	if !m.selected[skill] {
		delete(m.selected, skill)
	}
}

func (m skillPickerModel) categoryPaneLines() []string {
	lines := make([]string, 0, len(m.categories))
	for i, category := range m.categories {
		prefix := "  "
		if m.focusedPane == skillPickerCategoriesPane && i == m.categoryCursor {
			prefix = "> "
		}
		lines = append(lines, prefix+category)
	}
	return lines
}

func (m skillPickerModel) skillPaneLines(visible []string) []string {
	if len(visible) == 0 {
		return []string{"No matching skills"}
	}

	start := 0
	if m.skillCursor >= skillPickerPageSize {
		start = m.skillCursor - skillPickerPageSize + 1
	}
	end := start + skillPickerPageSize
	if end > len(visible) {
		end = len(visible)
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		prefix := "  "
		if m.focusedPane == skillPickerSkillsPane && i == m.skillCursor {
			prefix = "> "
		}
		marker := "[ ]"
		if m.selected[visible[i]] {
			marker = "[✓]"
		}
		lines = append(lines, prefix+marker+" "+visible[i])
	}
	return lines
}

func (m skillPickerModel) searchPaneLines(visible []string) []string {
	if len(visible) == 0 {
		return []string{"No matching skills"}
	}

	start := 0
	if m.searchCursor >= skillPickerPageSize {
		start = m.searchCursor - skillPickerPageSize + 1
	}
	end := start + skillPickerPageSize
	if end > len(visible) {
		end = len(visible)
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.searchCursor {
			prefix = "> "
		}
		marker := "[ ]"
		if m.selected[visible[i]] {
			marker = "[✓]"
		}
		lines = append(lines, prefix+marker+" "+visible[i])
	}
	return lines
}

func (m skillPickerModel) visibleSkills() []string {
	if m.searchMode {
		return fuzzyFilterSkills(m.skills, m.filter)
	}
	return m.visibleCategorySkills()
}

func (m skillPickerModel) visibleCategorySkills() []string {
	category := m.selectedCategory()
	if category == "" {
		return m.skillsUnderPath(m.categoryPath)
	}
	if m.selectedCategoryIsSynthetic() {
		if len(m.categoryPath) == 0 {
			return m.skillsUnderPath(m.categoryPath)
		}
		return m.tree.immediateSkills(m.categoryPath)
	}
	path := append(append([]string{}, m.categoryPath...), category)
	return m.tree.immediateSkills(path)
}

// skillsUnderPath returns the skills that live at path or in any descendant
// category beneath it, preserving the original skill ordering. An empty path
// returns every skill.
func (m skillPickerModel) skillsUnderPath(path []string) []string {
	if len(path) == 0 {
		return append([]string{}, m.skills...)
	}
	prefix := strings.Join(path, "/") + "/"
	out := make([]string, 0, len(m.skills))
	for _, skill := range m.skills {
		if strings.HasPrefix(skill, prefix) {
			out = append(out, skill)
		}
	}
	return out
}

func (m skillPickerModel) selectedCategory() string {
	if m.categoryCursor < 0 || m.categoryCursor >= len(m.categories) {
		return ""
	}
	return m.categories[m.categoryCursor]
}

func (m skillPickerModel) selectedCategoryHasChildren() bool {
	category := m.selectedCategory()
	if category == "" || m.selectedCategoryIsSynthetic() {
		return false
	}
	path := append(append([]string{}, m.categoryPath...), category)
	node := m.tree.nodeAt(path)
	if node == nil {
		return false
	}
	return len(node.children) > 0
}

func (m skillPickerModel) selectedCategoryIsSynthetic() bool {
	if m.categoryCursor != 0 {
		return false
	}
	if len(m.categoryPath) == 0 {
		return true
	}
	return len(m.tree.immediateSkills(m.categoryPath)) > 0
}

func (m skillPickerModel) categoriesForPath() []string {
	subs := m.tree.subcategoryNames(m.categoryPath)
	if len(m.categoryPath) == 0 {
		return categoryPaneEntries(subs)
	}
	if len(m.tree.immediateSkills(m.categoryPath)) == 0 {
		return subs
	}
	return append([]string{skillPickerUnclassifiedCategory}, subs...)
}

func (m skillPickerModel) categoryBreadcrumb() string {
	if len(m.categoryPath) == 0 {
		return ""
	}
	return strings.Join(m.categoryPath, " > ")
}

func skillInSelectedCategory(categories map[string][]string, selectedCategory string, skillName string) bool {
	for category, skills := range categories {
		if category != selectedCategory && !strings.HasPrefix(category, selectedCategory+"/") {
			continue
		}
		for _, skill := range skills {
			if skill == skillName {
				return true
			}
		}
	}
	return false
}

func (m skillPickerModel) selectedSkills() []string {
	skills := make([]string, 0, len(m.selected))
	for skill := range m.selected {
		skills = append(skills, skill)
	}
	return normalizeSkills(skills)
}

func fuzzyFilterSkills(skills []string, filter string) []string {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return append([]string{}, skills...)
	}

	filtered := make([]string, 0, len(skills))
	for _, skill := range skills {
		if fuzzyMatches(strings.ToLower(skill), filter) {
			filtered = append(filtered, skill)
		}
	}
	return filtered
}

func fuzzyMatches(value string, filter string) bool {
	next := 0
	for _, char := range value {
		if next < len(filter) && char == rune(filter[next]) {
			next++
		}
	}
	return next == len(filter)
}

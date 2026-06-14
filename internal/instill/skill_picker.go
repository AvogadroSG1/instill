package instill

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const skillPickerPageSize = 15

type skillPickerPane int

const (
	skillPickerCategoriesPane skillPickerPane = iota
	skillPickerSkillsPane
)

// PickSkillsTUIOptions configures the interactive skill picker.
type PickSkillsTUIOptions struct {
	Project     Project
	LibraryPath string
	Stdin       *os.File
	Stdout      io.Writer
	Stderr      io.Writer
}

// RunPickSkillsTUI lets a user choose project skills interactively.
func RunPickSkillsTUI(opts PickSkillsTUIOptions) error {
	if opts.Stdin == nil || !IsTerminal(opts.Stdin) {
		return NewExitError(ExitEnvironment, "error: pick-skills TUI requires a terminal")
	}

	librarySkills, err := ListLibrarySkills(opts.LibraryPath)
	if err != nil {
		return err
	}
	manifest, err := ReadManifest(opts.Project.ManifestPath)
	if err != nil {
		return err
	}

	output := opts.Stderr
	if output == nil {
		output = io.Discard
	}
	program := tea.NewProgram(
		newSkillPickerModel(librarySkills, manifest.Skills),
		tea.WithInput(opts.Stdin),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return NewExitError(ExitGeneral, "error: pick-skills TUI failed: "+err.Error())
	}

	model, ok := finalModel.(skillPickerModel)
	if !ok || !model.confirmed {
		return nil
	}
	return ApplySkillSelection(SkillSelectionOptions{
		Project:     opts.Project,
		LibraryPath: opts.LibraryPath,
		Skills:      model.selectedSkills(),
		Stdout:      opts.Stdout,
	})
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
	entries = append(entries, "All")
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
	if category == "" || category == "All" {
		return append([]string{}, m.skills...)
	}
	path := append(append([]string{}, m.categoryPath...), category)
	return m.tree.immediateSkills(path)
}

func (m skillPickerModel) selectedCategory() string {
	if m.categoryCursor < 0 || m.categoryCursor >= len(m.categories) {
		return ""
	}
	return m.categories[m.categoryCursor]
}

func (m skillPickerModel) selectedCategoryHasChildren() bool {
	category := m.selectedCategory()
	if category == "" || category == "All" {
		return false
	}
	path := append(append([]string{}, m.categoryPath...), category)
	node := m.tree.nodeAt(path)
	return node != nil && len(node.children) > 0
}

func (m skillPickerModel) categoriesForPath() []string {
	subs := m.tree.subcategoryNames(m.categoryPath)
	if len(m.categoryPath) == 0 {
		return categoryPaneEntries(subs)
	}
	return subs
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

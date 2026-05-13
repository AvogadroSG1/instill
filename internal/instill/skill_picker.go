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
	categoryAssignments := LoadCategoriesWithWarnings(opts.LibraryPath, nil)
	categoryEntries := topLevelCategories(categoryAssignments)

	output := opts.Stderr
	if output == nil {
		output = io.Discard
	}
	program := tea.NewProgram(
		newSkillPickerModel(librarySkills, manifest.Skills, categoryEntries, categoryAssignments),
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
	categorySkills map[string][]string
	categoryPath   []string
	categoryCursor int
	skillCursor    int
	focusedPane    skillPickerPane
	filter         string
	filtering      bool
	confirmed      bool
	cancelled      bool
}

func newSkillPickerModel(skills []string, selected []string, categories []string, categorySkills map[string][]string) skillPickerModel {
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
	return skillPickerModel{
		skills:         append([]string{}, skills...),
		selected:       selection,
		categories:     categoryPaneEntries(categories),
		categorySkills: copyCategories(categorySkills),
		focusedPane:    skillPickerSkillsPane,
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

func topLevelCategories(categories map[string][]string) []string {
	topLevel := map[string]struct{}{}
	for category := range categories {
		top, _, _ := strings.Cut(strings.Trim(category, "/"), "/")
		if top == "" {
			continue
		}
		topLevel[top] = struct{}{}
	}

	entries := make([]string, 0, len(topLevel))
	for category := range topLevel {
		entries = append(entries, category)
	}
	sort.Strings(entries)
	return entries
}

func skillPickerCategoriesForLibrary(libraryPath string) []string {
	return topLevelCategories(LoadCategoriesWithWarnings(libraryPath, nil))
}

func copyCategories(categories map[string][]string) map[string][]string {
	if len(categories) == 0 {
		return map[string][]string{}
	}
	copied := make(map[string][]string, len(categories))
	for category, skills := range categories {
		copied[category] = append([]string{}, skills...)
	}
	return copied
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
	case tea.KeyCtrlC, tea.KeyEsc:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyEnter:
		m.confirmed = true
		return m, tea.Quit
	case tea.KeyLeft:
		if m.focusedPane == skillPickerCategoriesPane && len(m.categoryPath) > 0 {
			m.categoryPath = m.categoryPath[:len(m.categoryPath)-1]
			m.categories = m.categoriesForPath()
			m.categoryCursor = 0
			m.skillCursor = 0
			break
		}
		m.focusedPane = skillPickerCategoriesPane
	case tea.KeyRight:
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
		if m.filtering && m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.clampCursor()
		}
	case tea.KeySpace:
		m.toggleCurrent()
	case tea.KeyRunes:
		switch key.String() {
		case "/":
			m.filtering = true
		case "q":
			if m.filtering {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.cancelled = true
			return m, tea.Quit
		case "j":
			if m.filtering {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.move(1)
		case "k":
			if m.filtering {
				m.filter += key.String()
				m.clampCursor()
				break
			}
			m.move(-1)
		default:
			if m.filtering {
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
	if m.filtering || m.filter != "" {
		builder.WriteString("/" + m.filter + "\n")
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
	builder.WriteString("Left/right changes pane, enter confirms, space toggles, / filters, q/Esc cancels\n")
	return builder.String()
}

func (m *skillPickerModel) move(delta int) {
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
		m.skillCursor = 0
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
	if m.focusedPane != skillPickerSkillsPane {
		return
	}
	visible := m.visibleSkills()
	if len(visible) == 0 {
		return
	}
	skill := visible[m.skillCursor]
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

func (m skillPickerModel) visibleSkills() []string {
	return fuzzyFilterSkills(m.visibleCategorySkills(), m.filter)
}

func (m skillPickerModel) visibleCategorySkills() []string {
	category := m.selectedCategoryPath()
	if category == "" || category == "All" || len(m.categorySkills) == 0 {
		return append([]string{}, m.skills...)
	}

	filtered := make([]string, 0, len(m.skills))
	for _, skill := range m.skills {
		if skillInSelectedCategory(m.categorySkills, category, skill) {
			filtered = append(filtered, skill)
		}
	}
	return filtered
}

func (m skillPickerModel) selectedCategory() string {
	if m.categoryCursor < 0 || m.categoryCursor >= len(m.categories) {
		return ""
	}
	return m.categories[m.categoryCursor]
}

func (m skillPickerModel) selectedCategoryPath() string {
	category := m.selectedCategory()
	if category == "" || category == "All" {
		return category
	}
	path := append([]string{}, m.categoryPath...)
	path = append(path, category)
	return strings.Join(path, "/")
}

func (m skillPickerModel) selectedCategoryHasChildren() bool {
	categoryPath := m.selectedCategoryPath()
	if categoryPath == "" || categoryPath == "All" {
		return false
	}
	prefix := categoryPath + "/"
	for category := range m.categorySkills {
		remainder, ok := strings.CutPrefix(category, prefix)
		if ok && remainder != "" {
			return true
		}
	}
	return false
}

func (m skillPickerModel) categoriesForPath() []string {
	if len(m.categoryPath) == 0 {
		return categoryPaneEntries(topLevelCategories(m.categorySkills))
	}

	prefix := strings.Join(m.categoryPath, "/") + "/"
	children := map[string]struct{}{}
	for category := range m.categorySkills {
		remainder, ok := strings.CutPrefix(category, prefix)
		if !ok || remainder == "" {
			continue
		}
		child, _, _ := strings.Cut(remainder, "/")
		if child != "" {
			children[child] = struct{}{}
		}
	}
	entries := make([]string, 0, len(children))
	for child := range children {
		entries = append(entries, child)
	}
	sort.Strings(entries)
	return entries
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

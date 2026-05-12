package instill

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const skillPickerPageSize = 15

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
	skills    []string
	selected  map[string]bool
	cursor    int
	filter    string
	filtering bool
	confirmed bool
	cancelled bool
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
	return skillPickerModel{
		skills:   append([]string{}, skills...),
		selected: selection,
	}
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
	if len(visible) == 0 {
		builder.WriteString("No matching skills\n")
		return builder.String()
	}

	start := 0
	if m.cursor >= skillPickerPageSize {
		start = m.cursor - skillPickerPageSize + 1
	}
	end := start + skillPickerPageSize
	if end > len(visible) {
		end = len(visible)
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		marker := "[ ]"
		if m.selected[visible[i]] {
			marker = "[✓]"
		}
		builder.WriteString(prefix + marker + " " + visible[i] + "\n")
	}
	builder.WriteString("Enter confirms, space toggles, / filters, q/Esc cancels\n")
	return builder.String()
}

func (m *skillPickerModel) move(delta int) {
	visible := m.visibleSkills()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	m.clampCursor()
}

func (m *skillPickerModel) clampCursor() {
	visible := m.visibleSkills()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
}

func (m *skillPickerModel) toggleCurrent() {
	visible := m.visibleSkills()
	if len(visible) == 0 {
		return
	}
	skill := visible[m.cursor]
	m.selected[skill] = !m.selected[skill]
	if !m.selected[skill] {
		delete(m.selected, skill)
	}
}

func (m skillPickerModel) visibleSkills() []string {
	return fuzzyFilterSkills(m.skills, m.filter)
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

package instill

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// DefaultAvailableTargets lists the agent targets offered by instill.
var DefaultAvailableTargets = []string{
	"codex",
	"opencode",
	"hermes",
	"pi",
	"claude",
	"antigravity",
}

// TargetPickerOptions configures the interactive target agent picker.
type TargetPickerOptions struct {
	Available []string
	Selected  []string
	Stdin     *os.File
	Stdout    io.Writer
	Stderr    io.Writer
	IsTTY     func(*os.File) bool
}

// RunTargetPickerTUI runs the interactive target agent picker program.
func RunTargetPickerTUI(opts TargetPickerOptions) ([]string, bool, error) {
	isTTY := opts.IsTTY
	if isTTY == nil {
		isTTY = IsTerminal
	}
	if opts.Stdin == nil || !isTTY(opts.Stdin) {
		return nil, false, NewExitError(ExitEnvironment, "error: target picker requires a terminal")
	}

	available := opts.Available
	if len(available) == 0 {
		available = DefaultAvailableTargets
	}

	output := opts.Stderr
	if output == nil {
		output = io.Discard
	}

	program := tea.NewProgram(
		newTargetPickerModel(available, opts.Selected),
		tea.WithInput(opts.Stdin),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return nil, false, NewExitError(ExitGeneral, "error: target picker failed: "+err.Error())
	}

	model, ok := finalModel.(targetPickerModel)
	if !ok || !model.confirmed {
		return nil, false, nil
	}
	return model.selectedTargets(), true, nil
}

type targetPickerModel struct {
	targets   []string
	selected  map[string]bool
	cursor    int
	confirmed bool
	cancelled bool
}

func newTargetPickerModel(available []string, selected []string) targetPickerModel {
	selectedMap := make(map[string]bool, len(selected))
	for _, s := range selected {
		selectedMap[s] = true
	}
	targets := make([]string, 0, len(available)+len(selected))
	targets = append(targets, available...)
	for _, s := range selected {
		found := false
		for _, a := range available {
			if a == s {
				found = true
				break
			}
		}
		if !found && s != "" {
			targets = append(targets, s)
		}
	}
	return targetPickerModel{
		targets:  targets,
		selected: selectedMap,
	}
}

func (m targetPickerModel) Init() tea.Cmd {
	return nil
}

func (m targetPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if len(m.targets) > 0 {
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(m.targets) - 1
			}
		}
		return m, nil
	case tea.KeyDown:
		if len(m.targets) > 0 {
			m.cursor++
			if m.cursor >= len(m.targets) {
				m.cursor = 0
			}
		}
		return m, nil
	case tea.KeySpace:
		m.toggleCurrent()
		return m, nil
	case tea.KeyRunes:
		switch key.String() {
		case "k", "K":
			if len(m.targets) > 0 {
				m.cursor--
				if m.cursor < 0 {
					m.cursor = len(m.targets) - 1
				}
			}
		case "j", "J":
			if len(m.targets) > 0 {
				m.cursor++
				if m.cursor >= len(m.targets) {
					m.cursor = 0
				}
			}
		case " ":
			m.toggleCurrent()
		case "a", "A":
			m.toggleAll()
		case "q", "Q":
			m.cancelled = true
			return m, tea.Quit
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m *targetPickerModel) toggleCurrent() {
	if len(m.targets) == 0 || m.cursor < 0 || m.cursor >= len(m.targets) {
		return
	}
	target := m.targets[m.cursor]
	m.selected[target] = !m.selected[target]
	if !m.selected[target] {
		delete(m.selected, target)
	}
}

func (m *targetPickerModel) toggleAll() {
	allSelected := true
	for _, target := range m.targets {
		if !m.selected[target] {
			allSelected = false
			break
		}
	}
	if allSelected {
		m.selected = make(map[string]bool, len(m.targets))
	} else {
		for _, target := range m.targets {
			m.selected[target] = true
		}
	}
}

func (m targetPickerModel) View() string {
	var b strings.Builder
	b.WriteString("Select target agents:\n\n")
	for i, target := range m.targets {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		marker := "[ ]"
		if m.selected[target] {
			marker = "[✓]"
		}
		_, _ = fmt.Fprintf(&b, "%s%s %s\n", prefix, marker, target)
	}
	b.WriteString("\n(↑/↓ to move, space to toggle, enter to confirm, esc to cancel)\n")
	return b.String()
}

func (m targetPickerModel) selectedTargets() []string {
	selected := make([]string, 0, len(m.selected))
	for _, target := range m.targets {
		if m.selected[target] {
			selected = append(selected, target)
		}
	}
	return selected
}

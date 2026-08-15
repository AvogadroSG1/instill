package instill

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewTargetPickerModelDefaults(t *testing.T) {
	t.Parallel()

	model := newTargetPickerModel(DefaultAvailableTargets, []string{"codex", "claude"})

	if len(model.targets) != len(DefaultAvailableTargets) {
		t.Fatalf("len(targets) = %d, want %d", len(model.targets), len(DefaultAvailableTargets))
	}
	if !model.selected["codex"] {
		t.Error("codex not selected, want selected")
	}
	if !model.selected["claude"] {
		t.Error("claude not selected, want selected")
	}
	if model.selected["opencode"] {
		t.Error("opencode selected, want not selected")
	}
}

func TestNewTargetPickerModelAppendsExtraSelected(t *testing.T) {
	t.Parallel()

	model := newTargetPickerModel(DefaultAvailableTargets, []string{"cursor"})

	found := false
	for _, target := range model.targets {
		if target == "cursor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cursor not found in targets, want appended")
	}
	if !model.selected["cursor"] {
		t.Error("cursor not selected, want selected")
	}
}

func TestTargetPickerNavigationAndSelection(t *testing.T) {
	t.Parallel()

	model := newTargetPickerModel(DefaultAvailableTargets, nil)

	// Move down with down key
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	m := updated.(targetPickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	// Move down with 'j'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(targetPickerModel)
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}

	// Move up with up key
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(targetPickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	// Move up with 'k'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(targetPickerModel)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}

	// Toggle selection with space
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(targetPickerModel)
	if !m.selected["codex"] {
		t.Error("codex should be selected after space")
	}

	// Toggle selection off with space rune
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = updated.(targetPickerModel)
	if m.selected["codex"] {
		t.Error("codex should be deselected after space")
	}

	// Select all with 'a'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(targetPickerModel)
	for _, target := range DefaultAvailableTargets {
		if !m.selected[target] {
			t.Errorf("target %s should be selected after toggle all", target)
		}
	}

	// Deselect all with 'a'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(targetPickerModel)
	if len(m.selected) != 0 {
		t.Errorf("len(selected) = %d, want 0 after second toggle all", len(m.selected))
	}
}

func TestTargetPickerConfirmAndCancel(t *testing.T) {
	t.Parallel()

	model := newTargetPickerModel(DefaultAvailableTargets, []string{"codex", "claude"})

	// Enter confirms
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(targetPickerModel)
	if !m.confirmed {
		t.Error("confirmed should be true after Enter")
	}
	if cmd == nil {
		t.Error("cmd should not be nil after Enter (tea.Quit)")
	}

	selected := m.selectedTargets()
	if len(selected) != 2 || selected[0] != "codex" || selected[1] != "claude" {
		t.Fatalf("selectedTargets() = %#v, want [codex, claude]", selected)
	}

	// Esc cancels
	model2 := newTargetPickerModel(DefaultAvailableTargets, nil)
	updated, _ = model2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(targetPickerModel)
	if !m2.cancelled {
		t.Error("cancelled should be true after Esc")
	}

	// 'q' cancels
	model3 := newTargetPickerModel(DefaultAvailableTargets, nil)
	updated, _ = model3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m3 := updated.(targetPickerModel)
	if !m3.cancelled {
		t.Error("cancelled should be true after 'q'")
	}
}

func TestTargetPickerView(t *testing.T) {
	t.Parallel()

	model := newTargetPickerModel(DefaultAvailableTargets, []string{"claude"})
	view := model.View()

	if !strings.Contains(view, "Select target agents:") {
		t.Error("view missing title")
	}
	if !strings.Contains(view, "> [ ] codex") {
		t.Error("view missing cursor at codex")
	}
	if !strings.Contains(view, "[✓] claude") {
		t.Error("view missing checked claude")
	}
	if !strings.Contains(view, "antigravity") {
		t.Error("view missing antigravity")
	}
}

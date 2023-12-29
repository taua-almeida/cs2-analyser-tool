package multiselect

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type Selection struct {
	SelectedChoices []string
}

func (s *Selection) SetChoices(value string) {
	s.SelectedChoices = append(s.SelectedChoices, value)
}

type model struct {
	viewMsg    string
	cursor     int
	choices    []string
	selected   map[int]struct{}
	selections *Selection
}

func InitialModelMultiSelect(msg string, choices []string, selections *Selection) model {
	m := model{
		viewMsg:    "Select the players you want to analyse:",
		cursor:     0,
		choices:    choices,
		selections: selections,
		selected:   make(map[int]struct{}),
	}
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "crtl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter", " ":
			if _, ok := m.selected[m.cursor]; ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		case "y":
			for i := range m.selected {
				m.selections.SetChoices(m.choices[i])
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	s := m.viewMsg + "\n\n"

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		checked := " "
		if _, ok := m.selected[i]; ok {
			checked = "x"
		}

		s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	s += "\nPress q to quit and y to confirm selection\n"

	return s
}

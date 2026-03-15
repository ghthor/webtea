package teamodel

import (
	tea "charm.land/bubbletea/v2"
)

type String string

func (m String) Init() tea.Cmd {
	return nil
}

func (m String) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m String) View() tea.View {
	return tea.View{Content: string(m)}
}

type ReadonlyView interface {
	View() tea.View
}

type Readonly struct {
	ReadonlyView
}

func (m Readonly) Init() tea.Cmd {
	return nil
}

func (m Readonly) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

package wiztui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── listModel: a single-select list with type-to-filter ──────────────────────

var (
	cursorStyle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	optStyle      = lipgloss.NewStyle().Foreground(colText)
	optDimStyle   = lipgloss.NewStyle().Foreground(colMuted)
	selOptStyle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	titleTxtStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	filterStyle   = lipgloss.NewStyle().Foreground(colYellow)
	posStyle      = lipgloss.NewStyle().Foreground(colMuted)
)

type listModel struct {
	title     string
	context   string // optional context line under the title
	all       []Candidate
	cursor    int
	filter    string
	filtering bool
	chosen    bool
}

func newListModel(title string, opts []Candidate) listModel {
	return listModel{title: title, all: opts}
}

func (m *listModel) setCursorByValue(v string) {
	for i, c := range m.all {
		if c.Value == v {
			m.cursor = i
			return
		}
	}
}

func (m listModel) visible() []Candidate {
	if m.filter == "" {
		return m.all
	}
	f := strings.ToLower(m.filter)
	var out []Candidate
	for _, c := range m.all {
		if strings.Contains(strings.ToLower(c.Label), f) {
			out = append(out, c)
		}
	}
	return out
}

func (m listModel) value() string {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return ""
	}
	return vis[m.cursor].Value
}

func (m listModel) Update(msg tea.KeyMsg) (listModel, tea.Cmd) {
	vis := m.visible()
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			m.filtering = false
		case tea.KeyEsc:
			m.filtering = false
			m.filter = ""
		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			m.filter += string(msg.Runes)
		}
		if m.cursor >= len(m.visible()) {
			m.cursor = len(m.visible()) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case "/":
		m.filtering = true
	case "enter":
		if len(vis) > 0 {
			m.chosen = true
		}
	}
	return m, nil
}

func (m listModel) view(maxHeight int) string {
	var b strings.Builder
	b.WriteString(titleTxtStyle.Render(m.title))
	b.WriteString("\n")
	if m.context != "" {
		b.WriteString(contextStyle.Render(m.context))
		b.WriteString("\n")
	}
	if m.filtering || m.filter != "" {
		b.WriteString(filterStyle.Render("filter: " + m.filter + g.Caret))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	vis := m.visible()
	start, end, more := windowBounds(m.cursor, len(vis), maxHeight)
	for i := start; i < end; i++ {
		c := vis[i]
		cursor := "  "
		line := optStyle.Render(c.Label)
		if i == m.cursor {
			cursor = cursorStyle.Render(g.Cursor)
			line = selOptStyle.Render(c.Label)
		}
		b.WriteString(cursor + line + "\n")
	}
	if more != "" {
		b.WriteString(posStyle.Render(more))
	}
	return b.String()
}

// ── multiListModel: a multi-select list with [ ]/[✓] and type-to-filter ───────

type multiListModel struct {
	title     string
	context   string // optional context line(s) under the title
	all       []Candidate
	cursor    int
	filter    string
	filtering bool
	chosen    bool
}

func newMultiListModel(title string, opts []Candidate) multiListModel {
	return multiListModel{title: title, all: opts}
}

func (m multiListModel) visibleIdx() []int {
	if m.filter == "" {
		idx := make([]int, len(m.all))
		for i := range m.all {
			idx[i] = i
		}
		return idx
	}
	f := strings.ToLower(m.filter)
	var out []int
	for i, c := range m.all {
		if strings.Contains(strings.ToLower(c.Label), f) {
			out = append(out, i)
		}
	}
	return out
}

func (m multiListModel) values() []string {
	var out []string
	for _, c := range m.all {
		if c.Selected {
			out = append(out, c.Value)
		}
	}
	return out
}

func (m multiListModel) selectedCount() int {
	n := 0
	for _, c := range m.all {
		if c.Selected {
			n++
		}
	}
	return n
}

func (m multiListModel) Update(msg tea.KeyMsg) (multiListModel, tea.Cmd) {
	vis := m.visibleIdx()
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			m.filtering = false
		case tea.KeyEsc:
			m.filtering = false
			m.filter = ""
		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
		case tea.KeyRunes:
			m.filter += string(msg.Runes)
		}
		if m.cursor >= len(m.visibleIdx()) {
			m.cursor = len(m.visibleIdx()) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case " ":
		if m.cursor >= 0 && m.cursor < len(vis) {
			realIdx := vis[m.cursor]
			m.all[realIdx].Selected = !m.all[realIdx].Selected
		}
	case "a":
		for _, i := range vis {
			if m.all[i].Value != RescanSentinel {
				m.all[i].Selected = true
			}
		}
	case "n":
		for _, i := range vis {
			m.all[i].Selected = false
		}
	case "/":
		m.filtering = true
	case "enter":
		m.chosen = true
	}
	return m, nil
}

func (m multiListModel) view(maxHeight int) string {
	var b strings.Builder
	count := m.selectedCount()
	header := fmt.Sprintf("%s  (%d selected)", m.title, count)
	b.WriteString(titleTxtStyle.Render(header))
	b.WriteString("\n")
	if m.context != "" {
		b.WriteString(helpTextStyle.Render(m.context))
		b.WriteString("\n")
	}
	if m.filtering || m.filter != "" {
		b.WriteString(filterStyle.Render("filter: " + m.filter + g.Caret))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	vis := m.visibleIdx()
	start, end, more := windowBounds(m.cursor, len(vis), maxHeight)
	for i := start; i < end; i++ {
		realIdx := vis[i]
		c := m.all[realIdx]
		box := g.BoxOff
		lineStyle := optStyle
		if c.Selected {
			box = g.BoxOn
			lineStyle = selOptStyle
		}
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render(g.Cursor)
			lineStyle = lineStyle.Bold(true)
		}
		label := c.Label
		if c.Value == RescanSentinel {
			label = optDimStyle.Render(c.Label)
			box = "    "
		} else {
			label = lineStyle.Render(label)
		}
		b.WriteString(cursor + box + label + "\n")
	}
	if more != "" {
		b.WriteString(posStyle.Render(more))
	}
	return b.String()
}

// ── inputModel: a single text input ──────────────────────────────────────────

type inputModel struct {
	title       string
	description string
	ti          textinput.Model
	optional    bool
	done        bool
}

func newInputModel(title, description, value string, optional bool) inputModel {
	ti := textinput.New()
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Prompt = g.Cursor
	ti.PromptStyle = cursorStyle
	ti.Width = 50
	// Focus immediately on the constructed value so the stored inputModel
	// (copied into Model.nameInput / Model.docsInput by the caller) is
	// already focused. A method that focused m.ti via a value receiver would
	// only mutate a throwaway copy — textinput.Model.Focus() sets its focus
	// flag directly on the receiver rather than through the returned Cmd, so
	// that mutation would never reach the caller's copy (the bug this fixes).
	ti.Focus()
	return inputModel{title: title, description: description, ti: ti, optional: optional}
}

func (m inputModel) value() string { return m.ti.Value() }

func (m inputModel) Update(msg tea.KeyMsg) (inputModel, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		m.done = true
		return m, nil
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m inputModel) view() string {
	var b strings.Builder
	b.WriteString(titleTxtStyle.Render(m.title))
	b.WriteString("\n")
	if m.description != "" {
		b.WriteString(optDimStyle.Render(m.description))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.ti.View())
	return b.String()
}

// windowBounds returns the [start,end) slice of items to render given a cursor
// position, total count, and a max number of visible rows, plus a position
// indicator string when the list overflows (long lists scroll; short lists show
// fully — the action screen's 4 items always fit).
func windowBounds(cursor, total, maxRows int) (start, end int, indicator string) {
	if maxRows < 3 {
		maxRows = 3
	}
	if total <= maxRows {
		return 0, total, ""
	}
	// Center the cursor in the window where possible.
	start = cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	end = start + maxRows
	if end > total {
		end = total
		start = end - maxRows
	}
	indicator = fmt.Sprintf("  ── %d–%d of %d ──", start+1, end, total)
	return start, end, indicator
}

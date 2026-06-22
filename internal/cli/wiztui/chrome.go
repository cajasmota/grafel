package wiztui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Step identifies a stage in the wizard flow, used to highlight the step rail.
type Step int

const (
	StepAction Step = iota
	StepSelect
	StepName
	StepIndex
	StepDone
)

// stepRail is the ordered set of step labels shown in the header.
var stepRail = []struct {
	step  Step
	label string
}{
	{StepAction, "Action"},
	{StepSelect, "Select"},
	{StepName, "Name"},
	{StepIndex, "Index"},
}

// Palette — derived from the charm theme so the TUI feels of-a-piece with huh.
var (
	colAccent  = lipgloss.Color("212") // pink/magenta accent
	colAccent2 = lipgloss.Color("99")  // purple
	colMuted   = lipgloss.Color("241")
	colFaint   = lipgloss.Color("238")
	colText    = lipgloss.Color("252")
	colGreen   = lipgloss.Color("42")
	colYellow  = lipgloss.Color("214")
	colRed     = lipgloss.Color("203")
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(colAccent).
			Bold(true).
			Padding(0, 1)

	railActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(colAccent2).
			Bold(true).
			Padding(0, 1)

	railDoneStyle = lipgloss.NewStyle().
			Foreground(colGreen).
			Padding(0, 1)

	railPendingStyle = lipgloss.NewStyle().
				Foreground(colFaint).
				Padding(0, 1)

	railSepStyle = lipgloss.NewStyle().Foreground(colFaint)

	footerStyle = lipgloss.NewStyle().
			Foreground(colMuted).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colFaint)

	bodyStyle = lipgloss.NewStyle().Padding(1, 1)

	helpTextStyle = lipgloss.NewStyle().Foreground(colMuted)

	contextStyle = lipgloss.NewStyle().Foreground(colAccent2).Italic(true)

	errStyle = lipgloss.NewStyle().Foreground(colRed).Bold(true)
)

// header renders the grafel title plus the step rail with the current step
// highlighted. width bounds the rendering.
func header(current Step, width int) string {
	title := titleStyle.Render("grafel wizard")

	var rail []string
	for i, s := range stepRail {
		var seg string
		switch {
		case s.step == current:
			seg = railActiveStyle.Render(s.label)
		case s.step < current:
			seg = railDoneStyle.Render("✓ " + s.label)
		default:
			seg = railPendingStyle.Render(s.label)
		}
		rail = append(rail, seg)
		if i < len(stepRail)-1 {
			rail = append(rail, railSepStyle.Render("›"))
		}
	}
	railLine := lipgloss.JoinHorizontal(lipgloss.Center, rail...)

	head := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", railLine)
	if width > 0 {
		head = lipgloss.NewStyle().Width(width).Render(head)
	}
	return head
}

// footer renders the contextual key-hint status bar. hint is the per-screen
// key-hint string; width bounds it.
func footer(hint string, width int) string {
	s := footerStyle
	if width > 0 {
		s = s.Width(width)
	}
	return s.Render(helpTextStyle.Render(hint))
}

// frame assembles a full screen: header, body (given the remaining height), and
// footer. body is the already-rendered active screen.
func frame(current Step, body, hint string, width, height int) string {
	head := header(current, width)
	foot := footer(hint, width)

	headH := lipgloss.Height(head)
	footH := lipgloss.Height(foot)
	bodyH := height - headH - footH - 1 // -1 spacer
	if bodyH < 3 {
		bodyH = 3
	}

	bs := bodyStyle
	if width > 0 {
		bs = bs.Width(width)
	}
	renderedBody := bs.Height(bodyH).Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, head, renderedBody, foot)
}

// Common footer hints per screen.
const (
	hintList     = "↑/↓ move · enter confirm · / filter · esc back · ctrl-c quit"
	hintMulti    = "↑/↓ move · space select · a all · n none · enter confirm · / filter · esc back · ctrl-c quit"
	hintInput    = "type to edit · enter confirm · esc back · ctrl-c quit"
	hintInputOpt = "optional · enter to skip · esc back · ctrl-c quit"
	hintIndex    = "indexing… · ctrl-c quit"
	hintDone     = "enter / q to finish"
)

// truncate shortens s to max display columns, appending an ellipsis.
func truncate(s string, max int) string {
	if max <= 1 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	// Coarse rune-based truncation (paths/labels are ASCII-dominant here).
	runes := []rune(s)
	if len(runes) > max-1 {
		runes = runes[:max-1]
	}
	return strings.TrimRight(string(runes), " ") + "…"
}

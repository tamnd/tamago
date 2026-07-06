package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	colAccent = lipgloss.Color("#F2C14E") // yolk
	colDim    = lipgloss.Color("240")
	colGood   = lipgloss.Color("42")
	colBad    = lipgloss.Color("203")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	dimStyle    = lipgloss.NewStyle().Foreground(colDim)
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	paneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colDim).Padding(0, 1)
	statusStyle = lipgloss.NewStyle().Foreground(colDim)
	passStyle   = lipgloss.NewStyle().Foreground(colGood)
	failStyle   = lipgloss.NewStyle().Foreground(colBad)
)

func (m model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView("loading...")
		v.AltScreen = true
		return v
	}
	header := titleStyle.Render("tamago 卵") + dimStyle.Render("  agents that write agents")
	body := m.viewBody()
	status := statusStyle.Render(truncLine(m.status, m.width-2))
	v := tea.NewView(header + "\n" + body + "\n" + status)
	v.AltScreen = true
	return v
}

func (m model) viewBody() string {
	bodyH := max(3, m.height-4) // header + status + borders
	switch m.mode {
	case modeInput:
		label := "new agent, describe the job"
		if m.inKind == inputTask {
			label = "task input"
		}
		box := paneStyle.Width(m.width - 2).Render(titleStyle.Render(label) + "\n" + m.input.View() + "\n" + dimStyle.Render("enter to start, esc to cancel"))
		return lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, box)
	case modeBusy:
		return m.viewStream(bodyH)
	case modeSpec:
		return m.viewSpec(bodyH)
	default:
		return m.viewGarden(bodyH)
	}
}

func (m model) viewGarden(h int) string {
	listW := min(36, m.width/3)
	detailW := m.width - listW - 6

	var list strings.Builder
	if len(m.entries) == 0 {
		list.WriteString(dimStyle.Render("empty garden\n\npress n to hatch\nyour first agent"))
	}
	for i, e := range m.entries {
		line := e.Spec.Name
		if i == m.cursor {
			line = cursorStyle.Render("> " + line)
		} else {
			line = "  " + line
		}
		list.WriteString(truncLine(line, listW) + "\n")
	}

	var detail strings.Builder
	if e := m.selected(); e != nil {
		s := e.Spec
		fmt.Fprintf(&detail, "%s\n", titleStyle.Render(s.Name))
		fmt.Fprintf(&detail, "%s\n\n", truncLine(s.Job, detailW))
		fmt.Fprintf(&detail, "risk %s · tier %s · generation %d\n", s.Risk, s.Tier, s.Meta.Generation)
		if e.LastScore >= 0 {
			style := failStyle
			if e.LastScore >= 70 {
				style = passStyle
			}
			fmt.Fprintf(&detail, "last eval %s\n", style.Render(fmt.Sprintf("%.1f", e.LastScore)))
		} else {
			detail.WriteString(dimStyle.Render("never evaluated") + "\n")
		}
		fmt.Fprintf(&detail, "tools: %s\n", strings.Join(s.Tools, ", "))
		if s.Meta.Parent != "" {
			fmt.Fprintf(&detail, "parent: %s\n", s.Meta.Parent)
		}
		fmt.Fprintf(&detail, "\n%s\n", dimStyle.Render(truncLine(s.Role, detailW*3)))
	} else {
		detail.WriteString(dimStyle.Render("nothing selected"))
	}

	left := paneStyle.Width(listW).Height(h - 2).Render(list.String())
	right := paneStyle.Width(detailW).Height(h - 2).Render(detail.String())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) viewStream(h int) string {
	head := m.spin.View() + " " + titleStyle.Render(m.busyLabel) + dimStyle.Render("  (esc cancels)")
	inner := h - 3
	lines := m.out
	// wrap long lines to the pane width
	wrapped := make([]string, 0, len(lines))
	w := max(10, m.width-6)
	for _, l := range lines {
		for len(l) > w {
			wrapped = append(wrapped, l[:w])
			l = l[w:]
		}
		wrapped = append(wrapped, l)
	}
	start := max(0, len(wrapped)-inner-m.outScroll)
	end := min(len(wrapped), start+inner)
	body := strings.Join(wrapped[start:end], "\n")
	return head + "\n" + paneStyle.Width(m.width-4).Height(h-2).Render(body)
}

func (m model) viewSpec(h int) string {
	lines := strings.Split(m.specText, "\n")
	inner := h - 2
	start := min(m.specScroll, max(0, len(lines)-inner))
	end := min(len(lines), start+inner)
	body := strings.Join(lines[start:end], "\n")
	return paneStyle.Width(m.width - 4).Height(h - 2).Render(body)
}

func truncLine(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if w > 3 && len(s) > w {
		return s[:w-3] + "..."
	}
	return s
}

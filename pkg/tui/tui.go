// Package tui is tamago's dashboard: the garden on the left, the selected
// agent on the right, and live streaming output for hatch, run, eval, and
// grow without leaving the terminal.
package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"

	"github.com/tamnd/tamago/pkg/designer"
	"github.com/tamnd/tamago/pkg/evals"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/grow"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/runner"
)

type mode int

const (
	modeList  mode = iota // browsing the garden
	modeInput             // typing a job (new) or a task (run)
	modeBusy              // an LLM op is streaming
	modeSpec              // scrolling a spec
)

type inputKind int

const (
	inputJob inputKind = iota
	inputTask
)

// messages crossing from worker goroutines into the update loop
type (
	deltaMsg   string
	eventMsg   string
	doneMsg    struct{ summary string }
	failMsg    struct{ err error }
	entriesMsg []garden.Entry
)

type model struct {
	width, height int
	entries       []garden.Entry
	cursor        int
	mode          mode
	inKind        inputKind
	input         textinput.Model
	spin          spinner.Model
	busyLabel     string
	out           []string // streaming pane, one entry per chunk or line
	outScroll     int
	specText      string
	specScroll    int
	status        string
	ch            chan tea.Msg
	cancel        context.CancelFunc
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "describe the job"
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return model{
		input:  ti,
		spin:   sp,
		status: "n new · r run · e eval · g grow · enter spec · q quit",
	}
}

// Run starts the TUI.
func Run() error {
	p := tea.NewProgram(initialModel())
	_, err := p.Run()
	return err
}

func loadEntries() tea.Msg {
	entries, err := garden.List()
	if err != nil {
		return failMsg{err}
	}
	return entriesMsg(entries)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadEntries, m.spin.Tick)
}

// listen pulls the next message a worker goroutine pushed on the channel.
func listen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m model) selected() *garden.Entry {
	if len(m.entries) == 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(20, m.width-8))
		return m, nil

	case entriesMsg:
		m.entries = msg
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case deltaMsg:
		m.appendChunk(string(msg))
		return m, listen(m.ch)

	case eventMsg:
		m.out = append(m.out, string(msg))
		return m, listen(m.ch)

	case doneMsg:
		m.mode = modeList
		m.status = msg.summary
		m.cancel = nil
		return m, loadEntries

	case failMsg:
		m.mode = modeList
		m.status = "error: " + msg.err.Error()
		m.cancel = nil
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}

	switch m.mode {
	case modeInput:
		switch key {
		case "esc":
			m.mode = modeList
			m.input.Blur()
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m, nil
			}
			m.input.Blur()
			m.input.SetValue("")
			return m.startOp(value)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeBusy:
		switch key {
		case "esc":
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		case "up", "k":
			m.outScroll = max(0, m.outScroll-1)
		case "down", "j":
			m.outScroll++
		}
		return m, nil

	case modeSpec:
		switch key {
		case "esc", "q", "enter":
			m.mode = modeList
		case "up", "k":
			m.specScroll = max(0, m.specScroll-1)
		case "down", "j":
			m.specScroll++
		}
		return m, nil
	}

	// modeList
	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.entries)-1, m.cursor+1)
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "n":
		m.mode = modeInput
		m.inKind = inputJob
		m.input.Placeholder = "describe the job, e.g. summarize merged PRs into a changelog"
		return m, m.input.Focus()
	case "r":
		if m.selected() != nil {
			m.mode = modeInput
			m.inKind = inputTask
			m.input.Placeholder = "task for " + m.selected().Spec.Name
			return m, m.input.Focus()
		}
	case "e":
		if e := m.selected(); e != nil {
			return m.startEval(e.Spec.Name)
		}
	case "g":
		if e := m.selected(); e != nil {
			return m.startGrow(e.Spec.Name)
		}
	case "enter":
		if e := m.selected(); e != nil {
			b, err := yaml.Marshal(e.Spec)
			if err == nil {
				m.specText = string(b)
				m.specScroll = 0
				m.mode = modeSpec
			}
		}
	}
	return m, nil
}

// appendChunk merges streamed text chunks into lines for the output pane.
func (m *model) appendChunk(chunk string) {
	if len(m.out) == 0 {
		m.out = append(m.out, "")
	}
	parts := strings.Split(chunk, "\n")
	m.out[len(m.out)-1] += parts[0]
	for _, p := range parts[1:] {
		m.out = append(m.out, p)
	}
}

func (m model) beginBusy(label string) (model, context.Context) {
	m.mode = modeBusy
	m.busyLabel = label
	m.out = nil
	m.outScroll = 0
	ctx, cancel := context.WithCancel(context.Background())
	m.ch = make(chan tea.Msg, 64)
	m.cancel = cancel
	return m, ctx
}

func (m model) startOp(value string) (tea.Model, tea.Cmd) {
	switch m.inKind {
	case inputJob:
		return m.startNew(value)
	default:
		e := m.selected()
		if e == nil {
			m.mode = modeList
			return m, nil
		}
		return m.startRun(e.Spec.Name, value)
	}
}

func (m model) startNew(job string) (tea.Model, tea.Cmd) {
	next, ctx := m.beginBusy("hatching: " + job)
	ch := next.ch
	go func() {
		cl := llm.New()
		s, err := designer.Design(ctx, cl, job, designer.Options{}, func(chunk string) {
			ch <- deltaMsg(chunk)
		})
		if err != nil {
			ch <- failMsg{err}
			return
		}
		if err := garden.Save(s); err != nil {
			ch <- failMsg{err}
			return
		}
		ch <- doneMsg{fmt.Sprintf("hatched %s (risk %s, tier %s, %d fixtures)", s.Name, s.Risk, s.Tier, len(s.Evals))}
	}()
	return next, listen(ch)
}

func (m model) startRun(name, task string) (tea.Model, tea.Cmd) {
	next, ctx := m.beginBusy("running " + name)
	ch := next.ch
	go func() {
		cl := llm.New()
		s, err := garden.Load(name)
		if err != nil {
			ch <- failMsg{err}
			return
		}
		if _, err := runner.Run(ctx, cl, s, task, func(chunk string) {
			ch <- deltaMsg(chunk)
		}); err != nil {
			ch <- failMsg{err}
			return
		}
		ch <- doneMsg{name + " finished"}
	}()
	return next, listen(ch)
}

func (m model) startEval(name string) (tea.Model, tea.Cmd) {
	next, ctx := m.beginBusy("evaluating " + name)
	ch := next.ch
	go func() {
		cl := llm.New()
		s, err := garden.Load(name)
		if err != nil {
			ch <- failMsg{err}
			return
		}
		rep, err := evals.Run(ctx, cl, s, func(e evals.Event) {
			switch e.Stage {
			case "run":
				ch <- eventMsg(fmt.Sprintf("[%d/%d] running: %.60s", e.Fixture, e.Total, e.Detail))
			case "done":
				ch <- eventMsg(fmt.Sprintf("[%d/%d] %s", e.Fixture, e.Total, e.Detail))
			}
		})
		if err != nil {
			ch <- failMsg{err}
			return
		}
		if err := evals.Persist(name, rep); err != nil {
			ch <- failMsg{err}
			return
		}
		verdict := "FAIL"
		if rep.Pass {
			verdict = "PASS"
		}
		ch <- doneMsg{fmt.Sprintf("%s mean %.1f, %s", name, rep.Mean, verdict)}
	}()
	return next, listen(ch)
}

func (m model) startGrow(name string) (tea.Model, tea.Cmd) {
	next, ctx := m.beginBusy("growing " + name)
	ch := next.ch
	go func() {
		cl := llm.New()
		s, err := garden.Load(name)
		if err != nil {
			ch <- failMsg{err}
			return
		}
		results, err := grow.Grow(ctx, cl, s, 1,
			func(e grow.Event) { ch <- eventMsg(fmt.Sprintf("round %d %s: %s", e.Round, e.Stage, e.Detail)) },
			func(e evals.Event) {
				if e.Stage == "done" {
					ch <- eventMsg(fmt.Sprintf("  fixture %d/%d: %s", e.Fixture, e.Total, e.Detail))
				}
			})
		if err != nil {
			ch <- failMsg{err}
			return
		}
		last := results[len(results)-1]
		if last.Kept {
			ch <- doneMsg{fmt.Sprintf("%s grew to generation %d: %.1f -> %.1f", name, last.Generation, last.Before, last.After)}
		} else {
			ch <- doneMsg{fmt.Sprintf("%s unchanged: candidate scored %.1f vs %.1f", name, last.After, last.Before)}
		}
	}()
	return next, listen(ch)
}

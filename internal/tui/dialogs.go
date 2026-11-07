package tui

// The small layers over a view: a line to type into and a question to
// answer.

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// prompt asks for one line.
type prompt struct {
	title, hint string
	value       string
	submit      func(m model, value string) (model, tea.Cmd)
	// live runs on every change, like the log filter.
	live func(m model, value string) model
}

// confirm asks before an action. With name set the user types it first,
// for what cannot be undone.
type confirm struct {
	title, detail, verb string
	danger              bool
	name, typed         string
	run                 tea.Cmd
}

// edit applies a key to a line of text; ok is false when the key is not
// for the line.
func edit(value string, k tea.KeyPressMsg) (string, bool) {
	switch k.String() {
	case "backspace":
		r := []rune(value)
		if len(r) > 0 {
			value = string(r[:len(r)-1])
		}
		return value, true
	case "ctrl+u":
		return "", true
	case "ctrl+w":
		value = strings.TrimRight(value, " ")
		if i := strings.LastIndex(value, " "); i >= 0 {
			return value[:i+1], true
		}
		return "", true
	case "space":
		return value + " ", true
	}
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		return value + k.Text, true
	}
	return value, false
}

func (m model) ask1(title, detail, verb string, danger bool, run tea.Cmd) model {
	m.ask = &confirm{title: title, detail: detail, verb: verb, danger: danger, run: run}
	return m
}

func (m model) askName(title, detail, name string, run tea.Cmd) model {
	m.ask = &confirm{title: title, detail: detail, verb: "restore", danger: true, name: name, run: run}
	return m
}

func (m model) input(title, hint, value string, submit func(m model, value string) (model, tea.Cmd)) model {
	m.prompt = &prompt{title: title, hint: hint, value: value, submit: submit}
	return m
}

func (m model) askKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := *m.ask
	s := k.String()
	if s == "esc" {
		m.ask = nil
		return m, nil
	}
	if a.name != "" {
		if s == "enter" {
			if a.typed != a.name {
				return m, nil
			}
			m.ask = nil
			return m, a.run
		}
		a.typed, _ = edit(a.typed, k)
		m.ask = &a
		return m, nil
	}
	switch s {
	case "y", "enter":
		m.ask = nil
		return m, a.run
	case "n", "q":
		m.ask = nil
	}
	return m, nil
}

func (m model) promptKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.prompt
	switch k.String() {
	case "esc":
		m.prompt = nil
		if p.live != nil {
			m = p.live(m, "")
		}
		return m, nil
	case "enter":
		m.prompt = nil
		if p.submit == nil {
			return m, nil
		}
		return p.submit(m, strings.TrimSpace(p.value))
	}
	value, ok := edit(p.value, k)
	if !ok {
		return m, nil
	}
	p.value = value
	m.prompt = &p
	if p.live != nil {
		m = p.live(m, value)
	}
	return m, nil
}

// ago is a rough age: "now", "40s", "5m", "3h", "2d".
func ago(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 5*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// span is a duration in a few characters: "850ms", "16s", "2m 5s".
func span(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour && int(d.Seconds())%60 == 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case int(d.Minutes())%60 == 0:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

// Counts, gg and G move in every list.
func TestMotions(t *testing.T) {
	m := frames()["apps"]
	if m = press(m, "2", "j"); m.appSelected != 2 {
		t.Errorf("2j selects %d", m.appSelected)
	}
	if m = press(m, "G"); m.appSelected != len(m.apps)-1 {
		t.Errorf("G selects %d", m.appSelected)
	}
	if m = press(m, "g", "g"); m.appSelected != 0 {
		t.Errorf("gg selects %d", m.appSelected)
	}
	if m = press(m, "2", "G"); m.appSelected != 1 {
		t.Errorf("2G selects %d", m.appSelected)
	}
	if m = press(m, "9", "9", "j"); m.appSelected != len(m.apps)-1 || m.count != 0 {
		t.Errorf("99j selects %d, count %d left", m.appSelected, m.count)
	}
	if m = press(m, "ctrl+u"); m.appSelected != 0 {
		t.Errorf("ctrl+u selects %d", m.appSelected)
	}
	if m = command(m, "3"); m.appSelected != 2 {
		t.Errorf(":3 selects %d", m.appSelected)
	}
	servers := press(frames()["servers"], "G")
	if servers.selected != len(servers.servers)-1 {
		t.Errorf("G on servers selects %d", servers.selected)
	}
}

// gt and gT go through the tabs, a count picks one.
func TestTabKeys(t *testing.T) {
	m := frames()["servers"]
	if m = press(m, "g", "t"); m.tab != tabApps {
		t.Errorf("gt goes to %d", m.tab)
	}
	if m = press(m, "g", "T", "g", "T"); m.tab != tabSettings {
		t.Errorf("gT twice goes to %d", m.tab)
	}
	if m = press(m, "3", "g", "t"); m.tab != tabActivity {
		t.Errorf("3gt goes to %d", m.tab)
	}
	if m = command(m, "team"); m.tab != tabTeam {
		t.Errorf(":team goes to %d", m.tab)
	}
}

// l opens an app and h goes back; gh opens GitHub.
func TestIntoAndBack(t *testing.T) {
	m := frames()["detail"]
	if m = press(m, "j"); m.deployPick == 0 {
		t.Fatal("j picks nothing in an open app")
	}
	if m = press(m, "h"); m.deployPick != 0 || m.detail == nil {
		t.Errorf("h drops the pick first")
	}
	if m = press(m, "h"); m.detail != nil {
		t.Errorf("h does not leave the app")
	}
	if m = press(frames()["detail"], "g", "h"); m.mode != modeGitHub {
		t.Errorf("gh opens mode %d", m.mode)
	}
}

// / finds a row, n and N go on around the ends, a miss says so.
func TestSearch(t *testing.T) {
	m := frames()["apps"]
	m = press(m, "/")
	m.input = "o"
	if m = press(m, "enter"); m.appSelected != 1 || m.search != "o" {
		t.Fatalf("/o selects %d", m.appSelected)
	}
	if m = press(m, "n"); m.appSelected != 2 {
		t.Errorf("n selects %d", m.appSelected)
	}
	if m = press(m, "n", "n"); m.appSelected != 0 || !strings.Contains(m.notice, "BOTTOM") {
		t.Errorf("n past the end selects %d, notice %q", m.appSelected, m.notice)
	}
	if m = press(m, "N"); m.appSelected != 3 {
		t.Errorf("N selects %d", m.appSelected)
	}
	m = press(m, "/")
	m.input = "Shop"
	if m = press(m, "enter"); m.err == nil {
		t.Errorf("a capital letter still ignores case")
	}
	m = press(m, "/")
	m.input = "nothing"
	if m = press(m, "enter"); m.err == nil || !strings.Contains(m.err.Error(), "E486") {
		t.Errorf("a miss says %v", m.err)
	}
}

// The command line runs keys by name, completes and says what it does not know.
func TestCommandLine(t *testing.T) {
	m := press(frames()["apps"], ":")
	m.input = "q"
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !quits(cmd) {
		t.Error(":q does not quit")
	}
	if m := command(frames()["apps"], "bogus"); m.err == nil || !strings.Contains(m.err.Error(), "E492") {
		t.Errorf(":bogus says %v", m.err)
	}
	if m := command(frames()["servers"], "restart"); m.err == nil || !strings.Contains(m.err.Error(), "apps tab") {
		t.Errorf(":restart on servers says %v", m.err)
	}
	if m := command(frames()["apps"], "env"); m.mode != modeEnv {
		t.Errorf(":env opens mode %d", m.mode)
	}
	if m := command(frames()["servers"], "rename box-2"); m.mode != modeForm || m.input != "box-2" {
		t.Errorf(":rename box-2 opens mode %d with %q", m.mode, m.input)
	}
	m = press(frames()["apps"], ":")
	m.input = "red"
	if m = press(m, "tab"); m.input != "redeploy " {
		t.Errorf("tab completes red to %q", m.input)
	}
	m.input = "re"
	if m = press(m, "tab"); m.input != "re" || !strings.Contains(m.notice, "restart") {
		t.Errorf("tab on re: %q, notice %q", m.input, m.notice)
	}
	if m := command(frames()["servers"], "e blog"); m.tab != tabApps || m.appSelected != 1 {
		t.Errorf(":e blog: tab %d, app %d", m.tab, m.appSelected)
	}
	if m := command(frames()["servers"], "exec ls /data"); m.err == nil || !strings.Contains(m.err.Error(), "pick an app") {
		t.Errorf(":exec on servers says %v", m.err)
	}
	if m := command(frames()["apps"], "exec"); m.err == nil || !strings.Contains(m.err.Error(), "give a command") {
		t.Errorf(":exec alone says %v", m.err)
	}
	m = frames()["apps"]
	m.outputID, m.running = 7, true
	next, _ := m.Update(followDoneMsg{id: 7, err: errors.New("ls exited with 2 in web"), label: "exit 2"})
	if m = next.(model); m.running || !strings.Contains(strip(m.outputNote), "exit 2") {
		t.Errorf("an exec that failed ends with %q", strip(m.outputNote))
	}
}

// ZZ quits, ? opens help and esc closes it.
func TestQuitAndHelp(t *testing.T) {
	m := press(frames()["apps"], "Z")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'Z', Text: "Z"}); !quits(cmd) {
		t.Error("ZZ does not quit")
	}
	m = frames()["apps"]
	m.width, m.height = 100, 20
	if m = press(m, "?"); m.mode != modeHelp || !strings.Contains(strip(m.View().Content), "half a page down") {
		t.Fatalf("? opens no help")
	}
	if m = press(m, "G"); m.helpTop == 0 {
		t.Error("G does not scroll help")
	}
	if m = press(m, "q"); m.mode != modeList {
		t.Error("q does not close help")
	}
}

// The logs scroll with the vim keys, stay put while lines come in and G
// follows the end again.
func TestPanelScroll(t *testing.T) {
	m := frames()["apps"]
	m.shared = &shared{}
	m.width, m.height = 100, 30
	m.outputTitle, m.outputID, m.panelFocus = "logs of hello", 1, true
	for index := range 100 {
		m.output = append(m.output, fmt.Sprintf("web-1  | line %d", index))
	}
	m.View()
	if m = press(m, "k", "k"); m.outputScroll != 2 {
		t.Fatalf("kk scrolls %d", m.outputScroll)
	}
	next, _ := m.Update(lineMsg{id: 1, line: "web-1  | new"})
	if m = next.(model); m.outputScroll != 3 {
		t.Errorf("a new line moves the view: %d", m.outputScroll)
	}
	if !strings.Contains(strip(m.View().Content), "3 more below") {
		t.Errorf("the panel does not say what is below")
	}
	if m = press(m, "g", "g"); !strings.Contains(strip(m.View().Content), "line 0") {
		t.Errorf("gg does not show the first line")
	}
	m = press(m, "/")
	m.input = "line 97"
	if m = press(m, "enter"); m.panelMatch != 98 || !strings.Contains(strip(m.View().Content), "line 97") {
		t.Errorf("/ in the panel matches line %d", m.panelMatch-1)
	}
	if m = press(m, "G"); m.outputScroll != 0 {
		t.Errorf("G leaves %d lines below", m.outputScroll)
	}
	if m = press(m, "esc"); m.panelFocus || m.outputTitle == "" {
		t.Errorf("esc closes the panel instead of leaving it")
	}
	if m = press(m, "ctrl+w"); !m.panelFocus {
		t.Errorf("ctrl+w does not go back to the panel")
	}
}

// ctrl+w takes the last word off an input.
func TestCtrlW(t *testing.T) {
	m := press(frames()["apps"], "e")
	m.input = "KEY=one two  "
	if m = press(m, "ctrl+w"); m.input != "KEY=one " {
		t.Errorf("ctrl+w leaves %q", m.input)
	}
}

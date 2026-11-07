package pages

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
)

// LogLine is one line of a deploy log as the web shows it: its number, its
// text and its kind (step, out, err or ok). A step that names a long path
// shows the path short and dim, the whole one on hover.
type LogLine struct {
	N    int
	Text string
	Kind string
	Path string
}

// LogFold is the tool output between two steps, folded into one line that
// says how many lines it holds.
type LogFold struct {
	ID    int
	Step  string
	Lines []LogLine
	Count int
}

// LogItem is a step on its own line, or a fold.
type LogItem struct {
	Line *LogLine
	Fold *LogFold
}

// LogChunk is what one message of the log stream adds: lines that continue
// the fold the message before left open, then new items under them.
type LogChunk struct {
	Continue *LogFold
	Items    []LogItem
}

// LogFolder sorts a deploy log into steps and folds as it streams, and
// keeps its place from one chunk to the next.
type LogFolder struct {
	n     int
	folds int
	open  *LogFold
	step  string
	last  string
}

// Add sorts the next lines of the log.
func (f *LogFolder) Add(lines []string) LogChunk {
	var chunk LogChunk
	var fresh *LogFold // a fold this chunk opened
	for _, text := range lines {
		f.n++
		f.last = text
		if logOutput(text) {
			if f.open == nil {
				f.folds++
				f.open = &LogFold{ID: f.folds, Step: f.step}
				fresh = f.open
				chunk.Items = append(chunk.Items, LogItem{Fold: f.open})
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			f.open.Count++
			line := LogLine{N: f.n, Text: text, Kind: logKind(text)}
			if f.open == fresh {
				f.open.Lines = append(f.open.Lines, line)
				continue
			}
			if chunk.Continue == nil {
				chunk.Continue = &LogFold{ID: f.open.ID, Step: f.open.Step}
			}
			chunk.Continue.Lines = append(chunk.Continue.Lines, line)
			chunk.Continue.Count = f.open.Count
			continue
		}
		f.open = nil
		f.step = strings.Fields(text + " ")[0]
		line := logStep(f.n, text)
		chunk.Items = append(chunk.Items, LogItem{Line: &line})
	}
	return chunk
}

// End is the last line of a finished deploy, unless the log says it already.
func (f *LogFolder) End(number int, status, reason string) LogChunk {
	end, kind := "", ""
	switch status {
	case "succeeded":
		end, kind = fmt.Sprintf("v%d is live", number), "ok"
	case "failed":
		end, kind = fmt.Sprintf("v%d failed: %s", number, reason), "err"
	}
	if end == "" || f.last == end {
		return LogChunk{}
	}
	f.n++
	f.open = nil
	f.last = end
	return LogChunk{Items: []LogItem{{Line: &LogLine{N: f.n, Text: end, Kind: kind}}}}
}

var (
	logOutputLine = regexp.MustCompile(`^\s|^#\d|^\[`)
	logErrorWord  = regexp.MustCompile(`(?i)\b(error|failed|fatal)\b`)
	logLiveLine   = regexp.MustCompile(`^v\d+ is live$`)
	logStepPath   = regexp.MustCompile(`^(.*\S)\s+\((/[^)\s]{24,})\)$`)
)

// logOutput is a line of tool output (a build, a pull), not one of moin's steps.
func logOutput(line string) bool {
	return strings.TrimSpace(line) == "" || logOutputLine.MatchString(line)
}

func logKind(line string) string {
	switch {
	case logOutput(line) && logErrorWord.MatchString(line):
		return "err"
	case logOutput(line):
		return "out"
	case logLiveLine.MatchString(line):
		return "ok"
	case logErrorWord.MatchString(line):
		return "err"
	}
	return "step"
}

func logStep(n int, text string) LogLine {
	line := LogLine{N: n, Text: text, Kind: logKind(text)}
	if line.Kind == "step" {
		if m := logStepPath.FindStringSubmatch(text); m != nil {
			line.Text, line.Path = m[1], m[2]
		}
	}
	return line
}

// foldLabel says what a fold holds: "12 build lines".
func foldLabel(fold LogFold) string {
	what := map[string]string{"building": "build", "starting": "compose", "pulling": "pull"}[fold.Step]
	if what == "" {
		what = "output"
	}
	if fold.Count == 1 {
		return fmt.Sprintf("1 %s line", what)
	}
	return fmt.Sprintf("%d %s lines", fold.Count, what)
}

// selectedNumber is the deploy whose log shows: the one picked, or the
// newest.
func selectedNumber(deploys []api.Deploy, selected int) int {
	if selected > 0 {
		return selected
	}
	if len(deploys) > 0 {
		return deploys[0].Number
	}
	return 0
}

func selectedDeploy(deploys []api.Deploy, selected int) *api.Deploy {
	number := selectedNumber(deploys, selected)
	for i := range deploys {
		if deploys[i].Number == number {
			return &deploys[i]
		}
	}
	return nil
}

// deployHref is the page of a deploy's log; the newest one's is the
// Deploys section itself, which follows the next deploy.
func deployHref(basePath, app string, deploys []api.Deploy, number int) string {
	if len(deploys) > 0 && deploys[0].Number == number {
		return basePath + "/apps/" + app + "/deploys"
	}
	return basePath + "/apps/" + app + "/deploys/" + strconv.Itoa(number)
}

// detailHref reloads the deploys on a change; it says which log shows, so
// a new deploy can take the log when the page follows.
func detailHref(basePath, app string, selected int, follow bool, deploys []api.Deploy) string {
	query := url.Values{}
	query.Set("n", strconv.Itoa(selected))
	if follow {
		query.Set("shown", strconv.Itoa(selectedNumber(deploys, 0)))
	}
	return basePath + "/apps/" + app + "/detail?" + query.Encode()
}

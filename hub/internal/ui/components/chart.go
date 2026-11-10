package components

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

// ChartLine is one line of a Chart: what it is ("In") and a value for each
// time of the chart. NaN is no value: the line breaks there.
type ChartLine struct {
	Label  string
	Values []float64
}

// ChartProps is a Chart: the times, one or two lines over them, and how
// its values and times read.
type ChartProps struct {
	Times []time.Time
	Lines []ChartLine
	// Top is the top of the scale, like the memory a server has; 0 is the
	// highest value. The scale always starts at 0.
	Top float64
	// Gap breaks the lines where two times lie further apart than this,
	// so a pause in the data shows as one; 0 never breaks.
	Gap time.Duration
	// Value reads a value ("12%"), Time a time under the pointer
	// ("Nov 3, 12:04 UTC"), Tick a time on the axis ("12:04").
	Value func(float64) string
	Time  func(time.Time) string
	Tick  func(time.Time) string
	// Marks are words at a time, by its index: "v12 went live".
	Marks map[int]string
}

// chartLineData and chartData are the data of a <moin-chart> (ui/docs,
// Line chart): the numbers it draws and the words it shows.
type chartLineData struct {
	Label  string     `json:"label"`
	Values []*float64 `json:"values"`
	Text   []string   `json:"text"`
}

type chartData struct {
	Lines []chartLineData `json:"lines"`
	At    []float64       `json:"at"`
	Gap   float64         `json:"gap,omitempty"`
	Top   float64         `json:"top"`
	Scale []string        `json:"scale"`
	Times []string        `json:"times"`
	Ticks []string        `json:"ticks"`
	Marks map[int]string  `json:"marks,omitempty"`
}

// chartJSON is the data of the chart as JSON; the encoder escapes < and >,
// so it cannot end the script element it sits in.
func chartJSON(p ChartProps) string {
	top := chartTop(p)
	data := chartData{
		Lines: make([]chartLineData, len(p.Lines)),
		At:    make([]float64, len(p.Times)),
		Gap:   p.Gap.Seconds(),
		Top:   top,
		Scale: []string{p.Value(top), p.Value(top / 2), p.Value(0)},
		Times: make([]string, len(p.Times)),
		Ticks: chartTicks(p),
	}
	for i, at := range p.Times {
		data.At[i] = at.Sub(p.Times[0]).Seconds()
		data.Times[i] = p.Time(at)
	}
	for n, line := range p.Lines {
		values := make([]*float64, len(p.Times))
		text := make([]string, len(p.Times))
		for i := range p.Times {
			text[i] = "—"
			if i < len(line.Values) && !math.IsNaN(line.Values[i]) {
				values[i] = &line.Values[i]
				text[i] = p.Value(line.Values[i])
			}
		}
		data.Lines[n] = chartLineData{Label: line.Label, Values: values, Text: text}
	}
	for index, words := range p.Marks {
		if index >= 0 && index < len(p.Times) {
			if data.Marks == nil {
				data.Marks = map[int]string{}
			}
			data.Marks[index] = words
		}
	}
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	if err := encoder.Encode(data); err != nil {
		return "{}"
	}
	return strings.TrimSpace(out.String())
}

// chartTop is the top of the scale: Top, or the highest value a little
// rounded up, and 1 when there is nothing above 0.
func chartTop(p ChartProps) float64 {
	if p.Top > 0 {
		return p.Top
	}
	top := 0.0
	for _, line := range p.Lines {
		for _, v := range line.Values {
			if !math.IsNaN(v) && v > top {
				top = v
			}
		}
	}
	if top <= 0 {
		return 1
	}
	return niceCeil(top * 1.1)
}

// niceCeil rounds up to 1, 2, 2.5 or 5 times a power of ten, so the axis
// reads 50%, not 47.3%.
func niceCeil(v float64) float64 {
	power := math.Pow(10, math.Floor(math.Log10(v)))
	for _, step := range []float64{1, 2, 2.5, 5, 10} {
		if v <= step*power {
			return step * power
		}
	}
	return 10 * power
}

// chartTicks are the times on the axis: the first, the middle and the last.
func chartTicks(p ChartProps) []string {
	n := len(p.Times)
	switch {
	case n == 0:
		return []string{}
	case n == 1:
		return []string{p.Tick(p.Times[0])}
	case n == 2:
		return []string{p.Tick(p.Times[0]), p.Tick(p.Times[1])}
	}
	return []string{p.Tick(p.Times[0]), p.Tick(p.Times[n/2]), p.Tick(p.Times[n-1])}
}

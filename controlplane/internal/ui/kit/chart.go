package kit

import (
	"encoding/json"
	"math"
	"strconv"
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

// The plot is drawn in a box of chartWidth × chartHeight; the SVG
// stretches it to its place, and the strokes keep their width.
const (
	chartWidth  = 1000
	chartHeight = 100
)

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

// chartXs places the times from 0 (the first) to 1 (the last); a single
// time sits in the middle.
func chartXs(times []time.Time) []float64 {
	xs := make([]float64, len(times))
	if len(times) == 1 {
		xs[0] = 0.5
		return xs
	}
	if len(times) == 0 {
		return xs
	}
	span := times[len(times)-1].Sub(times[0]).Seconds()
	for i, t := range times {
		if span > 0 {
			xs[i] = t.Sub(times[0]).Seconds() / span
		} else {
			xs[i] = float64(i) / float64(len(times)-1)
		}
	}
	return xs
}

// chartBreaks marks each time that starts a new piece of line: the time
// before it is more than gap away.
func chartBreaks(times []time.Time, gap time.Duration) []bool {
	breaks := make([]bool, len(times))
	if gap <= 0 {
		return breaks
	}
	for i := 1; i < len(times); i++ {
		breaks[i] = times[i].Sub(times[i-1]) > gap
	}
	return breaks
}

// chartPaths is one line in the box, as SVG path data: the line, and the
// area under it down to 0. xs run from 0 to 1, values from 0 to top. A NaN
// value, or a time in breaks, starts a new piece; a piece of one point is
// a dot (a round cap on a line of no length) and has no area. No values
// is no path.
func chartPaths(xs, values []float64, breaks []bool, top float64) (line, area string) {
	var l, a strings.Builder
	var piece [][2]float64
	flush := func() {
		if len(piece) == 0 {
			return
		}
		for i, pt := range piece {
			if i == 0 {
				l.WriteString("M" + chartNum(pt[0]) + " " + chartNum(pt[1]))
			} else {
				l.WriteString("L" + chartNum(pt[0]) + " " + chartNum(pt[1]))
			}
		}
		if len(piece) == 1 {
			l.WriteString("h0")
		} else {
			a.WriteString("M" + chartNum(piece[0][0]) + " " + chartNum(chartHeight))
			for _, pt := range piece {
				a.WriteString("L" + chartNum(pt[0]) + " " + chartNum(pt[1]))
			}
			a.WriteString("L" + chartNum(piece[len(piece)-1][0]) + " " + chartNum(chartHeight) + "Z")
		}
		piece = piece[:0]
	}
	for i, v := range values {
		if i >= len(xs) {
			break
		}
		if math.IsNaN(v) || (i < len(breaks) && breaks[i]) {
			flush()
		}
		if math.IsNaN(v) {
			continue
		}
		piece = append(piece, [2]float64{xs[i] * chartWidth, chartY(v, top)})
	}
	flush()
	return l.String(), a.String()
}

// chartY is where a value sits from the top of the box, inside it.
func chartY(v, top float64) float64 {
	y := chartHeight - v/top*chartHeight
	return math.Max(0, math.Min(chartHeight, y))
}

// chartNum is a coordinate, one decimal at most: short paths.
func chartNum(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}

// chartPoint is what the pointer shows at one time: where it is (x and the
// y of each line in percent of the plot), the values read, the time and
// a mark. The script of Chart reads them from data-chart-points.
type chartPoint struct {
	X    float64   `json:"x"`
	Y    []float64 `json:"y"`
	V    []string  `json:"v"`
	T    string    `json:"t"`
	Mark string    `json:"m,omitempty"`
}

// chartPoints are the points of the chart for its script, as JSON; the
// encoder escapes < and >, so it cannot end the script it sits in.
func chartPoints(p ChartProps, xs []float64, top float64) string {
	points := make([]chartPoint, len(p.Times))
	for i := range p.Times {
		point := chartPoint{X: math.Round(xs[i]*10000) / 100, T: p.Time(p.Times[i]), Mark: p.Marks[i]}
		for _, line := range p.Lines {
			v := math.NaN()
			if i < len(line.Values) {
				v = line.Values[i]
			}
			if math.IsNaN(v) {
				point.Y = append(point.Y, -1)
				point.V = append(point.V, "—")
				continue
			}
			point.Y = append(point.Y, math.Round(chartY(v, top)*100)/100)
			point.V = append(point.V, p.Value(v))
		}
		points[i] = point
	}
	data, _ := json.Marshal(points)
	return string(data)
}

// chartLast is the index of the newest time with a value, -1 for none: what
// the chart shows while the pointer is away.
func chartLast(p ChartProps) int {
	for i := len(p.Times) - 1; i >= 0; i-- {
		for _, line := range p.Lines {
			if i < len(line.Values) && !math.IsNaN(line.Values[i]) {
				return i
			}
		}
	}
	return -1
}

// chartTicks are the times on the axis: the first, the middle and the last.
func chartTicks(p ChartProps) []string {
	n := len(p.Times)
	switch {
	case n == 0:
		return nil
	case n == 1:
		return []string{p.Tick(p.Times[0])}
	case n == 2:
		return []string{p.Tick(p.Times[0]), p.Tick(p.Times[1])}
	}
	return []string{p.Tick(p.Times[0]), p.Tick(p.Times[n/2]), p.Tick(p.Times[n-1])}
}

// chartLineValue is the value of line at index, or "—".
func chartLineValue(p ChartProps, line ChartLine, index int) string {
	if index < 0 || index >= len(line.Values) || math.IsNaN(line.Values[index]) {
		return "—"
	}
	return p.Value(line.Values[index])
}

// chartPercent is a share of the plot as a CSS percentage.
func chartPercent(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) + "%"
}

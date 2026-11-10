package components

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestChartPaths(t *testing.T) {
	nan := math.NaN()
	for _, c := range []struct {
		name       string
		xs, values []float64
		breaks     []bool
		line, area string
		top        float64
	}{
		{name: "empty", top: 1},
		{name: "one point is a dot", xs: []float64{0.5}, values: []float64{50}, top: 100, line: "M500 50h0"},
		{
			name: "a line and its area", xs: []float64{0, 0.5, 1}, values: []float64{0, 50, 100}, top: 100,
			line: "M0 100L500 50L1000 0", area: "M0 100L0 100L500 50L1000 0L1000 100Z",
		},
		{
			name: "no value breaks the line", xs: []float64{0, 0.25, 0.5, 1}, values: []float64{10, 20, nan, 30}, top: 100,
			line: "M0 90L250 80M1000 70h0", area: "M0 100L0 90L250 80L250 100Z",
		},
		{
			name: "a gap in time breaks the line", xs: []float64{0, 0.1, 0.9, 1}, values: []float64{10, 10, 20, 20}, breaks: []bool{false, false, true, false}, top: 100,
			line: "M0 90L100 90M900 80L1000 80", area: "M0 100L0 90L100 90L100 100ZM900 100L900 80L1000 80L1000 100Z",
		},
		{name: "values over the top stay in the box", xs: []float64{0, 1}, values: []float64{-5, 150}, top: 100, line: "M0 100L1000 0", area: "M0 100L0 100L1000 0L1000 100Z"},
	} {
		line, area := chartPaths(c.xs, c.values, c.breaks, c.top)
		if line != c.line || area != c.area {
			t.Errorf("%s: got line %q area %q, want %q %q", c.name, line, area, c.line, c.area)
		}
	}
}

func TestChartScale(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if xs := chartXs([]time.Time{at}); xs[0] != 0.5 {
		t.Errorf("one time sits at %v, want the middle", xs[0])
	}
	xs := chartXs([]time.Time{at, at.Add(time.Minute), at.Add(4 * time.Minute)})
	if xs[0] != 0 || xs[1] != 0.25 || xs[2] != 1 {
		t.Errorf("xs %v follow the times", xs)
	}
	breaks := chartBreaks([]time.Time{at, at.Add(time.Minute), at.Add(10 * time.Minute)}, 2*time.Minute)
	if breaks[1] || !breaks[2] {
		t.Errorf("breaks %v: only after the pause", breaks)
	}
	for _, c := range []struct {
		values    []float64
		top, want float64
	}{
		{nil, 0, 1},
		{[]float64{0, 0}, 0, 1},
		{[]float64{43}, 0, 50},
		{[]float64{43}, 100, 100},
		{[]float64{1.9e9}, 0, 2.5e9},
	} {
		if got := chartTop(ChartProps{Lines: []ChartLine{{Values: c.values}}, Top: c.top}); got != c.want {
			t.Errorf("top of %v (%v) is %v, want %v", c.values, c.top, got, c.want)
		}
	}
}

// A chart renders with no times, one time and a gap, and shows the newest
// value and its mark.
func TestChartRenders(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	value := func(v float64) string { return chartNum(v) + "%" }
	clock := func(t time.Time) string { return t.Format("15:04") }
	for _, p := range []ChartProps{
		{Lines: []ChartLine{{Label: "CPU"}}, Value: value, Time: clock, Tick: clock},
		{Times: []time.Time{at}, Lines: []ChartLine{{Label: "CPU", Values: []float64{12}}}, Value: value, Time: clock, Tick: clock},
		{
			Times: []time.Time{at, at.Add(time.Minute), at.Add(time.Hour)}, Gap: 3 * time.Minute,
			Lines: []ChartLine{{Label: "In", Values: []float64{1, 2, 3}}, {Label: "Out", Values: []float64{math.NaN(), 1, 2}}},
			Value: value, Time: clock, Tick: clock, Marks: map[int]string{1: "v12 went live"},
		},
	} {
		var html strings.Builder
		if err := Chart(p).Render(context.Background(), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if len(p.Times) == 3 && (!strings.Contains(page, "v12 went live") || !strings.Contains(page, ">3%<") || !strings.Contains(page, "13:00")) {
			t.Errorf("the chart misses its newest value, time or mark:\n%s", page)
		}
		if len(p.Times) == 3 && !strings.Contains(page, "--x:") {
			t.Errorf("a mark lost its place, the CSS variables of its style:\n%s", page)
		}
		if len(p.Times) == 0 && !strings.Contains(page, `data-chart-last="-1"`) {
			t.Errorf("an empty chart has a newest point:\n%s", page)
		}
	}
}

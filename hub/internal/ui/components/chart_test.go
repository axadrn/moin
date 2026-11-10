package components

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChartScale(t *testing.T) {
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

// The chart prints what the element draws: numbers, with no value as null,
// seconds since the first time, the pause that breaks a line, and the words.
func TestChartData(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	clock := func(t time.Time) string { return t.Format("15:04") }
	value := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }
	for _, c := range []struct {
		name  string
		props ChartProps
		want  string
	}{
		{
			name:  "nothing yet",
			props: ChartProps{Lines: []ChartLine{{Label: "CPU"}}, Value: value, Time: clock, Tick: clock},
			want:  `{"lines":[{"label":"CPU","values":[],"text":[]}],"at":[],"top":1,"scale":["1%","0.5%","0%"],"times":[],"ticks":[]}`,
		},
		{
			name: "no value is null and a dash",
			props: ChartProps{
				Times: []time.Time{at, at.Add(time.Minute)}, Lines: []ChartLine{{Label: "CPU", Values: []float64{12, math.NaN()}}},
				Top: 100, Value: value, Time: clock, Tick: clock,
			},
			want: `{"lines":[{"label":"CPU","values":[12,null],"text":["12%","—"]}],"at":[0,60],"top":100,"scale":["100%","50%","0%"],"times":["12:00","12:01"],"ticks":["12:00","12:01"]}`,
		},
		{
			name: "a line shorter than the times, a pause and marks",
			props: ChartProps{
				Times: []time.Time{at, at.Add(time.Minute), at.Add(time.Hour)}, Gap: 3 * time.Minute,
				Lines: []ChartLine{{Label: "In", Values: []float64{1, 2, 3}}, {Label: "Out", Values: []float64{1}}},
				Top:   10, Value: value, Time: clock, Tick: clock, Marks: map[int]string{1: "v12 went live", 7: "outside"},
			},
			want: `{"lines":[{"label":"In","values":[1,2,3],"text":["1%","2%","3%"]},{"label":"Out","values":[1,null,null],"text":["1%","—","—"]}],"at":[0,60,3600],"gap":180,"top":10,"scale":["10%","5%","0%"],"times":["12:00","12:01","13:00"],"ticks":["12:00","12:01","13:00"],"marks":{"1":"v12 went live"}}`,
		},
	} {
		if got := chartJSON(c.props); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// The JSON cannot end the script element it sits in, and the element gets
// it as one script.
func TestChartPrintsItsDataInTheElement(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	clock := func(t time.Time) string { return t.Format("15:04") }
	p := ChartProps{
		Times: []time.Time{at}, Lines: []ChartLine{{Label: "</script><b>", Values: []float64{1}}},
		Value: func(float64) string { return "1" }, Time: clock, Tick: clock,
	}
	var html strings.Builder
	if err := Chart(p).Render(context.Background(), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	body := strings.TrimSuffix(strings.TrimPrefix(page, `<moin-chart><script type="application/json">`), `</script></moin-chart>`)
	if body == page || strings.Contains(body, "</script") {
		t.Fatalf("the data is not one safe script element:\n%s", page)
	}
	var data chartData
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatalf("%v:\n%s", err, body)
	}
	if data.Lines[0].Label != "</script><b>" {
		t.Errorf("the label changed: %q", data.Lines[0].Label)
	}
}

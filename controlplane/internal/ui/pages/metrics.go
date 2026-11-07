package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/kit"
)

// ServerMetricsView is the metrics page of a server.
type ServerMetricsView struct {
	Server  model.Server
	Metrics service.ServerMetrics
}

// AppMetricsView is the metrics tab of an app.
type AppMetricsView struct {
	App     model.App
	Metrics service.AppMetrics
}

// metricsChartProps is one chart of a metrics page: its lines, how their
// values read, the top of its scale (0 for the highest value) and the
// deploys marked on it.
type metricsChartProps struct {
	ID    string
	Lines []kit.ChartLine
	Value func(float64) string
	Top   float64
	Marks map[int]string
}

// metricsTimes are the times of the points.
func metricsTimes(points []service.MetricsPoint) []time.Time {
	times := make([]time.Time, len(points))
	for i, point := range points {
		times[i] = point.At.UTC()
	}
	return times
}

// metricsLine is one metric of the points as a line of a chart.
func metricsLine(label string, points []service.MetricsPoint, of func(service.MetricsPoint) float64) kit.ChartLine {
	values := make([]float64, len(points))
	for i, point := range points {
		values[i] = of(point)
	}
	return kit.ChartLine{Label: label, Values: values}
}

func cpuOf(p service.MetricsPoint) float64    { return math.Round(p.CPU*10) / 10 }
func memoryOf(p service.MetricsPoint) float64 { return float64(p.Memory) }
func diskOf(p service.MetricsPoint) float64   { return float64(p.Disk) }
func rxOf(p service.MetricsPoint) float64     { return math.Round(p.NetRx) }
func txOf(p service.MetricsPoint) float64     { return math.Round(p.NetTx) }

// deployMarks marks the first point at or after each deploy went live.
func deployMarks(points []service.MetricsPoint, deploys []model.MetricsDeploy) map[int]string {
	marks := map[int]string{}
	for _, deploy := range deploys {
		for index, point := range points {
			if !point.At.Before(deploy.FinishedAt.Add(-time.Minute)) || index == len(points)-1 {
				marks[index] = fmt.Sprintf("v%d went live", deploy.Number)
				break
			}
		}
	}
	return marks
}

// metricsChart is a chart of the range: the lines break where the agent
// sent nothing for a while, the axis reads the clock for a day or less
// and the date for longer.
func metricsChart(p metricsChartProps, times []time.Time, r service.MetricsRange) kit.ChartProps {
	tick := func(at time.Time) string { return at.Format("15:04") }
	if r.Name == "7d" || r.Name == "30d" {
		tick = func(at time.Time) string { return at.Format("Jan 2") }
	}
	return kit.ChartProps{
		Times: times,
		Lines: p.Lines,
		Top:   p.Top,
		Gap:   max(3*r.Step, 3*time.Minute),
		Value: p.Value,
		Time:  func(at time.Time) string { return at.Format("Jan 2, 15:04 UTC") },
		Tick:  tick,
		Marks: p.Marks,
	}
}

// percentText is a share in percent, one decimal below 10.
func percentText(value float64) string {
	if value < 10 && value != 0 {
		return fmt.Sprintf("%.1f%%", value)
	}
	return fmt.Sprintf("%.0f%%", value)
}

// bytesText is a size for people: 512 MB, 1.5 GB.
func bytesText(value float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 || value >= 10 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func rateText(value float64) string { return bytesText(value) + "/s" }

func loadText(value float64) string { return fmt.Sprintf("%.2f", value) }

// usedOf is "1.2 GB of 4 GB"; without a total, the used part only.
func usedOf(used, total uint64) string {
	if total == 0 {
		return bytesText(float64(used))
	}
	return bytesText(float64(used)) + " of " + bytesText(float64(total))
}

// shareOf is the share of used in total in percent, "" without a total.
func shareOf(used, total uint64) string {
	if total == 0 {
		return ""
	}
	return percentText(100*float64(used)/float64(total)) + " used"
}

// metricsRefresh is how the panel of the last hour stays live: htmx loads
// it again every 15 seconds. Longer ranges stay as they are.
func metricsRefresh(url, rangeName string) templ.Attributes {
	if rangeName != "1h" {
		return templ.Attributes{}
	}
	return templ.Attributes{"hx-get": url, "hx-trigger": "every 15s", "hx-select": "#metrics", "hx-target": "#metrics", "hx-swap": "outerHTML"}
}

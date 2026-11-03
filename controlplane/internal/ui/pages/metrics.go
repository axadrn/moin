package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/a-h/templ"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/components/chart"
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

// metricsChartProps is one chart of a metrics page: its rows, the series
// it draws and how their values read.
type metricsChartProps struct {
	ID     string
	Config chart.Config
	Rows   []chart.Datum
	Range  string
	Value  func(float64) string
	Domain []any
	// Deploys marks the rows with a deploy on the first series.
	Deploys bool
}

// metricsRows are the points as chart rows: t is the time, the series
// keys the values. A deploy marks the first point at or after it went live.
func metricsRows(points []service.MetricsPoint, deploys []model.MetricsDeploy, app bool) []chart.Datum {
	rows := make([]chart.Datum, 0, len(points))
	for _, point := range points {
		row := chart.Datum{
			"t":      point.At.UTC().Format(time.RFC3339),
			"cpu":    math.Round(point.CPU*10) / 10,
			"memory": float64(point.Memory),
			"rx":     math.Round(point.NetRx),
			"tx":     math.Round(point.NetTx),
		}
		if !app {
			row["disk"] = float64(point.Disk)
			row["load"] = math.Round(point.Load*100) / 100
		}
		rows = append(rows, row)
	}
	for _, deploy := range deploys {
		for index, point := range points {
			if !point.At.Before(deploy.FinishedAt.Add(-time.Minute)) || index == len(points)-1 {
				rows[index]["deploy"] = fmt.Sprintf("v%d", deploy.Number)
				break
			}
		}
	}
	return rows
}

// metricsTick is a time on the x axis: the clock for a day or less, the
// date for longer.
func metricsTick(rangeName string) func(any) string {
	return func(value any) string {
		at, err := time.Parse(time.RFC3339, fmt.Sprint(value))
		if err != nil {
			return fmt.Sprint(value)
		}
		if rangeName == "7d" || rangeName == "30d" {
			return at.Format("Jan 2")
		}
		return at.Format("15:04")
	}
}

// metricsLabel is the time a tooltip is about.
func metricsLabel(value any) string {
	at, err := time.Parse(time.RFC3339, fmt.Sprint(value))
	if err != nil {
		return fmt.Sprint(value)
	}
	return at.Format("Jan 2, 15:04 UTC")
}

func numberOf(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	}
	return 0
}

// axisValue formats the ticks of the y axis.
func axisValue(format func(float64) string) func(any) string {
	return func(value any) string { return format(numberOf(value)) }
}

// metricsTooltip is a tooltip row: the series, its value and, on the first
// series, the deploy that went live there.
func metricsTooltip(config chart.Config, format func(float64) string) func(any, string, chart.Datum, int) templ.Component {
	return func(value any, name string, row chart.Datum, index int) templ.Component {
		deploy := ""
		if index == 0 {
			deploy, _ = row["deploy"].(string)
		}
		return metricsTooltipRow(config.Label(name), format(numberOf(value)), deploy)
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

var (
	cpuConfig     = chart.Config{{Key: "cpu", Label: "CPU", Color: "var(--chart-2)"}}
	memoryConfig  = chart.Config{{Key: "memory", Label: "Memory", Color: "var(--chart-3)"}}
	diskConfig    = chart.Config{{Key: "disk", Label: "Disk", Color: "var(--chart-5)"}}
	networkConfig = chart.Config{{Key: "rx", Label: "In", Color: "var(--chart-2)"}, {Key: "tx", Label: "Out", Color: "var(--chart-4)"}}
)

// metricsRefresh is how the panel of the last hour stays live: htmx loads
// it again every 15 seconds. Longer ranges stay as they are.
func metricsRefresh(url, rangeName string) templ.Attributes {
	if rangeName != "1h" {
		return templ.Attributes{}
	}
	return templ.Attributes{"hx-get": url, "hx-trigger": "every 15s", "hx-select": "#metrics", "hx-target": "#metrics", "hx-swap": "outerHTML"}
}

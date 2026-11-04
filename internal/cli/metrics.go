package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// sparkWidth is how many characters a sparkline takes.
const sparkWidth = 40

// metrics shows the metrics of an app or a server now, with a sparkline of
// the range: metrics [app|server], metrics --server <server>.
func metrics(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("metrics", "metrics [app|server] [--server <server>] [--range 1h|24h|7d|30d]").withApp()
	server := flags.String("server", "", "show a server instead of an app")
	rangeName := flags.String("range", "1h", "how far back: 1h, 24h, 7d or 30d")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 || (*server != "" && flags.NArg() > 0) {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if *server != "" {
		return showServerMetrics(ctx, api, *server, *rangeName, stdout)
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	answer, err := api.AppMetrics(ctx, name, *rangeName)
	// A name that is no app may be a server.
	if client.IsNotFound(err) && flags.Arg(0) != "" {
		if serverErr := showServerMetrics(ctx, api, flags.Arg(0), *rangeName, stdout); !client.IsNotFound(serverErr) {
			return serverErr
		}
	}
	if err != nil {
		return err
	}
	printAppMetrics(stdout, answer)
	return nil
}

func showServerMetrics(ctx context.Context, api *client.Client, server, rangeName string, stdout io.Writer) error {
	answer, err := api.ServerMetrics(ctx, server, rangeName)
	if err != nil {
		return err
	}
	printServerMetrics(stdout, answer)
	return nil
}

// series picks one value of every point.
func series(points []client.MetricsPoint, value func(client.MetricsPoint) float64) []float64 {
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, value(point))
	}
	return values
}

func spark(points []client.MetricsPoint, value func(client.MetricsPoint) float64) string {
	return style.Accent.Render(client.Sparkline(series(points, value), sparkWidth))
}

func percentNow(value float64) string { return strconv.FormatFloat(value, 'f', 1, 64) + "%" }

func rateNow(value float64) string { return client.Bytes(value) + "/s" }

func rangeText(name string) string {
	switch name {
	case "24h":
		return "the last 24 hours"
	case "7d":
		return "the last 7 days"
	case "30d":
		return "the last 30 days"
	}
	return "the last hour"
}

func printAppMetrics(w io.Writer, answer client.AppMetrics) {
	fmt.Fprintln(w, style.Bold.Render(answer.App)+style.Muted.Render(" on "+answer.Server+", "+rangeText(answer.Range)))
	if !answer.ServerOnline && answer.Server != "" {
		fmt.Fprintln(w, style.Danger.Render(style.SymbolFailed)+" The server "+answer.Server+" is offline: its agent sends no metrics.")
	}
	if answer.Now == nil && len(answer.Points) == 0 {
		fmt.Fprintf(w, "No metrics yet. The agent sends them every 15 seconds while the app runs; an older agent sends none: %s\n", command(w, build.Name+" servers update <server>"))
		return
	}
	now := answer.Now
	value := func(text func(*client.AppMetricsNow) string) string {
		if now == nil {
			return "-"
		}
		return text(now)
	}
	memory := value(func(now *client.AppMetricsNow) string {
		if now.MemoryLimit == 0 {
			return client.Bytes(float64(now.MemoryUsed))
		}
		return client.Bytes(float64(now.MemoryUsed)) + " of " + client.Bytes(float64(now.MemoryLimit))
	})
	fmt.Fprintln(w)
	table(w, []string{"", "NOW", strings.ToUpper(answer.Range)}, [][]string{
		{"CPU", value(func(now *client.AppMetricsNow) string { return percentNow(now.CPU) }), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.CPU })},
		{"Memory", memory, spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Memory) })},
		{"Net in", value(func(now *client.AppMetricsNow) string { return rateNow(now.NetRx) }), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetRx })},
		{"Net out", value(func(now *client.AppMetricsNow) string { return rateNow(now.NetTx) }), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetTx })},
	})
	if len(answer.Deploys) > 0 {
		deploys := []string{}
		for _, deploy := range answer.Deploys {
			deploys = append(deploys, "v"+strconv.Itoa(deploy.Number)+" "+deploy.At.UTC().Format("Jan 2 15:04"))
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Muted.Render("Deploys that went live (UTC): ")+strings.Join(deploys, ", "))
	}
}

func printServerMetrics(w io.Writer, answer client.ServerMetrics) {
	fmt.Fprintln(w, style.Bold.Render(answer.Server)+style.Muted.Render(", "+rangeText(answer.Range)))
	if !answer.Online {
		fmt.Fprintln(w, style.Danger.Render(style.SymbolFailed)+" The server is offline: its agent sends no metrics.")
	}
	if answer.Now == nil && len(answer.Points) == 0 {
		fmt.Fprintf(w, "No metrics yet. The agent sends them every 15 seconds; an older agent sends none: %s\n", command(w, build.Name+" servers update "+answer.Server))
		return
	}
	now := answer.Now
	value := func(text func(*client.ServerMetricsNow) string) string {
		if now == nil {
			return "-"
		}
		return text(now)
	}
	fmt.Fprintln(w)
	table(w, []string{"", "NOW", strings.ToUpper(answer.Range)}, [][]string{
		{"CPU", value(func(now *client.ServerMetricsNow) string {
			return percentNow(now.CPU) + style.Muted.Render(fmt.Sprintf(" %d cores, load %.2f", now.Cores, now.Load))
		}), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.CPU })},
		{"Memory", value(func(now *client.ServerMetricsNow) string {
			return client.Bytes(float64(now.MemoryUsed)) + " of " + client.Bytes(float64(now.MemoryTotal))
		}), spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Memory) })},
		{"Disk", value(func(now *client.ServerMetricsNow) string {
			return client.Bytes(float64(now.DiskUsed)) + " of " + client.Bytes(float64(now.DiskTotal)) + style.Muted.Render(" "+client.Share(float64(now.DiskUsed), float64(now.DiskTotal)))
		}), spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Disk) })},
		{"Net in", value(func(now *client.ServerMetricsNow) string { return rateNow(now.NetRx) }), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetRx })},
		{"Net out", value(func(now *client.ServerMetricsNow) string { return rateNow(now.NetTx) }), spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetTx })},
	})
	if len(answer.Apps) == 0 {
		return
	}
	rows := [][]string{}
	for _, app := range answer.Apps {
		if app.Now == nil {
			rows = append(rows, []string{app.App, "-", style.Muted.Render("not running"), "-"})
			continue
		}
		rows = append(rows, []string{app.App, percentNow(app.Now.CPU), client.Bytes(float64(app.Now.MemoryUsed)), rateNow(app.Now.NetRx + app.Now.NetTx)})
	}
	fmt.Fprintln(w)
	table(w, []string{"APP", "CPU", "MEMORY", "NETWORK"}, rows)
}

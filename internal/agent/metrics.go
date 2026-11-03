package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/axadrn/ops/protocol"
)

// The agent samples its server and the containers of its apps every
// protocol.MetricsEvery and sends the sample over the session. The server's
// numbers come from /proc, the containers' from the Docker API over its
// socket. Nothing else runs for it, and it sends numbers only.

// sampler keeps the counters of the sample before, since CPU and network
// are rates.
type sampler struct {
	proc    string
	dataDir string
	docker  *http.Client

	at   time.Time
	cpu  cpuTimes
	net  netBytes
	apps map[string]containerCounters
}

// containerCounters are the counters of one container at a sample.
type containerCounters struct {
	cpu, system uint64
	rx, tx      uint64
}

func newSampler(dataDir string) *sampler {
	return &sampler{proc: "/proc", dataDir: dataDir, docker: dockerClient(), apps: map[string]containerCounters{}}
}

// metrics samples every MetricsEvery until ctx ends. The first sample only
// sets the counters, so every sample sent has rates. Without a session
// send drops it: metrics are live, not queued.
func (r *runner) metrics(ctx context.Context) {
	sampler := newSampler(r.apps.dir)
	sampler.sample(ctx, time.Now())
	ticker := time.NewTicker(protocol.MetricsEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			sample := sampler.sample(ctx, now)
			if sample.Server != nil || len(sample.Apps) > 0 {
				r.send(protocol.TypeMetrics, "", sample)
			}
		}
	}
}

// sample reads the numbers now. A part it cannot read stays out.
func (s *sampler) sample(ctx context.Context, now time.Time) protocol.Metrics {
	sample := protocol.Metrics{At: now.UTC()}
	seconds := now.Sub(s.at).Seconds()
	first := s.at.IsZero()
	s.at = now
	server, err := s.server(seconds, first)
	if err == nil {
		sample.Server = &server
	}
	memoryTotal := uint64(0)
	if sample.Server != nil {
		memoryTotal = sample.Server.MemoryTotal
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	apps, err := s.containers(ctx, seconds, first, memoryTotal)
	if err != nil {
		slog.Debug("container metrics", "error", err)
	}
	sample.Apps = apps
	return sample
}

// server reads /proc and the data disk.
func (s *sampler) server(seconds float64, first bool) (protocol.ServerMetrics, error) {
	var server protocol.ServerMetrics
	cpu, err := readCPU(filepath.Join(s.proc, "stat"))
	if err != nil {
		return server, err
	}
	if !first && cpu.total > s.cpu.total {
		server.CPU = clampPercent(100 * float64(cpu.busy-min(s.cpu.busy, cpu.busy)) / float64(cpu.total-s.cpu.total))
	}
	s.cpu = cpu
	server.Cores = cpu.cores
	server.MemoryTotal, server.MemoryUsed, err = readMemory(filepath.Join(s.proc, "meminfo"))
	if err != nil {
		return server, err
	}
	network, err := readNetwork(filepath.Join(s.proc, "net", "dev"))
	if err == nil {
		if !first && seconds > 0 {
			server.NetRx = rate(s.net.rx, network.rx, seconds)
			server.NetTx = rate(s.net.tx, network.tx, seconds)
		}
		s.net = network
	}
	server.Load, _ = readLoad(filepath.Join(s.proc, "loadavg"))
	server.DiskUsed, server.DiskTotal = diskUsage(s.dataDir)
	return server, nil
}

// cpuTimes are the jiffies of all cores from the first line of /proc/stat:
// busy is all but idle and iowait.
type cpuTimes struct {
	busy, total uint64
	cores       int
}

func readCPU(path string) (cpuTimes, error) {
	file, err := os.Open(path)
	if err != nil {
		return cpuTimes{}, err
	}
	defer file.Close()
	return parseCPU(file)
}

func parseCPU(r io.Reader) (cpuTimes, error) {
	var times cpuTimes
	found := false
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			times.cores++
			continue
		}
		found = true
		for index, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuTimes{}, fmt.Errorf("/proc/stat: %w", err)
			}
			// guest and guest_nice are counted in user and nice already.
			if index >= 8 {
				break
			}
			times.total += value
			if index != 3 && index != 4 {
				times.busy += value
			}
		}
	}
	if !found {
		return cpuTimes{}, errors.New("/proc/stat has no cpu line")
	}
	return times, scanner.Err()
}

func readMemory(path string) (total, used uint64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	return parseMemory(file)
}

// parseMemory reads /proc/meminfo: used is what is not available, so the
// page cache the kernel gives back on demand counts as free.
func parseMemory(r io.Reader) (total, used uint64, err error) {
	values := map[string]uint64{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		key, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total, ok := values["MemTotal"]
	if !ok {
		return 0, 0, errors.New("/proc/meminfo has no MemTotal")
	}
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return total, total - min(available, total), scanner.Err()
}

// netBytes are the bytes received and sent over all interfaces counted.
type netBytes struct{ rx, tx uint64 }

func readNetwork(path string) (netBytes, error) {
	file, err := os.Open(path)
	if err != nil {
		return netBytes{}, err
	}
	defer file.Close()
	return parseNetwork(file)
}

// parseNetwork adds up /proc/net/dev over the server's own interfaces.
// Loopback and Docker's bridges and veth pairs only carry what a real
// interface carries too, or nothing that leaves the server.
func parseNetwork(r io.Reader) (netBytes, error) {
	var bytes netBytes
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		name, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		bytes.rx += rx
		bytes.tx += tx
	}
	return bytes, scanner.Err()
}

func readLoad(path string) (float64, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return parseLoad(string(body))
}

// parseLoad reads the one minute load average from /proc/loadavg.
func parseLoad(text string) (float64, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return 0, errors.New("/proc/loadavg is empty")
	}
	return strconv.ParseFloat(fields[0], 64)
}

// diskUsage is how full the file system of dir is, like df: used counts
// the blocks reserved for root.
func diskUsage(dir string) (used, total uint64) {
	var stat syscall.Statfs_t
	for dir != "" {
		if syscall.Statfs(dir, &stat) == nil {
			size := uint64(stat.Bsize)
			total = stat.Blocks * size
			return total - min(stat.Bavail*size, total), total
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return 0, 0
}

// dockerClient talks to the Docker API over its socket: DOCKER_HOST when it
// names a unix socket, else /var/run/docker.sock.
func dockerClient() *http.Client {
	socket := "/var/run/docker.sock"
	if host := os.Getenv("DOCKER_HOST"); strings.HasPrefix(host, "unix://") {
		socket = strings.TrimPrefix(host, "unix://")
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socket)
			},
			MaxIdleConns: 4,
		},
	}
}

// dockerContainer is a running container of an ops app, from
// GET /containers/json.
type dockerContainer struct {
	ID     string            `json:"Id"`
	Labels map[string]string `json:"Labels"`
}

// dockerStats is the part of GET /containers/{id}/stats the agent reads.
type dockerStats struct {
	CPU struct {
		Usage struct {
			Total uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		System uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	Memory struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		Rx uint64 `json:"rx_bytes"`
		Tx uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

// containers adds up the running containers of every app. One stats call
// per container, a few at a time; one-shot answers at once instead of
// waiting a second for a second reading.
func (s *sampler) containers(ctx context.Context, seconds float64, first bool, memoryTotal uint64) ([]protocol.AppMetrics, error) {
	filters := url.QueryEscape(`{"label":["com.docker.compose.project"]}`)
	var running []dockerContainer
	err := s.dockerGet(ctx, "/containers/json?filters="+filters, &running)
	if err != nil {
		return nil, err
	}
	type result struct {
		app   string
		id    string
		stats dockerStats
		err   error
	}
	results := make([]result, len(running))
	limit := make(chan struct{}, 4)
	var wait sync.WaitGroup
	for index, container := range running {
		app, ok := strings.CutPrefix(container.Labels["com.docker.compose.project"], "ops-")
		if !ok || !appIDPattern.MatchString(app) {
			results[index].err = errors.New("not an ops app")
			continue
		}
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			var stats dockerStats
			err := s.dockerGet(ctx, "/containers/"+url.PathEscape(container.ID)+"/stats?stream=false&one-shot=true", &stats)
			results[index] = result{app: app, id: container.ID, stats: stats, err: err}
		})
	}
	wait.Wait()
	byApp := map[string]*protocol.AppMetrics{}
	order := []string{}
	unlimited := map[string]bool{}
	seen := map[string]containerCounters{}
	for _, result := range results {
		if result.err != nil {
			continue
		}
		metrics := byApp[result.app]
		if metrics == nil {
			metrics = &protocol.AppMetrics{App: result.app}
			byApp[result.app] = metrics
			order = append(order, result.app)
		}
		counters := containerCounters{cpu: result.stats.CPU.Usage.Total, system: result.stats.CPU.System}
		for _, network := range result.stats.Networks {
			counters.rx += network.Rx
			counters.tx += network.Tx
		}
		seen[result.id] = counters
		metrics.Containers++
		metrics.MemoryUsed += containerMemory(result.stats)
		limit := result.stats.Memory.Limit
		if limit == 0 || (memoryTotal > 0 && limit >= memoryTotal) {
			unlimited[result.app] = true
		}
		metrics.MemoryLimit += limit
		before, ok := s.apps[result.id]
		if first || !ok {
			continue
		}
		if counters.system > before.system && counters.cpu >= before.cpu {
			metrics.CPU += 100 * float64(counters.cpu-before.cpu) / float64(counters.system-before.system)
		}
		if seconds > 0 {
			metrics.NetRx += rate(before.rx, counters.rx, seconds)
			metrics.NetTx += rate(before.tx, counters.tx, seconds)
		}
	}
	s.apps = seen
	apps := make([]protocol.AppMetrics, 0, len(order))
	for _, app := range order {
		metrics := byApp[app]
		metrics.CPU = clampPercent(metrics.CPU)
		if unlimited[app] {
			metrics.MemoryLimit = 0
		}
		apps = append(apps, *metrics)
	}
	return apps, nil
}

// containerMemory is the memory a container uses without the page cache it
// could give back, like docker stats: inactive_file on cgroup v2,
// total_inactive_file on v1.
func containerMemory(stats dockerStats) uint64 {
	usage := stats.Memory.Usage
	cache, ok := stats.Memory.Stats["inactive_file"]
	if !ok {
		cache = stats.Memory.Stats["total_inactive_file"]
	}
	return usage - min(cache, usage)
}

func (s *sampler) dockerGet(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	response, err := s.docker.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("docker API: %s", response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}

// rate is bytes per second between two counter readings; a counter that
// went back, such as after a reboot or a new container, counts as zero.
func rate(before, now uint64, seconds float64) float64 {
	if now < before || seconds <= 0 {
		return 0
	}
	return float64(now-before) / seconds
}

func clampPercent(value float64) float64 {
	return max(0, min(100, value))
}

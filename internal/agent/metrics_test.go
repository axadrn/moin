package agent

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseProcFixtures(t *testing.T) {
	cpu, err := readCPU("testdata/proc/stat")
	if err != nil || cpu.busy != 1500 || cpu.total != 10000 || cpu.cores != 2 {
		t.Fatalf("cpu = %+v, %v", cpu, err)
	}
	total, used, err := readMemory("testdata/proc/meminfo")
	if err != nil || total != 4000000*1024 || used != 1000000*1024 {
		t.Fatalf("memory = %d %d, %v", total, used, err)
	}
	network, err := readNetwork("testdata/proc/net/dev")
	if err != nil || network.rx != 1000000 || network.tx != 500000 {
		t.Fatalf("network = %+v, %v (lo, docker0 and veth must not count)", network, err)
	}
	load, err := readLoad("testdata/proc/loadavg")
	if err != nil || load != 0.42 {
		t.Fatalf("load = %v, %v", load, err)
	}
	// An older kernel without MemAvailable falls back to free plus cache.
	total, used, err = parseMemory(strings.NewReader("MemTotal: 1000 kB\nMemFree: 200 kB\nBuffers: 100 kB\nCached: 300 kB\n"))
	if err != nil || total != 1000*1024 || used != 400*1024 {
		t.Fatalf("memory without MemAvailable = %d %d, %v", total, used, err)
	}
	if _, err := parseCPU(strings.NewReader("intr 1\n")); err == nil {
		t.Fatal("a stat without a cpu line parsed")
	}
}

// fakeDocker answers the two Docker API calls the sampler makes. Each stats
// call of a container adds to its counters, as time passes.
func fakeDocker(t *testing.T) *http.Client {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/json":
			if !strings.Contains(r.URL.Query().Get("filters"), "com.docker.compose.project") {
				t.Errorf("filters = %q", r.URL.Query().Get("filters"))
			}
			_, _ = w.Write([]byte(`[
				{"Id":"a1","Labels":{"com.docker.compose.project":"moin-shop"}},
				{"Id":"a2","Labels":{"com.docker.compose.project":"moin-shop"}},
				{"Id":"b1","Labels":{"com.docker.compose.project":"someone-else"}}]`))
		case strings.HasPrefix(r.URL.Path, "/containers/a") && strings.HasSuffix(r.URL.Path, "/stats"):
			if r.URL.Query().Get("one-shot") != "true" || r.URL.Query().Get("stream") != "false" {
				t.Errorf("stats query = %q", r.URL.RawQuery)
			}
			round := uint64(calls.Add(1)+1) / 2 // 1, 1, 2, 2: both containers per round
			stats := map[string]any{
				"cpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": round * 1000}, "system_cpu_usage": round * 10000},
				"memory_stats": map[string]any{"usage": 300 << 20, "limit": 512 << 20,
					"stats": map[string]any{"inactive_file": 100 << 20}},
				"networks": map[string]any{"eth0": map[string]any{"rx_bytes": round * 15000, "tx_bytes": round * 1500}},
			}
			_ = json.NewEncoder(w).Encode(stats)
		default:
			t.Errorf("unexpected %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}}
}

func TestSamplerRates(t *testing.T) {
	proc := t.TempDir()
	for _, name := range []string{"stat", "meminfo", "loadavg", "net/dev"} {
		body, err := os.ReadFile(filepath.Join("testdata/proc", name))
		if err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Dir(filepath.Join(proc, name)), 0o755)
		if err := os.WriteFile(filepath.Join(proc, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sampler := &sampler{proc: proc, dataDir: t.TempDir(), docker: fakeDocker(t), apps: map[string]containerCounters{}}
	start := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	first := sampler.sample(context.Background(), start)
	if first.Server == nil || first.Server.CPU != 0 || first.Server.NetRx != 0 {
		t.Fatalf("first sample = %+v: it has no rates yet", first.Server)
	}
	if len(first.Apps) != 1 || first.Apps[0].App != "shop" || first.Apps[0].Containers != 2 || first.Apps[0].CPU != 0 {
		t.Fatalf("first apps = %+v", first.Apps)
	}

	// 15 seconds later: 900 of 1500 jiffies busy, 150 KB in, 30 KB out.
	_ = os.WriteFile(filepath.Join(proc, "stat"), []byte("cpu  1600 0 800 8500 600 0 0 0 0 0\ncpu0 1 0 0 0\ncpu1 1 0 0 0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(proc, "net/dev"), []byte("  eth0: 1150000 0 0 0 0 0 0 0 530000 0 0 0 0 0 0 0\n"), 0o644)
	second := sampler.sample(context.Background(), start.Add(15*time.Second))
	server := second.Server
	if server == nil || math.Abs(server.CPU-60) > 0.001 || server.NetRx != 10000 || server.NetTx != 2000 || server.Load != 0.42 || server.Cores != 2 {
		t.Fatalf("server = %+v", server)
	}
	if server.MemoryTotal != 4000000*1024 || server.MemoryUsed != 1000000*1024 || server.DiskTotal == 0 || server.DiskUsed > server.DiskTotal {
		t.Fatalf("server memory and disk = %+v", server)
	}
	if len(second.Apps) != 1 {
		t.Fatalf("apps = %+v", second.Apps)
	}
	app := second.Apps[0]
	// Each container used 1000 of 10000 ns of the server's CPU time: 10%,
	// 20% for both. Memory leaves out the inactive page cache.
	if math.Abs(app.CPU-20) > 0.001 || app.MemoryUsed != 2*200<<20 || app.MemoryLimit != 2*512<<20 || app.NetRx != 2000 || app.NetTx != 200 {
		t.Fatalf("app = %+v", app)
	}
}

func TestContainerMemoryLimit(t *testing.T) {
	stats := dockerStats{}
	stats.Memory.Usage = 100
	stats.Memory.Stats = map[string]uint64{"total_inactive_file": 30}
	if got := containerMemory(stats); got != 70 {
		t.Fatalf("cgroup v1 memory = %d", got)
	}
	stats.Memory.Stats = map[string]uint64{"inactive_file": 500}
	if got := containerMemory(stats); got != 0 {
		t.Fatalf("memory below zero = %d", got)
	}
	if rate(100, 50, 15) != 0 || rate(0, 150, 15) != 10 {
		t.Fatal("rate")
	}
}

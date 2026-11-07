package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// Every daemon sends a sample of its server and apps every 15 seconds. The
// last hour of samples stays in memory, a ring per server and app. Each
// minute adds up into an average, and once a minute all finished minutes go
// to SQLite in one transaction; once an hour they add up into hours. A
// server with five apps costs six rows a minute, not a row per sample.
const (
	// metricsRing holds an hour of samples at the daemon's pace.
	metricsRing = int(time.Hour / protocol.MetricsEvery)
	// metricsMinGap drops samples that come faster, so a daemon cannot
	// push the hour out of its ring.
	metricsMinGap = 10 * time.Second
	// metricsStale is how old the newest sample may be to count as now.
	metricsStale       = 2 * time.Minute
	metricsFlushEvery  = time.Minute
	metricsMinutesKept = 24 * time.Hour
	metricsHoursKept   = 30 * 24 * time.Hour
	metricsMaxApps     = 200
)

// ErrMetricsRange means a range other than 1h, 24h, 7d or 30d.
var ErrMetricsRange = errors.New("the range must be 1h, 24h, 7d or 30d")

// MetricsRange is a time range a chart shows and the step of its points:
// the samples themselves for the last hour, averages for longer ones.
type MetricsRange struct {
	Name string
	Span time.Duration
	Step time.Duration
}

// MetricsRanges are the ranges in their order.
var MetricsRanges = []MetricsRange{
	{Name: "1h", Span: time.Hour, Step: protocol.MetricsEvery},
	{Name: "24h", Span: 24 * time.Hour, Step: 5 * time.Minute},
	{Name: "7d", Span: 7 * 24 * time.Hour, Step: time.Hour},
	{Name: "30d", Span: 30 * 24 * time.Hour, Step: 4 * time.Hour},
}

// metricsRangeOf is the range of a name; "" is the last hour.
func metricsRangeOf(name string) (MetricsRange, error) {
	if name == "" {
		return MetricsRanges[0], nil
	}
	for _, r := range MetricsRanges {
		if r.Name == name {
			return r, nil
		}
	}
	return MetricsRange{}, ErrMetricsRange
}

// MetricsPoint is the average of a step, or one sample in the last hour.
// Apps have no disk and no load.
type MetricsPoint struct {
	At     time.Time
	CPU    float64
	Memory uint64
	Disk   uint64
	NetRx  float64
	NetTx  float64
	Load   float64
}

// MetricsNow is the newest sample of a server or an app.
type MetricsNow struct {
	At          time.Time
	CPU         float64
	Cores       int
	MemoryUsed  uint64
	MemoryTotal uint64
	DiskUsed    uint64
	DiskTotal   uint64
	NetRx       float64
	NetTx       float64
	Load        float64
	// MemoryLimit and Containers are of an app; a limit of 0 is none.
	MemoryLimit uint64
	Containers  int
}

// ServerMetrics are the metrics of a server: now, over the range, and the
// apps on it now, the one with the most memory first.
type ServerMetrics struct {
	Range  MetricsRange
	Online bool
	Now    *MetricsNow
	Points []MetricsPoint
	Apps   []AppMetricsNow
}

// AppMetricsNow is an app on a server and its newest sample, nil without
// one.
type AppMetricsNow struct {
	Name string
	Now  *MetricsNow
}

// AppMetrics are the metrics of an app: now, over the range, and the
// deploys that went live in it.
type AppMetrics struct {
	Range        MetricsRange
	Server       string
	ServerOnline bool
	Now          *MetricsNow
	Points       []MetricsPoint
	Deploys      []model.MetricsDeploy
}

// metricsKey names a series the way the daemon does: its server, and the
// app's name, "" for the server itself.
type metricsKey struct{ server, app string }

// metricsValues are cpu, memory, disk, net rx, net tx and load.
type metricsValues [6]float32

type metricsSample struct {
	at int64
	v  metricsValues
}

// metricsSeries is the memory of one server or app: the ring of the last
// hour, the newest sample in full, the minute being added up and the
// finished minutes not written yet.
type metricsSeries struct {
	ring    []metricsSample
	head    int
	now     MetricsNow
	minute  int64
	sum     [6]float64
	n       int
	pending []metricsSample
}

type metricsStore struct {
	mu     sync.Mutex
	series map[metricsKey]*metricsSeries
	// rolledUp is the start of the first hour not rolled up yet.
	rolledUp int64
	pruned   time.Time
}

func newMetricsStore() *metricsStore {
	return &metricsStore{series: map[metricsKey]*metricsSeries{}}
}

// receiveMetrics takes a sample of a daemon. The hub's clock
// dates it, not the server's.
func (s *AppService) receiveMetrics(serverID string, sample protocol.Metrics) {
	if sample.Server != nil {
		s.servers.checkDisk(serverID, sample.Server.DiskUsed, sample.Server.DiskTotal)
	}
	now := s.now()
	store := s.metrics
	store.mu.Lock()
	defer store.mu.Unlock()
	if sample.Server != nil {
		server := sample.Server
		store.add(metricsKey{server: serverID}, now, MetricsNow{
			CPU: percent(server.CPU), Cores: server.Cores, MemoryUsed: server.MemoryUsed, MemoryTotal: server.MemoryTotal,
			DiskUsed: server.DiskUsed, DiskTotal: server.DiskTotal, NetRx: nonNegative(server.NetRx), NetTx: nonNegative(server.NetTx),
			Load: nonNegative(server.Load),
		})
	}
	for index, app := range sample.Apps {
		if index >= metricsMaxApps || !protocol.ValidAppName(app.App) {
			continue
		}
		store.add(metricsKey{server: serverID, app: app.App}, now, MetricsNow{
			CPU: percent(app.CPU), MemoryUsed: app.MemoryUsed, MemoryLimit: app.MemoryLimit,
			NetRx: nonNegative(app.NetRx), NetTx: nonNegative(app.NetTx), Containers: app.Containers,
		})
	}
}

func percent(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	return max(0, min(100, value))
}

func nonNegative(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return max(0, value)
}

// add puts a sample into its series. The caller holds the lock.
func (m *metricsStore) add(key metricsKey, at time.Time, now MetricsNow) {
	series := m.series[key]
	if series == nil {
		series = &metricsSeries{}
		m.series[key] = series
	}
	if !series.now.At.IsZero() && at.Sub(series.now.At) < metricsMinGap {
		return
	}
	now.At = at
	series.now = now
	sample := metricsSample{at: at.Unix(), v: metricsValues{
		float32(now.CPU), float32(now.MemoryUsed), float32(now.DiskUsed), float32(now.NetRx), float32(now.NetTx), float32(now.Load),
	}}
	if len(series.ring) < metricsRing {
		series.ring = append(series.ring, sample)
	} else {
		series.ring[series.head] = sample
		series.head = (series.head + 1) % metricsRing
	}
	minute := sample.at - sample.at%60
	if series.n > 0 && minute != series.minute {
		series.finish()
	}
	series.minute = minute
	for index, value := range sample.v {
		series.sum[index] += float64(value)
	}
	series.n++
}

// finish ends the minute being added up.
func (s *metricsSeries) finish() {
	if s.n == 0 {
		return
	}
	average := metricsSample{at: s.minute}
	for index, sum := range s.sum {
		average.v[index] = float32(sum / float64(s.n))
	}
	s.pending = append(s.pending, average)
	s.sum, s.n = [6]float64{}, 0
}

// samples are the samples of the ring since a time, oldest first.
func (s *metricsSeries) samples(since int64) []metricsSample {
	ordered := append(slices.Clone(s.ring[s.head:]), s.ring[:s.head]...)
	from, _ := slices.BinarySearchFunc(ordered, since, func(sample metricsSample, at int64) int {
		return int(sample.at - at)
	})
	return ordered[from:]
}

// minutes are the minutes in memory since a time: those not written yet
// and the one being added up.
func (s *metricsSeries) minutes(since int64) []metricsSample {
	var minutes []metricsSample
	for _, minute := range s.pending {
		if minute.at >= since {
			minutes = append(minutes, minute)
		}
	}
	if s.n > 0 && s.minute >= since {
		current := metricsSample{at: s.minute}
		for index, sum := range s.sum {
			current.v[index] = float32(sum / float64(s.n))
		}
		minutes = append(minutes, current)
	}
	return minutes
}

// RunMetrics writes the finished minutes once a minute until ctx ends, then
// once more. Wiring starts it once, in a goroutine.
func (s *AppService) RunMetrics(ctx context.Context) {
	ticker := time.NewTicker(metricsFlushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.FlushMetrics(true)
			return
		case <-ticker.C:
			s.FlushMetrics(false)
		}
	}
}

// FlushMetrics writes the finished minutes in one transaction, rolls the
// finished hours up and forgets what is too old. final also writes the
// minute being added up, for a shutdown.
func (s *AppService) FlushMetrics(final bool) {
	now := s.now()
	minute := now.Unix() - now.Unix()%60
	store := s.metrics
	type pending struct {
		key     metricsKey
		minutes []metricsSample
	}
	var batch []pending
	store.mu.Lock()
	for key, series := range store.series {
		if series.n > 0 && (final || series.minute < minute) {
			series.finish()
		}
		if len(series.pending) > 0 {
			batch = append(batch, pending{key: key, minutes: series.pending})
			series.pending = nil
		}
		if series.n == 0 && now.Sub(series.now.At) > time.Hour {
			delete(store.series, key)
		}
	}
	store.mu.Unlock()
	if len(batch) > 0 {
		apps, err := s.apps.MetricsApps()
		if err != nil {
			slog.Error("load apps for metrics failed", "error", err)
			return
		}
		ids := make(map[metricsKey]string, len(apps))
		for _, app := range apps {
			ids[metricsKey{server: app.ServerID, app: app.Folder}] = app.ID
		}
		var rows []model.MetricsRow
		for _, item := range batch {
			target := item.key.server
			if item.key.app != "" {
				target = ids[item.key]
				if target == "" {
					continue
				}
			}
			for _, minute := range item.minutes {
				rows = append(rows, metricsRow(target, minute))
			}
		}
		err = s.apps.AddMetricsMinutes(rows)
		if err != nil {
			slog.Error("store metrics failed", "error", err, "rows", len(rows))
		}
	}
	// An hour rolls up two minutes after it ended, so its last minute is
	// written. After a restart the two hours before count again.
	hour := (now.Unix() - 120) - (now.Unix()-120)%3600
	if store.rolledUp == 0 {
		store.rolledUp = hour - 2*3600
	}
	if store.rolledUp < hour {
		err := s.apps.RollUpMetrics(store.rolledUp, hour)
		if err != nil {
			slog.Error("roll up metrics failed", "error", err)
		} else {
			store.rolledUp = hour
		}
	}
	if now.Sub(store.pruned) >= time.Hour {
		err := s.apps.PruneMetrics(now.Add(-metricsMinutesKept).Unix(), now.Add(-metricsHoursKept).Unix())
		if err != nil {
			slog.Error("prune metrics failed", "error", err)
		}
		store.pruned = now
	}
}

func metricsRow(target string, sample metricsSample) model.MetricsRow {
	return model.MetricsRow{
		Target: target, At: sample.at,
		CPU: int64(math.Round(float64(sample.v[0]) * 10)), Memory: int64(sample.v[1]), Disk: int64(sample.v[2]),
		NetRx: int64(math.Round(float64(sample.v[3]))), NetTx: int64(math.Round(float64(sample.v[4]))),
		Load: int64(math.Round(float64(sample.v[5]) * 100)),
	}
}

func rowSample(row model.MetricsRow) metricsSample {
	return metricsSample{at: row.At, v: metricsValues{
		float32(row.CPU) / 10, float32(row.Memory), float32(row.Disk), float32(row.NetRx), float32(row.NetTx), float32(row.Load) / 100,
	}}
}

// ServerMetrics returns the metrics of a server over a range: 1h, 24h, 7d
// or 30d, "" for 1h.
func (s *AppService) ServerMetrics(workspaceID, idOrName, rangeName string) (*model.Server, ServerMetrics, error) {
	r, err := metricsRangeOf(rangeName)
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	server, err := s.servers.Get(workspaceID, idOrName)
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	now := s.now()
	metrics := ServerMetrics{Range: r, Online: server.Online, Apps: []AppMetricsNow{}}
	metrics.Now = s.metricsNow(metricsKey{server: server.ID}, now)
	metrics.Points, err = s.metricsPoints(metricsKey{server: server.ID}, server.ID, r, now)
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	for _, app := range apps {
		if app.ServerID == server.ID {
			metrics.Apps = append(metrics.Apps, AppMetricsNow{Name: app.Name, Now: s.metricsNow(metricsKey{server: server.ID, app: app.Folder}, now)})
		}
	}
	slices.SortStableFunc(metrics.Apps, func(a, b AppMetricsNow) int {
		var used [2]uint64
		for index, app := range []AppMetricsNow{a, b} {
			if app.Now != nil {
				used[index] = app.Now.MemoryUsed
			}
		}
		switch {
		case used[0] > used[1]:
			return -1
		case used[0] < used[1]:
			return 1
		}
		return 0
	})
	return server, metrics, nil
}

// AppMetrics returns the metrics of an app over a range, with the deploys
// that went live in it.
func (s *AppService) AppMetrics(workspaceID, idOrName, rangeName string) (*model.App, AppMetrics, error) {
	r, err := metricsRangeOf(rangeName)
	if err != nil {
		return nil, AppMetrics{}, err
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, AppMetrics{}, err
	}
	now := s.now()
	key := metricsKey{server: app.ServerID, app: app.Folder}
	metrics := AppMetrics{Range: r, Server: app.ServerName, ServerOnline: s.servers.Online(app.ServerID)}
	metrics.Now = s.metricsNow(key, now)
	metrics.Points, err = s.metricsPoints(key, app.ID, r, now)
	if err != nil {
		return nil, AppMetrics{}, err
	}
	metrics.Deploys, err = s.apps.LiveDeploys(app.ID, now.Add(-r.Span))
	if err != nil {
		return nil, AppMetrics{}, err
	}
	return app, metrics, nil
}

// metricsNow is the newest sample of a series, nil when there is none from
// the last two minutes.
func (s *AppService) metricsNow(key metricsKey, now time.Time) *MetricsNow {
	store := s.metrics
	store.mu.Lock()
	defer store.mu.Unlock()
	series := store.series[key]
	if series == nil || now.Sub(series.now.At) > metricsStale {
		return nil
	}
	current := series.now
	return &current
}

// metricsPoints are the points of a range, oldest first. The last hour is
// the samples in memory, with stored minutes before the first one, such as
// after a restart. Longer ranges average stored hours, stored minutes and
// the minutes in memory into steps.
func (s *AppService) metricsPoints(key metricsKey, target string, r MetricsRange, now time.Time) ([]MetricsPoint, error) {
	since := now.Add(-r.Span).Unix()
	end := now.Unix() + 1
	store := s.metrics
	store.mu.Lock()
	var ring, memory []metricsSample
	if series := store.series[key]; series != nil {
		if r.Step == protocol.MetricsEvery {
			ring = series.samples(since)
		}
		memory = series.minutes(since)
	}
	store.mu.Unlock()

	var samples []metricsSample
	if r.Step == protocol.MetricsEvery {
		before := end
		if len(ring) > 0 {
			before = ring[0].at - ring[0].at%60
		}
		minutes, err := s.storedMinutes(target, since, before, memory)
		if err != nil {
			return nil, err
		}
		return metricsPoints(append(minutes, ring...)), nil
	}
	from := since
	if r.Step >= time.Hour {
		hours, err := s.apps.MetricsHours(target, since-since%3600, end)
		if err != nil {
			return nil, err
		}
		for _, row := range hours {
			samples = append(samples, rowSample(row))
		}
		if len(hours) > 0 {
			from = max(from, hours[len(hours)-1].At+3600)
		}
	}
	minutes, err := s.storedMinutes(target, from, end, memory)
	if err != nil {
		return nil, err
	}
	samples = append(samples, minutes...)
	return metricsPoints(averageSteps(samples, int64(r.Step/time.Second))), nil
}

// storedMinutes are the minutes from SQLite and those in memory between two
// times, oldest first; memory wins a minute in both.
func (s *AppService) storedMinutes(target string, from, to int64, memory []metricsSample) ([]metricsSample, error) {
	if from >= to {
		return nil, nil
	}
	rows, err := s.apps.MetricsMinutes(target, from, to)
	if err != nil {
		return nil, err
	}
	inMemory := map[int64]bool{}
	for _, minute := range memory {
		inMemory[minute.at] = true
	}
	var minutes []metricsSample
	for _, row := range rows {
		if !inMemory[row.At] {
			minutes = append(minutes, rowSample(row))
		}
	}
	for _, minute := range memory {
		if minute.at >= from && minute.at < to {
			minutes = append(minutes, minute)
		}
	}
	slices.SortFunc(minutes, func(a, b metricsSample) int { return int(a.at - b.at) })
	return minutes, nil
}

// averageSteps averages sorted samples into steps of a number of seconds,
// each dated at its start.
func averageSteps(samples []metricsSample, step int64) []metricsSample {
	var steps []metricsSample
	var sum [6]float64
	n := 0
	flush := func(at int64) {
		if n == 0 {
			return
		}
		average := metricsSample{at: at}
		for index := range sum {
			average.v[index] = float32(sum[index] / float64(n))
		}
		steps = append(steps, average)
		sum, n = [6]float64{}, 0
	}
	current := int64(-1)
	for _, sample := range samples {
		start := sample.at - sample.at%step
		if start != current {
			flush(current)
			current = start
		}
		for index, value := range sample.v {
			sum[index] += float64(value)
		}
		n++
	}
	flush(current)
	return steps
}

func metricsPoints(samples []metricsSample) []MetricsPoint {
	points := make([]MetricsPoint, 0, len(samples))
	for _, sample := range samples {
		points = append(points, MetricsPoint{
			At: time.Unix(sample.at, 0).UTC(), CPU: float64(sample.v[0]), Memory: uint64(sample.v[1]), Disk: uint64(sample.v[2]),
			NetRx: float64(sample.v[3]), NetTx: float64(sample.v[4]), Load: float64(sample.v[5]),
		})
	}
	return points
}

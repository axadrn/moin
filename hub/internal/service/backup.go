package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

const (
	backupTimeout = 10 * time.Minute
	// restoreTimeout is how long the hub waits for a restore.
	// The daemon runs it to the end in any case.
	restoreTimeout = 3 * time.Hour
)

var (
	ErrBackupEndpoint  = errors.New("the endpoint must be an https:// URL, or empty for AWS S3")
	ErrBackupBucket    = errors.New("bucket, access key and secret key are required")
	ErrBackupAccess    = errors.New("moin could not write to the bucket with these keys")
	ErrBucketPrivate   = errors.New("the endpoint points to a private or local address: moin only reaches buckets on the public internet")
	ErrNoBackups       = errors.New("backups are not set up: add a bucket first")
	ErrRestoreTarget   = errors.New("pick a moment to go back to")
	ErrMoment          = errors.New("there is no such moment: pick one from the list")
	ErrMomentOutside   = errors.New("there is no copy that old: pick a moment from the list")
	ErrRestoreDatabase = errors.New("the app has several databases: name the one")
	ErrRestoreRunning  = errors.New("a restore of this app runs: wait until it is done")
	ErrRestoreNotFound = errors.New("there is no such restore: it is older than a week, or of another app")
	ErrNoCopy          = errors.New("there is no copy yet: the first one is taken within a minute of a deploy with a bucket")
	ErrScheduleInFile  = errors.New("the backup schedule is set in compose.yml: change it there and deploy")
	ErrBackupEvery     = errors.New("how often: every 15 minutes, hourly or daily")
	ErrBackupKeep      = errors.New("keep: 1, 3, 7 or 30 days")
)

// AllowOnlyPublicBuckets makes the bucket check refuse private and local
// addresses. The hosted hub turns it on: otherwise a user could
// make it call into its own network. A self-hosted one may use a bucket
// next to it.
func (s *AppService) AllowOnlyPublicBuckets() { s.publicBucketsOnly = true }

// checkBucket writes and deletes a small object in the workspace's folder,
// which needs the same rights as backups. publicOnly refuses addresses that
// are not on the public internet.
func checkBucket(ctx context.Context, bucket protocol.Bucket, publicOnly bool) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := bucketClient(bucket)
	if publicOnly {
		client = s3.New(client.Options(), func(options *s3.Options) {
			options.HTTPClient = publicHTTPClient()
			// A refused address stays refused: do not try it again.
			options.Retryer = retry.NewStandard(func(retries *retry.StandardOptions) {
				refused := retry.IsErrorRetryableFunc(func(err error) aws.Ternary {
					if errors.Is(err, ErrBucketPrivate) {
						return aws.FalseTernary
					}
					return aws.UnknownTernary
				})
				retries.Retryables = append([]retry.IsErrorRetryable{refused}, retries.Retryables...)
			})
		})
	}
	key := strings.TrimLeft(path.Join(bucket.Path, bucket.Workspace, "."+build.Name+"-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("moin"))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key)})
	return err
}

// publicHTTPClient connects only to public addresses. The check runs when
// the connection opens, after DNS: a name that points inside cannot slip
// past it. It goes without a proxy, so the check sees the bucket itself.
func publicHTTPClient() *awshttp.BuildableClient {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: refusePrivate}
	return awshttp.NewBuildableClient().WithTransportOptions(func(transport *http.Transport) {
		transport.Proxy = nil
		transport.DialContext = dialer.DialContext
	})
}

// sharedNetwork is 100.64.0.0/10: carrier networks and Tailscale.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// refusePrivate stops a connection to loopback, private, link-local,
// shared or unspecified addresses, like 127.0.0.1, 10.0.0.5, 169.254.169.254
// or 0.0.0.0.
func refusePrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || sharedNetwork.Contains(ip) {
		return ErrBucketPrivate
	}
	return nil
}

func bucketClient(bucket protocol.Bucket) *s3.Client {
	endpoint := bucket.Endpoint
	if endpoint == "" {
		region := bucket.Region
		if region == "" || region == "auto" {
			region = "us-east-1"
		}
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return s3.New(s3.Options{
		Region:       bucket.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(bucket.AccessKey, bucket.SecretKey, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: bucket.Endpoint != "",
	})
}

// backupConfig is what the daemon of a server gets: the default bucket in
// plain text, the bucket of each of its apps that uses another one, and
// the schedule of each; nothing without a bucket. An app whose bucket
// cannot be opened is paused, the others go on. An error means none of
// it can be told: half a config would stop or prune backups.
func (s *AppService) backupConfig(workspaceID, serverID string) (protocol.BackupConfig, error) {
	bucket, err := s.apps.DefaultBucket(workspaceID)
	if errors.Is(err, repository.ErrNoBucket) {
		return protocol.BackupConfig{}, nil
	}
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	open, err := s.openBucket(bucket)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	config := protocol.BackupConfig{Bucket: open, Schedules: map[string]protocol.Schedule{}}
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	for _, app := range apps {
		if app.ServerID == serverID {
			config.Schedules[app.Folder] = scheduleOf(&app)
		}
		if app.ServerID != serverID || app.BucketID == nil || *app.BucketID == bucket.ID {
			continue
		}
		own, err := s.openAppBucket(&app)
		if err != nil {
			if config.Paused == nil {
				config.Paused = map[string]string{}
			}
			config.Paused[app.Folder] = "the bucket of " + app.Name + " cannot be opened: " + err.Error()
			continue
		}
		if config.AppBuckets == nil {
			config.AppBuckets = map[string]*protocol.Bucket{}
		}
		config.AppBuckets[app.Folder] = own
	}
	return config, nil
}

// reconfigure sends the backup config to a server, when it is online, on
// its own: an app came or went, so its schedule does too.
func (s *AppService) reconfigure(serverID, workspaceID string) {
	if s.servers.Online(serverID) {
		go s.configureDaemon(serverID, workspaceID)
	}
}

// configureDaemons sends the backup config to every online server of the
// user and waits for them, so the next request finds it in place. One
// that is offline gets it when it connects.
func (s *AppService) configureDaemons(workspaceID string) {
	servers, err := s.servers.servers.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list servers for backups failed", "workspace_id", workspaceID, "error", err)
		return
	}
	var wait sync.WaitGroup
	for _, server := range servers {
		if s.servers.Online(server.ID) {
			wait.Go(func() { s.configureDaemon(server.ID, workspaceID) })
		}
	}
	wait.Wait()
}

// configuring keeps the sends to one server in a row, each with the
// config as it is then: the last one sent is the latest.
var configuring sync.Map

func (s *AppService) configureDaemon(serverID, workspaceID string) {
	lock, _ := configuring.LoadOrStore(serverID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := s.backupConfig(workspaceID, serverID)
	if err != nil {
		slog.Error("the backup config is not sent", "server_id", serverID, "error", err)
		return
	}
	config.Apps = s.appIDs(serverID, workspaceID)
	err = s.servers.Request(ctx, serverID, protocol.TypeBackupConfig, config, nil)
	if err != nil {
		slog.Warn("send backup config failed", "server_id", serverID, "error", err)
	}
}

// appIDs are the IDs of a server's apps by folder, the name the
// daemon knows each by.
func (s *AppService) appIDs(serverID, workspaceID string) map[string]string {
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list apps for backups failed", "workspace_id", workspaceID, "error", err)
		return nil
	}
	ids := map[string]string{}
	for _, app := range apps {
		if app.ServerID == serverID {
			ids[app.Folder] = app.ID
		}
	}
	return ids
}

// DatabaseBackups is the backup of one database as people see it: what
// the daemon reports, Problem as one short sentence when it does not work,
// and the moments a restore can go back to.
type DatabaseBackups struct {
	protocol.DatabaseBackup
	Problem string
	Moments []Moment
}

// Moment is one moment of a database to go back to: a minute a few
// minutes or hours ago (SQLite with Every change), a copy, named "Before
// v12" when moin took it before a change, or the state before a restore,
// which undoes it. ID is what the CLI, the API and MCP take; Copy and
// Undo name what a restore goes back to.
type Moment struct {
	ID    string
	Label string
	At    time.Time
	Kind  string
	Copy  string
	Undo  string
}

// Kinds of a Moment, besides the kinds of a remembered copy
// (model.MomentDeploy, MomentUpdate, MomentEdit).
const (
	MomentMinute = "minute"
	MomentCopy   = "copy"
	MomentUndo   = "undo"
)

// quickMoments are the minutes a few minutes and hours back.
var quickMoments = []struct {
	id, label string
	ago       time.Duration
}{
	{"1m", "1 minute ago", time.Minute},
	{"15m", "15 minutes ago", 15 * time.Minute},
	{"1h", "1 hour ago", time.Hour},
	{"1d", "Yesterday this time", 24 * time.Hour},
}

// keepDays is how long an app keeps its copies.
func keepDays(app *model.App) time.Duration {
	if app.BackupKeep <= 0 {
		return time.Duration(protocol.DefaultSchedule.Keep) * 24 * time.Hour
	}
	return time.Duration(app.BackupKeep) * 24 * time.Hour
}

// everyChangeReach is how far Every change reaches: from its oldest
// moment to now while it works, else to its newest change.
func everyChangeReach(backup protocol.DatabaseBackup, now time.Time) (since, until time.Time, ok bool) {
	if backup.Since == nil {
		return since, until, false
	}
	since, until = *backup.Since, now
	if backup.Error != "" && backup.Until != nil {
		until = *backup.Until
	}
	return since, until, true
}

// Moments are the moments a database can go back to: the quick minutes,
// then the copies and the undos, newest first. A copy moin took before a
// change is named after it.
func Moments(backup protocol.DatabaseBackup, remembered []model.BackupMoment, now time.Time) []Moment {
	moments := []Moment{}
	if since, until, ok := everyChangeReach(backup, now); ok {
		for _, quick := range quickMoments {
			if at := now.Add(-quick.ago); !at.Before(since) && !at.After(until) {
				moments = append(moments, Moment{ID: quick.id, Label: quick.label, At: at, Kind: MomentMinute})
			}
		}
	}
	later := []Moment{}
	for _, copy := range backup.Copies {
		moment := Moment{ID: copy.ID, Label: atWhen(copy.At, now), At: copy.At, Kind: MomentCopy, Copy: copy.ID}
		for _, named := range remembered {
			// The copy before a change by its full ID; old ones by the time.
			if (copy.ID == protocol.BeforeCopyID(named.At) || copy.ID == named.At.UTC().Format(protocol.CopyIDLayout)) && (named.Database == "" || named.Database == backup.Database) {
				moment.ID, moment.Label, moment.Kind = rememberedMoment(named)
			}
		}
		later = append(later, moment)
	}
	for _, undo := range backup.Undos {
		when := atWhen(undo.At, now)
		if after, today := strings.CutPrefix(when, "At "); today {
			when = "at " + after
		} else {
			when = "on " + when
		}
		later = append(later, Moment{ID: "undo-" + undo.ID, Label: "Before the restore " + when, At: undo.At, Kind: MomentUndo, Undo: undo.ID})
	}
	slices.SortStableFunc(later, func(x, y Moment) int { return y.At.Compare(x.At) })
	return append(moments, later...)
}

// rememberedMoment names a copy moin took before a change.
func rememberedMoment(moment model.BackupMoment) (id, label, kind string) {
	switch moment.Kind {
	case model.MomentDeploy:
		return "before-v" + moment.Detail, "Before v" + moment.Detail, moment.Kind
	case model.MomentUpdate:
		number, version, _ := strings.Cut(moment.Detail, " ")
		return "before-v" + number, "Before the update to " + version, moment.Kind
	}
	what, table, _ := strings.Cut(moment.Detail, ":")
	id = "edit-" + strconv.FormatInt(moment.ID, 10)
	switch what {
	case "cell":
		return id, "Before your edit in " + table, moment.Kind
	case "add":
		return id, "Before the row added to " + table, moment.Kind
	case "delete":
		return id, "Before the row deleted from " + table, moment.Kind
	}
	return id, "Before your SQL change", moment.Kind
}

// atWhen says when a copy was, in UTC: "At 15:04" today, else "Nov 6 at
// 15:04".
func atWhen(at, now time.Time) string {
	at, now = at.UTC(), now.UTC()
	if at.YearDay() == now.YearDay() && at.Year() == now.Year() {
		return "At " + at.Format("15:04")
	}
	return at.Format("Jan 2") + " at " + at.Format("15:04")
}

// findMoment is the moment named by its ID, or by its label when no other
// moment has the same one, in any case.
func findMoment(moments []Moment, name string) (Moment, error) {
	name = strings.TrimSpace(name)
	var found []Moment
	for _, moment := range moments {
		if strings.EqualFold(moment.ID, name) {
			return moment, nil
		}
		if strings.EqualFold(moment.Label, name) {
			found = append(found, moment)
		}
	}
	switch len(found) {
	case 0:
		return Moment{}, ErrMoment
	case 1:
		return found[0], nil
	}
	ids := make([]string, 0, len(found))
	for _, moment := range found {
		ids = append(ids, moment.ID)
	}
	return Moment{}, &AmbiguousMomentError{Label: found[0].Label, IDs: ids}
}

// AmbiguousMomentError refuses a label several moments have.
type AmbiguousMomentError struct {
	Label string
	IDs   []string
}

func (e *AmbiguousMomentError) Error() string {
	return "several moments are called " + e.Label + ": name one by its id, " + strings.Join(e.IDs, " or ")
}

// BackupProblem says in one short sentence why a backup does not work,
// from what the daemon and its tools said. "" when it works.
func BackupProblem(raw string) string {
	text := strings.ToLower(raw)
	switch {
	case raw == "":
		return ""
	case strings.Contains(text, "once more to start its backups"):
		return "Deploy the app once more to start its backups."
	case strings.Contains(text, "no space left"):
		return "The server's disk is full."
	case strings.Contains(text, "nosuchbucket") || strings.Contains(text, "bucket does not exist") || strings.Contains(text, "specified bucket does not exist"):
		return "The bucket does not exist."
	case strings.Contains(text, "accessdenied") || strings.Contains(text, "access denied") || strings.Contains(text, "invalidaccesskeyid") || strings.Contains(text, "signaturedoesnotmatch") || strings.Contains(text, "forbidden") || strings.Contains(text, "statuscode: 403"):
		return "The bucket refuses the keys."
	case strings.Contains(text, "no such host") || strings.Contains(text, "connection refused") || strings.Contains(text, "i/o timeout") || strings.Contains(text, "dial tcp") || strings.Contains(text, "deadline exceeded") || strings.Contains(text, "tls:") || strings.Contains(text, "x509") || strings.Contains(text, "connection reset"):
		return "The bucket can't be reached."
	case strings.Contains(text, "is not running"):
		return "The database is not running."
	case strings.Contains(text, "cannot be opened"):
		return "The app's bucket can't be opened: check its keys under Data."
	case strings.Contains(text, "copies are late"):
		return "The copies are late."
	case strings.Contains(text, "daily check failed"):
		return "The daily check could not restore the newest copy."
	case strings.Contains(text, "every change"):
		return "Every change is not written down."
	case strings.Contains(text, "cannot back up"):
		return "moin can't back up this database."
	case strings.Contains(text, "too old for this"):
		return "The daemon on the server is too old: update it."
	}
	return "The backup does not work."
}

// Backups asks the daemon how the databases of an app are backed up and
// adds the moments each can go back to.
func (s *AppService) Backups(ctx context.Context, workspaceID, idOrName string) (*model.App, []DatabaseBackups, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	databases, err := s.backups(ctx, app)
	return app, databases, err
}

func (s *AppService) backups(ctx context.Context, app *model.App) ([]DatabaseBackups, error) {
	if _, err := s.AppBucket(app); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	var backups protocol.Backups
	err := s.servers.Request(ctx, app.ServerID, protocol.TypeBackupList, protocol.BackupRequest{App: app.Folder}, &backups)
	if err != nil {
		return nil, err
	}
	s.storeBackupStatus(app, backups)
	s.applyRestoreStates(app, backups.Restores)
	now := s.now().UTC()
	remembered, err := s.apps.BackupMoments(app.ID, now.Add(-keepDays(app)-24*time.Hour))
	if err != nil {
		return nil, err
	}
	databases := make([]DatabaseBackups, 0, len(backups.Databases))
	for _, backup := range backups.Databases {
		problem := BackupProblem(backup.Error)
		if problem == "" && late(app, backup.CopiedAt, now) {
			problem = "The copies are late."
		}
		databases = append(databases, DatabaseBackups{DatabaseBackup: backup, Problem: problem, Moments: Moments(backup, remembered, now)})
	}
	return databases, nil
}

// late says the newest copy is older than twice how often the app is
// copied, and than one more copy may take (an hour): the copies stopped
// coming.
func late(app *model.App, copiedAt *time.Time, now time.Time) bool {
	every := time.Duration(scheduleOf(app).Every) * time.Minute
	if every <= 0 {
		every = time.Duration(protocol.DefaultSchedule.Every) * time.Minute
	}
	return !app.Stopped && copiedAt != nil && now.Sub(*copiedAt) > max(2*every, every+time.Hour)
}

// SetBackupSchedule sets how often the daemon copies an app's databases,
// how long it keeps each copy and whether it records every change of a
// SQLite file, and tells the daemon.
func (s *AppService) SetBackupSchedule(actor Actor, idOrName string, schedule protocol.Schedule) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(protocol.Everies, schedule.Every) {
		return nil, ErrBackupEvery
	}
	if !slices.Contains(protocol.Keeps, schedule.Keep) {
		return nil, ErrBackupKeep
	}
	if s.ScheduleInFile(app) {
		return nil, ErrScheduleInFile
	}
	if err := s.saveSchedule(actor, app, schedule); err != nil {
		return nil, err
	}
	return app, nil
}

// ScheduleInFile says the live deploy's compose.yml sets the backup
// schedule. Then only the file changes it.
func (s *AppService) ScheduleInFile(app *model.App) bool {
	detected := s.liveDetected(app)
	return detected != nil && detected.Config != nil && detected.Config.Backups != nil
}

// applyScheduleInFile saves the backup schedule compose.yml asks for.
func (s *AppService) applyScheduleInFile(app *model.App, backups protocol.ConfigBackups) {
	schedule, err := backups.Schedule()
	if err != nil {
		slog.Error("the backup schedule in compose.yml is wrong", "app_id", app.ID, "error", err.Error())
		return
	}
	if err := s.saveSchedule(System(app.WorkspaceID), app, schedule); err != nil {
		slog.Error("save the backup schedule failed", "app_id", app.ID, "error", err.Error())
	}
}

// saveSchedule stores a schedule, writes the change to the activity and
// tells the daemon.
func (s *AppService) saveSchedule(actor Actor, app *model.App, schedule protocol.Schedule) error {
	before := scheduleOf(app)
	if err := s.apps.SetBackupSchedule(app.ID, schedule.Every, schedule.Keep, schedule.EveryChange); err != nil {
		return err
	}
	app.BackupEvery, app.BackupKeep, app.BackupEveryChange = schedule.Every, schedule.Keep, schedule.EveryChange
	if before != schedule {
		s.servers.record(actor, "backup.scheduled", "app", app.Name, map[string]string{
			"every":        EveryText(before.Every) + " → " + EveryText(schedule.Every),
			"keep":         KeepText(before.Keep) + " → " + KeepText(schedule.Keep),
			"every_change": onOff(before.EveryChange) + " → " + onOff(schedule.EveryChange),
		})
	}
	if s.servers.Online(app.ServerID) {
		go s.configureDaemon(app.ServerID, app.WorkspaceID)
	}
	s.notify(app.WorkspaceID)
	return nil
}

// scheduleOf is the backup schedule of an app.
func scheduleOf(app *model.App) protocol.Schedule {
	return protocol.Schedule{Every: app.BackupEvery, Keep: app.BackupKeep, EveryChange: app.BackupEveryChange}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// EveryText says how often, as people read it: "every 15 minutes",
// "hourly", "daily".
func EveryText(minutes int) string {
	switch minutes {
	case 60:
		return "hourly"
	case 24 * 60:
		return "daily"
	}
	return "every " + strconv.Itoa(minutes) + " minutes"
}

// KeepText says how long, as people read it: "1 day", "7 days".
func KeepText(days int) string {
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}

// RestoreTarget is where a restore goes: Moment, the ID or label of one
// of the moments, or At, a time in the history. Database names the
// database when the app has several.
type RestoreTarget struct {
	Database string
	Moment   string
	At       *time.Time
	// Confirm is the app's name as the user typed it: a restore replaces
	// the live data, so every surface asks, and the service checks.
	Confirm string
}

// Restore is a restore the daemon runs. It runs on its own, as a big
// database takes longer than a request lasts, and across sessions and
// restarts of the hub: RestoreOf says how it stands. It is kept
// in the database a week.
type Restore = model.BackupRestore

// Restore states. Unknown is a restore whose server went offline during
// it: the daemon tells how it ended when it is back, and until then no
// other restore of the app starts.
const (
	RestoreRunning = model.RestoreRunning
	RestoreUnknown = model.RestoreUnknown
	RestoreDone    = model.RestoreDone
	RestoreFailed  = model.RestoreFailed
)

// keepRestores is how long a restore's record stays.
const keepRestores = 7 * 24 * time.Hour

// StartRestore puts a database of an app back to a moment. The daemon
// builds the restored database beside the live one and switches in one
// step; the state before stays as long as the app keeps copies: going
// back to it undoes the restore. It reserves the restore, checks the
// moment, then the daemon runs the restore on its own; RestoreOf follows
// it.
func (s *AppService) StartRestore(ctx context.Context, actor Actor, idOrName string, target RestoreTarget) (*Restore, error) {
	if strings.TrimSpace(target.Moment) == "" && target.At == nil {
		return nil, ErrRestoreTarget
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := confirmed(target.Confirm, app.Name); err != nil {
		return nil, err
	}
	restore, err := s.reserveRestore(app, target.Database)
	if err != nil {
		return nil, err
	}
	request, label, err := s.restoreRequest(ctx, app, target)
	if err != nil {
		if deleteErr := s.apps.DeleteRestore(restore.ID); deleteErr != nil {
			slog.Error("forget a restore that never started", "id", restore.ID, "error", deleteErr)
		}
		return nil, err
	}
	request.ID = restore.ID
	restore.Database, restore.To = request.Database, label
	if err := s.apps.SetRestore(*restore); err != nil {
		return nil, err
	}
	s.notify(app.WorkspaceID)
	s.mu.Lock()
	if s.waiting == nil {
		s.waiting = map[string]bool{}
	}
	s.waiting[restore.ID] = true
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
		defer cancel()
		err := s.servers.Request(ctx, app.ServerID, protocol.TypeBackupRestore, request, nil)
		s.mu.Lock()
		delete(s.waiting, restore.ID)
		s.mu.Unlock()
		switch {
		case errors.Is(err, ErrDaemonGone) || errors.Is(err, context.DeadlineExceeded):
			// The daemon goes on with it: it tells how it ended when it is
			// back (reconcileRestores).
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreUnknown, Error: "the server went offline during the restore; moin asks it how it ended when it is back"})
		case err != nil:
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: errorText(err)})
		default:
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreDone})
		}
	}()
	copied := *restore
	return &copied, nil
}

// reserveRestore stores a new restore of an app unless one may still run:
// two at once would race for the data.
func (s *AppService) reserveRestore(app *model.App, database string) (*Restore, error) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	restores, err := s.apps.Restores(app.ID)
	if err != nil {
		return nil, err
	}
	for _, restore := range restores {
		if restore.Active() {
			return nil, ErrRestoreRunning
		}
	}
	restore := &Restore{ID: randomToken()[:16], WorkspaceID: app.WorkspaceID, AppID: app.ID, App: app.Name, Database: database, State: RestoreRunning, StartedAt: s.now().UTC()}
	if err := s.apps.AddRestore(*restore, s.now().Add(-keepRestores)); err != nil {
		return nil, err
	}
	return restore, nil
}

// endRestore stores how a restore stands after the daemon said so. A
// restore that ended stays ended: a late word changes nothing.
func (s *AppService) endRestore(actor Actor, app *model.App, id string, state protocol.RestoreState) {
	s.restoreMu.Lock()
	restore, err := s.apps.Restore(id)
	if err != nil || !restore.Active() || restore.AppID != app.ID {
		s.restoreMu.Unlock()
		return
	}
	if state.State == RestoreRunning && restore.State == RestoreRunning {
		s.restoreMu.Unlock()
		return
	}
	restore.State, restore.Error = state.State, clipText(state.Error, 2000)
	if state.State == RestoreDone || state.State == RestoreFailed {
		finished := s.now().UTC()
		if state.FinishedAt != nil {
			finished = state.FinishedAt.UTC()
		}
		restore.FinishedAt = &finished
	}
	err = s.apps.SetRestore(*restore)
	s.restoreMu.Unlock()
	if err != nil {
		slog.Error("store how a restore stands failed", "id", id, "error", err)
		return
	}
	if state.State == RestoreDone {
		slog.Info("database restored", "app_id", app.ID, "database", restore.Database, "to", restore.To)
		s.servers.record(actor, "backup.restored", "app", app.Name, map[string]string{"database": restore.Database, "to": restore.To})
	}
	s.notify(app.WorkspaceID)
}

// applyRestoreStates takes what a daemon says of the restores of an app:
// those that run, and how the last one of each database ended.
func (s *AppService) applyRestoreStates(app *model.App, states []protocol.RestoreState) {
	for _, state := range states {
		if state.ID == "" {
			continue
		}
		s.endRestore(System(app.WorkspaceID), app, state.ID, state)
	}
}

// RestoreNoWord begins the error of a restore whose daemon does not report
// restores: nobody can say how it ended.
const RestoreNoWord = "Unknown how it ended"

// reconcileRestores asks the daemon of a server how the restores stand that
// the hub lost track of: those whose server went offline during
// them, and those that ran when the hub restarted. It runs when
// the daemon connects and every minute while one may still run
// (RunRestores). It asks with backup.restores, which needs no bucket and
// lists nothing in it, so an app without a bucket or one that is gone
// still answers. The daemon writes a restore down the moment it arrives,
// so one it knows nothing of never reached it. A daemon that reports no
// restores at all cannot say: the restore ends with that, and the app is
// to check.
func (s *AppService) reconcileRestores(serverID string) {
	restores, err := s.apps.ActiveRestoresOn(serverID)
	if err != nil {
		slog.Error("list the restores to reconcile failed", "server_id", serverID, "error", err)
		return
	}
	for _, restore := range restores {
		s.mu.Lock()
		waited := s.waiting[restore.ID]
		s.mu.Unlock()
		if waited {
			continue
		}
		app, err := s.apps.ByID(restore.AppID)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
		var backups protocol.Backups
		err = s.servers.Request(ctx, serverID, protocol.TypeBackupRestores, protocol.BackupRequest{App: app.Folder}, &backups)
		var daemonErr *DaemonError
		if errors.As(err, &daemonErr) {
			// A daemon before backup.restores.
			backups = protocol.Backups{}
			err = s.servers.Request(ctx, serverID, protocol.TypeBackupList, protocol.BackupRequest{App: app.Folder}, &backups)
		}
		cancel()
		if err != nil && len(backups.Restores) == 0 {
			slog.Warn("ask how a restore stands failed", "id", restore.ID, "error", err)
			continue
		}
		if backups.Restores == nil {
			s.endRestore(System(app.WorkspaceID), app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: RestoreNoWord + ": the daemon on the server does not report restores. Check the app and its data; update the daemon so moin can tell next time."})
			continue
		}
		s.applyRestoreStates(app, backups.Restores)
		known := slices.ContainsFunc(backups.Restores, func(state protocol.RestoreState) bool { return state.ID == restore.ID })
		if !known {
			s.endRestore(System(app.WorkspaceID), app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: "the restore never reached the server; the data is as it was"})
		}
	}
}

// restoreAskEvery is how often RunRestores asks the servers of unknown
// restores how they ended.
const restoreAskEvery = time.Minute

// RunRestores asks, every minute, the daemons of restores that may still
// run how they stand: one whose server went offline during it, once the
// server is back, and one that runs while no request waits for it, as
// after a restart of the hub, whose last word may be lost.
func (s *AppService) RunRestores(ctx context.Context) {
	ticker := time.NewTicker(restoreAskEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.askUnknownRestores()
		}
	}
}

// askUnknownRestores asks each online server with a restore that may
// still run: unknown, or running without a request that waits for it, as
// after a restart of the hub. Once one ends it is not asked
// again.
func (s *AppService) askUnknownRestores() {
	servers, err := s.apps.ActiveRestoreServers()
	if err != nil {
		slog.Error("list the servers of unknown restores failed", "error", err)
		return
	}
	for _, serverID := range servers {
		if s.servers.Online(serverID) {
			s.reconcileRestores(serverID)
		}
	}
}

// RestoreOf is a restore of an app by its ID.
func (s *AppService) RestoreOf(workspaceID, idOrName, id string) (*Restore, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	restore, err := s.apps.Restore(id)
	if errors.Is(err, repository.ErrRestoreNotFound) || err == nil && (restore.WorkspaceID != workspaceID || restore.AppID != app.ID) {
		return nil, ErrRestoreNotFound
	}
	return restore, err
}

// LatestRestore is the newest restore of a database of an app from the
// last day, for the page to show while it runs and how it ended; nil
// without one.
func (s *AppService) LatestRestore(workspaceID, idOrName, database string) (*Restore, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	restores, err := s.apps.Restores(app.ID)
	if err != nil {
		return nil, err
	}
	for _, restore := range restores {
		if (database == "" || restore.Database == database || strings.HasPrefix(restore.Database, database+"/")) && (restore.Active() || s.now().Sub(restore.StartedAt) < 24*time.Hour) {
			return &restore, nil
		}
	}
	return nil, nil
}

// pickDatabase is the database a request names, or the app's only one.
func pickDatabase(databases []DatabaseBackups, name string) (*DatabaseBackups, error) {
	for index := range databases {
		if databases[index].Database == name || name == "" && len(databases) == 1 {
			return &databases[index], nil
		}
	}
	if name == "" {
		return nil, ErrRestoreDatabase
	}
	return nil, ErrNoDatabase
}

// restoreRequest checks where a restore goes and says it as people read
// it. A time goes to that minute when Every change reaches it, else to
// the newest copy at or before it.
func (s *AppService) restoreRequest(ctx context.Context, app *model.App, target RestoreTarget) (protocol.BackupRestore, string, error) {
	databases, err := s.backups(ctx, app)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	backup, err := pickDatabase(databases, target.Database)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	request := protocol.BackupRestore{App: app.Folder, Database: backup.Database}
	now := s.now().UTC()
	if target.At != nil {
		at := target.At.UTC()
		if since, until, ok := everyChangeReach(backup.DatabaseBackup, now.Add(time.Minute)); ok && !at.Before(since) && !at.After(until) {
			request.At = &at
			return request, at.Format("Jan 2 15:04 UTC"), nil
		}
		for _, moment := range backup.Moments {
			if moment.Copy != "" && !moment.At.After(at) {
				request.Copy = moment.Copy
				return request, moment.Label, nil
			}
		}
		return protocol.BackupRestore{}, "", ErrMomentOutside
	}
	moment, err := findMoment(backup.Moments, target.Moment)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	request.Copy, request.Undo = moment.Copy, moment.Undo
	if moment.Kind == MomentMinute {
		// "1 hour ago" is an hour before the restore: the list was made a
		// moment earlier.
		for _, quick := range quickMoments {
			if quick.id == moment.ID {
				at := now.Add(-quick.ago)
				request.At = &at
			}
		}
	}
	return request, moment.Label, nil
}

// remember names the copy the daemon took before a change, by its ID, and
// forgets the names of copies older than the app keeps.
func (s *AppService) remember(app *model.App, database, kind, detail string, at time.Time) {
	err := s.apps.AddBackupMoment(model.BackupMoment{AppID: app.ID, Database: database, Kind: kind, Detail: detail, At: at.UTC()}, s.now().Add(-keepDays(app)-24*time.Hour))
	if err != nil {
		slog.Error("remember a moment failed", "app_id", app.ID, "kind", kind, "error", err)
	}
}

// markUpdate says a deploy updates a catalog app to version, so the copy
// before it reads "Before the update to 3.4.1".
func (s *AppService) markUpdate(deployID, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updates == nil {
		s.updates = map[string]string{}
	}
	s.updates[deployID] = version
}

// rememberDeploy names the copies the daemon took before a deploy.
func (s *AppService) rememberDeploy(deployID string, done protocol.DeployDone) {
	s.mu.Lock()
	version, update := s.updates[deployID]
	delete(s.updates, deployID)
	s.mu.Unlock()
	if done.Before == nil {
		return
	}
	deploy, err := s.apps.DeployByID(deployID)
	if err != nil {
		return
	}
	app, err := s.apps.ByID(deploy.AppID)
	if err != nil {
		return
	}
	number := strconv.Itoa(deploy.Number)
	if update {
		s.remember(app, "", model.MomentUpdate, number+" "+version, *done.Before)
		return
	}
	s.remember(app, "", model.MomentDeploy, number, *done.Before)
}

// Download is a copy of a database to download, straight from the app's
// own bucket, never through the hub: Link is a signed link to
// it there, valid 15 minutes.
type Download struct {
	App      string
	Database string
	Copy     string
	Label    string
	At       time.Time
	Size     int64
	Link     string
}

const downloadLinkLife = 15 * time.Minute

// Download signs a link to a copy of a database: the newest, or the one
// moment names.
func (s *AppService) Download(ctx context.Context, actor Actor, idOrName, database, moment string) (*Download, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	databases, err := s.backups(ctx, app)
	if err != nil {
		return nil, err
	}
	backup, err := pickDatabase(databases, database)
	if err != nil {
		return nil, err
	}
	var copy *Moment
	for index, found := range backup.Moments {
		if found.Copy != "" && (moment == "" || strings.EqualFold(found.ID, moment) || strings.EqualFold(found.Label, moment)) {
			copy = &backup.Moments[index]
			break
		}
	}
	if copy == nil && moment != "" {
		return nil, ErrMoment
	}
	if copy == nil {
		return nil, ErrNoCopy
	}
	bucket, err := s.openAppBucket(app)
	if err != nil {
		return nil, err
	}
	key := protocol.DatabaseFolder(*bucket, app.Folder, app.ID, backup.Database) + "/" + protocol.CopyFile(copy.Copy, backup.Kind == protocol.DatabasePostgres)
	name := app.Name + "-" + path.Base(protocol.DatabaseFolder(*bucket, app.Folder, app.ID, backup.Database)) + "-" + path.Base(key)
	link, err := s3.NewPresignClient(bucketClient(*bucket)).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(bucket.Bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(`attachment; filename="` + name + `"`),
	}, s3.WithPresignExpires(downloadLinkLife))
	if err != nil {
		return nil, err
	}
	var size int64
	for _, found := range backup.Copies {
		if found.ID == copy.Copy {
			size = found.Size
		}
	}
	s.servers.record(actor, "backup.downloaded", "app", app.Name, map[string]string{"database": backup.Database, "copy": copy.Label})
	return &Download{App: app.Name, Database: backup.Database, Copy: copy.Copy, Label: copy.Label, At: copy.At, Size: size, Link: link.URL}, nil
}

// errorText is an error as people read it.
func errorText(err error) string {
	var daemonErr *DaemonError
	if errors.As(err, &daemonErr) {
		return daemonErr.Message
	}
	return err.Error()
}

// BackupStatus is what the daemon last reported about the app's databases.
func (s *AppService) BackupStatus(workspaceID, idOrName string) ([]model.BackupStatus, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.apps.BackupStatus(app.ID)
}

// receiveBackupStatus stores a status the daemon sent on its own.
func (s *AppService) receiveBackupStatus(serverID string, backups protocol.Backups) {
	workspaceID, ok := s.servers.UserOf(serverID)
	if !ok {
		return
	}
	app, err := s.apps.ByFolderForWorkspace(backups.App, workspaceID)
	if err != nil || app.ServerID != serverID {
		return
	}
	if backups.Detected != nil {
		s.storeDetected(app, *backups.Detected)
	}
	s.storeBackupStatus(app, backups)
	s.applyRestoreStates(app, backups.Restores)
	s.notify(workspaceID)
}

// storeDetected replaces what the live deploy of an app has in it.
func (s *AppService) storeDetected(app *model.App, detected protocol.Detected) {
	deploy, err := s.apps.Deploy(app.ID, app.LiveNumber)
	if err != nil {
		return
	}
	body, err := json.Marshal(detected)
	if err == nil {
		err = s.apps.SetDeployDetected(deploy.ID, string(body))
	}
	if err != nil {
		slog.Error("store what the daemon found failed", "app_id", app.ID, "error", fmt.Sprint(err))
	}
}

// storeBackupStatus stores the backup status of each database. Backups
// that start failing or work again, a restore check that fails or passes
// again, and each passed check go into the activity as moin itself; the
// changes alert. The backups' own error and the check's are apart: a
// daemon from before check_error sends the check's failure as Error alone.
func (s *AppService) storeBackupStatus(app *model.App, backups protocol.Backups) {
	appID := app.ID
	before := map[string]model.BackupStatus{}
	if statuses, err := s.apps.BackupStatus(appID); err == nil {
		for _, status := range statuses {
			before[status.Database] = status
		}
	}
	for _, database := range backups.Databases {
		previous, known := before[database.Database]
		status := model.BackupStatus{
			AppID:      appID,
			Database:   database.Database,
			CopiedAt:   database.CopiedAt,
			Since:      database.Since,
			Until:      database.Until,
			CheckedAt:  database.CheckedAt,
			Error:      clipText(database.Error, 2000),
			CheckError: clipText(database.CheckError, 2000),
			UpdatedAt:  s.now().UTC(),
		}
		if app.Stopped && (strings.Contains(status.Error, "is not running") || strings.Contains(status.Error, "copies are late")) {
			// The user stopped the app: its database is meant to rest.
			status.Error = ""
		}
		if database.CheckedAt == nil {
			// No check since the daemon started: the last one still stands.
			status.CheckError = previous.CheckError
		}
		for _, change := range backupChanges(previous, known, status) {
			s.servers.record(System(app.WorkspaceID), change.action, "app", app.Name, change.details)
		}
		err := s.apps.SetBackupStatus(status)
		if err != nil {
			slog.Error("store backup status failed", "app_id", appID, "error", fmt.Sprint(err))
		}
	}
}

// backupChange is one action of the activity a backup status brings.
type backupChange struct {
	action  string
	details map[string]string
}

// backupChanges compares a database's backup status with the one before
// and says what changed: backups that fail or work again, a restore check
// that fails or passes again, and a passed check. Only a change counts, so
// a daemon that tells the same failure every minute alerts once.
func backupChanges(previous model.BackupStatus, known bool, now model.BackupStatus) []backupChange {
	changes := []backupChange{}
	add := func(action string, details map[string]string) {
		details["database"] = now.Database
		changes = append(changes, backupChange{action: action, details: details})
	}
	backupError, before := now.BackupError(), previous.BackupError()
	switch {
	case backupError != "" && (!known || before == ""):
		// The one short sentence: the raw output stays on the database page.
		add("backup.failed", map[string]string{"error": BackupProblem(backupError)})
	case backupError == "" && known && before != "":
		add("backup.recovered", map[string]string{})
	}
	if now.CheckedAt == nil {
		return changes
	}
	switch {
	case now.CheckError != "" && (!known || previous.CheckError == ""):
		add("check.failed", map[string]string{"error": BackupProblem(now.CheckError)})
	case now.CheckError == "" && known && previous.CheckError != "":
		add("check.recovered", map[string]string{})
	}
	if now.CheckError == "" && (!known || previous.CheckedAt == nil || now.CheckedAt.After(*previous.CheckedAt)) {
		add("backup.checked", map[string]string{"checked_at": now.CheckedAt.UTC().Format(time.RFC3339)})
	}
	return changes
}

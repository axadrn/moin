package service

// Downloading a database.

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// Download is a database to download, straight from the app's own
// bucket, never through the hub: Link is a signed link to it there, valid
// 15 minutes.
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

// Download signs a link to a database: for Postgres the newest copy, or
// the one moment names; for SQLite the daemon makes a download of the
// newest state first.
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
	bucket, err := s.openAppBucket(app)
	if err != nil {
		return nil, err
	}
	folder := protocol.DatabaseFolder(*bucket, app.Folder, app.ID, backup.Database)
	var download *Download
	var key string
	if backup.Kind == protocol.DatabasePostgres {
		download, key, err = newestCopy(backup, folder, moment)
	} else {
		download, key, err = s.newestState(ctx, app, backup, folder, moment)
	}
	if err != nil {
		return nil, err
	}
	name := app.Name + "-" + path.Base(folder) + "-" + path.Base(key)
	link, err := s3.NewPresignClient(bucketClient(*bucket)).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(bucket.Bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(`attachment; filename="` + name + `"`),
	}, s3.WithPresignExpires(downloadLinkLife))
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "backup.downloaded", "app", app.Name, map[string]string{"database": backup.Database, "copy": download.Label})
	download.App, download.Database, download.Link = app.Name, backup.Database, link.URL
	return download, nil
}

// newestCopy is the Postgres copy a download links to and its key.
func newestCopy(backup *DatabaseBackups, folder, moment string) (*Download, string, error) {
	var copy *Moment
	for index, found := range backup.Moments {
		if found.Copy != "" && (moment == "" || strings.EqualFold(found.ID, moment) || strings.EqualFold(found.Label, moment)) {
			copy = &backup.Moments[index]
			break
		}
	}
	if copy == nil && moment != "" {
		return nil, "", ErrMoment
	}
	if copy == nil {
		return nil, "", ErrNoCopy
	}
	var size int64
	for _, found := range backup.Copies {
		if found.ID == copy.Copy {
			size = found.Size
		}
	}
	return &Download{Copy: copy.Copy, Label: copy.Label, At: copy.At, Size: size}, folder + "/" + protocol.CopyFile(copy.Copy), nil
}

// newestState has the daemon put the newest state of a SQLite database in
// the bucket and says which download it is and its key.
func (s *AppService) newestState(ctx context.Context, app *model.App, backup *DatabaseBackups, folder, moment string) (*Download, string, error) {
	if moment != "" {
		return nil, "", ErrDownloadNewest
	}
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	var made protocol.Copy
	err := s.servers.Request(ctx, app.ServerID, protocol.TypeBackupDownload, protocol.BackupRequest{App: app.Folder, Database: backup.Database}, &made)
	if err != nil {
		return nil, "", err
	}
	if !protocol.ValidCopyID(made.ID) {
		return nil, "", errors.New("the daemon named no download")
	}
	return &Download{Copy: made.ID, Label: "The newest state", At: made.At}, protocol.DownloadFile(folder, made.ID), nil
}

// errorText is an error as people read it.
func errorText(err error) string {
	var daemonErr *DaemonError
	if errors.As(err, &daemonErr) {
		return daemonErr.Message
	}
	return err.Error()
}

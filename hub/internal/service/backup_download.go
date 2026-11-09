package service

// Downloading a copy.

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/protocol"
)

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
		return nil, backup.missing(ErrMoment)
	}
	if copy == nil {
		return nil, backup.missing(ErrNoCopy)
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

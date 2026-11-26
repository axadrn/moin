package service

// Downloading a copy.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// Download is a copy of a database to download, straight from the app's
// own bucket, never through the hub: Link is a link to it there, signed by
// the app's server, valid protocol.BackupLinkLife.
type Download struct {
	App      string
	Database string
	Copy     string
	Label    string
	At       time.Time
	Size     int64
	Link     string
}

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
	server, err := s.servers.servers.ByIDOrNameForWorkspace(app.ServerID, app.WorkspaceID)
	if err != nil {
		return nil, err
	}
	// The server signs the link with the bucket's key, which only it keeps.
	ctx, cancel := context.WithTimeout(ctx, secretTimeout)
	defer cancel()
	var link protocol.BackupLinkResult
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeBackupLink, protocol.BackupLink{App: app.Folder, Database: backup.Database, Copy: copy.Copy, Name: app.Name}, &link)
	if errors.Is(err, ErrServerOffline) {
		return nil, &OfflineError{Server: server.Name}
	}
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
	return &Download{App: app.Name, Database: backup.Database, Copy: copy.Copy, Label: copy.Label, At: copy.At, Size: size, Link: link.Link}, nil
}

// errorText is an error as people read it.
func errorText(err error) string {
	var daemonErr *DaemonError
	if errors.As(err, &daemonErr) {
		return daemonErr.Message
	}
	return err.Error()
}

package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/protocol"
)

// A deploy from a laptop goes into the user's own bucket: the CLI uploads
// the source there through a signed link, the agent downloads it from
// there and deletes it. ops signs the links and never sees the code.

var (
	ErrUpload       = errors.New("the upload is unknown or expired: run ops deploy again")
	ErrUploadBucket = errors.New("ops deploy uploads your code to your own bucket: add one first")
	uploadPattern   = regexp.MustCompile(`^up_[a-z0-9]{20,64}$`)
)

const (
	uploadLinkLifetime   = 15 * time.Minute
	downloadLinkLifetime = time.Hour
)

// Upload is where the CLI puts a source.
type Upload struct {
	ID  string
	URL string
}

// Upload signs a link the CLI uploads an app's source to, straight into
// the user's bucket.
func (s *AppService) Upload(ctx context.Context, workspaceID, idOrName string) (Upload, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return Upload{}, err
	}
	storage := s.backupConfig(workspaceID).Storage
	if storage == nil {
		return Upload{}, ErrUploadBucket
	}
	id := "up_" + randomToken()
	request, err := s3.NewPresignClient(bucketClient(*storage)).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(storage.Bucket),
		Key:    aws.String(uploadKey(*storage, app.Name, id)),
	}, s3.WithPresignExpires(uploadLinkLifetime))
	if err != nil {
		return Upload{}, err
	}
	return Upload{ID: id, URL: request.URL}, nil
}

// DeployUpload deploys a source the CLI uploaded. The agent downloads it
// from the user's bucket, checks its SHA-256 and deletes it there.
func (s *AppService) DeployUpload(ctx context.Context, actor Actor, idOrName, uploadID, sum string) (*model.Deploy, error) {
	if !uploadPattern.MatchString(uploadID) || len(sum) != 64 {
		return nil, ErrUpload
	}
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	storage := s.backupConfig(actor.WorkspaceID).Storage
	if storage == nil {
		return nil, ErrUploadBucket
	}
	presign := s3.NewPresignClient(bucketClient(*storage))
	key := aws.String(uploadKey(*storage, app.Name, uploadID))
	get, err := presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(storage.Bucket), Key: key}, s3.WithPresignExpires(downloadLinkLifetime))
	if err != nil {
		return nil, err
	}
	remove, err := presign.PresignDeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(storage.Bucket), Key: key}, s3.WithPresignExpires(downloadLinkLifetime))
	if err != nil {
		return nil, err
	}
	deploy := s.newDeploy(app)
	deploy.SourceKey, deploy.SourceSHA256 = "server", sum
	start := protocol.DeployStart{SourceURL: get.URL, SHA256: sum, SourceDeleteURL: remove.URL}
	return s.start(actor, "app.deployed", app, deploy, start)
}

func uploadKey(storage protocol.BackupStorage, app, id string) string {
	return strings.Trim(storage.Path+"/"+app+"/uploads/"+id+".tar.gz", "/")
}

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// The user's bucket holds the backups (see backup.go), each app in
// <path>/<workspace>/<app>/<app ID>/.

func s3Client(bucket *protocol.Bucket) *s3.Client {
	region, endpoint := bucket.Region, bucket.Endpoint
	if region == "" {
		region = "us-east-1"
	}
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return s3.New(s3.Options{Region: region, Credentials: credentials.NewStaticCredentialsProvider(bucket.AccessKey, bucket.SecretKey, ""), BaseEndpoint: aws.String(endpoint), UsePathStyle: bucket.Endpoint != ""})
}

// backupConfig answers backup.config: every bucket must be one the rules
// allow. Each gets the secret key the server keeps for it.
func (r *runner) backupConfig(message protocol.Message, rules *protocol.Rules) {
	var config protocol.BackupConfig
	if err := message.Decode(&config); err != nil {
		r.fail(message, err)
		return
	}
	for _, bucket := range append([]*protocol.Bucket{config.Bucket}, slices.Collect(maps.Values(config.AppBuckets))...) {
		err := checkBucket(rules, r.rulesFile, bucket)
		if err == nil && bucket != nil && bucket.Workspace == "" {
			err = errors.New("start backups: a bucket has no workspace folder, update the hub")
		}
		if err != nil {
			r.fail(message, err)
			return
		}
		if bucket != nil {
			bucket.SecretKey = r.security.bucketKey(bucket.ID)
		}
	}
	r.apps.keepIDs(config.Apps)
	r.backups.configure(config)
	go r.backups.tick(context.Background())
	r.send(protocol.TypeOK, message.RequestID, nil)
}

// bucketSet answers bucket.set (BucketSet): a bucket the rules allow, and a
// test file that goes in and out with the key, before the server keeps it.
func (r *runner) bucketSet(ctx context.Context, message protocol.Message, rules *protocol.Rules) {
	answer(r, message, protocol.TypeOK, func(request protocol.BucketSet) (any, error) {
		bucket := request.Bucket
		if err := checkBucket(rules, r.rulesFile, &bucket); err != nil {
			return nil, err
		}
		if bucket.ID == "" || bucket.Workspace == "" {
			return nil, errors.New("the bucket has no ID or workspace folder: update the hub")
		}
		bucket.SecretKey = request.SecretKey
		if bucket.SecretKey == "" {
			bucket.SecretKey = r.security.bucketKey(bucket.ID)
		}
		if bucket.SecretKey == "" {
			return nil, errors.New("this server has no secret key for the bucket: enter it")
		}
		if err := testBucket(ctx, bucket); err != nil {
			slog.Info("bucket check failed", "bucket", bucket.Name, "error", err)
			return nil, fmt.Errorf("moin could not write to the bucket with these keys: %s", lastLine(err.Error()))
		}
		slog.Info("bucket key set", "bucket", bucket.Name)
		return nil, r.security.setSecret(protocol.BucketSecret(bucket.ID), bucket.SecretKey)
	})
}

// testBucket writes and deletes a small object in the workspace's folder,
// which needs the same rights as backups.
func testBucket(ctx context.Context, bucket protocol.Bucket) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := s3Client(&bucket)
	key := strings.TrimLeft(path.Join(bucket.Path, bucket.Workspace, "."+build.Name+"-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(build.Name))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key)})
	return err
}

// backupLink answers backup.link (BackupLink) unless the rules say
// data: off: a download reads the app's data through moin, like SQL does.
func (r *runner) backupLink(ctx context.Context, request protocol.BackupLink, rules *protocol.Rules) (protocol.BackupLinkResult, error) {
	if rules != nil && rules.DataOff {
		return protocol.BackupLinkResult{}, refused(r.rulesFile, "data: off")
	}
	return r.backups.link(ctx, request)
}

// link signs a link to download a copy (BackupLink), with the key of the
// app's bucket, so the hub never holds it.
func (b *backups) link(ctx context.Context, request protocol.BackupLink) (protocol.BackupLinkResult, error) {
	database, err := b.one(request.App, request.Database)
	if err != nil {
		return protocol.BackupLinkResult{}, err
	}
	bucket, folder, err := b.where(request.App, database)
	if err != nil {
		return protocol.BackupLinkResult{}, err
	}
	key := copyKey(folder, request.Copy, database)
	name := request.Name + "-" + path.Base(folder) + "-" + path.Base(*key)
	link, err := s3.NewPresignClient(s3Client(bucket)).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(bucket.Bucket),
		Key:                        key,
		ResponseContentDisposition: aws.String(`attachment; filename="` + name + `"`),
	}, s3.WithPresignExpires(protocol.BackupLinkLife))
	if err != nil {
		return protocol.BackupLinkResult{}, err
	}
	slog.Info("backup link", "app", request.App, "database", database.Path, "copy", request.Copy)
	return protocol.BackupLinkResult{Link: link.URL}, nil
}

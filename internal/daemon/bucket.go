package daemon

import (
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/moin/protocol"
)

// The user's bucket holds what moin deploy uploads and the backups (see
// backup.go), each app in <path>/<workspace>/<app>/<app ID>/.

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
// allow.
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
	}
	r.apps.keepIDs(config.Apps)
	r.backups.configure(config)
	go r.backups.tick(context.Background())
	r.send(protocol.TypeOK, message.RequestID, nil)
}

// sweepUploads deletes what moin deploy left in <app>/<app ID>/uploads/
// after a day.
func (b *backups) sweepUploads(ctx context.Context) {
	b.mu.Lock()
	buckets := slices.Collect(maps.Values(b.buckets))
	if b.bucket != nil {
		buckets = append(buckets, b.bucket)
	}
	b.mu.Unlock()
	for _, bucket := range buckets {
		client := s3Client(bucket)
		for _, app := range folders(ctx, client, bucket.Bucket, strings.TrimLeft(path.Join(bucket.Path, bucket.Workspace)+"/", "/")) {
			for _, id := range folders(ctx, client, bucket.Bucket, app) {
				list, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket.Bucket), Prefix: aws.String(id + "uploads/")})
				if err != nil {
					continue
				}
				for _, object := range list.Contents {
					if object.LastModified != nil && time.Since(*object.LastModified) > day {
						_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: object.Key})
					}
				}
			}
		}
	}
}

func folders(ctx context.Context, client *s3.Client, bucket, prefix string) []string {
	list, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix), Delimiter: aws.String("/")})
	if err != nil {
		return nil
	}
	found := []string{}
	for _, folder := range list.CommonPrefixes {
		found = append(found, aws.ToString(folder.Prefix))
	}
	return found
}

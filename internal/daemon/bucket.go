package daemon

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/axadrn/moin/internal/build"
)

// The user's bucket holds the backups (see backup.go), each database in
// <path>/<server>/<app>/<database>/.

func s3Client(bucket *Bucket) *s3.Client {
	region, endpoint := bucket.Region, bucket.Endpoint
	if region == "" {
		region = "us-east-1"
	}
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return s3.New(s3.Options{Region: region, Credentials: credentials.NewStaticCredentialsProvider(bucket.AccessKey, bucket.SecretKey, ""), BaseEndpoint: aws.String(endpoint), UsePathStyle: bucket.Endpoint != ""})
}

// lockCheckText is what the object of the lock check says, to whoever
// finds it in the bucket.
const lockCheckText = build.Name + " writes this file to check whether the bucket is locked. In a locked bucket it stays until the lock ends.\n"

// checkBucket writes a small object into the server's folder, which needs
// the same rights as a backup, and tries to delete it again. locked says
// the bucket refused: a lock keeps the copies, also from a hacked server
// with the same keys. Every check writes the same key, so a locked bucket
// keeps one such object, not one per check. A bucket that refuses to
// overwrite the one from an earlier check is locked as well.
func checkBucket(ctx context.Context, config Config) (locked bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bucket := config.Bucket
	client := s3Client(bucket)
	name := aws.String(bucket.Bucket)
	key := aws.String(strings.TrimLeft(path.Join(bucket.Path, config.Name, build.Name+"-lock-check"), "/"))
	put, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: name, Key: key, Body: strings.NewReader(lockCheckText)})
	if err != nil {
		if _, there := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: name, Key: key}); there == nil {
			return true, nil
		}
		return false, err
	}
	// In a bucket with versions, as under S3 Object Lock, a delete without
	// the version only hides the object. Deleting its version deletes it.
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: name, Key: key, VersionId: put.VersionId})
	var refused smithy.APIError
	return errors.As(err, &refused), nil
}

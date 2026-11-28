package daemon

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

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
// with the same keys. Every check writes the same key, so a bucket that
// deletes keeps no such object. A bucket that refuses to overwrite the one
// from an earlier check gets one more per check, with the time in its name.
func checkBucket(ctx context.Context, config Config) (locked bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bucket := config.Bucket
	client := s3Client(bucket)
	name := aws.String(bucket.Bucket)
	key := aws.String(strings.TrimLeft(path.Join(bucket.Path, config.Name, build.Name+"-lock-check"), "/"))
	put, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: name, Key: key, Body: strings.NewReader(lockCheckText)})
	if refused(err) {
		if _, there := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: name, Key: key}); there != nil {
			return false, err
		}
		// An earlier check left the object, and the bucket keeps it. That
		// is a lock only when these keys can write at all: a read-only key
		// is refused too. So write one under a new name.
		fresh := aws.String(*key + "-" + time.Now().UTC().Format("20060102-150405"))
		if _, again := client.PutObject(ctx, &s3.PutObjectInput{Bucket: name, Key: fresh, Body: strings.NewReader(lockCheckText)}); again != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// In a bucket with versions, as under S3 Object Lock, a delete without
	// the version only hides the object. Deleting its version deletes it.
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: name, Key: key, VersionId: put.VersionId})
	if !refused(err) {
		return false, err
	}
	// A bucket with versions keeps this one, whatever is written over it.
	// The version "null" is no such version: the next write replaces it.
	if version := aws.ToString(put.VersionId); version != "" && version != "null" {
		return true, nil
	}
	// Without versions, keys that can't delete can still wipe a copy by
	// writing over it. Only a bucket that refuses that too is locked.
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: name, Key: key, Body: strings.NewReader(lockCheckText)})
	if !refused(err) {
		return false, err
	}
	return true, nil
}

// refused says the bucket said no: the keys lack the right, or a lock
// keeps the object. A bucket that is busy or broken said no such thing.
func refused(err error) bool {
	var answer smithy.APIError
	if errors.As(err, &answer) && slices.Contains([]string{"AccessDenied", "InvalidRequest"}, answer.ErrorCode()) {
		return true
	}
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && (response.HTTPStatusCode() == http.StatusForbidden || response.HTTPStatusCode() == http.StatusConflict)
}

package daemon

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

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

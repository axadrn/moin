package daemon

import (
	"bytes"
	"context"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

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

// testBucket writes and deletes a small object in the server's folder,
// which needs the same rights as backups.
func testBucket(ctx context.Context, config Config) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bucket := config.Bucket
	client := s3Client(bucket)
	key := strings.TrimLeft(path.Join(bucket.Path, config.Name, "."+build.Name+"-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(build.Name))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key)})
	return err
}

package copies

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Bucket is the bucket of moin cloud that holds every copy: Cloudflare R2,
// or any S3. On R2 a lock keeps each copy 14 days: nobody deletes or
// overwrites one before, not even moin cloud.
type Bucket struct {
	client  *s3.Client
	presign *s3.PresignClient
	name    string
}

// NewBucket is the bucket name at the S3 endpoint, with a key that may
// read, write and delete objects in it.
func NewBucket(endpoint, name, accessKey, secretKey string) *Bucket {
	client := s3.New(s3.Options{
		Region:       "auto",
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		// A checksum would go into the signed address, as the checksum of
		// no body at all, and every upload would fail on it.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
	return &Bucket{client: client, presign: s3.NewPresignClient(client), name: name}
}

// uploadWait is how long an address to upload to works.
const uploadWait = 15 * time.Minute

// downloadWait is how long an address to download from works.
const downloadWait = 5 * time.Minute

// UploadURL is an address that uploads one object of size bytes to key,
// for uploadWait. It is signed with the size and If-None-Match: *, so it
// writes exactly that many bytes, and only where nothing lies yet.
func (b *Bucket) UploadURL(ctx context.Context, key string, size int64) (string, error) {
	request, err := b.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.name),
		Key:           aws.String(key),
		ContentLength: aws.Int64(size),
		IfNoneMatch:   aws.String("*"),
	}, s3.WithPresignExpires(uploadWait))
	if err != nil {
		return "", err
	}
	return request.URL, nil
}

// DownloadURL is an address that downloads the object at key, for
// downloadWait.
func (b *Bucket) DownloadURL(ctx context.Context, key string) (string, error) {
	request, err := b.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)}, s3.WithPresignExpires(downloadWait))
	if err != nil {
		return "", err
	}
	return request.URL, nil
}

// errMissing is an object that is not in the bucket.
var errMissing = errors.New("not in the bucket")

// Size is how many bytes the object at key has, or errMissing.
func (b *Bucket) Size(ctx context.Context, key string) (int64, error) {
	object, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)})
	if code(err) == "NotFound" || code(err) == "NoSuchKey" {
		return 0, errMissing
	}
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(object.ContentLength), nil
}

// errLocked is an object the bucket's lock still keeps.
var errLocked = errors.New("the bucket's lock still keeps it")

// Delete deletes the object at key. One that is not there is gone too.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)})
	if code(err) == "ObjectLockedByBucketPolicy" {
		return errLocked
	}
	return err
}

// code is the error code the bucket answered with, "" for none.
func code(err error) string {
	var said smithy.APIError
	if errors.As(err, &said) {
		return said.ErrorCode()
	}
	return ""
}

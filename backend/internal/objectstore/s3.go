package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var ErrInvalid = errors.New("invalid object storage operation")

// Config describes a private S3-compatible bucket.
type Config struct {
	Bucket         string
	Prefix         string
	MaxObjectBytes int64
	DownloadTTL    time.Duration
}

// Store operates on objects that remain private; callers authorize access
// before uploading, deleting, or requesting a signed download URL.
type Store struct {
	client *s3.Client
	config Config
}

func New(client *s3.Client, config Config) (*Store, error) {
	if client == nil || config.Bucket == "" || config.MaxObjectBytes < 1 || config.DownloadTTL <= 0 || config.DownloadTTL > 15*time.Minute {
		return nil, fmt.Errorf("%w: S3 client, bucket, size limit and download TTL <= 15 minutes required", ErrInvalid)
	}
	prefix, err := cleanPrefix(config.Prefix)
	if err != nil {
		return nil, err
	}
	config.Prefix = prefix
	return &Store{client: client, config: config}, nil
}

func cleanPrefix(prefix string) (string, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return "", nil
	}
	cleaned := path.Clean(prefix)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "\\") {
		return "", fmt.Errorf("%w: invalid object prefix", ErrInvalid)
	}
	return cleaned + "/", nil
}

func (s *Store) key(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("%w: invalid object key", ErrInvalid)
	}
	cleaned := path.Clean(key)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: invalid object key", ErrInvalid)
	}
	return s.config.Prefix + cleaned, nil
}

// Put streams a bounded object to private storage. Declared size must equal the
// actual stream size and both remain within MaxObjectBytes.
func (s *Store) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	if body == nil || size < 0 || size > s.config.MaxObjectBytes || contentType == "" {
		return fmt.Errorf("%w: body, content type, and size within limit required", ErrInvalid)
	}
	objectKey, err := s.key(key)
	if err != nil {
		return err
	}
	limited := io.LimitReader(body, s.config.MaxObjectBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read object body: %w", err)
	}
	if int64(len(data)) != size || int64(len(data)) > s.config.MaxObjectBytes {
		return fmt.Errorf("%w: actual object size does not match declared size or exceeds limit", ErrInvalid)
	}
	input := &s3.PutObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(objectKey), Body: bytes.NewReader(data), ContentLength: aws.Int64(size), ContentType: aws.String(contentType)}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put private object: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	objectKey, err := s.key(key)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(objectKey)}); err != nil {
		return fmt.Errorf("delete private object: %w", err)
	}
	return nil
}

// PresignGet is a low-level operation; the caller must authorize the actor
// against the object's tenant and owner before exposing the returned URL.
func (s *Store) PresignGet(ctx context.Context, key, downloadName string) (string, time.Time, error) {
	objectKey, err := s.key(key)
	if err != nil {
		return "", time.Time{}, err
	}
	if downloadName == "" || strings.ContainsAny(downloadName, "\r\n\"\\") {
		return "", time.Time{}, fmt.Errorf("%w: unsafe download name", ErrInvalid)
	}
	presigner := s3.NewPresignClient(s.client)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.Bucket), Key: aws.String(objectKey),
		ResponseContentDisposition: aws.String("attachment; filename=\"" + downloadName + "\""),
	}, s3.WithPresignExpires(s.config.DownloadTTL))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign private object: %w", err)
	}
	expires := time.Now().Add(s.config.DownloadTTL)
	if parsed, err := url.Parse(result.URL); err == nil {
		if seconds, err := time.ParseDuration(parsed.Query().Get("X-Amz-Expires") + "s"); err == nil {
			expires = time.Now().Add(seconds)
		}
	}
	return result.URL, expires, nil
}

// NotFound reports whether an S3-compatible error is an object-not-found response.
func NotFound(err error) bool {
	var missing *types.NoSuchKey
	return errors.As(err, &missing)
}

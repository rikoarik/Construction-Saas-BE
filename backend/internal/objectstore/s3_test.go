package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fakeClient(t *testing.T, status int) *s3.Client {
	t.Helper()
	return s3.New(s3.Options{
		Region: "us-east-1",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
		}),
		UsePathStyle: true,
		BaseEndpoint: aws.String("http://127.0.0.1:9000"),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/private-bucket/prefix/object" && req.Method != http.MethodDelete {
				t.Errorf("request path = %q", req.URL.Path)
			}
			if req.Header.Get("x-amz-acl") != "" {
				t.Errorf("unexpected public ACL: %q", req.Header.Get("x-amz-acl"))
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		})},
	})
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	client := fakeClient(t, http.StatusOK)
	for _, config := range []Config{
		{Bucket: "", MaxObjectBytes: 10, DownloadTTL: time.Minute},
		{Bucket: "bucket", Prefix: "../../", MaxObjectBytes: 10, DownloadTTL: time.Minute},
		{Bucket: "bucket", MaxObjectBytes: 10, DownloadTTL: 16 * time.Minute},
	} {
		if _, err := New(client, config); !errors.Is(err, ErrInvalid) {
			t.Errorf("New(%+v) error = %v, want ErrInvalid", config, err)
		}
	}
}

func TestPutUsesPrivateScopedObjectAndValidatesSize(t *testing.T) {
	client := fakeClient(t, http.StatusOK)
	store, err := New(client, Config{Bucket: "private-bucket", Prefix: "prefix", MaxObjectBytes: 4, DownloadTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "object", "application/octet-stream", bytes.NewReader([]byte("12345")), 5); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversize Put error = %v, want ErrInvalid", err)
	}
	if err := store.Put(context.Background(), "../escape", "application/octet-stream", strings.NewReader("x"), 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal Put error = %v, want ErrInvalid", err)
	}
	if err := store.Put(context.Background(), "object", "application/octet-stream", strings.NewReader("data"), 4); err != nil {
		t.Fatal(err)
	}
}

func TestPresignGetHasBoundedExpiryAndSafeName(t *testing.T) {
	store, err := New(fakeClient(t, http.StatusOK), Config{Bucket: "private-bucket", Prefix: "prefix", MaxObjectBytes: 4, DownloadTTL: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PresignGet(context.Background(), "object", "bad\nname"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe filename error = %v, want ErrInvalid", err)
	}
	url, expiry, err := store.PresignGet(context.Background(), "object", "safe.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "X-Amz-Signature=") || !strings.Contains(url, "/private-bucket/prefix/object") {
		t.Fatalf("presigned URL missing expected private object signature/path: %q", url)
	}
	remaining := time.Until(expiry)
	if remaining <= 0 || remaining > 2*time.Minute {
		t.Fatalf("expiry remaining = %s, want within configured TTL", remaining)
	}
}

func TestDeleteUsesObjectKey(t *testing.T) {
	store, err := New(fakeClient(t, http.StatusNoContent), Config{Bucket: "private-bucket", Prefix: "prefix", MaxObjectBytes: 4, DownloadTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "object"); err != nil {
		t.Fatal(err)
	}
}

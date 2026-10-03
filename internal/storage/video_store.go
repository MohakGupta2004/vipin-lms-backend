package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	gcs "cloud.google.com/go/storage"
)

const (
	// MaxVideoSize is the largest video accepted, in bytes.
	MaxVideoSize int64 = 5 << 30 // 5 GiB

	videoUploadURLExpiry = 15 * time.Minute
)

// videoContentTypes maps an allowed file extension to the content type the upload must use.
var videoContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
}

// VideoContentType returns the content type for a video file extension such as ".mp4" (case-insensitive).
func VideoContentType(ext string) (string, bool) {
	ct, ok := videoContentTypes[strings.ToLower(ext)]
	return ct, ok
}

// VideoUploadURLExpiry is how long a signed upload URL stays valid.
func VideoUploadURLExpiry() time.Duration { return videoUploadURLExpiry }

// VideoStore hands out signed upload URLs and inspects uploaded video objects.
// The API server never proxies video bytes.
type VideoStore interface {
	// Enabled reports whether videos can be stored. When false every operation returns ErrStorageDisabled.
	Enabled() bool
	// SignedUploadURL returns a V4 signed PUT URL plus the headers the client must send with it.
	// GCS rejects uploads whose Content-Type differs or whose size exceeds maxBytes.
	SignedUploadURL(object, contentType string, maxBytes int64, expires time.Duration) (url string, headers map[string]string, err error)
	// ObjectInfo returns the size and content type of a stored object, or ErrObjectNotFound.
	ObjectInfo(ctx context.Context, object string) (size int64, contentType string, err error)
	// ReadSmallObject returns a stored object's bytes, or ErrObjectNotFound. Objects over maxBytes are an error.
	ReadSmallObject(ctx context.Context, object string, maxBytes int64) ([]byte, error)
	// URI returns the gs://bucket/object address of an object.
	URI(object string) string
	Close() error
}

// GCSVideoStore keeps videos in a Google Cloud Storage bucket.
// Credentials come from Application Default Credentials; signing needs a service account key
// or the iam.serviceAccountTokenCreator role.
type GCSVideoStore struct {
	client *gcs.Client
	bucket string
}

var _ VideoStore = (*GCSVideoStore)(nil)

func NewGCSVideoStore(ctx context.Context, bucket string) (*GCSVideoStore, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("storage: bucket name is required")
	}
	client, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: create gcs client: %w", err)
	}
	return &GCSVideoStore{client: client, bucket: bucket}, nil
}

func (s *GCSVideoStore) Enabled() bool { return true }

func (s *GCSVideoStore) SignedUploadURL(object, contentType string, maxBytes int64, expires time.Duration) (string, map[string]string, error) {
	headers := map[string]string{
		"Content-Type":                contentType,
		"x-goog-content-length-range": "0," + strconv.FormatInt(maxBytes, 10),
	}
	url, err := s.client.Bucket(s.bucket).SignedURL(object, &gcs.SignedURLOptions{
		Scheme:  gcs.SigningSchemeV4,
		Method:  "PUT",
		Expires: time.Now().Add(expires),
		Headers: []string{
			"Content-Type:" + contentType,
			"x-goog-content-length-range:" + headers["x-goog-content-length-range"],
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("storage: sign upload url: %w", err)
	}
	return url, headers, nil
}

func (s *GCSVideoStore) ObjectInfo(ctx context.Context, object string) (int64, string, error) {
	attrs, err := s.client.Bucket(s.bucket).Object(object).Attrs(ctx)
	if errors.Is(err, gcs.ErrObjectNotExist) {
		return 0, "", ErrObjectNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("storage: read object info: %w", err)
	}
	return attrs.Size, attrs.ContentType, nil
}

func (s *GCSVideoStore) ReadSmallObject(ctx context.Context, object string, maxBytes int64) ([]byte, error) {
	rc, err := s.client.Bucket(s.bucket).Object(object).NewReader(ctx)
	if errors.Is(err, gcs.ErrObjectNotExist) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: open object: %w", err)
	}
	defer rc.Close()
	if rc.Attrs.Size > maxBytes {
		return nil, fmt.Errorf("storage: object %s is larger than %d bytes", object, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("storage: read object: %w", err)
	}
	return data, nil
}

func (s *GCSVideoStore) URI(object string) string {
	return fmt.Sprintf("gs://%s/%s", s.bucket, object)
}

func (s *GCSVideoStore) Close() error { return s.client.Close() }

// DisabledVideoStore is used when GCS_ENABLE is not true. It never touches the network.
type DisabledVideoStore struct{}

var _ VideoStore = DisabledVideoStore{}

func (DisabledVideoStore) Enabled() bool { return false }

func (DisabledVideoStore) SignedUploadURL(string, string, int64, time.Duration) (string, map[string]string, error) {
	return "", nil, ErrStorageDisabled
}

func (DisabledVideoStore) ObjectInfo(context.Context, string) (int64, string, error) {
	return 0, "", ErrStorageDisabled
}

func (DisabledVideoStore) ReadSmallObject(context.Context, string, int64) ([]byte, error) {
	return nil, ErrStorageDisabled
}

func (DisabledVideoStore) URI(string) string { return "" }

func (DisabledVideoStore) Close() error { return nil }

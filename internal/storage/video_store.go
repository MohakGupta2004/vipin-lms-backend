package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	gcs "cloud.google.com/go/storage"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
)

const (
	// MaxHLSFileSize is the largest single file (playlist, init segment or media segment) a browser may upload.
	// A 6-second 1080p segment is a few MB; the cap leaves room for very busy footage.
	MaxHLSFileSize int64 = 64 << 20 // 64 MiB

	hlsUploadFormExpiry = time.Hour

	// deleteWorkers is how many objects DeletePrefix deletes at once.
	deleteWorkers = 64
)

// hlsContentTypes maps an HLS file extension to the content type it must be stored with.
var hlsContentTypes = map[string]string{
	".m3u8": "application/vnd.apple.mpegurl",
	".mp4":  "video/mp4",         // init segments
	".m4s":  "video/iso.segment", // CMAF media segments
}

// HLSContentType returns the content type for an HLS file extension such as ".m4s".
func HLSContentType(ext string) (string, bool) {
	ct, ok := hlsContentTypes[ext]
	return ct, ok
}

// HLSExtensions lists the file extensions a browser may upload into a video's HLS folder.
func HLSExtensions() []string { return []string{".m3u8", ".mp4", ".m4s"} }

// HLSUploadFormExpiry is how long a signed upload form stays valid. Long jobs renew it.
func HLSUploadFormExpiry() time.Duration { return hlsUploadFormExpiry }

// UploadForm is a signed POST policy: the browser sends Fields plus "key" (the object name, which must start
// with the folder it was signed for) and "file" (last) as multipart/form-data to URL.
type UploadForm struct {
	URL    string
	Fields map[string]string
}

// ObjectInfo describes one stored object.
type ObjectInfo struct {
	Name        string
	Size        int64
	ContentType string
}

// VideoStore hands out signed upload forms and inspects uploaded video files.
// The API server never proxies video bytes.
type VideoStore interface {
	// Enabled reports whether videos can be stored. When false every operation returns ErrStorageDisabled.
	Enabled() bool
	// SignedUploadForm returns a POST policy that lets the holder upload any number of files whose names start
	// with prefix, each with exactly this content type (and cache control, if set) and at most maxBytes long.
	SignedUploadForm(prefix, contentType, cacheControl string, maxBytes int64, expires time.Duration) (*UploadForm, error)
	// ObjectInfo returns the size and content type of a stored object, or ErrObjectNotFound.
	ObjectInfo(ctx context.Context, object string) (size int64, contentType string, err error)
	// ListObjects returns every object whose name starts with prefix. More than max objects is an error.
	ListObjects(ctx context.Context, prefix string, max int) ([]ObjectInfo, error)
	// ReadSmallObject returns a stored object's bytes, or ErrObjectNotFound. Objects over maxBytes are an error.
	ReadSmallObject(ctx context.Context, object string, maxBytes int64) ([]byte, error)
	// DeleteObject removes one object. A missing object is not an error.
	DeleteObject(ctx context.Context, object string) error
	// DeletePrefix removes every object whose name starts with prefix. Nothing to delete is not an error.
	DeletePrefix(ctx context.Context, prefix string) error
	Close() error
}

// ErrTooManyObjects means a listing found more objects than the caller allows.
var ErrTooManyObjects = errors.New("storage: too many objects")

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

func (s *GCSVideoStore) SignedUploadForm(prefix, contentType, cacheControl string, maxBytes int64, expires time.Duration) (*UploadForm, error) {
	opts, err := uploadFormOptions(prefix, contentType, cacheControl, maxBytes, expires)
	if err != nil {
		return nil, err
	}
	// An empty object name leaves "key" to the browser; the starts-with condition pins it to the folder.
	policy, err := s.client.Bucket(s.bucket).GenerateSignedPostPolicyV4("", opts)
	if err != nil {
		return nil, fmt.Errorf("storage: sign upload form: %w", err)
	}
	return &UploadForm{URL: policy.URL, Fields: policy.Fields}, nil
}

// uploadFormOptions builds the POST policy for SignedUploadForm. Signing credentials are filled in by the caller.
func uploadFormOptions(prefix, contentType, cacheControl string, maxBytes int64, expires time.Duration) (*gcs.PostPolicyV4Options, error) {
	// An empty or unanchored prefix would let the form write anywhere in the bucket.
	if strings.Trim(prefix, "/") == "" || !strings.HasSuffix(prefix, "/") {
		return nil, errors.New("storage: upload form needs a folder prefix ending in /")
	}
	if contentType == "" || maxBytes <= 0 {
		return nil, errors.New("storage: upload form needs a content type and a size limit")
	}
	return &gcs.PostPolicyV4Options{
		Expires: time.Now().Add(expires),
		Fields:  &gcs.PolicyV4Fields{ContentType: contentType, CacheControl: cacheControl},
		Conditions: []gcs.PostPolicyV4Condition{
			gcs.ConditionStartsWith("$key", prefix),
			gcs.ConditionContentLengthRange(1, uint64(maxBytes)),
		},
	}, nil
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

func (s *GCSVideoStore) ListObjects(ctx context.Context, prefix string, max int) ([]ObjectInfo, error) {
	if strings.Trim(prefix, "/") == "" {
		return nil, errors.New("storage: refusing to list an empty prefix")
	}
	query := &gcs.Query{Prefix: prefix}
	// Only these fields are fetched, which keeps each page of the listing small.
	if err := query.SetAttrSelection([]string{"Name", "Size", "ContentType"}); err != nil {
		return nil, fmt.Errorf("storage: list objects: %w", err)
	}
	it := s.client.Bucket(s.bucket).Objects(ctx, query)
	var objects []ObjectInfo
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return objects, nil
		}
		if err != nil {
			return nil, fmt.Errorf("storage: list objects: %w", err)
		}
		if len(objects) == max {
			return nil, ErrTooManyObjects
		}
		objects = append(objects, ObjectInfo{Name: attrs.Name, Size: attrs.Size, ContentType: attrs.ContentType})
	}
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

func (s *GCSVideoStore) DeleteObject(ctx context.Context, object string) error {
	err := s.client.Bucket(s.bucket).Object(object).Delete(ctx)
	if err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
		return fmt.Errorf("storage: delete object %s: %w", object, err)
	}
	return nil
}

func (s *GCSVideoStore) DeletePrefix(ctx context.Context, prefix string) error {
	// An empty prefix would match the whole bucket.
	if strings.Trim(prefix, "/") == "" {
		return errors.New("storage: refusing to delete an empty prefix")
	}
	bucket := s.client.Bucket(s.bucket)
	query := &gcs.Query{Prefix: prefix}
	if err := query.SetAttrSelection([]string{"Name", "Generation"}); err != nil {
		return err
	}
	it := bucket.Objects(ctx, query)
	// Every object is its own delete request, and a long video has thousands of segments: one at a time they
	// would outlast any sensible timeout. One failure doesn't stop the rest; the first error is returned once
	// all are tried.
	var g errgroup.Group
	g.SetLimit(deleteWorkers)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			_ = g.Wait()
			return fmt.Errorf("storage: list objects: %w", err)
		}
		// The generation precondition makes the delete idempotent, so the client retries it on transient errors.
		obj := bucket.Object(attrs.Name).Generation(attrs.Generation)
		g.Go(func() error {
			if err := obj.Delete(ctx); err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
				return fmt.Errorf("storage: delete object %s: %w", obj.ObjectName(), err)
			}
			return nil
		})
	}
	return g.Wait()
}

func (s *GCSVideoStore) Close() error { return s.client.Close() }

// DisabledVideoStore is used when GCS_ENABLE is not true. It never touches the network.
type DisabledVideoStore struct{}

var _ VideoStore = DisabledVideoStore{}

func (DisabledVideoStore) Enabled() bool { return false }

func (DisabledVideoStore) SignedUploadForm(string, string, string, int64, time.Duration) (*UploadForm, error) {
	return nil, ErrStorageDisabled
}

func (DisabledVideoStore) ObjectInfo(context.Context, string) (int64, string, error) {
	return 0, "", ErrStorageDisabled
}

func (DisabledVideoStore) ListObjects(context.Context, string, int) ([]ObjectInfo, error) {
	return nil, ErrStorageDisabled
}

func (DisabledVideoStore) ReadSmallObject(context.Context, string, int64) ([]byte, error) {
	return nil, ErrStorageDisabled
}

func (DisabledVideoStore) DeleteObject(context.Context, string) error { return ErrStorageDisabled }

func (DisabledVideoStore) DeletePrefix(context.Context, string) error { return ErrStorageDisabled }

func (DisabledVideoStore) Close() error { return nil }

// Package storage wraps Google Cloud Storage for file uploads.
package storage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	gcs "cloud.google.com/go/storage"
	"github.com/google/uuid"
)

const (
	// MaxPDFSize is the largest PDF accepted, in bytes.
	MaxPDFSize int64 = 25 << 20 // 25 MiB

	pdfContentType = "application/pdf"
	pdfMagic       = "%PDF-"
)

var (
	// ErrInvalidPDF means the upload is not a PDF. Safe to show the client.
	ErrInvalidPDF = errors.New("file is not a valid PDF")
	// ErrPDFTooLarge means the upload exceeds MaxPDFSize. Safe to show the client.
	ErrPDFTooLarge = fmt.Errorf("PDF must be at most %d MB", MaxPDFSize>>20)
	// ErrInvalidFolder means the target folder is empty or unsafe.
	ErrInvalidFolder = errors.New("invalid storage folder")
	// ErrObjectNotFound means the stored object does not exist.
	ErrObjectNotFound = errors.New("stored object not found")
)

// UploadedPDF describes a stored PDF.
type UploadedPDF struct {
	Bucket string
	Object string // object key inside the bucket, e.g. "courses/<id>/<uuid>.pdf"
	URI    string // gs://bucket/object
	Size   int64
}

// PDFStore stores, reads and deletes PDF files.
type PDFStore interface {
	UploadPDF(ctx context.Context, folder, originalName string, r io.Reader) (*UploadedPDF, error)
	// OpenPDF streams a stored object. Returns ErrObjectNotFound if it is gone. Caller must Close.
	OpenPDF(ctx context.Context, object string) (io.ReadCloser, error)
	// DeletePDF removes an object. Deleting a missing object is not an error.
	DeletePDF(ctx context.Context, object string) error
	Close() error
}

// GCSPDFStore uploads PDFs to a Google Cloud Storage bucket.
// Credentials come from Application Default Credentials (GOOGLE_APPLICATION_CREDENTIALS or workload identity).
type GCSPDFStore struct {
	client *gcs.Client
	bucket string
}

var _ PDFStore = (*GCSPDFStore)(nil)

func NewGCSPDFStore(ctx context.Context, bucket string) (*GCSPDFStore, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("storage: bucket name is required")
	}
	client, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: create gcs client: %w", err)
	}
	return &GCSPDFStore{client: client, bucket: bucket}, nil
}

// UploadPDF streams r into <folder>/<random uuid>.pdf. The client-supplied name is never used as
// the object key; it is kept only as metadata. Content is verified by magic bytes, not by the
// caller's claimed type, and capped at MaxPDFSize. Oversized uploads are aborted without leaving an object.
func (u *GCSPDFStore) UploadPDF(ctx context.Context, folder, originalName string, r io.Reader) (*UploadedPDF, error) {
	folder, err := cleanFolder(folder)
	if err != nil {
		return nil, err
	}

	br := bufio.NewReader(r)
	head, err := br.Peek(len(pdfMagic))
	if err != nil || !bytes.Equal(head, []byte(pdfMagic)) {
		return nil, ErrInvalidPDF
	}

	object := path.Join(folder, uuid.NewString()+".pdf")

	// Cancelling ctx before Close() makes GCS discard the partial upload.
	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := u.client.Bucket(u.bucket).Object(object).NewWriter(uploadCtx)
	w.ContentType = pdfContentType
	w.CacheControl = "private, max-age=0"
	w.Metadata = map[string]string{"original-name": sanitizeName(originalName)}

	// Read one byte past the limit to detect oversize.
	n, err := io.Copy(w, io.LimitReader(br, MaxPDFSize+1))
	if err != nil {
		cancel()
		_ = w.Close()
		return nil, fmt.Errorf("storage: upload pdf: %w", err)
	}
	if n > MaxPDFSize {
		cancel()
		_ = w.Close()
		return nil, ErrPDFTooLarge
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("storage: finalize pdf upload: %w", err)
	}

	return &UploadedPDF{
		Bucket: u.bucket,
		Object: object,
		URI:    fmt.Sprintf("gs://%s/%s", u.bucket, object),
		Size:   n,
	}, nil
}

func (u *GCSPDFStore) OpenPDF(ctx context.Context, object string) (io.ReadCloser, error) {
	rc, err := u.client.Bucket(u.bucket).Object(object).NewReader(ctx)
	if errors.Is(err, gcs.ErrObjectNotExist) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: open pdf: %w", err)
	}
	return rc, nil
}

func (u *GCSPDFStore) DeletePDF(ctx context.Context, object string) error {
	err := u.client.Bucket(u.bucket).Object(object).Delete(ctx)
	if err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
		return fmt.Errorf("storage: delete pdf: %w", err)
	}
	return nil
}

func (u *GCSPDFStore) Close() error {
	return u.client.Close()
}

// cleanFolder rejects traversal and absolute paths; returns a normalized relative folder.
func cleanFolder(folder string) (string, error) {
	folder = strings.Trim(strings.TrimSpace(folder), "/")
	if folder == "" || strings.Contains(folder, "..") || strings.Contains(folder, "\\") {
		return "", ErrInvalidFolder
	}
	return path.Clean(folder), nil
}

func sanitizeName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}

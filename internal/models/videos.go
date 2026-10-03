package models

import (
	"context"
	"database/sql"
	"time"
	"unicode/utf8"
)

const maxVideoErrorChars = 1000

// Video is an uploaded lesson video. Lifecycle: uploading → processing → ready | failed.
type Video struct {
	ID         string    `json:"id"`
	LessonID   string    `json:"lessonId"`
	CourseID   string    `json:"courseId"`
	Title      string    `json:"title"`
	IsFree     bool      `json:"isFree"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	SizeBytes  int64     `json:"sizeBytes,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	UploadedBy string    `json:"-"`

	OriginalKey  string `json:"-"`
	TranscodeJob string `json:"-"`
	HLSPrefix    string `json:"-"`
}

type VideoRepository struct {
	db *sql.DB
}

func NewVideoRepository(db *sql.DB) *VideoRepository {
	return &VideoRepository{db: db}
}

const videoSelect = `SELECT v.id, COALESCE(v.lesson_id::text, ''), COALESCE(v.course_id::text, ''), v.uploaded_by, v.title, v.is_free,
		v.status, COALESCE(v.error, ''), COALESCE(v.size_bytes, 0), v.created_at, v.updated_at,
		v.original_key, COALESCE(v.transcode_job, ''), COALESCE(v.hls_prefix, '')
	FROM videos v`

func scanVideo(row interface{ Scan(...any) error }, v *Video) error {
	return row.Scan(&v.ID, &v.LessonID, &v.CourseID, &v.UploadedBy, &v.Title, &v.IsFree,
		&v.Status, &v.Error, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt,
		&v.OriginalKey, &v.TranscodeJob, &v.HLSPrefix)
}

// Create inserts a video in the 'uploading' state. v.ID must be set by the caller (it is part of the object key).
func (r *VideoRepository) Create(ctx context.Context, v *Video) error {
	query := `INSERT INTO videos (id, uploaded_by, course_id, lesson_id, title, is_free, original_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING status, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, v.ID, v.UploadedBy, v.CourseID, v.LessonID, v.Title, v.IsFree, v.OriginalKey).
		Scan(&v.Status, &v.CreatedAt, &v.UpdatedAt)
}

// Get returns one video, or sql.ErrNoRows.
func (r *VideoRepository) Get(ctx context.Context, id string) (*Video, error) {
	var v Video
	if err := scanVideo(r.db.QueryRowContext(ctx, videoSelect+` WHERE v.id = $1`, id), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// VideoListFilter narrows List. CourseID is required.
type VideoListFilter struct {
	CourseID  string
	LessonID  string // empty = whole course
	OnlyReady bool   // students: ready videos of published lessons
	OnlyFree  bool   // not enrolled: free videos or videos of free lessons
}

// List returns a course's videos in lesson order. Videos of deleted lessons are left out.
func (r *VideoRepository) List(ctx context.Context, f VideoListFilter) ([]Video, error) {
	query := videoSelect + ` JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		WHERE v.course_id = $1
		  AND ($2::uuid IS NULL OR v.lesson_id = $2::uuid)
		  AND (NOT $3 OR (v.status = 'ready' AND l.is_published))
		  AND (NOT $4 OR v.is_free OR l.is_free)
		ORDER BY l.position, v.created_at`
	var lessonID any
	if f.LessonID != "" {
		lessonID = f.LessonID
	}
	rows, err := r.db.QueryContext(ctx, query, f.CourseID, lessonID, f.OnlyReady, f.OnlyFree)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	videos := []Video{}
	for rows.Next() {
		var v Video
		if err := scanVideo(rows, &v); err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// MarkProcessing moves an 'uploading' video to 'processing'. It returns sql.ErrNoRows if the video is
// missing or no longer 'uploading', so only one confirm can win.
func (r *VideoRepository) MarkProcessing(ctx context.Context, id string, sizeBytes int64) error {
	return r.mustAffect(ctx, `UPDATE videos SET status = 'processing', size_bytes = $2, error = NULL
		WHERE id = $1 AND status = 'uploading'`, id, sizeBytes)
}

// MarkRetrying moves a 'failed' video back to 'processing' with a clean slate (no job, no error).
// It returns sql.ErrNoRows if the video is missing or not 'failed', so only one retry can win.
func (r *VideoRepository) MarkRetrying(ctx context.Context, id string) error {
	return r.mustAffect(ctx, `UPDATE videos SET status = 'processing', error = NULL, transcode_job = NULL, hls_prefix = NULL
		WHERE id = $1 AND status = 'failed'`, id)
}

// RevertToUploading undoes MarkProcessing when the video could not be queued.
func (r *VideoRepository) RevertToUploading(ctx context.Context, id string) error {
	return r.mustAffect(ctx, `UPDATE videos SET status = 'uploading', size_bytes = NULL
		WHERE id = $1 AND status = 'processing' AND transcode_job IS NULL`, id)
}

func (r *VideoRepository) SetTranscodeJob(ctx context.Context, id, job string) error {
	return r.mustAffect(ctx, `UPDATE videos SET transcode_job = $2 WHERE id = $1`, id, job)
}

func (r *VideoRepository) MarkReady(ctx context.Context, id, hlsPrefix string) error {
	return r.mustAffect(ctx, `UPDATE videos SET status = 'ready', hls_prefix = $2, error = NULL
		WHERE id = $1 AND status = 'processing'`, id, hlsPrefix)
}

func (r *VideoRepository) MarkFailed(ctx context.Context, id, msg string) error {
	if utf8.RuneCountInString(msg) > maxVideoErrorChars {
		msg = string([]rune(msg)[:maxVideoErrorChars])
	}
	return r.mustAffect(ctx, `UPDATE videos SET status = 'failed', error = $2
		WHERE id = $1 AND status = 'processing'`, id, msg)
}

// ListProcessingIDs returns videos still being transcoded, oldest first. Used to resume after a restart.
func (r *VideoRepository) ListProcessingIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM videos WHERE status = 'processing' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// mustAffect runs an UPDATE and returns sql.ErrNoRows if it matched no row.
func (r *VideoRepository) mustAffect(ctx context.Context, query string, args ...any) error {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

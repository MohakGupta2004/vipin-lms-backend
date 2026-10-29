package models

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

const maxVideoErrorChars = 1000

// Video kinds: offline recordings and Meet (live class) recordings. Both are uploaded the same way.
const (
	VideoKindOffline = "offline"
	VideoKindMeet    = "meet"
)

// Video is an uploaded lesson video. Lifecycle: uploading (the browser transcodes and uploads HLS) → ready.
// processing and failed are left over from server-side transcoding.
type Video struct {
	ID       string `json:"id"`
	LessonID string `json:"lessonId"`
	CourseID string `json:"courseId"`
	Title    string `json:"title"`
	// Description is what the lecture covers, shown under the player. "" = none written.
	Description string `json:"description"`
	Kind        string `json:"kind"`
	// Tiers lists the tiers that can stream the video; ultra is implied. IsFree is read-only: free is in Tiers.
	Tiers      []string  `json:"tiers"`
	IsFree     bool      `json:"isFree"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	SizeBytes  int64     `json:"sizeBytes,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	UploadedBy string    `json:"-"`
	// Locked is set for users previewing a course: they see the title but cannot stream it.
	Locked bool `json:"locked,omitempty"`
	// Progress is the requesting user's own playback of this video, when listed for a viewer. Nil = never played.
	Progress *VideoProgress `json:"progress,omitempty"`

	// OriginalKey names the video's files: the uploaded source for server-transcoded videos
	// (courses/{c}/videos/{v}/source.mp4), the HLS manifest for browser-transcoded ones (.../{v}/hls/manifest.m3u8).
	OriginalKey string `json:"-"`
	HLSPrefix   string `json:"-"`
}

// VideoProgress is how far one user got in one video.
type VideoProgress struct {
	PositionSec int `json:"positionSec"`
	// DurationSec is the length the user's player reported; nil until it has reported one.
	DurationSec *int       `json:"durationSec"`
	CompletedAt *time.Time `json:"completedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// scanProgress reads the four nullable video_progress columns of a LEFT JOIN; it returns nil when there is no row.
func scanProgress(position, duration sql.NullInt32, completedAt, updatedAt sql.NullTime) *VideoProgress {
	if !updatedAt.Valid {
		return nil
	}
	p := &VideoProgress{PositionSec: int(position.Int32), UpdatedAt: updatedAt.Time}
	if duration.Valid {
		d := int(duration.Int32)
		p.DurationSec = &d
	}
	if completedAt.Valid {
		t := completedAt.Time
		p.CompletedAt = &t
	}
	return p
}

type VideoRepository struct {
	db *sql.DB
}

func NewVideoRepository(db *sql.DB) *VideoRepository {
	return &VideoRepository{db: db}
}

// videoColumns are the columns scanVideo reads, from videos aliased v.
const videoColumns = `v.id, COALESCE(v.lesson_id::text, ''), COALESCE(v.course_id::text, ''), v.uploaded_by, v.title, COALESCE(v.description, ''), v.kind, v.tiers, v.is_free,
		v.status, COALESCE(v.error, ''), COALESCE(v.size_bytes, 0), v.created_at, v.updated_at,
		v.original_key, COALESCE(v.hls_prefix, '')`

const videoSelect = `SELECT ` + videoColumns + `
	FROM videos v`

func scanVideo(row interface{ Scan(...any) error }, v *Video) error {
	return row.Scan(&v.ID, &v.LessonID, &v.CourseID, &v.UploadedBy, &v.Title, &v.Description, &v.Kind, scanTiers(&v.Tiers), &v.IsFree,
		&v.Status, &v.Error, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt,
		&v.OriginalKey, &v.HLSPrefix)
}

// Create inserts a video in the 'uploading' state. v.ID must be set by the caller (it is part of the object key).
func (r *VideoRepository) Create(ctx context.Context, v *Video) error {
	query := `INSERT INTO videos (id, uploaded_by, course_id, lesson_id, title, description, kind, tiers, original_key)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8::text[], $9)
		RETURNING status, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, v.ID, v.UploadedBy, v.CourseID, v.LessonID, v.Title, v.Description, v.Kind, tiersParam(v.Tiers), v.OriginalKey).
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
	Kind      string // empty = both kinds
}

// List returns a course's videos in lesson order. Videos of deleted lessons are left out.
func (r *VideoRepository) List(ctx context.Context, f VideoListFilter) ([]Video, error) {
	query := videoSelect + ` JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		WHERE v.course_id = $1
		  AND ($2::uuid IS NULL OR v.lesson_id = $2::uuid)
		  AND (NOT $3 OR (v.status = 'ready' AND l.is_published))
		  AND ($4 = '' OR v.kind = $4)
		ORDER BY l.position, v.created_at`
	var lessonID any
	if f.LessonID != "" {
		lessonID = f.LessonID
	}
	rows, err := r.db.QueryContext(ctx, query, f.CourseID, lessonID, f.OnlyReady, f.Kind)
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

// UpdateDetails saves a video's title, description, kind and tiers. It returns sql.ErrNoRows if the video does not exist.
func (r *VideoRepository) UpdateDetails(ctx context.Context, id, title, description, kind string, tiers []string) error {
	return r.mustAffect(ctx, `UPDATE videos SET title = $2, description = NULLIF($3, ''), kind = $4, tiers = $5::text[] WHERE id = $1`,
		id, title, description, kind, tiersParam(tiers))
}

// PublishUpload marks a video whose HLS files the browser has uploaded as ready to stream. It returns
// sql.ErrNoRows if the video is missing or no longer 'uploading', so only one confirm can win.
func (r *VideoRepository) PublishUpload(ctx context.Context, id, hlsPrefix string, sizeBytes int64) error {
	return r.mustAffect(ctx, `UPDATE videos SET status = 'ready', hls_prefix = $2, size_bytes = $3, error = NULL
		WHERE id = $1 AND status = 'uploading'`, id, hlsPrefix, sizeBytes)
}

// TouchUpload records that the browser is still uploading a video (it bumps updated_at), so the
// abandoned-upload sweep leaves it alone. It returns sql.ErrNoRows if the video is not 'uploading'.
func (r *VideoRepository) TouchUpload(ctx context.Context, id string) error {
	return r.mustAffect(ctx, `UPDATE videos SET updated_at = now() WHERE id = $1 AND status = 'uploading'`, id)
}

// ListStaleUploads returns videos left 'uploading' with no sign of life for longer than idle, oldest first.
func (r *VideoRepository) ListStaleUploads(ctx context.Context, idle time.Duration, limit int) ([]Video, error) {
	rows, err := r.db.QueryContext(ctx, videoSelect+`
		WHERE v.status = 'uploading' AND v.updated_at < now() - make_interval(secs => $1)
		ORDER BY v.updated_at LIMIT $2`, idle.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		if err := scanVideo(rows, &v); err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// DeleteStaleUpload removes a video that is still 'uploading' and still idle for longer than idle, so a
// browser that renewed its upload in the meantime keeps it, and queues its folder for removal (see
// ClaimFileDeletion). It returns sql.ErrNoRows if nothing was deleted.
func (r *VideoRepository) DeleteStaleUpload(ctx context.Context, id string, idle time.Duration) error {
	var key string
	return r.db.QueryRowContext(ctx, `WITH gone AS (
			DELETE FROM videos
			WHERE id = $1 AND status = 'uploading' AND updated_at < now() - make_interval(secs => $2)
			RETURNING original_key
		), queued AS (
			INSERT INTO video_file_deletions (original_key) SELECT original_key FROM gone
		)
		SELECT original_key FROM gone`, id, idle.Seconds()).Scan(&key)
}

// MarkReady finishes a video the old server-side transcoder was working on.
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

// ListProcessing returns videos the old server-side transcoder left 'processing', oldest first.
func (r *VideoRepository) ListProcessing(ctx context.Context) ([]Video, error) {
	rows, err := r.db.QueryContext(ctx, videoSelect+` WHERE v.status = 'processing' ORDER BY v.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		if err := scanVideo(rows, &v); err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// Delete removes a video row; its progress and shares go with it (ON DELETE CASCADE). It returns sql.ErrNoRows
// if the video does not exist, and leaves a video the old server-side transcoder is still on alone (busy = true).
// The video's folder is queued for removal in the same statement (see ClaimFileDeletion); a video still
// 'uploading' is queued a second time, resweepAfter later, for files a browser was still sending.
func (r *VideoRepository) Delete(ctx context.Context, id string, resweepAfter time.Duration) (busy bool, err error) {
	var status string
	err = r.db.QueryRowContext(ctx, `WITH gone AS (
			DELETE FROM videos WHERE id = $1 AND status <> 'processing' RETURNING status, original_key
		), queued AS (
			INSERT INTO video_file_deletions (original_key, run_after)
			SELECT original_key, now() FROM gone
			UNION ALL
			SELECT original_key, now() + make_interval(secs => $2) FROM gone WHERE status = 'uploading'
		)
		SELECT status FROM gone`, id, resweepAfter.Seconds()).Scan(&status)
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	// Nothing deleted: either the video is gone or it is processing.
	if err := r.db.QueryRowContext(ctx, `SELECT status FROM videos WHERE id = $1`, id).Scan(&status); err != nil {
		return false, err
	}
	return true, nil
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

// SaveProgress stores where the user stopped in a video, replacing any earlier position. A non-nil durationSec
// replaces the stored duration. reached marks the video completed; a completed video stays completed.
func (r *VideoRepository) SaveProgress(ctx context.Context, userID, videoID string, positionSec int, durationSec *int, reached bool) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO video_progress (user_id, video_id, position_sec, duration_sec, completed_at)
		VALUES ($1, $2, $3, $4, CASE WHEN $5::boolean THEN now() END)
		ON CONFLICT (user_id, video_id) DO UPDATE SET
			position_sec = EXCLUDED.position_sec,
			duration_sec = COALESCE(EXCLUDED.duration_sec, video_progress.duration_sec),
			completed_at = COALESCE(video_progress.completed_at, EXCLUDED.completed_at)`,
		userID, videoID, positionSec, durationSec, reached)
	return err
}

// SetCompleted marks a video completed (keeping the first completion time) or not completed for a user.
func (r *VideoRepository) SetCompleted(ctx context.Context, userID, videoID string, completed bool) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO video_progress (user_id, video_id, completed_at)
		VALUES ($1, $2, CASE WHEN $3::boolean THEN now() END)
		ON CONFLICT (user_id, video_id) DO UPDATE SET
			completed_at = CASE WHEN $3::boolean THEN COALESCE(video_progress.completed_at, now()) END`,
		userID, videoID, completed)
	return err
}

// ListProgress returns a user's progress in a course's videos, keyed by video ID. Videos never played are absent.
func (r *VideoRepository) ListProgress(ctx context.Context, userID, courseID string) (map[string]*VideoProgress, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT vp.video_id, vp.position_sec, vp.duration_sec, vp.completed_at, vp.updated_at
		FROM video_progress vp
		JOIN videos v ON v.id = vp.video_id
		WHERE vp.user_id = $1 AND v.course_id = $2`, userID, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	progress := map[string]*VideoProgress{}
	for rows.Next() {
		var videoID string
		var position, duration sql.NullInt32
		var completedAt, updatedAt sql.NullTime
		if err := rows.Scan(&videoID, &position, &duration, &completedAt, &updatedAt); err != nil {
			return nil, err
		}
		progress[videoID] = scanProgress(position, duration, completedAt, updatedAt)
	}
	return progress, rows.Err()
}

// GetProgress returns where the user stopped in a video, or 0 if they never played it.
func (r *VideoRepository) GetProgress(ctx context.Context, userID, videoID string) (int, error) {
	var positionSec int
	err := r.db.QueryRowContext(ctx, `SELECT position_sec FROM video_progress WHERE user_id = $1 AND video_id = $2`,
		userID, videoID).Scan(&positionSec)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return positionSec, err
}

// FilesInUse reports whether any video still points at this uploaded source file, and so at its folder.
// Batches copied from one course share their offline recordings.
func (r *VideoRepository) FilesInUse(ctx context.Context, originalKey string) (bool, error) {
	var inUse bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM videos WHERE original_key = $1)", originalKey).Scan(&inUse)
	return inUse, err
}

// FileDeletion is a deleted video's folder waiting to be emptied, named by the video's original_key.
type FileDeletion struct {
	ID          int64
	OriginalKey string
}

// ClaimFileDeletion takes the oldest queued folder that is due and hides it from other workers for lease. A
// worker that dies holding it leaves it to be claimed again once the lease runs out. It returns sql.ErrNoRows
// if nothing is due.
func (r *VideoRepository) ClaimFileDeletion(ctx context.Context, lease time.Duration) (*FileDeletion, error) {
	var d FileDeletion
	err := r.db.QueryRowContext(ctx, `UPDATE video_file_deletions SET run_after = now() + make_interval(secs => $1)
		WHERE id = (SELECT id FROM video_file_deletions WHERE run_after <= now()
			ORDER BY run_after LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, original_key`, lease.Seconds()).Scan(&d.ID, &d.OriginalKey)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FinishFileDeletion drops a queued folder once it is empty (or still used by another copy of the video).
func (r *VideoRepository) FinishFileDeletion(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM video_file_deletions WHERE id = $1`, id)
	return err
}

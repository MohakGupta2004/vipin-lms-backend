package models

import (
	"context"
	"database/sql"
	"time"
)

// Note is a PDF an instructor shared on a lesson. It is a row of the notes table with
// visibility "course" and a file attached. Text-only notes (no file) are not returned here.
type Note struct {
	ID           string    `json:"id"`
	CourseID     string    `json:"courseId"`
	LessonID     string    `json:"lessonId"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	FileName     string    `json:"fileName"`
	SizeBytes    int64     `json:"sizeBytes"`
	IsFree       bool      `json:"isFree"` // open to users previewing the course; a free lesson does not make its notes free
	UploadedBy   string    `json:"uploadedBy"`
	UploaderName string    `json:"uploaderName"`
	CreatedAt    time.Time `json:"createdAt"`
	// Locked is set for users previewing a course: they see the note but cannot open it.
	Locked bool `json:"locked,omitempty"`

	ObjectKey       string `json:"-"` // storage location, never sent to clients
	LessonPublished bool   `json:"-"` // students may only see notes of published lessons
}

type NoteRepository struct {
	db *sql.DB
}

func NewNoteRepository(db *sql.DB) *NoteRepository {
	return &NoteRepository{
		db: db,
	}
}

// Create saves a note row and fills in the generated fields.
func (r *NoteRepository) Create(ctx context.Context, n *Note) error {
	query := `INSERT INTO notes (author_id, course_id, lesson_id, title, content, visibility, file_name, object_key, size_bytes, is_free)
		VALUES ($1, $2, $3, $4, $5, 'course', $6, $7, $8, $9)
		RETURNING id, created_at`

	return r.db.QueryRowContext(ctx, query, n.UploadedBy, n.CourseID, n.LessonID, n.Title, n.Description,
		n.FileName, n.ObjectKey, n.SizeBytes, n.IsFree).Scan(&n.ID, &n.CreatedAt)
}

const noteSelect = `SELECT n.id, n.course_id, n.lesson_id, COALESCE(n.title, ''), n.content, n.file_name, n.size_bytes, n.is_free,
		n.author_id, u.first_name || ' ' || u.last_name, n.created_at, n.object_key, l.is_published
	FROM notes n
	JOIN users u ON u.id = n.author_id
	JOIN lessons l ON l.id = n.lesson_id AND l.deleted_at IS NULL
	WHERE n.object_key IS NOT NULL AND n.visibility = 'course' AND n.deleted_at IS NULL`

func scanNote(row interface{ Scan(...any) error }, n *Note) error {
	return row.Scan(&n.ID, &n.CourseID, &n.LessonID, &n.Title, &n.Description, &n.FileName, &n.SizeBytes, &n.IsFree,
		&n.UploadedBy, &n.UploaderName, &n.CreatedAt, &n.ObjectKey, &n.LessonPublished)
}

// ListByCourse returns a course's PDF notes, newest first. Empty lessonID means all lessons.
// Notes of unpublished lessons are left out unless includeUnpublished is set.
// A limit of 0 means no limit.
func (r *NoteRepository) ListByCourse(ctx context.Context, courseID, lessonID string, includeUnpublished bool, limit, offset int) ([]Note, error) {
	query := noteSelect + `
		AND n.course_id = $1
		AND ($2::uuid IS NULL OR n.lesson_id = $2)
		AND ($3 OR l.is_published)
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $4 OFFSET $5`

	var limitArg any
	if limit > 0 {
		limitArg = limit
	}
	rows, err := r.db.QueryContext(ctx, query, courseID, nullIfEmpty(lessonID), includeUnpublished, limitArg, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := scanNote(rows, &n); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// Get returns one PDF note, or sql.ErrNoRows.
func (r *NoteRepository) Get(ctx context.Context, noteID string) (*Note, error) {
	var n Note
	if err := scanNote(r.db.QueryRowContext(ctx, noteSelect+" AND n.id = $1", noteID), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Update saves a PDF note's title, description and free flag. It returns sql.ErrNoRows if the note
// does not exist.
func (r *NoteRepository) Update(ctx context.Context, n *Note) error {
	var id string
	query := `UPDATE notes SET title = $1, content = $2, is_free = $3
		WHERE id = $4 AND object_key IS NOT NULL AND deleted_at IS NULL RETURNING id`
	return r.db.QueryRowContext(ctx, query, n.Title, n.Description, n.IsFree, n.ID).Scan(&id)
}

// Delete removes the note row and returns its storage key. It returns sql.ErrNoRows if the note
// does not exist.
func (r *NoteRepository) Delete(ctx context.Context, noteID string) (objectKey string, err error) {
	query := "DELETE FROM notes WHERE id = $1 AND object_key IS NOT NULL RETURNING object_key"
	err = r.db.QueryRowContext(ctx, query, noteID).Scan(&objectKey)
	return objectKey, err
}

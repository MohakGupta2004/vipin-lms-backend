package models

import (
	"context"
	"database/sql"
	"time"
)

// CourseNote is a PDF an instructor uploaded for a course.
type CourseNote struct {
	ID           string    `json:"id"`
	CourseID     string    `json:"courseId"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	FileName     string    `json:"fileName"`
	SizeBytes    int64     `json:"sizeBytes"`
	UploadedBy   string    `json:"uploadedBy"`
	UploaderName string    `json:"uploaderName"`
	CreatedAt    time.Time `json:"createdAt"`

	ObjectKey string `json:"-"` // storage location, never sent to clients
}

type CourseNoteRepository struct {
	db *sql.DB
}

func NewCourseNoteRepository(db *sql.DB) *CourseNoteRepository {
	return &CourseNoteRepository{
		db: db,
	}
}

// CourseAccess says how a user relates to a course. It returns sql.ErrNoRows if the course
// does not exist or is deleted.
//   - isInstructor: the user teaches this course.
//   - isEnrolled: the user has an enrollment that is still valid.
func (r *CourseNoteRepository) CourseAccess(ctx context.Context, courseID, userID string) (isInstructor, isEnrolled bool, err error) {
	query := `
		SELECT c.instructor_id = $2,
		       EXISTS (
		           SELECT 1 FROM enrollments e
		           WHERE e.course_id = c.id
		             AND e.user_id = $2
		             AND e.status IN ('active', 'completed')
		             AND (e.expires_at IS NULL OR e.expires_at > now())
		       )
		FROM courses c
		WHERE c.id = $1 AND c.deleted_at IS NULL`

	err = r.db.QueryRowContext(ctx, query, courseID, userID).Scan(&isInstructor, &isEnrolled)
	return isInstructor, isEnrolled, err
}

// Create saves a note row and fills in the generated fields.
func (r *CourseNoteRepository) Create(ctx context.Context, n *CourseNote) error {
	query := `INSERT INTO course_notes (course_id, uploaded_by, title, description, file_name, object_key, size_bytes)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)
		RETURNING id, created_at`

	return r.db.QueryRowContext(ctx, query, n.CourseID, n.UploadedBy, n.Title, n.Description, n.FileName, n.ObjectKey, n.SizeBytes).
		Scan(&n.ID, &n.CreatedAt)
}

const courseNoteColumns = `n.id, n.course_id, n.title, COALESCE(n.description, ''), n.file_name, n.size_bytes,
	n.uploaded_by, u.first_name || ' ' || u.last_name, n.created_at, n.object_key`

func scanCourseNote(row interface{ Scan(...any) error }, n *CourseNote) error {
	return row.Scan(&n.ID, &n.CourseID, &n.Title, &n.Description, &n.FileName, &n.SizeBytes,
		&n.UploadedBy, &n.UploaderName, &n.CreatedAt, &n.ObjectKey)
}

// ListByCourse returns a course's notes, newest first.
func (r *CourseNoteRepository) ListByCourse(ctx context.Context, courseID string, limit, offset int) ([]CourseNote, error) {
	query := `SELECT ` + courseNoteColumns + `
		FROM course_notes n
		JOIN users u ON u.id = n.uploaded_by
		WHERE n.course_id = $1
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, courseID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []CourseNote{}
	for rows.Next() {
		var n CourseNote
		if err := scanCourseNote(rows, &n); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// Get returns one note, or sql.ErrNoRows.
func (r *CourseNoteRepository) Get(ctx context.Context, noteID string) (*CourseNote, error) {
	query := `SELECT ` + courseNoteColumns + `
		FROM course_notes n
		JOIN users u ON u.id = n.uploaded_by
		WHERE n.id = $1`

	var n CourseNote
	if err := scanCourseNote(r.db.QueryRowContext(ctx, query, noteID), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Delete removes the note row and returns its storage key. It returns sql.ErrNoRows if the note
// does not exist.
func (r *CourseNoteRepository) Delete(ctx context.Context, noteID string) (objectKey string, err error) {
	err = r.db.QueryRowContext(ctx, "DELETE FROM course_notes WHERE id = $1 RETURNING object_key", noteID).Scan(&objectKey)
	return objectKey, err
}

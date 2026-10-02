package models

import (
	"context"
	"database/sql"
	"time"
)

// Lesson is one chapter of a course. PDF notes hang off a lesson.
type Lesson struct {
	ID          string    `json:"id"`
	CourseID    string    `json:"courseId"`
	Title       string    `json:"title"`
	LessonType  string    `json:"lessonType"`
	Content     string    `json:"content"`
	IsFree      bool      `json:"isFree"`
	IsPublished bool      `json:"isPublished"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"createdAt"`
	Notes       []Note    `json:"notes,omitempty"`
}

type LessonRepository struct {
	db *sql.DB
}

func NewLessonRepository(db *sql.DB) *LessonRepository {
	return &LessonRepository{
		db: db,
	}
}

// CourseAccess says how a user relates to a course. It returns sql.ErrNoRows if the course
// does not exist or is deleted.
//   - isInstructor: the user teaches this course.
//   - isEnrolled: the user has an enrollment that is still valid.
func (r *LessonRepository) CourseAccess(ctx context.Context, courseID, userID string) (isInstructor, isEnrolled bool, err error) {
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

// CreateLesson saves a new lesson at the end of the course and fills in the generated fields.
func (r *LessonRepository) CreateLesson(ctx context.Context, l *Lesson) error {
	query := `INSERT INTO lessons (course_id, title, lesson_type, content, is_free, is_published, position)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6,
		        (SELECT COALESCE(MAX(position), 0) + 1 FROM lessons WHERE course_id = $1 AND deleted_at IS NULL))
		RETURNING id, position, created_at`

	return r.db.QueryRowContext(ctx, query, l.CourseID, l.Title, l.LessonType, l.Content, l.IsFree, l.IsPublished).
		Scan(&l.ID, &l.Position, &l.CreatedAt)
}

// ListLessons returns a course's lessons in order. Unpublished lessons are left out unless includeUnpublished is set.
func (r *LessonRepository) ListLessons(ctx context.Context, courseID string, includeUnpublished bool) ([]Lesson, error) {
	query := `SELECT id, course_id, title, lesson_type, COALESCE(content, ''),
		       is_free, is_published, position, created_at
		FROM lessons
		WHERE course_id = $1 AND deleted_at IS NULL AND ($2 OR is_published)
		ORDER BY position, created_at`

	rows, err := r.db.QueryContext(ctx, query, courseID, includeUnpublished)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lessons := []Lesson{}
	for rows.Next() {
		var l Lesson
		err := rows.Scan(&l.ID, &l.CourseID, &l.Title, &l.LessonType, &l.Content,
			&l.IsFree, &l.IsPublished, &l.Position, &l.CreatedAt)
		if err != nil {
			return nil, err
		}
		lessons = append(lessons, l)
	}
	return lessons, rows.Err()
}

// GetLesson returns one lesson that is not deleted, or sql.ErrNoRows.
func (r *LessonRepository) GetLesson(ctx context.Context, lessonID string) (*Lesson, error) {
	query := `SELECT id, course_id, title, lesson_type, COALESCE(content, ''),
		       is_free, is_published, position, created_at
		FROM lessons
		WHERE id = $1 AND deleted_at IS NULL`

	var l Lesson
	err := r.db.QueryRowContext(ctx, query, lessonID).Scan(&l.ID, &l.CourseID, &l.Title, &l.LessonType,
		&l.Content, &l.IsFree, &l.IsPublished, &l.Position, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

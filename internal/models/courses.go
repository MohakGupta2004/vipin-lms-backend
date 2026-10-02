package models

import (
	"context"
	"database/sql"
	"time"
)

// Course is one course an admin created.
type Course struct {
	ID               string    `json:"id"`
	ExamID           string    `json:"examId"`
	InstructorID     string    `json:"instructorId"`
	Title            string    `json:"title"`
	Slug             string    `json:"slug"`
	ShortDescription string    `json:"shortDescription"`
	Description      string    `json:"description"`
	Status           string    `json:"status"`
	IsFree           bool      `json:"isFree"`
	CreatedAt        time.Time `json:"createdAt"`
}

type CourseRepository struct {
	db *sql.DB
}

func NewCourseRepository(db *sql.DB) *CourseRepository {
	return &CourseRepository{
		db: db,
	}
}

// ExamExists reports whether an active exam with this id exists.
func (r *CourseRepository) ExamExists(ctx context.Context, examID string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS (SELECT 1 FROM exams WHERE id = $1 AND is_active)"
	err := r.db.QueryRowContext(ctx, query, examID).Scan(&exists)
	return exists, err
}

// InstructorExists reports whether an active user with role instructor has this id.
func (r *CourseRepository) InstructorExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS (
		SELECT 1 FROM users
		WHERE id = $1 AND role = 'instructor' AND is_active AND deleted_at IS NULL
	)`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

// CreateCourse saves a new course and fills in its id and created_at.
func (r *CourseRepository) CreateCourse(ctx context.Context, course *Course) error {
	query := `INSERT INTO courses
		(exam_id, instructor_id, title, slug, short_description, description, status, is_free)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`

	return r.db.QueryRowContext(ctx, query,
		course.ExamID, course.InstructorID, course.Title, course.Slug,
		course.ShortDescription, course.Description, course.Status, course.IsFree,
	).Scan(&course.ID, &course.CreatedAt)
}

// ListCourses returns courses that are not deleted, newest first.
func (r *CourseRepository) ListCourses(ctx context.Context, limit, offset int) ([]Course, error) {
	query := `SELECT id, exam_id, instructor_id, title, slug,
			COALESCE(short_description, ''), COALESCE(description, ''), status, is_free, created_at
		FROM courses
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []Course{}
	for rows.Next() {
		var c Course
		err := rows.Scan(&c.ID, &c.ExamID, &c.InstructorID, &c.Title, &c.Slug,
			&c.ShortDescription, &c.Description, &c.Status, &c.IsFree, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

// UpdateStatus changes the status of a course, but only if it belongs to this instructor.
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) UpdateStatus(ctx context.Context, courseID, instructorID, status string) (*Course, error) {
	query := `UPDATE courses SET status = $1
		WHERE id = $2 AND instructor_id = $3 AND deleted_at IS NULL
		RETURNING id, exam_id, instructor_id, title, slug,
			COALESCE(short_description, ''), COALESCE(description, ''), status, is_free, created_at`

	var c Course
	err := r.db.QueryRowContext(ctx, query, status, courseID, instructorID).Scan(
		&c.ID, &c.ExamID, &c.InstructorID, &c.Title, &c.Slug,
		&c.ShortDescription, &c.Description, &c.Status, &c.IsFree, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

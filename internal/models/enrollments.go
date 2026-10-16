package models

import (
	"context"
	"database/sql"
	"time"
)

// Enrollment links a student to a course.
type Enrollment struct {
	ID         string     `json:"id"`
	UserID     string     `json:"userId"`
	CourseID   string     `json:"courseId"`
	Status     string     `json:"status"`
	Tier       Tier       `json:"tier"` // plus, premium or ultra: how much of the course the student can open
	Source     string     `json:"source"`
	EnrolledAt time.Time  `json:"enrolledAt"`
	ExpiresAt  *time.Time `json:"expiresAt"` // null = never expires
}

type EnrollmentRepository struct {
	db *sql.DB
}

func NewEnrollmentRepository(db *sql.DB) *EnrollmentRepository {
	return &EnrollmentRepository{
		db: db,
	}
}

// CoursePublished reports whether the course exists, is published and is not deleted.
func (r *EnrollmentRepository) CoursePublished(ctx context.Context, courseID string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND status = 'published' AND deleted_at IS NULL)"
	err := r.db.QueryRowContext(ctx, query, courseID).Scan(&exists)
	return exists, err
}

// StudentExists reports whether an active user with role student has this id.
func (r *EnrollmentRepository) StudentExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS (
		SELECT 1 FROM users
		WHERE id = $1 AND role = 'student' AND is_active AND deleted_at IS NULL
	)`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

// CreateEnrollment saves a new admin-made enrollment and fills in the generated fields.
func (r *EnrollmentRepository) CreateEnrollment(ctx context.Context, e *Enrollment) error {
	query := `INSERT INTO enrollments (user_id, course_id, tier, source, expires_at)
		VALUES ($1, $2, $3, 'admin', $4)
		RETURNING id, status, source, enrolled_at`

	return r.db.QueryRowContext(ctx, query, e.UserID, e.CourseID, e.Tier, e.ExpiresAt).
		Scan(&e.ID, &e.Status, &e.Source, &e.EnrolledAt)
}

// ListEnrollments returns enrollments, newest first. Empty courseID or userID means "no filter".
func (r *EnrollmentRepository) ListEnrollments(ctx context.Context, courseID, userID string, limit, offset int) ([]Enrollment, error) {
	query := `SELECT id, user_id, course_id, status, tier, source, enrolled_at, expires_at
		FROM enrollments
		WHERE ($1::uuid IS NULL OR course_id = $1)
		  AND ($2::uuid IS NULL OR user_id = $2)
		ORDER BY enrolled_at DESC, id DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, nullIfEmpty(courseID), nullIfEmpty(userID), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	enrollments := []Enrollment{}
	for rows.Next() {
		var e Enrollment
		err := rows.Scan(&e.ID, &e.UserID, &e.CourseID, &e.Status, &e.Tier, &e.Source, &e.EnrolledAt, &e.ExpiresAt)
		if err != nil {
			return nil, err
		}
		enrollments = append(enrollments, e)
	}
	return enrollments, rows.Err()
}

// Update changes the status and/or the tier of an enrollment; a nil field is left as it is. It returns
// sql.ErrNoRows if the enrollment does not exist.
func (r *EnrollmentRepository) Update(ctx context.Context, enrollmentID string, status *string, tier *Tier) (*Enrollment, error) {
	query := `UPDATE enrollments SET status = COALESCE($1::text, status), tier = COALESCE($2::text, tier),
			completed_at = CASE WHEN $1::text IS NULL THEN completed_at
			                    WHEN $1::text = 'completed' THEN now() ELSE NULL END
		WHERE id = $3
		RETURNING id, user_id, course_id, status, tier, source, enrolled_at, expires_at`

	var tierArg *string
	if tier != nil {
		t := string(*tier)
		tierArg = &t
	}
	var e Enrollment
	err := r.db.QueryRowContext(ctx, query, status, tierArg, enrollmentID).
		Scan(&e.ID, &e.UserID, &e.CourseID, &e.Status, &e.Tier, &e.Source, &e.EnrolledAt, &e.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// nullIfEmpty turns "" into SQL NULL so an optional filter can be skipped in the query.
func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

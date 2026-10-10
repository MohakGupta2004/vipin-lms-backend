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
	// Access is only filled in by GET /courses/{id}: "owner", "full" (enrolled, or the course is free)
	// or "preview" (published course, not enrolled: free content only).
	Access string `json:"access,omitempty"`
}

// CatalogCourse is a published course as the catalog shows it, with how much of it is free to preview.
type CatalogCourse struct {
	Course
	InstructorName  string `json:"instructorName"`
	Enrolled        bool   `json:"enrolled"`
	LessonCount     int    `json:"lessonCount"`
	VideoCount      int    `json:"videoCount"`
	FreeLessonCount int    `json:"freeLessonCount"`
	FreeVideoCount  int    `json:"freeVideoCount"`
	FreeNoteCount   int    `json:"freeNoteCount"`
	FreeQuizCount   int    `json:"freeQuizCount"`
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

// InstructorExists reports whether an active user who may own courses (role instructor or admin) has this id.
func (r *CourseRepository) InstructorExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS (
		SELECT 1 FROM users
		WHERE id = $1 AND role IN ('instructor', 'admin') AND is_active AND deleted_at IS NULL
	)`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

const courseColumns = `id, exam_id, instructor_id, title, slug,
		COALESCE(short_description, ''), COALESCE(description, ''), status, is_free, created_at`

func scanCourse(row interface{ Scan(...any) error }, c *Course) error {
	return row.Scan(&c.ID, &c.ExamID, &c.InstructorID, &c.Title, &c.Slug,
		&c.ShortDescription, &c.Description, &c.Status, &c.IsFree, &c.CreatedAt)
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
	query := `SELECT ` + courseColumns + `
		FROM courses
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`
	return r.queryCourses(ctx, query, limit, offset)
}

// ListByInstructor returns the courses this user owns, drafts included, newest first.
func (r *CourseRepository) ListByInstructor(ctx context.Context, instructorID string, limit, offset int) ([]Course, error) {
	query := `SELECT ` + courseColumns + `
		FROM courses
		WHERE instructor_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`
	return r.queryCourses(ctx, query, instructorID, limit, offset)
}

// ListEnrolled returns the published courses this user has a valid enrollment in, newest enrollment first.
func (r *CourseRepository) ListEnrolled(ctx context.Context, userID string, limit, offset int) ([]Course, error) {
	query := `SELECT c.id, c.exam_id, c.instructor_id, c.title, c.slug,
			COALESCE(c.short_description, ''), COALESCE(c.description, ''), c.status, c.is_free, c.created_at
		FROM courses c
		JOIN enrollments e ON e.course_id = c.id
		WHERE e.user_id = $1
		  AND e.status IN ('active', 'completed')
		  AND (e.expires_at IS NULL OR e.expires_at > now())
		  AND c.status = 'published'
		  AND c.deleted_at IS NULL
		ORDER BY e.enrolled_at DESC, c.id DESC
		LIMIT $2 OFFSET $3`
	return r.queryCourses(ctx, query, userID, limit, offset)
}

// ListCatalog returns every published course, newest first, with counts of its free content as seen
// by a user who is not enrolled. In a free course everything counts as free.
// freeOnly keeps just the courses that have something free to preview.
func (r *CourseRepository) ListCatalog(ctx context.Context, userID string, freeOnly bool, limit, offset int) ([]CatalogCourse, error) {
	query := `
		WITH stats AS (
			SELECT c.id,
			       (SELECT COUNT(*) FROM lessons l
			         WHERE l.course_id = c.id AND l.deleted_at IS NULL AND l.is_published) AS lessons,
			       (SELECT COUNT(*) FROM lessons l
			         WHERE l.course_id = c.id AND l.deleted_at IS NULL AND l.is_published
			           AND (c.is_free OR l.is_free)) AS free_lessons,
			       (SELECT COUNT(*) FROM videos v JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
			         WHERE v.course_id = c.id AND v.status = 'ready' AND l.is_published) AS videos,
			       (SELECT COUNT(*) FROM videos v JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
			         WHERE v.course_id = c.id AND v.status = 'ready' AND l.is_published
			           AND (c.is_free OR v.is_free)) AS free_videos,
			       (SELECT COUNT(*) FROM notes n JOIN lessons l ON l.id = n.lesson_id AND l.deleted_at IS NULL
			         WHERE n.course_id = c.id AND n.object_key IS NOT NULL AND n.visibility = 'course'
			           AND n.deleted_at IS NULL AND l.is_published AND (c.is_free OR n.is_free)) AS free_notes,
			       (SELECT COUNT(*) FROM quizzes q JOIN lessons l ON l.id = q.lesson_id AND l.deleted_at IS NULL
			         WHERE q.course_id = c.id AND q.deleted_at IS NULL AND q.status = 'published'
			           AND l.is_published AND (c.is_free OR q.is_free)) AS free_quizzes
			FROM courses c
			WHERE c.status = 'published' AND c.deleted_at IS NULL
		)
		SELECT c.id, c.exam_id, c.instructor_id, c.title, c.slug,
		       COALESCE(c.short_description, ''), COALESCE(c.description, ''), c.status, c.is_free, c.created_at,
		       u.first_name || ' ' || u.last_name,
		       EXISTS (
		           SELECT 1 FROM enrollments e
		           WHERE e.course_id = c.id AND e.user_id = $1
		             AND e.status IN ('active', 'completed')
		             AND (e.expires_at IS NULL OR e.expires_at > now())
		       ),
		       s.lessons, s.videos, s.free_lessons, s.free_videos, s.free_notes, s.free_quizzes
		FROM courses c
		JOIN stats s ON s.id = c.id
		JOIN users u ON u.id = c.instructor_id
		WHERE NOT $2 OR (s.free_lessons + s.free_videos + s.free_notes + s.free_quizzes) > 0
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, userID, freeOnly, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []CatalogCourse{}
	for rows.Next() {
		var c CatalogCourse
		err := rows.Scan(&c.ID, &c.ExamID, &c.InstructorID, &c.Title, &c.Slug,
			&c.ShortDescription, &c.Description, &c.Status, &c.IsFree, &c.CreatedAt,
			&c.InstructorName, &c.Enrolled,
			&c.LessonCount, &c.VideoCount, &c.FreeLessonCount, &c.FreeVideoCount, &c.FreeNoteCount, &c.FreeQuizCount)
		if err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

func (r *CourseRepository) queryCourses(ctx context.Context, query string, args ...any) ([]Course, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []Course{}
	for rows.Next() {
		var c Course
		if err := scanCourse(rows, &c); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

// GetCourse returns one course that is not deleted, or sql.ErrNoRows.
func (r *CourseRepository) GetCourse(ctx context.Context, courseID string) (*Course, error) {
	var c Course
	query := `SELECT ` + courseColumns + ` FROM courses WHERE id = $1 AND deleted_at IS NULL`
	if err := scanCourse(r.db.QueryRowContext(ctx, query, courseID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// HasValidEnrollment reports whether the user has an enrollment in the course that is still valid.
func (r *CourseRepository) HasValidEnrollment(ctx context.Context, courseID, userID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS (
		SELECT 1 FROM enrollments
		WHERE course_id = $1 AND user_id = $2
		  AND status IN ('active', 'completed')
		  AND (expires_at IS NULL OR expires_at > now())
	)`
	err := r.db.QueryRowContext(ctx, query, courseID, userID).Scan(&exists)
	return exists, err
}

// UpdateCourse saves the editable fields of a course, but only if it belongs to this instructor.
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) UpdateCourse(ctx context.Context, c *Course, instructorID string) error {
	query := `UPDATE courses
		SET exam_id = $1, title = $2, slug = $3,
		    short_description = NULLIF($4, ''), description = NULLIF($5, ''), is_free = $6
		WHERE id = $7 AND instructor_id = $8 AND deleted_at IS NULL
		RETURNING ` + courseColumns

	return scanCourse(r.db.QueryRowContext(ctx, query,
		c.ExamID, c.Title, c.Slug, c.ShortDescription, c.Description, c.IsFree, c.ID, instructorID), c)
}

// SoftDelete marks a course deleted, but only if it belongs to this instructor.
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) SoftDelete(ctx context.Context, courseID, instructorID string) error {
	var id string
	query := `UPDATE courses SET deleted_at = now()
		WHERE id = $1 AND instructor_id = $2 AND deleted_at IS NULL
		RETURNING id`
	return r.db.QueryRowContext(ctx, query, courseID, instructorID).Scan(&id)
}

// UpdateStatus changes the status of a course, but only if it belongs to this instructor.
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) UpdateStatus(ctx context.Context, courseID, instructorID, status string) (*Course, error) {
	query := `UPDATE courses SET status = $1
		WHERE id = $2 AND instructor_id = $3 AND deleted_at IS NULL
		RETURNING ` + courseColumns

	var c Course
	if err := scanCourse(r.db.QueryRowContext(ctx, query, status, courseID, instructorID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

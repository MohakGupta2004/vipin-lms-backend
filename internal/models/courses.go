package models

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Course is one course an admin created.
type Course struct {
	ID               string `json:"id"`
	ExamID           string `json:"examId"`
	InstructorID     string `json:"instructorId"`
	Title            string `json:"title"`
	Slug             string `json:"slug"`
	ShortDescription string `json:"shortDescription"`
	Description      string `json:"description"`
	Status           string `json:"status"`
	IsFree           bool   `json:"isFree"`
	// Visibility is public or private. A private course stays open to the students already enrolled in it
	// (until their enrollment ends) and is hidden from everyone else.
	Visibility string `json:"visibility"`
	// BatchGroupID is shared by every batch of a course: the id of its first batch. BatchNumber counts the
	// batches from 1, so batch 2 of IFC is shown as "IFC 2.0".
	BatchGroupID string    `json:"batchGroupId"`
	BatchNumber  int       `json:"batchNumber"`
	CreatedAt    time.Time `json:"createdAt"`
	// Access is only filled in by GET /courses/{id}: "owner", "full" (enrolled, or the course is free)
	// or "preview" (published course, not enrolled: free content only).
	Access string `json:"access,omitempty"`
	// Tier is also only filled in by GET /courses/{id}: free, plus, premium or ultra. The owner and
	// anyone in a free course are ultra; an enrolled student has the tier of their enrollment.
	Tier Tier `json:"tier,omitempty"`
}

// Course visibility: a private course is hidden from everyone who is not enrolled in it.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// CatalogCourse is a published course as the catalog shows it, with how much of it is free to preview.
type CatalogCourse struct {
	Course
	InstructorName     string `json:"instructorName"`
	Enrolled           bool   `json:"enrolled"`
	LessonCount        int    `json:"lessonCount"`
	VideoCount         int    `json:"videoCount"`
	FreeLessonCount    int    `json:"freeLessonCount"`
	FreeVideoCount     int    `json:"freeVideoCount"`
	FreeNoteCount      int    `json:"freeNoteCount"`
	FreeQuizCount      int    `json:"freeQuizCount"`
	FreeFlashcardCount int    `json:"freeFlashcardCount"`
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
		COALESCE(short_description, ''), COALESCE(description, ''), status, is_free,
		visibility, batch_group_id, batch_number, created_at`

// courseColumnsC is courseColumns for a query where courses is aliased c.
const courseColumnsC = `c.id, c.exam_id, c.instructor_id, c.title, c.slug,
		COALESCE(c.short_description, ''), COALESCE(c.description, ''), c.status, c.is_free,
		c.visibility, c.batch_group_id, c.batch_number, c.created_at`

func scanCourse(row interface{ Scan(...any) error }, c *Course, extra ...any) error {
	return row.Scan(append([]any{&c.ID, &c.ExamID, &c.InstructorID, &c.Title, &c.Slug,
		&c.ShortDescription, &c.Description, &c.Status, &c.IsFree,
		&c.Visibility, &c.BatchGroupID, &c.BatchNumber, &c.CreatedAt}, extra...)...)
}

// coursePreviewableSQL holds when user $1 may see the course aliased c without being enrolled in it: the
// course is published and public, and the user has no valid enrollment in another batch of it (students
// of IFC don't see IFC 2.0 while their IFC access lasts).
const coursePreviewableSQL = `(c.status = 'published' AND c.visibility = 'public' AND NOT EXISTS (
		SELECT 1 FROM courses other
		JOIN enrollments oe ON oe.course_id = other.id
		WHERE other.batch_group_id = c.batch_group_id AND other.id <> c.id AND other.deleted_at IS NULL
		  AND oe.user_id = $1
		  AND oe.status IN ('active', 'completed')
		  AND (oe.expires_at IS NULL OR oe.expires_at > now())))`

// CreateCourse saves a new course and fills in its generated fields (id, visibility, batch, created_at).
func (r *CourseRepository) CreateCourse(ctx context.Context, course *Course) error {
	query := `INSERT INTO courses
		(exam_id, instructor_id, title, slug, short_description, description, status, is_free)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + courseColumns

	return scanCourse(r.db.QueryRowContext(ctx, query,
		course.ExamID, course.InstructorID, course.Title, course.Slug,
		course.ShortDescription, course.Description, course.Status, course.IsFree,
	), course)
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

// ListEnrolled returns the published courses this user has a valid enrollment in, newest enrollment first,
// each with the tier of that enrollment.
func (r *CourseRepository) ListEnrolled(ctx context.Context, userID string, limit, offset int) ([]Course, error) {
	query := `SELECT ` + courseColumnsC + `, e.tier
		FROM courses c
		JOIN enrollments e ON e.course_id = c.id
		WHERE e.user_id = $1
		  AND e.status IN ('active', 'completed')
		  AND (e.expires_at IS NULL OR e.expires_at > now())
		  AND c.status = 'published'
		  AND c.deleted_at IS NULL
		ORDER BY e.enrolled_at DESC, c.id DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []Course{}
	for rows.Next() {
		var c Course
		if err := scanCourse(rows, &c, &c.Tier); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

// ListCatalog returns the published courses the user may find, newest first: the ones they could preview
// (see coursePreviewableSQL) and the ones they are enrolled in, even when private. Each comes with counts
// of its free content as seen by a user who is not enrolled. In a free course everything counts as free.
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
			           AND l.is_published AND (c.is_free OR q.is_free)) AS free_quizzes,
			       (SELECT COUNT(*) FROM flashcards f JOIN flashcard_decks d ON d.id = f.deck_id AND d.deleted_at IS NULL
			         LEFT JOIN lessons l ON l.id = d.lesson_id AND l.deleted_at IS NULL
			         WHERE f.course_id = c.id AND f.deleted_at IS NULL AND d.status = 'published'
			           AND (d.lesson_id IS NULL OR l.is_published) AND (c.is_free OR f.is_free)) AS free_flashcards
			FROM courses c
			WHERE c.status = 'published' AND c.deleted_at IS NULL
		),
		enrolled AS (
			SELECT e.course_id FROM enrollments e
			WHERE e.user_id = $1
			  AND e.status IN ('active', 'completed')
			  AND (e.expires_at IS NULL OR e.expires_at > now())
		)
		SELECT ` + courseColumnsC + `,
		       u.first_name || ' ' || u.last_name,
		       c.id IN (SELECT course_id FROM enrolled),
		       s.lessons, s.videos, s.free_lessons, s.free_videos, s.free_notes, s.free_quizzes, s.free_flashcards
		FROM courses c
		JOIN stats s ON s.id = c.id
		JOIN users u ON u.id = c.instructor_id
		WHERE (c.id IN (SELECT course_id FROM enrolled) OR ` + coursePreviewableSQL + `)
		  AND (NOT $2 OR (s.free_lessons + s.free_videos + s.free_notes + s.free_quizzes + s.free_flashcards) > 0)
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
		err := scanCourse(rows, &c.Course,
			&c.InstructorName, &c.Enrolled,
			&c.LessonCount, &c.VideoCount, &c.FreeLessonCount, &c.FreeVideoCount, &c.FreeNoteCount, &c.FreeQuizCount, &c.FreeFlashcardCount)
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

// EnrollmentTier returns the tier of the user's enrollment in the course if it is still valid, or ""
// when they have none.
func (r *CourseRepository) EnrollmentTier(ctx context.Context, courseID, userID string) (Tier, error) {
	var tier Tier
	query := `SELECT tier FROM enrollments
		WHERE course_id = $1 AND user_id = $2
		  AND status IN ('active', 'completed')
		  AND (expires_at IS NULL OR expires_at > now())`
	err := r.db.QueryRowContext(ctx, query, courseID, userID).Scan(&tier)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tier, err
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

// SoftDelete marks a course deleted, but only if it belongs to this instructor, and in the same statement
// deletes its videos and queues their folders for removal (see VideoRepository.Delete for resweepAfter).
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) SoftDelete(ctx context.Context, courseID, instructorID string, resweepAfter time.Duration) error {
	var id string
	query := `WITH course AS (
			UPDATE courses SET deleted_at = now()
			WHERE id = $1 AND instructor_id = $2 AND deleted_at IS NULL
			RETURNING id
		), gone AS (
			DELETE FROM videos
			WHERE course_id IN (SELECT id FROM course)
			   OR lesson_id IN (SELECT l.id FROM lessons l JOIN course c ON c.id = l.course_id)
			RETURNING status, original_key
		), ` + queueDeletedVideoFiles("$3") + `
		SELECT id FROM course`
	return r.db.QueryRowContext(ctx, query, courseID, instructorID, resweepAfter.Seconds()).Scan(&id)
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

// Previewable reports whether the user may see the course without being enrolled in it (see coursePreviewableSQL).
func (r *CourseRepository) Previewable(ctx context.Context, courseID, userID string) (bool, error) {
	var ok bool
	query := `SELECT ` + coursePreviewableSQL + ` FROM courses c WHERE c.id = $2`
	err := r.db.QueryRowContext(ctx, query, userID, courseID).Scan(&ok)
	return ok, err
}

// UpdateVisibility makes a course public or private, but only if it belongs to this instructor.
// It returns sql.ErrNoRows when the course does not exist or is not theirs.
func (r *CourseRepository) UpdateVisibility(ctx context.Context, courseID, instructorID, visibility string) (*Course, error) {
	query := `UPDATE courses SET visibility = $1
		WHERE id = $2 AND instructor_id = $3 AND deleted_at IS NULL
		RETURNING ` + courseColumns

	var c Course
	if err := scanCourse(r.db.QueryRowContext(ctx, query, visibility, courseID, instructorID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

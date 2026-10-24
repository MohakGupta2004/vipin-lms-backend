package models

import (
	"context"
	"database/sql"
	"time"
)

// StudentCourse is how far one student got in one course an instructor owns, with their enrollment.
type StudentCourse struct {
	CourseProgress
	EnrollmentStatus string     `json:"enrollmentStatus"`
	Tier             Tier       `json:"tier"`
	EnrolledAt       time.Time  `json:"enrolledAt"`
	ExpiresAt        *time.Time `json:"expiresAt"` // null = never expires
	// FlashcardsTotal counts the cards of published decks; FlashcardsReviewed the ones the student rated at least once.
	FlashcardsTotal    int `json:"flashcardsTotal"`
	FlashcardsReviewed int `json:"flashcardsReviewed"`
	// LastActiveAt is the student's latest video progress, test submission or flashcard rating in the course.
	LastActiveAt *time.Time `json:"lastActiveAt"`
}

// RosterRow is one enrollment of a student in a course the instructor owns, with the student's progress in it.
type RosterRow struct {
	StudentID string
	FirstName string
	LastName  string
	Email     string
	Course    StudentCourse
}

// StudentVideo is a video a student has played in one of the instructor's courses.
type StudentVideo struct {
	VideoID     string         `json:"videoId"`
	Title       string         `json:"title"`
	CourseID    string         `json:"courseId"`
	CourseTitle string         `json:"courseTitle"`
	LessonTitle string         `json:"lessonTitle"`
	Progress    *VideoProgress `json:"progress"`
}

// StudentAttempt is a submitted test attempt by a student in one of the instructor's courses.
type StudentAttempt struct {
	ID          string    `json:"id"`
	QuizID      string    `json:"quizId"`
	QuizTitle   string    `json:"quizTitle"`
	QuizType    string    `json:"quizType"`
	CourseID    string    `json:"courseId"`
	CourseTitle string    `json:"courseTitle"`
	LessonTitle string    `json:"lessonTitle"`
	Score       int       `json:"score"`
	Total       int       `json:"total"`
	Percent     int       `json:"percent"`
	Passed      *bool     `json:"passed"` // nil for practice tests
	SubmittedAt time.Time `json:"submittedAt"`
}

// StudentRepository reads what students did in the courses one instructor owns. Every query is scoped by
// the instructor's ID through the roster CTE, so an instructor can never read a student of another course.
type StudentRepository struct {
	db *sql.DB
}

func NewStudentRepository(db *sql.DB) *StudentRepository {
	return &StudentRepository{db: db}
}

// roster is a CTE named r with the enrollments (not cancelled) in the live courses $1 owns, narrowed to the
// course $2 and the student $3 when they are set.
const roster = `WITH r AS (
		SELECT e.user_id, e.course_id, e.status, e.tier, e.enrolled_at, e.expires_at
		FROM enrollments e
		JOIN courses c ON c.id = e.course_id AND c.deleted_at IS NULL AND c.instructor_id = $1
		JOIN users u ON u.id = e.user_id AND u.deleted_at IS NULL
		WHERE e.status <> 'cancelled'
		  AND ($2::uuid IS NULL OR e.course_id = $2::uuid)
		  AND ($3::uuid IS NULL OR e.user_id = $3::uuid)
	)`

// ListRoster returns one row per enrollment in the instructor's courses (one course if courseID is set, one
// student if studentID is set), with what the student did there. Counts follow what the student can see:
// ready videos and published tests of published lessons. Rows are in student name, then course title order.
func (r *StudentRepository) ListRoster(ctx context.Context, instructorID, courseID, studentID string) ([]RosterRow, error) {
	query := roster + `,
	vids AS (
		SELECT r.user_id, r.course_id,
			COUNT(*) AS total,
			COUNT(vp.completed_at) AS completed,
			COUNT(*) FILTER (WHERE vp.completed_at IS NULL AND vp.position_sec > 0) AS in_progress,
			COALESCE(SUM(CASE WHEN vp.completed_at IS NOT NULL THEN GREATEST(COALESCE(vp.duration_sec, 0), vp.position_sec)
				ELSE vp.position_sec END), 0) AS watched_sec,
			MAX(vp.updated_at) AS last_at
		FROM r
		JOIN videos v ON v.course_id = r.course_id AND v.status = 'ready'
		JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL AND l.is_published
		LEFT JOIN video_progress vp ON vp.video_id = v.id AND vp.user_id = r.user_id
		GROUP BY r.user_id, r.course_id
	),
	per_quiz AS (
		SELECT r.user_id, r.course_id, q.type,
			COUNT(a.id) AS attempts,
			MAX(ROUND(a.score * 100.0 / NULLIF(a.total, 0)))::int AS best_pct,
			COALESCE(bool_or(a.passed), false) AS passed,
			MAX(a.submitted_at) AS last_at
		FROM r
		JOIN quizzes q ON q.course_id = r.course_id AND q.deleted_at IS NULL AND q.status = 'published'
		JOIN lessons l ON l.id = q.lesson_id AND l.deleted_at IS NULL AND l.is_published
		LEFT JOIN quiz_attempts a ON a.quiz_id = q.id AND a.user_id = r.user_id AND a.submitted_at IS NOT NULL
		GROUP BY r.user_id, r.course_id, q.id, q.type
	),
	quiz AS (
		SELECT user_id, course_id,
			COUNT(*) FILTER (WHERE type = 'practice') AS practice_total,
			COUNT(*) FILTER (WHERE type = 'practice' AND attempts > 0) AS practice_done,
			COUNT(*) FILTER (WHERE type = 'mock_test') AS mock_total,
			COUNT(*) FILTER (WHERE type = 'mock_test' AND attempts > 0) AS mock_done,
			COUNT(*) FILTER (WHERE type = 'mock_test' AND passed) AS mock_passed,
			COALESCE(SUM(best_pct) FILTER (WHERE type = 'mock_test'), 0) AS mock_best_sum,
			COALESCE(SUM(attempts), 0) AS attempts,
			MAX(last_at) AS last_at
		FROM per_quiz
		GROUP BY user_id, course_id
	),
	cards AS (
		SELECT r.user_id, r.course_id, COUNT(*) AS total, COUNT(fr.flashcard_id) AS reviewed, MAX(fr.last_reviewed_at) AS last_at
		FROM r
		JOIN flashcard_decks d ON d.course_id = r.course_id AND d.deleted_at IS NULL AND d.status = 'published'
		LEFT JOIN lessons l ON l.id = d.lesson_id
		JOIN flashcards f ON f.deck_id = d.id AND f.deleted_at IS NULL
		LEFT JOIN flashcard_reviews fr ON fr.flashcard_id = f.id AND fr.user_id = r.user_id
		WHERE d.lesson_id IS NULL OR (l.deleted_at IS NULL AND l.is_published)
		GROUP BY r.user_id, r.course_id
	),
	notes_n AS (
		SELECT n.course_id, COUNT(*) AS total
		FROM notes n
		JOIN lessons l ON l.id = n.lesson_id AND l.deleted_at IS NULL AND l.is_published
		WHERE n.deleted_at IS NULL AND n.course_id IN (SELECT course_id FROM r)
		GROUP BY n.course_id
	)
		SELECT u.id, u.first_name, u.last_name, u.email,
			c.id, c.title, r.status, r.tier, r.enrolled_at, r.expires_at,
			COALESCE(v.total, 0), COALESCE(v.completed, 0), COALESCE(v.in_progress, 0), COALESCE(v.watched_sec, 0),
			COALESCE(q.practice_total, 0), COALESCE(q.practice_done, 0),
			COALESCE(q.mock_total, 0), COALESCE(q.mock_done, 0), COALESCE(q.mock_passed, 0),
			COALESCE(q.mock_best_sum, 0), COALESCE(q.attempts, 0),
			COALESCE(n.total, 0), COALESCE(k.total, 0), COALESCE(k.reviewed, 0),
			GREATEST(v.last_at, q.last_at, k.last_at)
		FROM r
		JOIN users u ON u.id = r.user_id
		JOIN courses c ON c.id = r.course_id
		LEFT JOIN vids v ON v.user_id = r.user_id AND v.course_id = r.course_id
		LEFT JOIN quiz q ON q.user_id = r.user_id AND q.course_id = r.course_id
		LEFT JOIN cards k ON k.user_id = r.user_id AND k.course_id = r.course_id
		LEFT JOIN notes_n n ON n.course_id = r.course_id
		ORDER BY u.first_name, u.last_name, u.id, c.title, c.id`
	rows, err := r.db.QueryContext(ctx, query, instructorID, nullUUID(courseID), nullUUID(studentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RosterRow{}
	for rows.Next() {
		var row RosterRow
		c := &row.Course
		var expiresAt, lastAt sql.NullTime
		if err := rows.Scan(&row.StudentID, &row.FirstName, &row.LastName, &row.Email,
			&c.CourseID, &c.CourseTitle, &c.EnrollmentStatus, &c.Tier, &c.EnrolledAt, &expiresAt,
			&c.VideosTotal, &c.VideosCompleted, &c.VideosInProgress, &c.WatchedSec,
			&c.PracticeTotal, &c.PracticeDone,
			&c.MockTotal, &c.MockDone, &c.MockPassed,
			&c.MockBestSum, &c.QuizAttempts,
			&c.NotesTotal, &c.FlashcardsTotal, &c.FlashcardsReviewed,
			&lastAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			t := expiresAt.Time
			c.ExpiresAt = &t
		}
		if lastAt.Valid {
			t := lastAt.Time
			c.LastActiveAt = &t
		}
		c.Finish()
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListVideos returns the videos the student has played in the instructor's courses, most recently watched first.
func (r *StudentRepository) ListVideos(ctx context.Context, instructorID, studentID string) ([]StudentVideo, error) {
	query := roster + `
		SELECT v.id, v.title, c.id, c.title, l.title,
			vp.position_sec, vp.duration_sec, vp.completed_at, vp.updated_at
		FROM r
		JOIN video_progress vp ON vp.user_id = r.user_id
		JOIN videos v ON v.id = vp.video_id AND v.course_id = r.course_id
		JOIN courses c ON c.id = v.course_id
		JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		ORDER BY vp.updated_at DESC, v.id`
	rows, err := r.db.QueryContext(ctx, query, instructorID, nil, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := []StudentVideo{}
	for rows.Next() {
		var v StudentVideo
		var position, duration sql.NullInt32
		var completedAt, updatedAt sql.NullTime
		if err := rows.Scan(&v.VideoID, &v.Title, &v.CourseID, &v.CourseTitle, &v.LessonTitle,
			&position, &duration, &completedAt, &updatedAt); err != nil {
			return nil, err
		}
		v.Progress = scanProgress(position, duration, completedAt, updatedAt)
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// ListAttempts returns up to limit submitted test attempts of the student in the instructor's courses, newest first.
func (r *StudentRepository) ListAttempts(ctx context.Context, instructorID, studentID string, limit int) ([]StudentAttempt, error) {
	query := roster + `
		SELECT a.id, q.id, q.title, q.type, c.id, c.title, COALESCE(l.title, ''),
			COALESCE(a.score, 0), COALESCE(a.total, 0),
			COALESCE(ROUND(a.score * 100.0 / NULLIF(a.total, 0)), 0)::int,
			CASE WHEN q.type = 'mock_test' THEN a.passed END,
			a.submitted_at
		FROM r
		JOIN quiz_attempts a ON a.user_id = r.user_id AND a.submitted_at IS NOT NULL
		JOIN quizzes q ON q.id = a.quiz_id AND q.course_id = r.course_id
		JOIN courses c ON c.id = q.course_id
		LEFT JOIN lessons l ON l.id = q.lesson_id
		ORDER BY a.submitted_at DESC, a.id
		LIMIT $4`
	rows, err := r.db.QueryContext(ctx, query, instructorID, nil, studentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attempts := []StudentAttempt{}
	for rows.Next() {
		var a StudentAttempt
		var passed sql.NullBool
		if err := rows.Scan(&a.ID, &a.QuizID, &a.QuizTitle, &a.QuizType, &a.CourseID, &a.CourseTitle, &a.LessonTitle,
			&a.Score, &a.Total, &a.Percent, &passed, &a.SubmittedAt); err != nil {
			return nil, err
		}
		if passed.Valid {
			v := passed.Bool
			a.Passed = &v
		}
		attempts = append(attempts, a)
	}
	return attempts, rows.Err()
}

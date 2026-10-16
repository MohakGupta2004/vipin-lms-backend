package models

import (
	"context"
	"database/sql"
	"time"
)

// MyVideo is a video in one of the user's courses, with where it sits and how far the user got.
type MyVideo struct {
	Video
	CourseTitle string `json:"courseTitle"`
	LessonTitle string `json:"lessonTitle"`
}

// MyQuiz is a quiz in one of the user's courses, with where it sits and how the user did.
type MyQuiz struct {
	Quiz
	CourseTitle  string `json:"courseTitle"`
	LessonTitle  string `json:"lessonTitle"`
	AttemptCount int    `json:"attemptCount"`
	// BestPercent is the best mock test score in percent; nil for practice sets and untried quizzes.
	BestPercent *int `json:"bestPercent"`
	// Passed is true once any mock test attempt passed; nil for practice sets and untried quizzes.
	Passed        *bool      `json:"passed"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
}

// CourseProgress counts what the user has done in one course.
type CourseProgress struct {
	CourseID         string `json:"courseId,omitempty"`
	CourseTitle      string `json:"courseTitle,omitempty"`
	VideosTotal      int    `json:"videosTotal"`
	VideosCompleted  int    `json:"videosCompleted"`
	VideosInProgress int    `json:"videosInProgress"`
	// WatchedSec approximates time spent watching: the full length of completed videos plus the
	// position reached in the others.
	WatchedSec    int `json:"watchedSec"`
	PracticeTotal int `json:"practiceTotal"`
	PracticeDone  int `json:"practiceDone"`
	MockTotal     int `json:"mockTotal"`
	MockDone      int `json:"mockDone"`
	MockPassed    int `json:"mockPassed"`
	// MockAvgPercent averages the best score of each mock test taken; nil when none was taken.
	MockAvgPercent *int `json:"mockAvgPercent"`
	QuizAttempts   int  `json:"quizAttempts"`
	NotesTotal     int  `json:"notesTotal"`
	// Percent is the share of videos completed and quizzes attempted, 0 to 100.
	Percent int `json:"percent"`

	MockBestSum int `json:"-"` // sum of the best percent of each mock test taken, to average across courses
}

// Add adds another course's counts to p; call Finish after the last one.
func (p *CourseProgress) Add(o CourseProgress) {
	p.VideosTotal += o.VideosTotal
	p.VideosCompleted += o.VideosCompleted
	p.VideosInProgress += o.VideosInProgress
	p.WatchedSec += o.WatchedSec
	p.PracticeTotal += o.PracticeTotal
	p.PracticeDone += o.PracticeDone
	p.MockTotal += o.MockTotal
	p.MockDone += o.MockDone
	p.MockPassed += o.MockPassed
	p.MockBestSum += o.MockBestSum
	p.QuizAttempts += o.QuizAttempts
	p.NotesTotal += o.NotesTotal
}

// Finish fills in the derived fields: the average mock score and the overall percent.
func (p *CourseProgress) Finish() {
	p.MockAvgPercent = nil
	if p.MockDone > 0 {
		avg := (p.MockBestSum + p.MockDone/2) / p.MockDone
		p.MockAvgPercent = &avg
	}
	p.Percent = 0
	if total := p.VideosTotal + p.PracticeTotal + p.MockTotal; total > 0 {
		p.Percent = (p.VideosCompleted + p.PracticeDone + p.MockDone) * 100 / total
	}
}

// LearningRepository reads a user's own learning across all their courses: students' valid enrollments
// in published courses, or the courses an instructor or admin owns.
type LearningRepository struct {
	db *sql.DB
}

func NewLearningRepository(db *sql.DB) *LearningRepository {
	return &LearningRepository{db: db}
}

// myCourses is a CTE named mc with the IDs of the courses $1 learns ($2 = false) or owns ($2 = true),
// and the user's tier in each: the owner's is ultra, a learner's is the tier on their enrollment.
// Owners also see drafts and unpublished lessons, like everywhere else.
const myCourses = `WITH mc AS (
		SELECT c.id, CASE WHEN $2 THEN 'ultra' ELSE e.tier END AS tier
		FROM courses c
		LEFT JOIN enrollments e ON NOT $2 AND e.course_id = c.id AND e.user_id = $1
			AND e.status IN ('active', 'completed')
			AND (e.expires_at IS NULL OR e.expires_at > now())
		WHERE c.deleted_at IS NULL AND (
			($2 AND c.instructor_id = $1)
			OR (NOT $2 AND c.status = 'published' AND e.id IS NOT NULL)
		)
	)`

// withExtra appends more destinations to a row scan, so the shared scanVideo/scanQuiz can read joined columns too.
type withExtra struct {
	row   interface{ Scan(...any) error }
	extra []any
}

func (w withExtra) Scan(dest ...any) error { return w.row.Scan(append(dest, w.extra...)...) }

// ListVideos returns the ready videos of the user's courses (one course if courseID is set), in course and
// lesson order, each with the user's progress.
func (r *LearningRepository) ListVideos(ctx context.Context, userID string, owner bool, courseID string) ([]MyVideo, error) {
	query := myCourses + `
		SELECT ` + videoColumns + `, c.title, l.title,
			vp.position_sec, vp.duration_sec, vp.completed_at, vp.updated_at,
			NOT ` + TierSQL("v.tiers", "mc.tier") + `
		FROM videos v
		JOIN mc ON mc.id = v.course_id
		JOIN courses c ON c.id = v.course_id
		JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		LEFT JOIN video_progress vp ON vp.video_id = v.id AND vp.user_id = $1
		WHERE v.status = 'ready' AND ($2 OR l.is_published)
		  AND ($3::uuid IS NULL OR v.course_id = $3::uuid)
		ORDER BY c.title, c.id, l.position, v.created_at`
	rows, err := r.db.QueryContext(ctx, query, userID, owner, nullUUID(courseID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := []MyVideo{}
	for rows.Next() {
		var v MyVideo
		var position, duration sql.NullInt32
		var completedAt, updatedAt sql.NullTime
		extra := withExtra{rows, []any{&v.CourseTitle, &v.LessonTitle, &position, &duration, &completedAt, &updatedAt, &v.Locked}}
		if err := scanVideo(extra, &v.Video); err != nil {
			return nil, err
		}
		v.Progress = scanProgress(position, duration, completedAt, updatedAt)
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// ContinueWatching returns up to limit videos the user started but has not completed, most recently watched first.
func (r *LearningRepository) ContinueWatching(ctx context.Context, userID string, owner bool, limit int) ([]MyVideo, error) {
	query := myCourses + `
		SELECT ` + videoColumns + `, c.title, l.title,
			vp.position_sec, vp.duration_sec, vp.completed_at, vp.updated_at,
			NOT ` + TierSQL("v.tiers", "mc.tier") + `
		FROM video_progress vp
		JOIN videos v ON v.id = vp.video_id
		JOIN mc ON mc.id = v.course_id
		JOIN courses c ON c.id = v.course_id
		JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		WHERE vp.user_id = $1 AND vp.completed_at IS NULL AND vp.position_sec > 0
		  AND v.status = 'ready' AND ($2 OR l.is_published)
		ORDER BY vp.updated_at DESC
		LIMIT $3`
	rows, err := r.db.QueryContext(ctx, query, userID, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := []MyVideo{}
	for rows.Next() {
		var v MyVideo
		var position, duration sql.NullInt32
		var completedAt, updatedAt sql.NullTime
		extra := withExtra{rows, []any{&v.CourseTitle, &v.LessonTitle, &position, &duration, &completedAt, &updatedAt, &v.Locked}}
		if err := scanVideo(extra, &v.Video); err != nil {
			return nil, err
		}
		v.Progress = scanProgress(position, duration, completedAt, updatedAt)
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// ListQuizzes returns the quizzes of the user's courses (one course if courseID is set, one type if quizType
// is set), in course and lesson order, each with the user's attempt summary. Learners only get published
// quizzes of published lessons.
func (r *LearningRepository) ListQuizzes(ctx context.Context, userID string, owner bool, courseID, quizType string) ([]MyQuiz, error) {
	query := myCourses + `,
	tries AS (
		SELECT a.quiz_id, COUNT(*) AS attempts, MAX(a.submitted_at) AS last_at,
			MAX(ROUND(a.score * 100.0 / NULLIF(a.total, 0)))::int AS best_pct,
			bool_or(a.passed) AS passed
		FROM quiz_attempts a
		WHERE a.user_id = $1 AND a.submitted_at IS NOT NULL
		GROUP BY a.quiz_id
	)
		SELECT ` + quizColumns + `, c.title, l.title,
			COALESCE(t.attempts, 0), t.best_pct, t.passed, t.last_at,
			NOT ` + TierSQL("q.tiers", "mc.tier") + `
		FROM quizzes q
		JOIN mc ON mc.id = q.course_id
		JOIN courses c ON c.id = q.course_id
		JOIN lessons l ON l.id = q.lesson_id AND l.deleted_at IS NULL
		LEFT JOIN tries t ON t.quiz_id = q.id
		WHERE q.deleted_at IS NULL
		  AND ($2 OR (q.status = 'published' AND l.is_published))
		  AND ($3::uuid IS NULL OR q.course_id = $3::uuid)
		  AND ($4 = '' OR q.type = $4)
		ORDER BY c.title, c.id, l.position, q.created_at, q.id`
	rows, err := r.db.QueryContext(ctx, query, userID, owner, nullUUID(courseID), quizType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	quizzes := []MyQuiz{}
	for rows.Next() {
		var q MyQuiz
		var best sql.NullInt32
		var passed sql.NullBool
		var lastAt sql.NullTime
		extra := withExtra{rows, []any{&q.CourseTitle, &q.LessonTitle, &q.AttemptCount, &best, &passed, &lastAt, &q.Locked}}
		if err := scanQuiz(extra, &q.Quiz); err != nil {
			return nil, err
		}
		if best.Valid {
			v := int(best.Int32)
			q.BestPercent = &v
		}
		if passed.Valid {
			v := passed.Bool
			q.Passed = &v
		}
		if lastAt.Valid {
			t := lastAt.Time
			q.LastAttemptAt = &t
		}
		quizzes = append(quizzes, q)
	}
	return quizzes, rows.Err()
}

// ListCourseProgress counts, for each of the user's courses, the videos, quizzes and notes it has and how
// many of them the user has done. Courses are in title order.
func (r *LearningRepository) ListCourseProgress(ctx context.Context, userID string, owner bool) ([]CourseProgress, error) {
	query := myCourses + `,
	vids AS (
		SELECT v.course_id,
			COUNT(*) AS total,
			COUNT(vp.completed_at) AS completed,
			COUNT(*) FILTER (WHERE vp.completed_at IS NULL AND vp.position_sec > 0) AS in_progress,
			COALESCE(SUM(CASE WHEN vp.completed_at IS NOT NULL THEN GREATEST(COALESCE(vp.duration_sec, 0), vp.position_sec)
				ELSE vp.position_sec END), 0) AS watched_sec
		FROM videos v
		JOIN mc ON mc.id = v.course_id
		JOIN lessons l ON l.id = v.lesson_id AND l.deleted_at IS NULL
		LEFT JOIN video_progress vp ON vp.video_id = v.id AND vp.user_id = $1
		WHERE v.status = 'ready' AND ($2 OR l.is_published)
		GROUP BY v.course_id
	),
	per_quiz AS (
		SELECT q.id, q.course_id, q.type,
			COUNT(a.id) AS attempts,
			MAX(ROUND(a.score * 100.0 / NULLIF(a.total, 0)))::int AS best_pct,
			COALESCE(bool_or(a.passed), false) AS passed
		FROM quizzes q
		JOIN mc ON mc.id = q.course_id
		JOIN lessons l ON l.id = q.lesson_id AND l.deleted_at IS NULL
		LEFT JOIN quiz_attempts a ON a.quiz_id = q.id AND a.user_id = $1 AND a.submitted_at IS NOT NULL
		WHERE q.deleted_at IS NULL AND ($2 OR (q.status = 'published' AND l.is_published))
		GROUP BY q.id, q.course_id, q.type
	),
	quiz AS (
		SELECT course_id,
			COUNT(*) FILTER (WHERE type = 'practice') AS practice_total,
			COUNT(*) FILTER (WHERE type = 'practice' AND attempts > 0) AS practice_done,
			COUNT(*) FILTER (WHERE type = 'mock_test') AS mock_total,
			COUNT(*) FILTER (WHERE type = 'mock_test' AND attempts > 0) AS mock_done,
			COUNT(*) FILTER (WHERE type = 'mock_test' AND passed) AS mock_passed,
			COALESCE(SUM(best_pct) FILTER (WHERE type = 'mock_test'), 0) AS mock_best_sum,
			COALESCE(SUM(attempts), 0) AS attempts
		FROM per_quiz
		GROUP BY course_id
	),
	notes_n AS (
		SELECT n.course_id, COUNT(*) AS total
		FROM notes n
		JOIN mc ON mc.id = n.course_id
		JOIN lessons l ON l.id = n.lesson_id AND l.deleted_at IS NULL
		WHERE n.deleted_at IS NULL AND ($2 OR l.is_published)
		GROUP BY n.course_id
	)
		SELECT c.id, c.title,
			COALESCE(v.total, 0), COALESCE(v.completed, 0), COALESCE(v.in_progress, 0), COALESCE(v.watched_sec, 0),
			COALESCE(q.practice_total, 0), COALESCE(q.practice_done, 0),
			COALESCE(q.mock_total, 0), COALESCE(q.mock_done, 0), COALESCE(q.mock_passed, 0),
			COALESCE(q.mock_best_sum, 0), COALESCE(q.attempts, 0),
			COALESCE(n.total, 0)
		FROM mc
		JOIN courses c ON c.id = mc.id
		LEFT JOIN vids v ON v.course_id = c.id
		LEFT JOIN quiz q ON q.course_id = c.id
		LEFT JOIN notes_n n ON n.course_id = c.id
		ORDER BY c.title, c.id`
	rows, err := r.db.QueryContext(ctx, query, userID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []CourseProgress{}
	for rows.Next() {
		var p CourseProgress
		if err := rows.Scan(&p.CourseID, &p.CourseTitle,
			&p.VideosTotal, &p.VideosCompleted, &p.VideosInProgress, &p.WatchedSec,
			&p.PracticeTotal, &p.PracticeDone,
			&p.MockTotal, &p.MockDone, &p.MockPassed,
			&p.MockBestSum, &p.QuizAttempts,
			&p.NotesTotal); err != nil {
			return nil, err
		}
		p.Finish()
		courses = append(courses, p)
	}
	return courses, rows.Err()
}

// nullUUID turns an empty ID into SQL NULL, for optional filters.
func nullUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

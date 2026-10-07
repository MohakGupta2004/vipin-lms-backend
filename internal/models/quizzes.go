package models

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrQuizHasAttempts means the quiz's questions cannot be replaced because students already answered them.
var ErrQuizHasAttempts = errors.New("this quiz already has attempts, so its questions cannot be changed; create a new quiz instead")

// Quiz is a set of single-choice questions on a lesson.
type Quiz struct {
	ID            string     `json:"id"`
	CourseID      string     `json:"courseId"`
	LessonID      string     `json:"lessonId"`
	CreatedBy     string     `json:"createdBy"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	TimeLimitSec  *int       `json:"timeLimitSec"` // nil = untimed
	PassPercent   int        `json:"passPercent"`
	IsFree        bool       `json:"isFree"`
	Status        string     `json:"status"`
	QuestionCount int        `json:"questionCount"`
	CreatedAt     time.Time  `json:"createdAt"`
	Questions     []Question `json:"questions,omitempty"`

	LessonPublished bool `json:"-"` // students may only see quizzes of published lessons
	LessonFree      bool `json:"-"` // quizzes of free lessons are open to users previewing the course
}

// Question is one question of a quiz. Explanation is only sent to the instructor before submission.
type Question struct {
	ID           string           `json:"id"`
	QuestionText string           `json:"questionText"`
	Explanation  string           `json:"explanation,omitempty"`
	Position     int              `json:"position"`
	Options      []QuestionOption `json:"options"`
}

// QuestionOption is one choice of a question. IsCorrect is nil when the answer must stay hidden.
type QuestionOption struct {
	ID         string `json:"id"`
	OptionText string `json:"optionText"`
	IsCorrect  *bool  `json:"isCorrect,omitempty"`
	Position   int    `json:"position"`
}

// QuizAttempt is one graded submission of a quiz by a student.
type QuizAttempt struct {
	ID          string          `json:"id"`
	QuizID      string          `json:"quizId"`
	UserID      string          `json:"userId"`
	StartedAt   time.Time       `json:"startedAt"`
	SubmittedAt time.Time       `json:"submittedAt"`
	Score       int             `json:"score"`
	Total       int             `json:"total"`
	Passed      bool            `json:"passed"`
	Answers     []AttemptAnswer `json:"answers,omitempty"`
}

// AttemptAnswer is the student's answer to one question, with the correct answer revealed.
type AttemptAnswer struct {
	QuestionID       string  `json:"questionId"`
	SelectedOptionID *string `json:"selectedOptionId"` // nil = skipped
	CorrectOptionID  string  `json:"correctOptionId"`
	IsCorrect        bool    `json:"isCorrect"`
	Explanation      string  `json:"explanation"`
}

type QuizRepository struct {
	db *sql.DB
}

func NewQuizRepository(db *sql.DB) *QuizRepository {
	return &QuizRepository{
		db: db,
	}
}

// CreateQuiz saves the quiz with all its questions and options and fills in the generated fields.
// If any insert fails, nothing is saved (transaction rollback).
func (r *QuizRepository) CreateQuiz(ctx context.Context, q *Quiz) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Rollback does nothing if Commit already succeeded.
	defer tx.Rollback()

	query := `INSERT INTO quizzes (course_id, lesson_id, created_by, title, description, time_limit_sec, pass_percent, is_free, status)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
		RETURNING id, created_at`
	err = tx.QueryRowContext(ctx, query, q.CourseID, q.LessonID, q.CreatedBy, q.Title, q.Description,
		q.TimeLimitSec, q.PassPercent, q.IsFree, q.Status).Scan(&q.ID, &q.CreatedAt)
	if err != nil {
		return err
	}

	if err := insertQuestions(ctx, tx, q.ID, q.Questions); err != nil {
		return err
	}
	q.QuestionCount = len(q.Questions)

	return tx.Commit()
}

// insertQuestions saves questions with their options under a quiz and fills in their ids.
func insertQuestions(ctx context.Context, tx *sql.Tx, quizID string, questions []Question) error {
	for i := range questions {
		qu := &questions[i]
		query := `INSERT INTO questions (quiz_id, question_text, explanation, position)
			VALUES ($1, $2, NULLIF($3, ''), $4) RETURNING id`
		err := tx.QueryRowContext(ctx, query, quizID, qu.QuestionText, qu.Explanation, qu.Position).Scan(&qu.ID)
		if err != nil {
			return err
		}
		for j := range qu.Options {
			o := &qu.Options[j]
			query := `INSERT INTO question_options (question_id, option_text, is_correct, position)
				VALUES ($1, $2, $3, $4) RETURNING id`
			err := tx.QueryRowContext(ctx, query, qu.ID, o.OptionText, o.IsCorrect != nil && *o.IsCorrect, o.Position).Scan(&o.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateQuiz saves a quiz's details. When replaceQuestions is set, its questions are swapped for
// q.Questions, which is refused with ErrQuizHasAttempts once anyone has attempted the quiz.
// It returns sql.ErrNoRows if the quiz does not exist.
func (r *QuizRepository) UpdateQuiz(ctx context.Context, q *Quiz, replaceQuestions bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// FOR UPDATE blocks new attempts (their foreign key check needs this row) until we commit,
	// so no attempt can be saved against questions that are about to be deleted.
	var id string
	err = tx.QueryRowContext(ctx, "SELECT id FROM quizzes WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", q.ID).Scan(&id)
	if err != nil {
		return err
	}

	query := `UPDATE quizzes SET title = $1, description = NULLIF($2, ''), time_limit_sec = $3,
			pass_percent = $4, is_free = $5, status = $6
		WHERE id = $7`
	_, err = tx.ExecContext(ctx, query, q.Title, q.Description, q.TimeLimitSec, q.PassPercent, q.IsFree, q.Status, q.ID)
	if err != nil {
		return err
	}

	if replaceQuestions {
		var hasAttempts bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM quiz_attempts WHERE quiz_id = $1)", q.ID).Scan(&hasAttempts)
		if err != nil {
			return err
		}
		if hasAttempts {
			return ErrQuizHasAttempts
		}
		// Options go with their questions (ON DELETE CASCADE).
		if _, err := tx.ExecContext(ctx, "DELETE FROM questions WHERE quiz_id = $1", q.ID); err != nil {
			return err
		}
		if err := insertQuestions(ctx, tx, q.ID, q.Questions); err != nil {
			return err
		}
		q.QuestionCount = len(q.Questions)
	}

	return tx.Commit()
}

// SoftDelete hides a quiz. Attempts are kept. It returns sql.ErrNoRows if the quiz does not exist.
func (r *QuizRepository) SoftDelete(ctx context.Context, quizID string) error {
	var id string
	return r.db.QueryRowContext(ctx, "UPDATE quizzes SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING id", quizID).Scan(&id)
}

const quizSelect = `SELECT q.id, q.course_id, q.lesson_id, q.created_by, q.title, COALESCE(q.description, ''),
		q.time_limit_sec, q.pass_percent, q.is_free, q.status,
		(SELECT COUNT(*) FROM questions qu WHERE qu.quiz_id = q.id), q.created_at, l.is_published, l.is_free
	FROM quizzes q
	JOIN lessons l ON l.id = q.lesson_id AND l.deleted_at IS NULL
	WHERE q.deleted_at IS NULL`

func scanQuiz(row interface{ Scan(...any) error }, q *Quiz) error {
	var timeLimit sql.NullInt32
	err := row.Scan(&q.ID, &q.CourseID, &q.LessonID, &q.CreatedBy, &q.Title, &q.Description,
		&timeLimit, &q.PassPercent, &q.IsFree, &q.Status, &q.QuestionCount, &q.CreatedAt, &q.LessonPublished, &q.LessonFree)
	if err != nil {
		return err
	}
	if timeLimit.Valid {
		v := int(timeLimit.Int32)
		q.TimeLimitSec = &v
	}
	return nil
}

// GetQuiz returns one quiz without its questions, or sql.ErrNoRows.
func (r *QuizRepository) GetQuiz(ctx context.Context, quizID string) (*Quiz, error) {
	var q Quiz
	if err := scanQuiz(r.db.QueryRowContext(ctx, quizSelect+" AND q.id = $1", quizID), &q); err != nil {
		return nil, err
	}
	return &q, nil
}

// UpdateStatus changes a quiz's status. It returns sql.ErrNoRows if the quiz does not exist.
func (r *QuizRepository) UpdateStatus(ctx context.Context, quizID, status string) error {
	var id string
	return r.db.QueryRowContext(ctx, "UPDATE quizzes SET status = $1 WHERE id = $2 AND deleted_at IS NULL RETURNING id", status, quizID).Scan(&id)
}

// ListByLesson returns a lesson's quizzes, oldest first. Drafts are left out unless includeDrafts is set.
func (r *QuizRepository) ListByLesson(ctx context.Context, lessonID string, includeDrafts bool) ([]Quiz, error) {
	query := quizSelect + `
		AND q.lesson_id = $1 AND ($2 OR q.status = 'published')
		ORDER BY q.created_at, q.id`

	rows, err := r.db.QueryContext(ctx, query, lessonID, includeDrafts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	quizzes := []Quiz{}
	for rows.Next() {
		var q Quiz
		if err := scanQuiz(rows, &q); err != nil {
			return nil, err
		}
		quizzes = append(quizzes, q)
	}
	return quizzes, rows.Err()
}

// ListQuestions returns a quiz's questions in order, each with its options in order.
// Correct answers and explanations are always filled in; the caller hides them if needed.
func (r *QuizRepository) ListQuestions(ctx context.Context, quizID string) ([]Question, error) {
	query := `SELECT qu.id, qu.question_text, COALESCE(qu.explanation, ''), qu.position,
		       o.id, o.option_text, o.is_correct, o.position
		FROM questions qu
		JOIN question_options o ON o.question_id = qu.id
		WHERE qu.quiz_id = $1
		ORDER BY qu.position, qu.id, o.position, o.id`

	rows, err := r.db.QueryContext(ctx, query, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := []Question{}
	for rows.Next() {
		var qu Question
		var o QuestionOption
		var isCorrect bool
		err := rows.Scan(&qu.ID, &qu.QuestionText, &qu.Explanation, &qu.Position,
			&o.ID, &o.OptionText, &isCorrect, &o.Position)
		if err != nil {
			return nil, err
		}
		o.IsCorrect = &isCorrect
		if n := len(questions); n == 0 || questions[n-1].ID != qu.ID {
			questions = append(questions, qu)
		}
		last := &questions[len(questions)-1]
		last.Options = append(last.Options, o)
	}
	return questions, rows.Err()
}

// CreateAttempt saves a graded attempt with all its answers and fills in the generated fields.
func (r *QuizRepository) CreateAttempt(ctx context.Context, a *QuizAttempt) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `INSERT INTO quiz_attempts (user_id, quiz_id, submitted_at, score, total, passed)
		VALUES ($1, $2, now(), $3, $4, $5)
		RETURNING id, started_at, submitted_at`
	err = tx.QueryRowContext(ctx, query, a.UserID, a.QuizID, a.Score, a.Total, a.Passed).
		Scan(&a.ID, &a.StartedAt, &a.SubmittedAt)
	if err != nil {
		return err
	}

	for _, ans := range a.Answers {
		_, err := tx.ExecContext(ctx, `INSERT INTO attempt_answers (attempt_id, question_id, selected_option_id, is_correct)
			VALUES ($1, $2, $3, $4)`, a.ID, ans.QuestionID, ans.SelectedOptionID, ans.IsCorrect)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListAttempts returns a user's submitted attempts at a quiz, newest first, each with its graded
// answers in question order, the correct option and the explanation.
func (r *QuizRepository) ListAttempts(ctx context.Context, quizID, userID string) ([]QuizAttempt, error) {
	query := `SELECT id, quiz_id, user_id, started_at, submitted_at, score, total, passed
		FROM quiz_attempts
		WHERE quiz_id = $1 AND user_id = $2 AND submitted_at IS NOT NULL
		ORDER BY submitted_at DESC, id DESC`

	rows, err := r.db.QueryContext(ctx, query, quizID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attempts := []QuizAttempt{}
	for rows.Next() {
		var a QuizAttempt
		err := rows.Scan(&a.ID, &a.QuizID, &a.UserID, &a.StartedAt, &a.SubmittedAt, &a.Score, &a.Total, &a.Passed)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(attempts) == 0 {
		return attempts, nil
	}

	answers, err := r.listAttemptAnswers(ctx, quizID, userID)
	if err != nil {
		return nil, err
	}
	for i := range attempts {
		attempts[i].Answers = answers[attempts[i].ID]
	}
	return attempts, nil
}

// listAttemptAnswers returns the answers of all a user's attempts at a quiz, keyed by attempt id.
func (r *QuizRepository) listAttemptAnswers(ctx context.Context, quizID, userID string) (map[string][]AttemptAnswer, error) {
	query := `SELECT aa.attempt_id, aa.question_id, aa.selected_option_id,
		       COALESCE(correct.id::text, ''), aa.is_correct, COALESCE(qu.explanation, '')
		FROM attempt_answers aa
		JOIN quiz_attempts qa ON qa.id = aa.attempt_id
		JOIN questions qu ON qu.id = aa.question_id
		LEFT JOIN LATERAL (
		    SELECT o.id FROM question_options o
		    WHERE o.question_id = qu.id AND o.is_correct
		    ORDER BY o.position LIMIT 1
		) correct ON true
		WHERE qa.quiz_id = $1 AND qa.user_id = $2 AND qa.submitted_at IS NOT NULL
		ORDER BY aa.attempt_id, qu.position, qu.id`

	rows, err := r.db.QueryContext(ctx, query, quizID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	answers := map[string][]AttemptAnswer{}
	for rows.Next() {
		var attemptID string
		var selected sql.NullString
		var a AttemptAnswer
		err := rows.Scan(&attemptID, &a.QuestionID, &selected, &a.CorrectOptionID, &a.IsCorrect, &a.Explanation)
		if err != nil {
			return nil, err
		}
		if selected.Valid {
			a.SelectedOptionID = &selected.String
		}
		answers[attemptID] = append(answers[attemptID], a)
	}
	return answers, rows.Err()
}

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// DiagnosticTest is one diagnostic a student can take, with whether this user already took it.
// It never carries a score: only admins can see results.
type DiagnosticTest struct {
	ID              string     `json:"id"`
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	ExamCode        string     `json:"examCode"`
	ExamName        string     `json:"examName"`
	QuestionCount   int        `json:"questionCount"`
	DurationMinutes int        `json:"durationMinutes"`
	Taken           bool       `json:"taken"`
	TakenAt         *time.Time `json:"takenAt"`
}

// DiagnosticQuestionKey is a question with its chapter and correct answer. It is used to score
// attempts and is shown to admins; students only ever get a DiagnosticQuestion.
type DiagnosticQuestionKey struct {
	ID       int      `json:"id"`
	Element  int      `json:"element"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   int      `json:"answer"`
}

// DiagnosticQuestion is what a student taking the test sees: no chapter, no answer.
type DiagnosticQuestion struct {
	ID       int      `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// DiagnosticElementResult is one of the nine elements in a diagnostic result.
type DiagnosticElementResult struct {
	Element   int `json:"element"`
	Correct   int `json:"correct"`
	Total     int `json:"total"`
	Points    int `json:"points"`
	MaxPoints int `json:"maxPoints"`
}

// DiagnosticAnswer is one question the user answered. Picked is -1 when skipped.
type DiagnosticAnswer struct {
	QuestionID int    `json:"questionId"`
	Picked     int    `json:"picked"`
	Confidence string `json:"confidence"`
}

// DiagnosticResult is returned right after a user saves a result.
// It has no score on purpose: only admins can see results.
type DiagnosticResult struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

// AdminDiagnosticResult is a row in the admin list.
type AdminDiagnosticResult struct {
	ID        string                    `json:"id"`
	UserID    string                    `json:"userId"`
	FirstName string                    `json:"firstName"`
	LastName  string                    `json:"lastName"`
	Email     string                    `json:"email"`
	TestSlug  string                    `json:"testSlug"`
	TestTitle string                    `json:"testTitle"`
	Correct   int                       `json:"correct"`
	Total     int                       `json:"total"`
	Elements  []DiagnosticElementResult `json:"elements"`
	CreatedAt time.Time                 `json:"createdAt"`
}

// AdminDiagnosticResultDetail is the admin list row plus every answer and the test's questions with their answers.
type AdminDiagnosticResultDetail struct {
	AdminDiagnosticResult
	Answers   []DiagnosticAnswer      `json:"answers"`
	Questions []DiagnosticQuestionKey `json:"questions"`
	testID    string
}

type AdminDiagnosticResultsPage struct {
	Total int                     `json:"total"`
	Items []AdminDiagnosticResult `json:"items"`
}

type DiagnosticRepository struct {
	db *sql.DB
}

func NewDiagnosticRepository(db *sql.DB) *DiagnosticRepository {
	return &DiagnosticRepository{db: db}
}

const diagnosticTestColumns = `t.id, t.slug, t.title, t.description,
		COALESCE(e.code, ''), COALESCE(e.name, ''),
		(SELECT COUNT(*) FROM diagnostic_questions q WHERE q.test_id = t.id),
		t.duration_minutes, dr.created_at`

const diagnosticTestFrom = `FROM diagnostic_tests t
		LEFT JOIN exams e ON e.id = t.exam_id
		LEFT JOIN diagnostic_results dr ON dr.test_id = t.id AND dr.user_id = $1`

func scanDiagnosticTest(row interface{ Scan(...any) error }) (*DiagnosticTest, error) {
	var test DiagnosticTest
	err := row.Scan(
		&test.ID,
		&test.Slug,
		&test.Title,
		&test.Description,
		&test.ExamCode,
		&test.ExamName,
		&test.QuestionCount,
		&test.DurationMinutes,
		&test.TakenAt,
	)
	if err != nil {
		return nil, err
	}
	test.Taken = test.TakenAt != nil
	return &test, nil
}

// ListActiveTests returns every active test in display order, each with whether userID already took it.
func (r *DiagnosticRepository) ListActiveTests(ctx context.Context, userID string) ([]DiagnosticTest, error) {
	query := `SELECT ` + diagnosticTestColumns + ` ` + diagnosticTestFrom + `
		WHERE t.is_active
		ORDER BY t.sort_order, t.title`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tests := []DiagnosticTest{}
	for rows.Next() {
		test, err := scanDiagnosticTest(rows)
		if err != nil {
			return nil, err
		}
		tests = append(tests, *test)
	}
	return tests, rows.Err()
}

// GetActiveTest returns one active test by slug, with whether userID already took it.
// It returns sql.ErrNoRows when there is no such active test.
func (r *DiagnosticRepository) GetActiveTest(ctx context.Context, userID, slug string) (*DiagnosticTest, error) {
	query := `SELECT ` + diagnosticTestColumns + ` ` + diagnosticTestFrom + `
		WHERE t.is_active AND t.slug = $2`
	return scanDiagnosticTest(r.db.QueryRowContext(ctx, query, userID, slug))
}

// ListQuestions returns every question of a test with its answer, in order.
func (r *DiagnosticRepository) ListQuestions(ctx context.Context, testID string) ([]DiagnosticQuestionKey, error) {
	query := `SELECT number, element, question, options::text, answer
		FROM diagnostic_questions
		WHERE test_id = $1
		ORDER BY number`
	rows, err := r.db.QueryContext(ctx, query, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := []DiagnosticQuestionKey{}
	for rows.Next() {
		var q DiagnosticQuestionKey
		var optionsJSON string
		if err := rows.Scan(&q.ID, &q.Element, &q.Question, &optionsJSON, &q.Answer); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(optionsJSON), &q.Options); err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}

// Create saves one diagnostic result for a test and returns its id and creation time.
// It returns sql.ErrNoRows when this user already has a result for that test.
func (r *DiagnosticRepository) Create(ctx context.Context, userID, testID string, correct, total int, elements []DiagnosticElementResult, answers []DiagnosticAnswer) (*DiagnosticResult, error) {
	elementsJSON, err := json.Marshal(elements)
	if err != nil {
		return nil, err
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return nil, err
	}

	result := &DiagnosticResult{}
	query := `INSERT INTO diagnostic_results (user_id, test_id, correct, total, elements, answers)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb)
		ON CONFLICT (user_id, test_id) DO NOTHING
		RETURNING id, created_at`
	err = r.db.QueryRowContext(ctx, query, userID, testID, correct, total, string(elementsJSON), string(answersJSON)).
		Scan(&result.ID, &result.CreatedAt)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// List returns one page of results, newest first, with the user's name and email.
func (r *DiagnosticRepository) List(ctx context.Context, q string, limit, offset int) (*AdminDiagnosticResultsPage, error) {
	qLike := "%" + q + "%"

	queryTotal := `SELECT COUNT(*)
		FROM diagnostic_results dr
		JOIN users u ON u.id = dr.user_id
		WHERE ($1::text = '' OR u.first_name ILIKE $2 OR u.last_name ILIKE $2 OR u.email ILIKE $2)`
	page := &AdminDiagnosticResultsPage{}
	err := r.db.QueryRowContext(ctx, queryTotal, q, qLike).Scan(&page.Total)
	if err != nil {
		return nil, err
	}

	queryItems := `SELECT dr.id, dr.user_id, u.first_name, u.last_name, u.email,
		t.slug, t.title, dr.correct, dr.total, dr.elements::text, dr.created_at
		FROM diagnostic_results dr
		JOIN users u ON u.id = dr.user_id
		JOIN diagnostic_tests t ON t.id = dr.test_id
		WHERE ($1::text = '' OR u.first_name ILIKE $2 OR u.last_name ILIKE $2 OR u.email ILIKE $2)
		ORDER BY dr.created_at DESC, dr.id DESC
		LIMIT $3 OFFSET $4`
	rows, err := r.db.QueryContext(ctx, queryItems, q, qLike, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page.Items = []AdminDiagnosticResult{}
	for rows.Next() {
		var item AdminDiagnosticResult
		var elementsJSON string
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.FirstName,
			&item.LastName,
			&item.Email,
			&item.TestSlug,
			&item.TestTitle,
			&item.Correct,
			&item.Total,
			&elementsJSON,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal([]byte(elementsJSON), &item.Elements)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return page, nil
}

// Get returns one result with the user and every answer. It returns sql.ErrNoRows when missing.
func (r *DiagnosticRepository) Get(ctx context.Context, id string) (*AdminDiagnosticResultDetail, error) {
	query := `SELECT dr.id, dr.user_id, u.first_name, u.last_name, u.email,
		t.id, t.slug, t.title, dr.correct, dr.total, dr.elements::text, dr.answers::text, dr.created_at
		FROM diagnostic_results dr
		JOIN users u ON u.id = dr.user_id
		JOIN diagnostic_tests t ON t.id = dr.test_id
		WHERE dr.id = $1`
	var detail AdminDiagnosticResultDetail
	var elementsJSON string
	var answersJSON string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&detail.ID,
		&detail.UserID,
		&detail.FirstName,
		&detail.LastName,
		&detail.Email,
		&detail.testID,
		&detail.TestSlug,
		&detail.TestTitle,
		&detail.Correct,
		&detail.Total,
		&elementsJSON,
		&answersJSON,
		&detail.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(elementsJSON), &detail.Elements)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(answersJSON), &detail.Answers)
	if err != nil {
		return nil, err
	}
	detail.Questions, err = r.ListQuestions(ctx, detail.testID)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

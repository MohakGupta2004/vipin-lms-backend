package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

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
	Correct   int                       `json:"correct"`
	Total     int                       `json:"total"`
	Elements  []DiagnosticElementResult `json:"elements"`
	CreatedAt time.Time                 `json:"createdAt"`
}

// AdminDiagnosticResultDetail is the admin list row plus every answer.
type AdminDiagnosticResultDetail struct {
	AdminDiagnosticResult
	Answers []DiagnosticAnswer `json:"answers"`
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

// Create saves one diagnostic result and returns its id and creation time.
// It returns sql.ErrNoRows when this user already has a result.
func (r *DiagnosticRepository) Create(ctx context.Context, userID string, correct, total int, elements []DiagnosticElementResult, answers []DiagnosticAnswer) (*DiagnosticResult, error) {
	elementsJSON, err := json.Marshal(elements)
	if err != nil {
		return nil, err
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return nil, err
	}

	result := &DiagnosticResult{}
	query := `INSERT INTO diagnostic_results (user_id, correct, total, elements, answers)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING id, created_at`
	err = r.db.QueryRowContext(ctx, query, userID, correct, total, string(elementsJSON), string(answersJSON)).
		Scan(&result.ID, &result.CreatedAt)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// HasTaken tells if the user already has a diagnostic result.
func (r *DiagnosticRepository) HasTaken(ctx context.Context, userID string) (bool, error) {
	var taken bool
	query := `SELECT EXISTS (SELECT 1 FROM diagnostic_results WHERE user_id = $1)`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&taken)
	return taken, err
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
		dr.correct, dr.total, dr.elements::text, dr.created_at
		FROM diagnostic_results dr
		JOIN users u ON u.id = dr.user_id
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
		dr.correct, dr.total, dr.elements::text, dr.answers::text, dr.created_at
		FROM diagnostic_results dr
		JOIN users u ON u.id = dr.user_id
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
	return &detail, nil
}

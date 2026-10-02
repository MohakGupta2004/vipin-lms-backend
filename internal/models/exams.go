package models

import (
	"context"
	"database/sql"
)

// Exam is a certification that courses prepare students for.
type Exam struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ExamRepository struct {
	db *sql.DB
}

func NewExamRepository(db *sql.DB) *ExamRepository {
	return &ExamRepository{
		db: db,
	}
}

// ListActiveExams returns all active exams, sorted by code.
func (r *ExamRepository) ListActiveExams(ctx context.Context) ([]Exam, error) {
	query := `SELECT id, code, name, COALESCE(description, '')
		FROM exams
		WHERE is_active
		ORDER BY code`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	exams := []Exam{}
	for rows.Next() {
		var e Exam
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.Description); err != nil {
			return nil, err
		}
		exams = append(exams, e)
	}
	return exams, rows.Err()
}

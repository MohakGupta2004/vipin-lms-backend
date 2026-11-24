package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

var ErrDiagnosticResultNotFound = errors.New("diagnostic result not found")

const (
	defaultDiagnosticLimit = 50
	maxDiagnosticLimit     = 200
	diagnosticElementCount = 9
	maxDiagnosticAnswers   = 100
)

var diagnosticConfidences = []string{"high", "medium", "low", "none"}

type DiagnosticService struct {
	repo *models.DiagnosticRepository
}

func NewDiagnosticService(repo *models.DiagnosticRepository) *DiagnosticService {
	return &DiagnosticService{repo: repo}
}

type SaveDiagnosticResultInput struct {
	Correct  int
	Total    int
	Elements []models.DiagnosticElementResult
	Answers  []models.DiagnosticAnswer
}

// SaveResult checks one diagnostic submission and stores it for the logged-in user.
func (s *DiagnosticService) SaveResult(ctx context.Context, user *models.User, input SaveDiagnosticResultInput) (*models.DiagnosticResult, error) {
	if err := validateDiagnosticResult(input); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, user.ID, input.Correct, input.Total, input.Elements, input.Answers)
}

type DiagnosticResultsFilter struct {
	Q      string
	Limit  int
	Offset int
}

func (s *DiagnosticService) ListResults(ctx context.Context, user *models.User, filter DiagnosticResultsFilter) (*models.AdminDiagnosticResultsPage, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	filter.Q = strings.TrimSpace(filter.Q)
	if filter.Limit == 0 {
		filter.Limit = defaultDiagnosticLimit
	}
	if filter.Limit < 1 || filter.Limit > maxDiagnosticLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxDiagnosticLimit)
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must be 0 or more", ErrInvalidInput)
	}
	return s.repo.List(ctx, filter.Q, filter.Limit, filter.Offset)
}

func (s *DiagnosticService) GetResult(ctx context.Context, user *models.User, id string) (*models.AdminDiagnosticResultDetail, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(id) {
		return nil, ErrDiagnosticResultNotFound
	}
	result, err := s.repo.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDiagnosticResultNotFound
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validateDiagnosticResult(input SaveDiagnosticResultInput) error {
	if input.Total < 1 || input.Total > 100 {
		return fmt.Errorf("%w: total must be between 1 and 100", ErrInvalidInput)
	}
	if input.Correct < 0 || input.Correct > input.Total {
		return fmt.Errorf("%w: correct must be between 0 and total", ErrInvalidInput)
	}

	if len(input.Elements) != diagnosticElementCount {
		return fmt.Errorf("%w: exactly %d elements are required", ErrInvalidInput, diagnosticElementCount)
	}
	seen := map[int]bool{}
	for _, element := range input.Elements {
		if element.Element < 1 || element.Element > diagnosticElementCount {
			return fmt.Errorf("%w: element must be between 1 and %d", ErrInvalidInput, diagnosticElementCount)
		}
		if seen[element.Element] {
			return fmt.Errorf("%w: element %d is repeated", ErrInvalidInput, element.Element)
		}
		seen[element.Element] = true
		if element.Correct < 0 || element.Correct > element.Total {
			return fmt.Errorf("%w: element correct must be between 0 and its total", ErrInvalidInput)
		}
		if element.MaxPoints < 0 {
			return fmt.Errorf("%w: element maxPoints must be 0 or more", ErrInvalidInput)
		}
	}

	if len(input.Answers) > maxDiagnosticAnswers {
		return fmt.Errorf("%w: at most %d answers are allowed", ErrInvalidInput, maxDiagnosticAnswers)
	}
	for _, answer := range input.Answers {
		if answer.Picked < -1 || answer.Picked > 3 {
			return fmt.Errorf("%w: picked must be between -1 and 3", ErrInvalidInput)
		}
		if !isValidDiagnosticConfidence(answer.Confidence) {
			return fmt.Errorf("%w: confidence must be one of high, medium, low or none", ErrInvalidInput)
		}
	}

	return nil
}

func isValidDiagnosticConfidence(confidence string) bool {
	for _, allowed := range diagnosticConfidences {
		if confidence == allowed {
			return true
		}
	}
	return false
}

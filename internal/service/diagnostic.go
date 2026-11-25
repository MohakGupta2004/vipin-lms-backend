package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

var ErrDiagnosticResultNotFound = errors.New("diagnostic result not found")
var ErrDiagnosticTestNotFound = errors.New("diagnostic test not found")
var ErrDiagnosticAlreadyTaken = errors.New("the diagnostic test was already taken with this email")

const (
	defaultDiagnosticLimit = 50
	maxDiagnosticLimit     = 200
	maxDiagnosticAnswers   = 100
	maxDiagnosticOptions   = 10
	// DefaultDiagnosticTestSlug is the test used when a request names none,
	// so clients from before tests were listed keep working.
	DefaultDiagnosticTestSlug = "cire-advanced"
)

var diagnosticConfidences = []string{"high", "medium", "low", "none"}

var diagnosticSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type DiagnosticService struct {
	repo *models.DiagnosticRepository
}

func NewDiagnosticService(repo *models.DiagnosticRepository) *DiagnosticService {
	return &DiagnosticService{repo: repo}
}

type SaveDiagnosticResultInput struct {
	// TestSlug picks the test. Empty means DefaultDiagnosticTestSlug.
	TestSlug string
	Answers  []models.DiagnosticAnswer
}

// ListTests returns every active diagnostic test, each with whether the logged-in user already took it.
func (s *DiagnosticService) ListTests(ctx context.Context, user *models.User) ([]models.DiagnosticTest, error) {
	return s.repo.ListActiveTests(ctx, user.ID)
}

// GetTest returns one active diagnostic test by slug, with whether the logged-in user already took it.
// An empty slug means DefaultDiagnosticTestSlug.
func (s *DiagnosticService) GetTest(ctx context.Context, user *models.User, slug string) (*models.DiagnosticTest, error) {
	if slug == "" {
		slug = DefaultDiagnosticTestSlug
	}
	if !diagnosticSlugPattern.MatchString(slug) {
		return nil, ErrDiagnosticTestNotFound
	}
	test, err := s.repo.GetActiveTest(ctx, user.ID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDiagnosticTestNotFound
	}
	if err != nil {
		return nil, err
	}
	return test, nil
}

// GetQuestions returns the questions of a test for the logged-in user to take, without chapters or answers.
// Once the user has taken the test there is nothing left to answer, so it returns ErrDiagnosticAlreadyTaken.
func (s *DiagnosticService) GetQuestions(ctx context.Context, user *models.User, slug string) ([]models.DiagnosticQuestion, error) {
	test, err := s.GetTest(ctx, user, slug)
	if err != nil {
		return nil, err
	}
	if test.Taken {
		return nil, ErrDiagnosticAlreadyTaken
	}
	keys, err := s.repo.ListQuestions(ctx, test.ID)
	if err != nil {
		return nil, err
	}
	questions := make([]models.DiagnosticQuestion, 0, len(keys))
	for _, key := range keys {
		questions = append(questions, models.DiagnosticQuestion{
			ID:       key.ID,
			Question: key.Question,
			Options:  key.Options,
		})
	}
	return questions, nil
}

// SaveResult scores one diagnostic attempt on the server and stores it for the logged-in user.
// The answer key never leaves the server and the score is never returned: only admins can see results.
// Each user (one email) can take each diagnostic test only once.
func (s *DiagnosticService) SaveResult(ctx context.Context, user *models.User, input SaveDiagnosticResultInput) (*models.DiagnosticResult, error) {
	if err := validateDiagnosticAnswers(input.Answers); err != nil {
		return nil, err
	}
	test, err := s.GetTest(ctx, user, input.TestSlug)
	if err != nil {
		return nil, err
	}
	if test.Taken {
		return nil, ErrDiagnosticAlreadyTaken
	}
	questions, err := s.repo.ListQuestions(ctx, test.ID)
	if err != nil {
		return nil, err
	}
	score, err := scoreDiagnostic(questions, input.Answers)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.Create(ctx, user.ID, test.ID, score.Correct, score.Total, score.Elements, score.Answers)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDiagnosticAlreadyTaken
	}
	return result, err
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

// validateDiagnosticAnswers checks what can be checked without the test's questions.
func validateDiagnosticAnswers(answers []models.DiagnosticAnswer) error {
	if len(answers) > maxDiagnosticAnswers {
		return fmt.Errorf("%w: at most %d answers are allowed", ErrInvalidInput, maxDiagnosticAnswers)
	}
	seen := map[int]bool{}
	for _, answer := range answers {
		if seen[answer.QuestionID] {
			return fmt.Errorf("%w: question %d is answered twice", ErrInvalidInput, answer.QuestionID)
		}
		seen[answer.QuestionID] = true
		if answer.Picked < -1 || answer.Picked >= maxDiagnosticOptions {
			return fmt.Errorf("%w: picked must be between -1 and %d", ErrInvalidInput, maxDiagnosticOptions-1)
		}
		if !isValidDiagnosticConfidence(answer.Confidence) {
			return fmt.Errorf("%w: confidence must be one of high, medium, low or none", ErrInvalidInput)
		}
	}
	return nil
}

type diagnosticScore struct {
	Correct  int
	Total    int
	Elements []models.DiagnosticElementResult
	// Answers has one entry per question, in question order; questions left out count as skipped.
	Answers []models.DiagnosticAnswer
}

// scoreDiagnostic marks every question and rolls the points up per element (chapter).
// A question scores -1 when wrong, 0 when skipped, +1 when right and +2 when right with high confidence.
func scoreDiagnostic(questions []models.DiagnosticQuestionKey, answers []models.DiagnosticAnswer) (*diagnosticScore, error) {
	byQuestion := map[int]models.DiagnosticAnswer{}
	for _, answer := range answers {
		byQuestion[answer.QuestionID] = answer
	}

	elementCount := 0
	known := map[int]models.DiagnosticQuestionKey{}
	for _, question := range questions {
		known[question.ID] = question
		elementCount = max(elementCount, question.Element)
	}
	for _, answer := range answers {
		question, ok := known[answer.QuestionID]
		if !ok {
			return nil, fmt.Errorf("%w: question %d is not in this test", ErrInvalidInput, answer.QuestionID)
		}
		if answer.Picked >= len(question.Options) {
			return nil, fmt.Errorf("%w: question %d has only %d options", ErrInvalidInput, answer.QuestionID, len(question.Options))
		}
	}

	score := &diagnosticScore{
		Total:    len(questions),
		Elements: make([]models.DiagnosticElementResult, elementCount),
		Answers:  make([]models.DiagnosticAnswer, 0, len(questions)),
	}
	for i := range score.Elements {
		score.Elements[i].Element = i + 1
	}

	for _, question := range questions {
		answer, ok := byQuestion[question.ID]
		if !ok || answer.Picked < 0 {
			answer = models.DiagnosticAnswer{QuestionID: question.ID, Picked: -1, Confidence: "none"}
		}
		score.Answers = append(score.Answers, answer)

		element := &score.Elements[question.Element-1]
		element.Total++
		element.MaxPoints += 2
		switch {
		case answer.Picked < 0:
		case answer.Picked != question.Answer:
			element.Points--
		default:
			element.Correct++
			score.Correct++
			if answer.Confidence == "high" {
				element.Points += 2
			} else {
				element.Points++
			}
		}
	}
	return score, nil
}

func isValidDiagnosticConfidence(confidence string) bool {
	for _, allowed := range diagnosticConfidences {
		if confidence == allowed {
			return true
		}
	}
	return false
}

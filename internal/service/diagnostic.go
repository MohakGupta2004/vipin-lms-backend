package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/email"
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
	maxDiagnosticNoteRunes = 1000
	maxEmailAddressBytes   = 254
	// DefaultDiagnosticTestSlug is the test used when a request names none,
	// so clients from before tests were listed keep working.
	DefaultDiagnosticTestSlug = "cire-advanced"
)

var diagnosticConfidences = []string{"high", "medium", "low", "none"}

var diagnosticSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// diagnosticElementNames are the chapter names of each test, by slug, in element order.
// They match DIAGNOSTIC_ELEMENTS in the frontend. A test missing here shows "Element N".
var diagnosticElementNames = map[string][]string{
	DefaultDiagnosticTestSlug: {
		"Regulatory framework",
		"Prospective client relationships",
		"Scope of client relationships",
		"Complaint handling",
		"Market and company analysis",
		"Market integrity and trading",
		"Securities and managed products",
		"Derivatives",
		"Ethics and conflicts",
	},
}

type DiagnosticService struct {
	repo   *models.DiagnosticRepository
	mailer EmailEnqueuer
}

func NewDiagnosticService(repo *models.DiagnosticRepository, mailer EmailEnqueuer) *DiagnosticService {
	return &DiagnosticService{repo: repo, mailer: mailer}
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
	Q string
	// TestSlug keeps only the results of one test. Empty means every test.
	TestSlug string
	Limit    int
	Offset   int
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
	filter.TestSlug = strings.TrimSpace(filter.TestSlug)
	if filter.TestSlug != "" && !diagnosticSlugPattern.MatchString(filter.TestSlug) {
		return nil, fmt.Errorf("%w: test must be a diagnostic test slug, e.g. %s", ErrInvalidInput, DefaultDiagnosticTestSlug)
	}
	return s.repo.List(ctx, filter.Q, filter.TestSlug, filter.Limit, filter.Offset)
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

type EmailDiagnosticResultInput struct {
	// To is the recipient. Empty means the email of the person who took the test.
	To string
	// Note is an optional personal message shown above the results.
	Note string
}

// EmailResult queues an email with one result's score and chapter breakdown (no individual answers)
// and returns the address it goes to. Admin only. Delivery is asynchronous: a Resend failure is logged, not returned.
func (s *DiagnosticService) EmailResult(ctx context.Context, user *models.User, id string, input EmailDiagnosticResultInput) (string, error) {
	if user.Role != models.RoleAdmin {
		return "", ErrForbidden
	}
	to, note, err := validateDiagnosticEmailInput(input)
	if err != nil {
		return "", err
	}
	result, err := s.GetResult(ctx, user, id)
	if err != nil {
		return "", err
	}
	if to == "" {
		to = result.Email
	}
	msg, err := email.RenderDiagnosticResult(to, diagnosticEmailData(result, note))
	if err != nil {
		return "", err
	}
	if err := s.mailer.Enqueue(msg); err != nil {
		return "", err
	}
	return to, nil
}

// validateDiagnosticEmailInput trims the input and checks the recipient is one bare address.
func validateDiagnosticEmailInput(input EmailDiagnosticResultInput) (to, note string, err error) {
	to = strings.TrimSpace(input.To)
	if to != "" {
		parsed, parseErr := mail.ParseAddress(to)
		if parseErr != nil || parsed.Address != to || len(to) > maxEmailAddressBytes {
			return "", "", fmt.Errorf("%w: to must be one email address", ErrInvalidInput)
		}
	}
	note = strings.TrimSpace(input.Note)
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > maxDiagnosticNoteRunes {
		return "", "", fmt.Errorf("%w: note must be at most %d characters", ErrInvalidInput, maxDiagnosticNoteRunes)
	}
	return to, note, nil
}

// diagnosticEmailData turns a stored result into what the results email shows.
// Percent, level, strongest and weakest follow the admin result page.
func diagnosticEmailData(result *models.AdminDiagnosticResultDetail, note string) email.DiagnosticResultData {
	names := diagnosticElementNames[result.TestSlug]
	name := func(element int) string {
		if element >= 1 && element <= len(names) {
			return names[element-1]
		}
		return fmt.Sprintf("Element %d", element)
	}

	data := email.DiagnosticResultData{
		FirstName: result.FirstName,
		TestTitle: result.TestTitle,
		Correct:   result.Correct,
		Total:     result.Total,
		Note:      note,
		Elements:  make([]email.DiagnosticElementLine, 0, len(result.Elements)),
	}
	for _, element := range result.Elements {
		percent := diagnosticElementPercent(element)
		data.Elements = append(data.Elements, email.DiagnosticElementLine{
			Name:    name(element.Element),
			Correct: element.Correct,
			Total:   element.Total,
			Percent: percent,
			Level:   diagnosticLevel(percent),
		})
	}

	if len(result.Elements) > 0 {
		best, worst := result.Elements[0], result.Elements[0]
		for _, element := range result.Elements {
			percent := diagnosticElementPercent(element)
			if p := diagnosticElementPercent(best); percent > p || (percent == p && element.Points > best.Points) {
				best = element
			}
			if p := diagnosticElementPercent(worst); percent < p || (percent == p && element.Points < worst.Points) {
				worst = element
			}
		}
		data.Strongest = name(best.Element)
		data.Weakest = name(worst.Element)
	}
	return data
}

// diagnosticElementPercent is points as a 0-100 percent, with a negative total counted as zero.
func diagnosticElementPercent(element models.DiagnosticElementResult) int {
	if element.MaxPoints <= 0 {
		return 0
	}
	return int(math.Round(float64(max(0, element.Points)) / float64(element.MaxPoints) * 100))
}

func diagnosticLevel(percent int) email.DiagnosticLevel {
	switch {
	case percent >= 70:
		return email.LevelStrong
	case percent <= 40:
		return email.LevelWeak
	default:
		return email.LevelSteady
	}
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

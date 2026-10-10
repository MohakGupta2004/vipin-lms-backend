package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

const (
	maxQuizTitleLength       = 200
	maxQuizDescriptionLength = 2000
	maxQuestionsPerQuiz      = 200
	maxQuestionTextLength    = 2000
	maxExplanationLength     = 5000
	minOptionsPerQuestion    = 2
	maxOptionsPerQuestion    = 10
	maxOptionTextLength      = 1000
	maxTimeLimitSec          = 24 * 60 * 60
	defaultPassPercent       = 70
)

var (
	// ErrQuizNotFound means the quiz does not exist, or the user may not see it.
	ErrQuizNotFound = errors.New("quiz not found")
	// ErrQuizHasAttempts means the questions or type cannot be changed because students already attempted the quiz.
	ErrQuizHasAttempts = models.ErrQuizHasAttempts
)

// CreateQuizInput is everything an instructor sends to create a quiz in one go.
type CreateQuizInput struct {
	Title        string
	Description  string
	Type         string // "" = mock_test
	TimeLimitSec *int   // mock_test only
	PassPercent  *int   // mock_test only, nil = 70
	IsFree       bool
	Status       string // "" = published
	Questions    []CreateQuestionInput
}

// UpdateQuizInput holds the quiz fields to change. Nil fields are left as they are.
// TimeLimitSec 0 makes the quiz untimed. Questions, when set, replace all existing questions.
type UpdateQuizInput struct {
	Title        *string
	Description  *string
	Type         *string
	TimeLimitSec *int
	PassPercent  *int
	IsFree       *bool
	Status       *string
	Questions    *[]CreateQuestionInput
}

type CreateQuestionInput struct {
	QuestionText string
	Explanation  string
	Options      []CreateOptionInput
}

type CreateOptionInput struct {
	OptionText string
	IsCorrect  bool
}

// SubmitAnswerInput is the option a student picked for one question.
type SubmitAnswerInput struct {
	QuestionID string
	OptionID   string
}

type QuizService struct {
	quizRepo   *models.QuizRepository
	lessonRepo *models.LessonRepository
}

func NewQuizService(quizRepo *models.QuizRepository, lessonRepo *models.LessonRepository) *QuizService {
	return &QuizService{
		quizRepo:   quizRepo,
		lessonRepo: lessonRepo,
	}
}

// CreateQuiz lets the course owner (instructor or admin) create a quiz with its questions and options on a
// lesson of their course. Questions and options keep the order they were sent in.
func (s *QuizService) CreateQuiz(ctx context.Context, user *models.User, lessonID string, in CreateQuizInput) (*models.Quiz, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(lessonID) {
		return nil, ErrLessonNotFound
	}

	quiz, err := buildQuiz(in)
	if err != nil {
		return nil, err
	}

	lesson, err := s.lessonRepo.GetLesson(ctx, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	// Not the teacher of this course: pretend the lesson does not exist.
	if err := requireTeacher(ctx, s.lessonRepo, user, lesson.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, ErrLessonNotFound
		}
		return nil, err
	}

	quiz.CourseID = lesson.CourseID
	quiz.LessonID = lesson.ID
	quiz.CreatedBy = user.ID
	if err := s.quizRepo.CreateQuiz(ctx, quiz); err != nil {
		return nil, err
	}
	return quiz, nil
}

// buildQuiz validates the input and turns it into a quiz ready to save.
func buildQuiz(in CreateQuizInput) (*models.Quiz, error) {
	quiz := &models.Quiz{
		Title:        strings.TrimSpace(in.Title),
		Description:  strings.TrimSpace(in.Description),
		Type:         strings.TrimSpace(in.Type),
		TimeLimitSec: in.TimeLimitSec,
		PassPercent:  in.PassPercent,
		IsFree:       in.IsFree,
		Status:       strings.TrimSpace(in.Status),
	}
	if quiz.Type == "" {
		quiz.Type = models.QuizTypeMockTest
	}
	if quiz.Type == models.QuizTypeMockTest && quiz.PassPercent == nil {
		pass := defaultPassPercent
		quiz.PassPercent = &pass
	}
	if quiz.Status == "" {
		quiz.Status = "published"
	}
	if err := validateQuizDetails(quiz); err != nil {
		return nil, err
	}

	questions, err := buildQuestions(in.Questions)
	if err != nil {
		return nil, err
	}
	quiz.Questions = questions
	return quiz, nil
}

// validateQuizDetails checks the quiz's own fields, not its questions.
func validateQuizDetails(quiz *models.Quiz) error {
	switch {
	case quiz.Title == "":
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	case utf8.RuneCountInString(quiz.Title) > maxQuizTitleLength:
		return fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxQuizTitleLength)
	case utf8.RuneCountInString(quiz.Description) > maxQuizDescriptionLength:
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxQuizDescriptionLength)
	case quiz.Type != models.QuizTypeMockTest && quiz.Type != models.QuizTypePractice:
		return fmt.Errorf("%w: type must be mock_test or practice", ErrInvalidInput)
	case quiz.Type == models.QuizTypePractice && quiz.TimeLimitSec != nil:
		return fmt.Errorf("%w: practice sets have no time limit", ErrInvalidInput)
	case quiz.Type == models.QuizTypePractice && quiz.PassPercent != nil:
		return fmt.Errorf("%w: practice sets have no pass percent", ErrInvalidInput)
	case quiz.TimeLimitSec != nil && (*quiz.TimeLimitSec < 1 || *quiz.TimeLimitSec > maxTimeLimitSec):
		return fmt.Errorf("%w: timeLimitSec must be between 1 and %d", ErrInvalidInput, maxTimeLimitSec)
	case quiz.PassPercent != nil && (*quiz.PassPercent < 0 || *quiz.PassPercent > 100):
		return fmt.Errorf("%w: passPercent must be between 0 and 100", ErrInvalidInput)
	case quiz.Status != "draft" && quiz.Status != "published":
		return fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	}
	return nil
}

// buildQuestions validates questions with their options and numbers them in the order they were sent.
func buildQuestions(in []CreateQuestionInput) ([]models.Question, error) {
	switch {
	case len(in) == 0:
		return nil, fmt.Errorf("%w: at least one question is required", ErrInvalidInput)
	case len(in) > maxQuestionsPerQuiz:
		return nil, fmt.Errorf("%w: a quiz can have at most %d questions", ErrInvalidInput, maxQuestionsPerQuiz)
	}

	questions := make([]models.Question, 0, len(in))
	for i, qin := range in {
		n := i + 1
		question := models.Question{
			QuestionText: strings.TrimSpace(qin.QuestionText),
			Explanation:  strings.TrimSpace(qin.Explanation),
			Position:     n,
		}
		switch {
		case question.QuestionText == "":
			return nil, fmt.Errorf("%w: question %d: questionText is required", ErrInvalidInput, n)
		case utf8.RuneCountInString(question.QuestionText) > maxQuestionTextLength:
			return nil, fmt.Errorf("%w: question %d: questionText must be at most %d characters", ErrInvalidInput, n, maxQuestionTextLength)
		case utf8.RuneCountInString(question.Explanation) > maxExplanationLength:
			return nil, fmt.Errorf("%w: question %d: explanation must be at most %d characters", ErrInvalidInput, n, maxExplanationLength)
		case len(qin.Options) < minOptionsPerQuestion || len(qin.Options) > maxOptionsPerQuestion:
			return nil, fmt.Errorf("%w: question %d: must have between %d and %d options", ErrInvalidInput, n, minOptionsPerQuestion, maxOptionsPerQuestion)
		}

		correct := 0
		for j, oin := range qin.Options {
			isCorrect := oin.IsCorrect
			option := models.QuestionOption{
				OptionText: strings.TrimSpace(oin.OptionText),
				IsCorrect:  &isCorrect,
				Position:   j + 1,
			}
			if option.OptionText == "" {
				return nil, fmt.Errorf("%w: question %d, option %d: optionText is required", ErrInvalidInput, n, j+1)
			}
			if utf8.RuneCountInString(option.OptionText) > maxOptionTextLength {
				return nil, fmt.Errorf("%w: question %d, option %d: optionText must be at most %d characters", ErrInvalidInput, n, j+1, maxOptionTextLength)
			}
			if isCorrect {
				correct++
			}
			question.Options = append(question.Options, option)
		}
		// One answer per question: attempt_answers stores a single selected option.
		if correct != 1 {
			return nil, fmt.Errorf("%w: question %d: exactly one option must be correct", ErrInvalidInput, n)
		}
		questions = append(questions, question)
	}
	return questions, nil
}

// UpdateQuizStatus lets the course owner (instructor or admin) publish a quiz or move it back to draft.
func (s *QuizService) UpdateQuizStatus(ctx context.Context, user *models.User, quizID, status string) (*models.Quiz, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	status = strings.TrimSpace(status)
	if status != "draft" && status != "published" {
		return nil, fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	}

	quiz, isTeacher, err := s.authorizeQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}
	// Enrolled but not the teacher: pretend the quiz does not exist.
	if !isTeacher {
		return nil, ErrQuizNotFound
	}

	err = s.quizRepo.UpdateStatus(ctx, quiz.ID, status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrQuizNotFound
	}
	if err != nil {
		return nil, err
	}
	quiz.Status = status
	return quiz, nil
}

// UpdateQuiz lets the course owner (instructor or admin) edit a quiz's details and, while nobody has
// attempted it yet, replace its questions.
func (s *QuizService) UpdateQuiz(ctx context.Context, user *models.User, quizID string, in UpdateQuizInput) (*models.Quiz, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	quiz, err := s.ownedQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}

	if in.Title != nil {
		quiz.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		quiz.Description = strings.TrimSpace(*in.Description)
	}
	// A type switch resets the type-specific fields; fields sent in the same request are applied on top.
	typeChanged := false
	if in.Type != nil {
		newType := strings.TrimSpace(*in.Type)
		if newType != quiz.Type {
			typeChanged = true
			quiz.Type = newType
			quiz.TimeLimitSec = nil
			quiz.PassPercent = nil
			if newType == models.QuizTypeMockTest {
				pass := defaultPassPercent
				quiz.PassPercent = &pass
			}
		}
	}
	if in.TimeLimitSec != nil {
		quiz.TimeLimitSec = in.TimeLimitSec
		if *in.TimeLimitSec == 0 {
			quiz.TimeLimitSec = nil
		}
	}
	if in.PassPercent != nil {
		quiz.PassPercent = in.PassPercent
	}
	if in.IsFree != nil {
		quiz.IsFree = *in.IsFree
	}
	if in.Status != nil {
		quiz.Status = strings.TrimSpace(*in.Status)
	}
	if err := validateQuizDetails(quiz); err != nil {
		return nil, err
	}

	replaceQuestions := in.Questions != nil
	if replaceQuestions {
		quiz.Questions, err = buildQuestions(*in.Questions)
		if err != nil {
			return nil, err
		}
	}

	err = s.quizRepo.UpdateQuiz(ctx, quiz, replaceQuestions, typeChanged)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrQuizNotFound
	}
	if err != nil {
		return nil, err
	}

	// Return the quiz as GetQuiz shows it to the owner, with every question.
	quiz.Questions, err = s.quizRepo.ListQuestions(ctx, quiz.ID)
	if err != nil {
		return nil, err
	}
	quiz.QuestionCount = len(quiz.Questions)
	return quiz, nil
}

// DeleteQuiz lets the course owner (instructor or admin) delete a quiz. It is hidden, not erased,
// so students' attempts and scores are kept.
func (s *QuizService) DeleteQuiz(ctx context.Context, user *models.User, quizID string) error {
	if !user.CanTeach() {
		return ErrForbidden
	}
	quiz, err := s.ownedQuiz(ctx, user, quizID)
	if err != nil {
		return err
	}
	err = s.quizRepo.SoftDelete(ctx, quiz.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrQuizNotFound
	}
	return err
}

// ownedQuiz loads a quiz of a course the user owns. Anything else looks like a missing quiz.
func (s *QuizService) ownedQuiz(ctx context.Context, user *models.User, quizID string) (*models.Quiz, error) {
	quiz, isTeacher, err := s.authorizeQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}
	if !isTeacher {
		return nil, ErrQuizNotFound
	}
	return quiz, nil
}

// ListQuizzes returns a lesson's quizzes, without questions, for the instructor or an enrolled student.
// Previewers get them too, with locked set on those they cannot open. A non-empty quizType keeps
// only quizzes of that type.
// Students only see published quizzes of published lessons.
func (s *QuizService) ListQuizzes(ctx context.Context, user *models.User, lessonID, quizType string) ([]models.Quiz, error) {
	if quizType != "" && quizType != models.QuizTypeMockTest && quizType != models.QuizTypePractice {
		return nil, fmt.Errorf("%w: type must be mock_test or practice", ErrInvalidInput)
	}
	if !uuidPattern.MatchString(lessonID) {
		return nil, ErrLessonNotFound
	}
	lesson, err := s.lessonRepo.GetLesson(ctx, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}

	isTeacher, preview, err := authorizePreview(ctx, s.lessonRepo, user, lesson.CourseID)
	if errors.Is(err, ErrCourseNotFound) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	if !isTeacher && !lesson.IsPublished {
		return nil, ErrLessonNotFound
	}
	quizzes, err := s.quizRepo.ListByLesson(ctx, lessonID, isTeacher, quizType)
	if err != nil || !preview {
		return quizzes, err
	}
	// Previewers see every quiz, but can only open those marked free; a free lesson does not unlock them.
	for i := range quizzes {
		quizzes[i].Locked = !quizzes[i].IsFree
	}
	return quizzes, nil
}

// GetQuiz returns a quiz with its questions. The instructor sees the correct options and explanations;
// a student gets the questions to answer with both hidden.
func (s *QuizService) GetQuiz(ctx context.Context, user *models.User, quizID string) (*models.Quiz, error) {
	quiz, isTeacher, err := s.authorizeQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}

	quiz.Questions, err = s.quizRepo.ListQuestions(ctx, quiz.ID)
	if err != nil {
		return nil, err
	}
	if !isTeacher {
		for i := range quiz.Questions {
			quiz.Questions[i].Explanation = ""
			for j := range quiz.Questions[i].Options {
				quiz.Questions[i].Options[j].IsCorrect = nil
			}
		}
	}
	return quiz, nil
}

// SubmitAttempt grades a student's answers to a quiz in one go and saves the attempt.
// Mock test: questions left out of answers count as skipped (wrong), and the attempt gets a score
// and pass mark. Practice set: only the answered questions are graded, without marks, so the
// student can check a few questions at a time. Either way the result reveals the correct
// options and explanations.
func (s *QuizService) SubmitAttempt(ctx context.Context, user *models.User, quizID string, answers []SubmitAnswerInput) (*models.QuizAttempt, error) {
	if user.Role != models.RoleStudent {
		return nil, ErrForbidden
	}
	quiz, _, err := s.authorizeQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}

	questions, err := s.quizRepo.ListQuestions(ctx, quiz.ID)
	if err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w: this quiz has no questions", ErrInvalidInput)
	}

	// questionID -> optionID -> is correct
	options := make(map[string]map[string]bool, len(questions))
	for _, q := range questions {
		options[q.ID] = make(map[string]bool, len(q.Options))
		for _, o := range q.Options {
			options[q.ID][o.ID] = *o.IsCorrect
		}
	}

	selected := make(map[string]string, len(answers))
	for i, a := range answers {
		questionID := strings.ToLower(strings.TrimSpace(a.QuestionID))
		optionID := strings.ToLower(strings.TrimSpace(a.OptionID))
		questionOptions, ok := options[questionID]
		if !ok {
			return nil, fmt.Errorf("%w: answer %d: question is not part of this quiz", ErrInvalidInput, i+1)
		}
		if _, dup := selected[questionID]; dup {
			return nil, fmt.Errorf("%w: answer %d: question answered more than once", ErrInvalidInput, i+1)
		}
		if _, ok := questionOptions[optionID]; !ok {
			return nil, fmt.Errorf("%w: answer %d: option is not part of this question", ErrInvalidInput, i+1)
		}
		selected[questionID] = optionID
	}
	if quiz.Type == models.QuizTypePractice && len(selected) == 0 {
		return nil, fmt.Errorf("%w: answer at least one question", ErrInvalidInput)
	}

	attempt := gradeAttempt(quiz, questions, selected)
	attempt.UserID = user.ID

	if err := s.quizRepo.CreateAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}

// ListAttempts returns the student's own past attempts at a quiz, newest first, with graded answers and explanations.
func (s *QuizService) ListAttempts(ctx context.Context, user *models.User, quizID string) ([]models.QuizAttempt, error) {
	if user.Role != models.RoleStudent {
		return nil, ErrForbidden
	}
	quiz, _, err := s.authorizeQuiz(ctx, user, quizID)
	if err != nil {
		return nil, err
	}
	return s.quizRepo.ListAttempts(ctx, quiz.ID, user.ID)
}

// authorizeQuiz loads a quiz the user may see: the course's instructor sees every quiz, an enrolled
// student only published quizzes of published lessons, and a user previewing the course only those
// that are marked free. Anything else looks like a missing quiz.
func (s *QuizService) authorizeQuiz(ctx context.Context, user *models.User, quizID string) (*models.Quiz, bool, error) {
	if !uuidPattern.MatchString(quizID) {
		return nil, false, ErrQuizNotFound
	}
	quiz, err := s.quizRepo.GetQuiz(ctx, quizID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrQuizNotFound
	}
	if err != nil {
		return nil, false, err
	}

	isTeacher, preview, err := authorizePreview(ctx, s.lessonRepo, user, quiz.CourseID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
		return nil, false, ErrQuizNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if !isTeacher && (quiz.Status != "published" || !quiz.LessonPublished) {
		return nil, false, ErrQuizNotFound
	}
	if preview && !quiz.IsFree {
		return nil, false, ErrQuizNotFound
	}
	return quiz, isTeacher, nil
}

// gradeAttempt grades the selected options (questionID -> optionID, already validated) against the
// quiz's questions. A mock test grades every question and fills in score, total and passed; a
// practice set only grades the questions that were answered and leaves the marks nil.
func gradeAttempt(quiz *models.Quiz, questions []models.Question, selected map[string]string) *models.QuizAttempt {
	practice := quiz.Type == models.QuizTypePractice
	attempt := &models.QuizAttempt{QuizID: quiz.ID}
	score := 0
	for _, q := range questions {
		optionID, answered := selected[q.ID]
		if practice && !answered {
			continue
		}
		answer := models.AttemptAnswer{
			QuestionID:  q.ID,
			Explanation: q.Explanation,
		}
		for _, o := range q.Options {
			if *o.IsCorrect {
				answer.CorrectOptionID = o.ID
			}
			if answered && o.ID == optionID {
				answer.SelectedOptionID = &optionID
				answer.IsCorrect = *o.IsCorrect
			}
		}
		if answer.IsCorrect {
			score++
		}
		attempt.Answers = append(attempt.Answers, answer)
	}
	if practice {
		return attempt
	}

	total := len(questions)
	passed := quiz.PassPercent != nil && score*100 >= *quiz.PassPercent*total
	attempt.Score = &score
	attempt.Total = &total
	attempt.Passed = &passed
	return attempt
}

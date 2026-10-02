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

// ErrQuizNotFound means the quiz does not exist, or the user may not see it.
var ErrQuizNotFound = errors.New("quiz not found")

// CreateQuizInput is everything an instructor sends to create a quiz in one go.
type CreateQuizInput struct {
	Title        string
	Description  string
	TimeLimitSec *int
	PassPercent  *int // nil = 70
	IsFree       bool
	Status       string // "" = published
	Questions    []CreateQuestionInput
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

// CreateQuiz lets an instructor create a quiz with its questions and options on a lesson of a course they teach.
// Questions and options keep the order they were sent in.
func (s *QuizService) CreateQuiz(ctx context.Context, user *models.User, lessonID string, in CreateQuizInput) (*models.Quiz, error) {
	if user.Role != models.RoleInstructor {
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
		TimeLimitSec: in.TimeLimitSec,
		PassPercent:  defaultPassPercent,
		IsFree:       in.IsFree,
		Status:       strings.TrimSpace(in.Status),
	}
	if in.PassPercent != nil {
		quiz.PassPercent = *in.PassPercent
	}
	if quiz.Status == "" {
		quiz.Status = "published"
	}

	switch {
	case quiz.Title == "":
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	case utf8.RuneCountInString(quiz.Title) > maxQuizTitleLength:
		return nil, fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxQuizTitleLength)
	case utf8.RuneCountInString(quiz.Description) > maxQuizDescriptionLength:
		return nil, fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxQuizDescriptionLength)
	case quiz.TimeLimitSec != nil && (*quiz.TimeLimitSec < 1 || *quiz.TimeLimitSec > maxTimeLimitSec):
		return nil, fmt.Errorf("%w: timeLimitSec must be between 1 and %d", ErrInvalidInput, maxTimeLimitSec)
	case quiz.PassPercent < 0 || quiz.PassPercent > 100:
		return nil, fmt.Errorf("%w: passPercent must be between 0 and 100", ErrInvalidInput)
	case quiz.Status != "draft" && quiz.Status != "published":
		return nil, fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	case len(in.Questions) == 0:
		return nil, fmt.Errorf("%w: at least one question is required", ErrInvalidInput)
	case len(in.Questions) > maxQuestionsPerQuiz:
		return nil, fmt.Errorf("%w: a quiz can have at most %d questions", ErrInvalidInput, maxQuestionsPerQuiz)
	}

	for i, qin := range in.Questions {
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
		quiz.Questions = append(quiz.Questions, question)
	}
	return quiz, nil
}

// UpdateQuizStatus lets the instructor of the course publish a quiz or move it back to draft.
func (s *QuizService) UpdateQuizStatus(ctx context.Context, user *models.User, quizID, status string) (*models.Quiz, error) {
	if user.Role != models.RoleInstructor {
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

// ListQuizzes returns a lesson's quizzes, without questions, for the instructor or an enrolled student.
// Students only see published quizzes of published lessons.
func (s *QuizService) ListQuizzes(ctx context.Context, user *models.User, lessonID string) ([]models.Quiz, error) {
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

	isTeacher, err := authorizeCourse(ctx, s.lessonRepo, user, lesson.CourseID)
	if errors.Is(err, ErrCourseNotFound) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	if !isTeacher && !lesson.IsPublished {
		return nil, ErrLessonNotFound
	}
	return s.quizRepo.ListByLesson(ctx, lessonID, isTeacher)
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
// Questions left out of answers count as skipped (wrong). The result reveals the correct
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

	attempt := &models.QuizAttempt{
		QuizID: quiz.ID,
		UserID: user.ID,
		Total:  len(questions),
	}
	for _, q := range questions {
		answer := models.AttemptAnswer{
			QuestionID:  q.ID,
			Explanation: q.Explanation,
		}
		for _, o := range q.Options {
			if *o.IsCorrect {
				answer.CorrectOptionID = o.ID
			}
		}
		if optionID, ok := selected[q.ID]; ok {
			answer.SelectedOptionID = &optionID
			answer.IsCorrect = options[q.ID][optionID]
		}
		if answer.IsCorrect {
			attempt.Score++
		}
		attempt.Answers = append(attempt.Answers, answer)
	}
	attempt.Passed = attempt.Score*100 >= quiz.PassPercent*attempt.Total

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
// student only published quizzes of published lessons. Anything else looks like a missing quiz.
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

	isTeacher, err := authorizeCourse(ctx, s.lessonRepo, user, quiz.CourseID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
		return nil, false, ErrQuizNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if !isTeacher && (quiz.Status != "published" || !quiz.LessonPublished) {
		return nil, false, ErrQuizNotFound
	}
	return quiz, isTeacher, nil
}

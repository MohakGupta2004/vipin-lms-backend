package service

import (
	"context"
	"fmt"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

// continueWatchingLimit is how many started videos the progress summary offers to resume.
const continueWatchingLimit = 4

// LearningService answers "what is in my courses and how far am I" across every course the user
// learns (a student's valid enrollments) or owns (an instructor or admin).
type LearningService struct {
	repo *models.LearningRepository
}

func NewLearningService(repo *models.LearningRepository) *LearningService {
	return &LearningService{repo: repo}
}

// ProgressSummary is the user's progress overall, per course, and the videos they can pick up again.
type ProgressSummary struct {
	CourseCount int `json:"courseCount"`
	// Totals adds up every course; it has no courseId or courseTitle.
	Totals           models.CourseProgress   `json:"totals"`
	Courses          []models.CourseProgress `json:"courses"`
	ContinueWatching []models.MyVideo        `json:"continueWatching"`
}

// ListVideos returns the ready videos of the user's courses, or of one of them, with the user's progress.
func (s *LearningService) ListVideos(ctx context.Context, user *models.User, courseID string) ([]models.MyVideo, error) {
	if err := validateOptionalID(courseID); err != nil {
		return nil, err
	}
	return s.repo.ListVideos(ctx, user.ID, user.CanTeach(), courseID)
}

// ListQuizzes returns the quizzes of the user's courses, or of one of them, optionally of one type,
// with the user's attempt summary.
func (s *LearningService) ListQuizzes(ctx context.Context, user *models.User, courseID, quizType string) ([]models.MyQuiz, error) {
	if err := validateOptionalID(courseID); err != nil {
		return nil, err
	}
	if quizType != "" && quizType != models.QuizTypeMockTest && quizType != models.QuizTypePractice {
		return nil, fmt.Errorf("%w: type must be %s or %s", ErrInvalidInput, models.QuizTypeMockTest, models.QuizTypePractice)
	}
	return s.repo.ListQuizzes(ctx, user.ID, user.CanTeach(), courseID, quizType)
}

// Progress counts what the user has done in each course and overall.
func (s *LearningService) Progress(ctx context.Context, user *models.User) (*ProgressSummary, error) {
	courses, err := s.repo.ListCourseProgress(ctx, user.ID, user.CanTeach())
	if err != nil {
		return nil, err
	}
	resume, err := s.repo.ContinueWatching(ctx, user.ID, user.CanTeach(), continueWatchingLimit)
	if err != nil {
		return nil, err
	}
	summary := &ProgressSummary{CourseCount: len(courses), Courses: courses, ContinueWatching: resume}
	for _, c := range courses {
		summary.Totals.Add(c)
	}
	summary.Totals.Finish()
	return summary, nil
}

func validateOptionalID(id string) error {
	if id != "" && !uuidPattern.MatchString(id) {
		return fmt.Errorf("%w: courseId must be a UUID", ErrInvalidInput)
	}
	return nil
}

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
	maxLessonTitleLength  = 200
	maxLessonContentChars = 50000
)

type LessonService struct {
	lessonRepo *models.LessonRepository
	noteRepo   *models.NoteRepository
}

func NewLessonService(lessonRepo *models.LessonRepository, noteRepo *models.NoteRepository) *LessonService {
	return &LessonService{
		lessonRepo: lessonRepo,
		noteRepo:   noteRepo,
	}
}

// CreateLessonInput is what the caller supplies for a new lesson.
type CreateLessonInput struct {
	Title       string
	Content     string
	IsFree      bool
	IsPublished bool
}

// authorizeCourse checks that the user may read a course's content: the instructor who teaches it
// or a student with a valid enrollment. isTeacher is true for the instructor.
func authorizeCourse(ctx context.Context, repo *models.LessonRepository, user *models.User, courseID string) (isTeacher bool, err error) {
	if !uuidPattern.MatchString(courseID) {
		return false, ErrCourseNotFound
	}
	teaches, enrolled, err := repo.CourseAccess(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrCourseNotFound
	}
	if err != nil {
		return false, err
	}
	isTeacher = teaches && user.Role == models.RoleInstructor
	if !isTeacher && !enrolled {
		return false, ErrForbidden
	}
	return isTeacher, nil
}

// requireTeacher is like authorizeCourse but only the course's instructor passes.
func requireTeacher(ctx context.Context, repo *models.LessonRepository, user *models.User, courseID string) error {
	if user.Role != models.RoleInstructor {
		return ErrForbidden
	}
	isTeacher, err := authorizeCourse(ctx, repo, user, courseID)
	if errors.Is(err, ErrForbidden) || (err == nil && !isTeacher) {
		return ErrForbidden
	}
	return err
}

// CreateLesson lets an instructor add a lesson (chapter) to a course they teach.
func (s *LessonService) CreateLesson(ctx context.Context, user *models.User, courseID string, in CreateLessonInput) (*models.Lesson, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)

	if in.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(in.Title) > maxLessonTitleLength {
		return nil, fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxLessonTitleLength)
	}
	if utf8.RuneCountInString(in.Content) > maxLessonContentChars {
		return nil, fmt.Errorf("%w: content must be at most %d characters", ErrInvalidInput, maxLessonContentChars)
	}

	if err := requireTeacher(ctx, s.lessonRepo, user, courseID); err != nil {
		return nil, err
	}

	lesson := &models.Lesson{
		CourseID:    courseID,
		Title:       in.Title,
		LessonType:  "article",
		Content:     in.Content,
		IsFree:      in.IsFree,
		IsPublished: in.IsPublished,
	}
	if err := s.lessonRepo.CreateLesson(ctx, lesson); err != nil {
		return nil, err
	}
	return lesson, nil
}

// ListLessons returns the course's lessons in order, each with its PDF notes.
// Students only see published lessons; the instructor sees everything.
func (s *LessonService) ListLessons(ctx context.Context, user *models.User, courseID string) ([]models.Lesson, error) {
	isTeacher, err := authorizeCourse(ctx, s.lessonRepo, user, courseID)
	if err != nil {
		return nil, err
	}

	lessons, err := s.lessonRepo.ListLessons(ctx, courseID, isTeacher)
	if err != nil {
		return nil, err
	}
	notes, err := s.noteRepo.ListByCourse(ctx, courseID, "", isTeacher, 0, 0)
	if err != nil {
		return nil, err
	}

	notesByLesson := map[string][]models.Note{}
	for _, n := range notes {
		notesByLesson[n.LessonID] = append(notesByLesson[n.LessonID], n)
	}
	for i := range lessons {
		lessons[i].Notes = notesByLesson[lessons[i].ID]
	}
	return lessons, nil
}

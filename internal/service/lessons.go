package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
)

const (
	maxLessonTitleLength  = 200
	maxLessonContentChars = 50000
)

type LessonService struct {
	lessonRepo *models.LessonRepository
	noteRepo   *models.NoteRepository
	store      storage.PDFStore
}

func NewLessonService(lessonRepo *models.LessonRepository, noteRepo *models.NoteRepository, store storage.PDFStore) *LessonService {
	return &LessonService{
		lessonRepo: lessonRepo,
		noteRepo:   noteRepo,
		store:      store,
	}
}

// CreateLessonInput is what the caller supplies for a new lesson.
type CreateLessonInput struct {
	Title       string
	Content     string
	IsFree      bool
	IsPublished bool
}

// UpdateLessonInput holds the lesson fields to change. Nil fields are left as they are.
type UpdateLessonInput struct {
	Title       *string
	Content     *string
	IsFree      *bool
	IsPublished *bool
}

// authorizeCourse checks that the user may read a course's content: its owner (the instructor or admin
// whose id is the course's instructor_id) or a user with a valid enrollment. isTeacher is true for the owner.
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
	isTeacher = teaches && user.CanTeach()
	if !isTeacher && !enrolled {
		return false, ErrForbidden
	}
	return isTeacher, nil
}

// requireTeacher is like authorizeCourse but only the course's owner passes.
func requireTeacher(ctx context.Context, repo *models.LessonRepository, user *models.User, courseID string) error {
	if !user.CanTeach() {
		return ErrForbidden
	}
	isTeacher, err := authorizeCourse(ctx, repo, user, courseID)
	if errors.Is(err, ErrForbidden) || (err == nil && !isTeacher) {
		return ErrForbidden
	}
	return err
}

// CreateLesson lets the course owner (instructor or admin) add a lesson (chapter) to their course.
func (s *LessonService) CreateLesson(ctx context.Context, user *models.User, courseID string, in CreateLessonInput) (*models.Lesson, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if err := validateLesson(in.Title, in.Content); err != nil {
		return nil, err
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

// UpdateLesson lets the course owner (instructor or admin) edit a lesson, including publishing or unpublishing it.
func (s *LessonService) UpdateLesson(ctx context.Context, user *models.User, lessonID string, in UpdateLessonInput) (*models.Lesson, error) {
	lesson, err := s.ownedLesson(ctx, user, lessonID)
	if err != nil {
		return nil, err
	}

	if in.Title != nil {
		lesson.Title = strings.TrimSpace(*in.Title)
	}
	if in.Content != nil {
		lesson.Content = strings.TrimSpace(*in.Content)
	}
	if in.IsFree != nil {
		lesson.IsFree = *in.IsFree
	}
	if in.IsPublished != nil {
		lesson.IsPublished = *in.IsPublished
	}
	if err := validateLesson(lesson.Title, lesson.Content); err != nil {
		return nil, err
	}

	err = s.lessonRepo.UpdateLesson(ctx, lesson)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	return lesson, nil
}

// DeleteLesson lets the course owner (instructor or admin) delete a lesson. Its notes and their PDF
// files are removed; its quizzes are hidden but students' attempts are kept.
func (s *LessonService) DeleteLesson(ctx context.Context, user *models.User, lessonID string) error {
	if _, err := s.ownedLesson(ctx, user, lessonID); err != nil {
		return err
	}

	// Deleting the note rows while storage is off would leave their files behind forever.
	if !s.store.Enabled() {
		hasFiles, err := s.lessonRepo.HasNoteFiles(ctx, lessonID)
		if err != nil {
			return err
		}
		if hasFiles {
			return ErrStorageDisabled
		}
	}

	objectKeys, err := s.lessonRepo.DeleteLesson(ctx, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLessonNotFound
	}
	if err != nil {
		return err
	}
	for _, key := range objectKeys {
		deleteStoredPDF(s.store, key)
	}
	return nil
}

// ownedLesson loads a lesson of a course the user owns. Anything else looks like a missing lesson.
func (s *LessonService) ownedLesson(ctx context.Context, user *models.User, lessonID string) (*models.Lesson, error) {
	return loadOwnedLesson(ctx, s.lessonRepo, user, lessonID)
}

// loadOwnedLesson loads a lesson of a course the user owns. Anything else looks like a missing lesson.
func loadOwnedLesson(ctx context.Context, repo *models.LessonRepository, user *models.User, lessonID string) (*models.Lesson, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(lessonID) {
		return nil, ErrLessonNotFound
	}
	lesson, err := repo.GetLesson(ctx, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := requireTeacher(ctx, repo, user, lesson.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
			return nil, ErrLessonNotFound
		}
		return nil, err
	}
	return lesson, nil
}

func validateLesson(title, content string) error {
	switch {
	case title == "":
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	case utf8.RuneCountInString(title) > maxLessonTitleLength:
		return fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxLessonTitleLength)
	case utf8.RuneCountInString(content) > maxLessonContentChars:
		return fmt.Errorf("%w: content must be at most %d characters", ErrInvalidInput, maxLessonContentChars)
	}
	return nil
}

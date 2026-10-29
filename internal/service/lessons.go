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
	videoFiles videoFileCleaner
}

func NewLessonService(lessonRepo *models.LessonRepository, noteRepo *models.NoteRepository, store storage.PDFStore, videoFiles videoFileCleaner) *LessonService {
	return &LessonService{
		lessonRepo: lessonRepo,
		noteRepo:   noteRepo,
		store:      store,
		videoFiles: videoFiles,
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

// authorizeCourse checks that the user may read all of a course's content: its owner (the instructor or
// admin whose id is the course's instructor_id), a user with a valid enrollment, or any logged-in user
// when the published course is free. isTeacher is true for the owner.
func authorizeCourse(ctx context.Context, repo *models.LessonRepository, user *models.User, courseID string) (isTeacher bool, err error) {
	isTeacher, tier, err := authorizeTier(ctx, repo, user, courseID)
	if err != nil {
		return false, err
	}
	if tier == models.TierFree {
		return false, ErrForbidden
	}
	return isTeacher, nil
}

// authorizeTier tells which tier a user has in a course: the owner and anyone in a free course get
// ultra, an enrolled student gets the tier on their enrollment, and any other user may preview a
// published, public course as free. A private course, or another batch of a course the user is
// enrolled in, is closed to everyone but its enrolled students. Free users only open the free lessons, plus the notes, quizzes,
// flashcards and videos tagged free on their own (a free lesson does not unlock them). Items
// tagged for other tiers are opened with models.CanAccess.
func authorizeTier(ctx context.Context, repo *models.LessonRepository, user *models.User, courseID string) (isTeacher bool, tier models.Tier, err error) {
	if !uuidPattern.MatchString(courseID) {
		return false, "", ErrCourseNotFound
	}
	access, err := repo.CourseAccess(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", ErrCourseNotFound
	}
	if err != nil {
		return false, "", err
	}
	isTeacher = access.IsInstructor && user.CanTeach()
	switch {
	case isTeacher:
		return true, models.TierUltra, nil
	case access.EnrolledTier != "":
		return false, access.EnrolledTier, nil
	case access.Previewable && access.Free:
		return false, models.TierUltra, nil // a free course is open to every logged-in user who can find it
	case access.Previewable:
		return false, models.TierFree, nil
	default:
		return false, "", ErrForbidden
	}
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
// Students only see published lessons; the instructor sees everything. Users previewing a course
// they are not enrolled in see every published lesson and note; paid lessons and the notes they
// cannot open come back with locked set.
func (s *LessonService) ListLessons(ctx context.Context, user *models.User, courseID string) ([]models.Lesson, error) {
	isTeacher, tier, err := authorizeTier(ctx, s.lessonRepo, user, courseID)
	if err != nil {
		return nil, err
	}
	preview := tier == models.TierFree

	lessons, err := s.lessonRepo.ListLessons(ctx, courseID, isTeacher)
	if err != nil {
		return nil, err
	}
	notes, err := s.noteRepo.ListByCourse(ctx, courseID, "", isTeacher, 0, 0)
	if err != nil {
		return nil, err
	}
	markLockedNotes(notes, tier)

	notesByLesson := map[string][]models.Note{}
	for _, n := range notes {
		notesByLesson[n.LessonID] = append(notesByLesson[n.LessonID], n)
	}
	for i := range lessons {
		lessons[i].Notes = notesByLesson[lessons[i].ID]
		lessons[i].Locked = preview && !lessons[i].IsFree
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

// DeleteLesson lets the course owner (instructor or admin) delete a lesson. Its notes and videos and their
// files are removed (files another batch still uses are kept); its quizzes are hidden but students' attempts are kept.
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

	objectKeys, err := s.lessonRepo.DeleteLesson(ctx, lessonID, videoResweepAfter())
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLessonNotFound
	}
	if err != nil {
		return err
	}
	s.videoFiles.StartFileCleanup()
	for _, key := range objectKeys {
		deleteUnusedPDF(ctx, s.noteRepo, s.store, key)
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

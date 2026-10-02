package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
)

const (
	maxNoteTitleLength       = 200
	maxNoteDescriptionLength = 2000
	cleanupTimeout           = 10 * time.Second
)

var (
	// ErrStorageDisabled means PDF storage is switched off. Safe to show the client.
	ErrStorageDisabled = storage.ErrStorageDisabled
	// ErrNoteNotFound means the note does not exist, or the user may not see it.
	ErrNoteNotFound = errors.New("note not found")
	// ErrLessonNotFound means the lesson does not exist, or the user may not see it.
	ErrLessonNotFound = errors.New("lesson not found")
)

type NoteService struct {
	noteRepo   *models.NoteRepository
	lessonRepo *models.LessonRepository
	store      storage.PDFStore
}

func NewNoteService(noteRepo *models.NoteRepository, lessonRepo *models.LessonRepository, store storage.PDFStore) *NoteService {
	return &NoteService{
		noteRepo:   noteRepo,
		lessonRepo: lessonRepo,
		store:      store,
	}
}

// UploadNote lets an instructor share a PDF on a lesson of a course they teach.
func (s *NoteService) UploadNote(ctx context.Context, user *models.User, lessonID, title, description, fileName string, file io.Reader) (*models.Note, error) {
	if user.Role != models.RoleInstructor {
		return nil, ErrForbidden
	}
	if !s.store.Enabled() {
		return nil, ErrStorageDisabled
	}
	if !uuidPattern.MatchString(lessonID) {
		return nil, ErrLessonNotFound
	}

	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(title) > maxNoteTitleLength {
		return nil, fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxNoteTitleLength)
	}
	if utf8.RuneCountInString(description) > maxNoteDescriptionLength {
		return nil, fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxNoteDescriptionLength)
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

	uploaded, err := s.store.UploadPDF(ctx, "courses/"+lesson.CourseID+"/notes", fileName, file)
	switch {
	case errors.Is(err, storage.ErrInvalidPDF), errors.Is(err, storage.ErrPDFTooLarge):
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	case err != nil:
		return nil, err
	}

	note := &models.Note{
		CourseID:     lesson.CourseID,
		LessonID:     lesson.ID,
		Title:        title,
		Description:  description,
		FileName:     pdfFileName(fileName, title),
		SizeBytes:    uploaded.Size,
		UploadedBy:   user.ID,
		UploaderName: user.FirstName + " " + user.LastName,
		ObjectKey:    uploaded.Object,
	}
	if err := s.noteRepo.Create(ctx, note); err != nil {
		// Do not leave an orphan file behind. Use a fresh context: the request may be cancelled.
		s.deleteObject(uploaded.Object)
		return nil, err
	}
	return note, nil
}

// ListNotes returns a course's PDF notes for its instructor or an enrolled student.
// lessonID narrows the list to one lesson; empty means the whole course.
func (s *NoteService) ListNotes(ctx context.Context, user *models.User, courseID, lessonID string, limit, offset int) ([]models.Note, error) {
	if lessonID != "" && !uuidPattern.MatchString(lessonID) {
		return nil, fmt.Errorf("%w: lessonId must be a valid id", ErrInvalidInput)
	}
	isTeacher, err := authorizeCourse(ctx, s.lessonRepo, user, courseID)
	if err != nil {
		return nil, err
	}
	return s.noteRepo.ListByCourse(ctx, courseID, lessonID, isTeacher, limit, offset)
}

// OpenNote checks access and returns the note with its PDF stream. Caller must Close the stream.
func (s *NoteService) OpenNote(ctx context.Context, user *models.User, noteID string) (*models.Note, io.ReadCloser, error) {
	note, err := s.getNote(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}

	isTeacher, err := authorizeCourse(ctx, s.lessonRepo, user, note.CourseID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
		return nil, nil, ErrNoteNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	// Students cannot open notes of lessons the instructor has not published.
	if !isTeacher && !note.LessonPublished {
		return nil, nil, ErrNoteNotFound
	}

	rc, err := s.store.OpenPDF(ctx, note.ObjectKey)
	if errors.Is(err, storage.ErrObjectNotFound) {
		slog.Error("note file missing in storage", "noteId", note.ID, "object", note.ObjectKey)
		return nil, nil, ErrNoteNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return note, rc, nil
}

// DeleteNote lets the instructor who uploaded a note delete it.
func (s *NoteService) DeleteNote(ctx context.Context, user *models.User, noteID string) error {
	if user.Role != models.RoleInstructor {
		return ErrForbidden
	}
	// Deleting the row while storage is off would leave the file behind forever.
	if !s.store.Enabled() {
		return ErrStorageDisabled
	}
	note, err := s.getNote(ctx, noteID)
	if err != nil {
		return err
	}
	if note.UploadedBy != user.ID {
		return ErrNoteNotFound
	}

	objectKey, err := s.noteRepo.Delete(ctx, noteID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoteNotFound
	}
	if err != nil {
		return err
	}
	s.deleteObject(objectKey)
	return nil
}

func (s *NoteService) getNote(ctx context.Context, noteID string) (*models.Note, error) {
	if !uuidPattern.MatchString(noteID) {
		return nil, ErrNoteNotFound
	}
	note, err := s.noteRepo.Get(ctx, noteID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoteNotFound
	}
	return note, err
}

// deleteObject removes a stored file on a best-effort basis; a failure only leaves an orphan file.
func (s *NoteService) deleteObject(object string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := s.store.DeletePDF(ctx, object); err != nil {
		slog.Error("failed to delete stored note file", "object", object, "err", err)
	}
}

// pdfFileName returns the uploaded name, or the title, always ending in .pdf.
func pdfFileName(uploaded, title string) string {
	name := strings.TrimSpace(uploaded)
	if name == "" {
		name = title
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

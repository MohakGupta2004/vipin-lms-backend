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

// ErrNoteNotFound means the note does not exist.
var ErrNoteNotFound = errors.New("note not found")

type CourseNoteService struct {
	noteRepo *models.CourseNoteRepository
	store    storage.PDFStore
}

func NewCourseNoteService(noteRepo *models.CourseNoteRepository, store storage.PDFStore) *CourseNoteService {
	return &CourseNoteService{
		noteRepo: noteRepo,
		store:    store,
	}
}

// requireCourseReader allows the course's instructor and students with a valid enrollment.
func (s *CourseNoteService) requireCourseReader(ctx context.Context, user *models.User, courseID string) error {
	if !uuidPattern.MatchString(courseID) {
		return ErrCourseNotFound
	}
	isInstructor, isEnrolled, err := s.noteRepo.CourseAccess(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCourseNotFound
	}
	if err != nil {
		return err
	}
	if !isInstructor && !isEnrolled {
		return ErrForbidden
	}
	return nil
}

// UploadNote lets an instructor add a PDF note to a course they teach.
func (s *CourseNoteService) UploadNote(ctx context.Context, user *models.User, courseID, title, description, fileName string, file io.Reader) (*models.CourseNote, error) {
	if user.Role != models.RoleInstructor {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(courseID) {
		return nil, ErrCourseNotFound
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

	isInstructor, _, err := s.noteRepo.CourseAccess(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	if !isInstructor {
		return nil, ErrForbidden
	}

	uploaded, err := s.store.UploadPDF(ctx, "courses/"+courseID+"/notes", fileName, file)
	switch {
	case errors.Is(err, storage.ErrInvalidPDF), errors.Is(err, storage.ErrPDFTooLarge):
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	case err != nil:
		return nil, err
	}

	note := &models.CourseNote{
		CourseID:     courseID,
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

// ListNotes returns a course's notes for its instructor or an enrolled student.
func (s *CourseNoteService) ListNotes(ctx context.Context, user *models.User, courseID string, limit, offset int) ([]models.CourseNote, error) {
	if err := s.requireCourseReader(ctx, user, courseID); err != nil {
		return nil, err
	}
	return s.noteRepo.ListByCourse(ctx, courseID, limit, offset)
}

// OpenNote checks access and returns the note with its PDF stream. Caller must Close the stream.
func (s *CourseNoteService) OpenNote(ctx context.Context, user *models.User, noteID string) (*models.CourseNote, io.ReadCloser, error) {
	note, err := s.getNote(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}
	// A note in a course the user cannot access looks the same as a missing note.
	if err := s.requireCourseReader(ctx, user, note.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
			return nil, nil, ErrNoteNotFound
		}
		return nil, nil, err
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
func (s *CourseNoteService) DeleteNote(ctx context.Context, user *models.User, noteID string) error {
	if user.Role != models.RoleInstructor {
		return ErrForbidden
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

func (s *CourseNoteService) getNote(ctx context.Context, noteID string) (*models.CourseNote, error) {
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
func (s *CourseNoteService) deleteObject(object string) {
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

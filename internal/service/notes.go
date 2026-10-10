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

// UploadNote lets the course owner (instructor or admin) share a PDF on a lesson of their course.
// isFree opens it to users previewing the course; otherwise only enrolled students can open it.
func (s *NoteService) UploadNote(ctx context.Context, user *models.User, lessonID, title, description, fileName string, isFree bool, file io.Reader) (*models.Note, error) {
	if !user.CanTeach() {
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
	if err := validateNoteText(title, description); err != nil {
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
		IsFree:       isFree,
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

// ListNotes returns a course's PDF notes for its instructor or an enrolled student. Users previewing
// the course get every note, with locked set on those they cannot open.
// lessonID narrows the list to one lesson; empty means the whole course.
func (s *NoteService) ListNotes(ctx context.Context, user *models.User, courseID, lessonID string, limit, offset int) ([]models.Note, error) {
	if lessonID != "" && !uuidPattern.MatchString(lessonID) {
		return nil, fmt.Errorf("%w: lessonId must be a valid id", ErrInvalidInput)
	}
	isTeacher, preview, err := authorizePreview(ctx, s.lessonRepo, user, courseID)
	if err != nil {
		return nil, err
	}
	notes, err := s.noteRepo.ListByCourse(ctx, courseID, lessonID, isTeacher, limit, offset)
	if err != nil {
		return nil, err
	}
	markLockedNotes(notes, preview)
	return notes, nil
}

// markLockedNotes sets locked on the notes a user previewing the course cannot open: every note the
// instructor has not marked free. The lesson's own free flag does not unlock its notes.
func markLockedNotes(notes []models.Note, preview bool) {
	if !preview {
		return
	}
	for i := range notes {
		notes[i].Locked = noteLocked(&notes[i])
	}
}

// noteLocked reports whether a previewer is shut out of a note.
func noteLocked(n *models.Note) bool {
	return !n.IsFree
}

// OpenNote checks access and returns the note with its PDF stream. Caller must Close the stream.
func (s *NoteService) OpenNote(ctx context.Context, user *models.User, noteID string) (*models.Note, io.ReadCloser, error) {
	note, err := s.getNote(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}

	isTeacher, preview, err := authorizePreview(ctx, s.lessonRepo, user, note.CourseID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
		return nil, nil, ErrNoteNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	// Students cannot open notes of lessons the instructor has not published,
	// and previewers only notes marked free.
	if !isTeacher && (!note.LessonPublished || (preview && noteLocked(note))) {
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

// validateNoteText checks a note's trimmed title and description.
func validateNoteText(title, description string) error {
	switch {
	case title == "":
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	case utf8.RuneCountInString(title) > maxNoteTitleLength:
		return fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxNoteTitleLength)
	case utf8.RuneCountInString(description) > maxNoteDescriptionLength:
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxNoteDescriptionLength)
	}
	return nil
}

// UpdateNoteInput holds the note fields to change. Nil fields are left as they are.
type UpdateNoteInput struct {
	Title       *string
	Description *string
	IsFree      *bool
}

// UpdateNote lets the course owner (instructor or admin) edit a note of their course: its title,
// description, and whether it is free to preview. The PDF itself cannot be replaced.
func (s *NoteService) UpdateNote(ctx context.Context, user *models.User, noteID string, in UpdateNoteInput) (*models.Note, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	note, err := s.getNote(ctx, noteID)
	if err != nil {
		return nil, err
	}
	// Not the owner of this course: pretend the note does not exist.
	if err := requireTeacher(ctx, s.lessonRepo, user, note.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
			return nil, ErrNoteNotFound
		}
		return nil, err
	}

	if in.Title != nil {
		note.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		note.Description = strings.TrimSpace(*in.Description)
	}
	if in.IsFree != nil {
		note.IsFree = *in.IsFree
	}
	if err := validateNoteText(note.Title, note.Description); err != nil {
		return nil, err
	}

	err = s.noteRepo.Update(ctx, note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoteNotFound
	}
	if err != nil {
		return nil, err
	}
	return note, nil
}

// DeleteNote lets the course owner (instructor or admin) delete a note of their course.
func (s *NoteService) DeleteNote(ctx context.Context, user *models.User, noteID string) error {
	if !user.CanTeach() {
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
	// Not the owner of this course: pretend the note does not exist.
	if err := requireTeacher(ctx, s.lessonRepo, user, note.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
			return ErrNoteNotFound
		}
		return err
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
	deleteStoredPDF(s.store, object)
}

// deleteStoredPDF removes a stored file on a best-effort basis; a failure only leaves an orphan file.
// It uses a fresh context because the request may already be cancelled.
func deleteStoredPDF(store storage.PDFStore, object string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := store.DeletePDF(ctx, object); err != nil {
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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func rosterRow(student, course string, videosDone, videosTotal, mockBest int, lastAt *time.Time) models.RosterRow {
	c := models.StudentCourse{LastActiveAt: lastAt, FlashcardsTotal: 4, FlashcardsReviewed: 1}
	c.CourseID = course
	c.VideosTotal, c.VideosCompleted = videosTotal, videosDone
	c.MockTotal, c.MockDone, c.MockBestSum = 1, 1, mockBest
	c.Finish()
	return models.RosterRow{StudentID: student, FirstName: student, Course: c}
}

func TestGroupRosterFoldsCoursesPerStudent(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(48 * time.Hour)
	students := groupRoster([]models.RosterRow{
		rosterRow("a", "c1", 1, 3, 60, &early),
		rosterRow("a", "c2", 3, 3, 80, &late),
		rosterRow("b", "c1", 0, 3, 0, nil),
	})
	if len(students) != 2 {
		t.Fatalf("got %d students, want 2", len(students))
	}
	a := students[0]
	if len(a.Courses) != 2 || a.Totals.VideosCompleted != 4 || a.Totals.VideosTotal != 6 {
		t.Errorf("a: courses=%d videos=%d/%d", len(a.Courses), a.Totals.VideosCompleted, a.Totals.VideosTotal)
	}
	if a.Totals.MockAvgPercent == nil || *a.Totals.MockAvgPercent != 70 {
		t.Errorf("a: mock avg = %v, want 70", a.Totals.MockAvgPercent)
	}
	if a.FlashcardsTotal != 8 || a.FlashcardsReviewed != 2 {
		t.Errorf("a: flashcards %d/%d, want 2/8", a.FlashcardsReviewed, a.FlashcardsTotal)
	}
	if a.LastActiveAt == nil || !a.LastActiveAt.Equal(late) {
		t.Errorf("a: last active = %v, want %v", a.LastActiveAt, late)
	}
	if students[1].LastActiveAt != nil {
		t.Errorf("b: last active = %v, want nil", students[1].LastActiveAt)
	}
}

func TestGroupRosterEmpty(t *testing.T) {
	if got := groupRoster(nil); got == nil || len(got) != 0 {
		t.Errorf("got %v, want an empty, non-nil slice", got)
	}
}

// Students never reach the repository: the role check comes first, so a nil repo is never touched.
func TestStudentServiceRejectsStudents(t *testing.T) {
	s := NewStudentService(nil)
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}
	if _, err := s.ListStudents(context.Background(), student, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListStudents err = %v, want ErrForbidden", err)
	}
	if _, err := s.GetStudent(context.Background(), student, student.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetStudent err = %v, want ErrForbidden", err)
	}
}

func TestStudentServiceRejectsBadIDs(t *testing.T) {
	s := NewStudentService(nil)
	teacher := &models.User{ID: "00000000-0000-0000-0000-000000000002", Role: models.RoleInstructor}
	if _, err := s.ListStudents(context.Background(), teacher, "nope"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListStudents err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.GetStudent(context.Background(), teacher, "nope"); !errors.Is(err, ErrStudentNotFound) {
		t.Errorf("GetStudent err = %v, want ErrStudentNotFound", err)
	}
}

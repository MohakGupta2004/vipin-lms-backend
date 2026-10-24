package service

import (
	"context"
	"errors"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

// ErrStudentNotFound means the student is not enrolled in any course the instructor owns.
var ErrStudentNotFound = errors.New("student not found")

// studentAttemptsLimit is how many recent test attempts a student's page shows.
const studentAttemptsLimit = 100

// StudentService lets an instructor (or an admin, for the courses they own) follow the students enrolled in
// their courses. Ownership is the only key: a student who is not enrolled in one of the caller's courses
// does not exist for them.
type StudentService struct {
	repo *models.StudentRepository
}

func NewStudentService(repo *models.StudentRepository) *StudentService {
	return &StudentService{repo: repo}
}

// StudentProgress is one student's progress across the caller's courses, overall and per course.
type StudentProgress struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	// Totals adds up every course; it has no courseId or courseTitle.
	Totals             models.CourseProgress  `json:"totals"`
	FlashcardsTotal    int                    `json:"flashcardsTotal"`
	FlashcardsReviewed int                    `json:"flashcardsReviewed"`
	LastActiveAt       *time.Time             `json:"lastActiveAt"`
	Courses            []models.StudentCourse `json:"courses"`
}

// StudentDetail is one student's progress plus the videos they played and the tests they submitted.
type StudentDetail struct {
	StudentProgress
	Videos   []models.StudentVideo   `json:"videos"`
	Attempts []models.StudentAttempt `json:"attempts"`
}

// ListStudents returns the students enrolled in the caller's courses (or one of them), in name order.
func (s *StudentService) ListStudents(ctx context.Context, user *models.User, courseID string) ([]StudentProgress, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if err := validateOptionalID(courseID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListRoster(ctx, user.ID, courseID, "")
	if err != nil {
		return nil, err
	}
	return groupRoster(rows), nil
}

// GetStudent returns one student's progress, videos and test attempts in the caller's courses.
func (s *StudentService) GetStudent(ctx context.Context, user *models.User, studentID string) (*StudentDetail, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(studentID) {
		return nil, ErrStudentNotFound
	}
	rows, err := s.repo.ListRoster(ctx, user.ID, "", studentID)
	if err != nil {
		return nil, err
	}
	students := groupRoster(rows)
	if len(students) == 0 {
		return nil, ErrStudentNotFound
	}
	videos, err := s.repo.ListVideos(ctx, user.ID, studentID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.repo.ListAttempts(ctx, user.ID, studentID, studentAttemptsLimit)
	if err != nil {
		return nil, err
	}
	return &StudentDetail{StudentProgress: students[0], Videos: videos, Attempts: attempts}, nil
}

// groupRoster folds roster rows (sorted by student) into one entry per student with their totals.
func groupRoster(rows []models.RosterRow) []StudentProgress {
	students := []StudentProgress{}
	for _, row := range rows {
		if n := len(students); n == 0 || students[n-1].ID != row.StudentID {
			students = append(students, StudentProgress{
				ID:        row.StudentID,
				FirstName: row.FirstName,
				LastName:  row.LastName,
				Email:     row.Email,
				Courses:   []models.StudentCourse{},
			})
		}
		st := &students[len(students)-1]
		c := row.Course
		st.Courses = append(st.Courses, c)
		st.Totals.Add(c.CourseProgress)
		st.FlashcardsTotal += c.FlashcardsTotal
		st.FlashcardsReviewed += c.FlashcardsReviewed
		if c.LastActiveAt != nil && (st.LastActiveAt == nil || c.LastActiveAt.After(*st.LastActiveAt)) {
			st.LastActiveAt = c.LastActiveAt
		}
	}
	for i := range students {
		students[i].Totals.Finish()
	}
	return students
}

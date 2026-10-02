package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxTitleLength            = 200
	maxSlugLength             = 200
	maxShortDescriptionLength = 500
	maxDescriptionLength      = 10000
)

var (
	// ErrSlugTaken means another course already uses this slug.
	ErrSlugTaken = errors.New("a course with this slug already exists")
	// ErrCourseNotFound means the course does not exist or does not belong to the user.
	ErrCourseNotFound = errors.New("course not found")
)

// slugPattern allows lowercase words joined by single dashes, like "neet-physics-2026".
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// CreateCourseInput is everything an admin sends to create a course.
type CreateCourseInput struct {
	ExamID           string
	InstructorID     string
	Title            string
	Slug             string
	ShortDescription string
	Description      string
	Status           string
	IsFree           bool
}

type CourseService struct {
	courseRepo *models.CourseRepository
}

func NewCourseService(courseRepo *models.CourseRepository) *CourseService {
	return &CourseService{
		courseRepo: courseRepo,
	}
}

// CreateCourse lets an admin create a course for an instructor.
func (s *CourseService) CreateCourse(ctx context.Context, user *models.User, input CreateCourseInput) (*models.Course, error) {

	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	course := &models.Course{
		ExamID:           strings.TrimSpace(input.ExamID),
		InstructorID:     strings.TrimSpace(input.InstructorID),
		Title:            strings.TrimSpace(input.Title),
		Slug:             strings.TrimSpace(input.Slug),
		ShortDescription: strings.TrimSpace(input.ShortDescription),
		Description:      strings.TrimSpace(input.Description),
		Status:           strings.TrimSpace(input.Status),
		IsFree:           input.IsFree,
	}
	if course.Status == "" {
		course.Status = "draft"
	}

	if err := validateCourse(course); err != nil {
		return nil, err
	}

	examExists, err := s.courseRepo.ExamExists(ctx, course.ExamID)
	if err != nil {
		return nil, err
	}
	if !examExists {
		return nil, fmt.Errorf("%w: exam not found", ErrInvalidInput)
	}

	instructorExists, err := s.courseRepo.InstructorExists(ctx, course.InstructorID)
	if err != nil {
		return nil, err
	}
	if !instructorExists {
		return nil, fmt.Errorf("%w: instructor not found", ErrInvalidInput)
	}

	err = s.courseRepo.CreateCourse(ctx, course)
	if isUniqueViolation(err) {
		return nil, ErrSlugTaken
	}
	if err != nil {
		return nil, err
	}
	return course, nil
}

// ListCourses lets an admin see all courses.
func (s *CourseService) ListCourses(ctx context.Context, user *models.User, limit, offset int) ([]models.Course, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	return s.courseRepo.ListCourses(ctx, limit, offset)
}

func validateCourse(c *models.Course) error {
	switch {
	case !uuidPattern.MatchString(c.ExamID):
		return fmt.Errorf("%w: examId must be a valid id", ErrInvalidInput)
	case !uuidPattern.MatchString(c.InstructorID):
		return fmt.Errorf("%w: instructorId must be a valid id", ErrInvalidInput)
	case c.Title == "":
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	case utf8.RuneCountInString(c.Title) > maxTitleLength:
		return fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxTitleLength)
	case len(c.Slug) > maxSlugLength || !slugPattern.MatchString(c.Slug):
		return fmt.Errorf("%w: slug must be lowercase letters, numbers and dashes, like \"neet-physics\"", ErrInvalidInput)
	case utf8.RuneCountInString(c.ShortDescription) > maxShortDescriptionLength:
		return fmt.Errorf("%w: shortDescription must be at most %d characters", ErrInvalidInput, maxShortDescriptionLength)
	case utf8.RuneCountInString(c.Description) > maxDescriptionLength:
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxDescriptionLength)
	case !isValidCourseStatus(c.Status):
		return fmt.Errorf("%w: status must be draft, published or archived", ErrInvalidInput)
	}
	return nil
}

// isUniqueViolation reports whether Postgres rejected a duplicate value (error code 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// UpdateCourseStatus lets an instructor change the status of a course they teach.
func (s *CourseService) UpdateCourseStatus(ctx context.Context, user *models.User, courseID, status string) (*models.Course, error) {
	if user.Role != models.RoleInstructor {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(courseID) {
		return nil, ErrCourseNotFound
	}
	status = strings.TrimSpace(status)
	if !isValidCourseStatus(status) {
		return nil, fmt.Errorf("%w: status must be draft, published or archived", ErrInvalidInput)
	}

	course, err := s.courseRepo.UpdateStatus(ctx, courseID, user.ID, status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	return course, nil
}

func isValidCourseStatus(status string) bool {
	return status == "draft" || status == "published" || status == "archived"
}

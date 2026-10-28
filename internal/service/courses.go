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
	InstructorID     string // empty = the admin creating it
	Title            string
	Slug             string
	ShortDescription string
	Description      string
	Status           string
	IsFree           bool
}

// UpdateCourseInput holds the course fields to change. Nil fields are left as they are.
type UpdateCourseInput struct {
	ExamID           *string
	Title            *string
	Slug             *string
	ShortDescription *string
	Description      *string
	IsFree           *bool
}

type CourseService struct {
	courseRepo *models.CourseRepository
}

func NewCourseService(courseRepo *models.CourseRepository) *CourseService {
	return &CourseService{
		courseRepo: courseRepo,
	}
}

// CreateCourse lets an admin create a course. The admin owns it unless they name another
// instructor (or admin) as its instructor.
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
	if course.InstructorID == "" {
		course.InstructorID = user.ID
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
		return nil, fmt.Errorf("%w: instructor not found (must be an active instructor or admin)", ErrInvalidInput)
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

// GetCourse returns one course with the user's Access to it. Admins see any course and the owner sees
// theirs (drafts included). Every other logged-in user sees a published course in full when enrolled.
// When they are not, a public course is open in full if it is free and as a preview of its free content
// otherwise, unless they are enrolled in another batch of it. Anything else is ErrCourseNotFound.
func (s *CourseService) GetCourse(ctx context.Context, user *models.User, courseID string) (*models.Course, error) {
	if !uuidPattern.MatchString(courseID) {
		return nil, ErrCourseNotFound
	}
	course, err := s.courseRepo.GetCourse(ctx, courseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}

	if user.CanTeach() && course.InstructorID == user.ID {
		course.Access, course.Tier = "owner", models.TierUltra
		return course, nil
	}
	if course.Status != "published" {
		if user.Role == models.RoleAdmin {
			return course, nil // admins may read any course, but its content stays the owner's
		}
		return nil, ErrCourseNotFound
	}
	tier, err := s.courseRepo.EnrollmentTier(ctx, course.ID, user.ID)
	if err != nil {
		return nil, err
	}
	if tier != "" {
		course.Access, course.Tier = "full", tier
		return course, nil
	}

	// Not enrolled: a private course, or another batch of a course they are enrolled in, stays hidden.
	previewable, err := s.courseRepo.Previewable(ctx, course.ID, user.ID)
	if err != nil {
		return nil, err
	}
	if !previewable {
		if user.Role == models.RoleAdmin {
			return course, nil
		}
		return nil, ErrCourseNotFound
	}
	switch {
	case course.IsFree:
		course.Access, course.Tier = "full", models.TierUltra
	default:
		course.Access, course.Tier = "preview", models.TierFree
	}
	return course, nil
}

// ListCatalog returns every published course with how much of it is free to preview, for any
// logged-in user. freeOnly keeps just the courses with free content.
func (s *CourseService) ListCatalog(ctx context.Context, user *models.User, freeOnly bool, limit, offset int) ([]models.CatalogCourse, error) {
	return s.courseRepo.ListCatalog(ctx, user.ID, freeOnly, limit, offset)
}

// ListMyCourses returns the courses an instructor or admin owns (drafts included), or the published
// courses a student has a valid enrollment in.
func (s *CourseService) ListMyCourses(ctx context.Context, user *models.User, limit, offset int) ([]models.Course, error) {
	if user.CanTeach() {
		return s.courseRepo.ListByInstructor(ctx, user.ID, limit, offset)
	}
	return s.courseRepo.ListEnrolled(ctx, user.ID, limit, offset)
}

// UpdateCourse lets the course owner (instructor or admin) edit its details.
func (s *CourseService) UpdateCourse(ctx context.Context, user *models.User, courseID string, in UpdateCourseInput) (*models.Course, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	course, err := s.ownedCourse(ctx, user, courseID)
	if err != nil {
		return nil, err
	}

	if in.ExamID != nil {
		course.ExamID = strings.TrimSpace(*in.ExamID)
	}
	if in.Title != nil {
		course.Title = strings.TrimSpace(*in.Title)
	}
	if in.Slug != nil {
		course.Slug = strings.TrimSpace(*in.Slug)
	}
	if in.ShortDescription != nil {
		course.ShortDescription = strings.TrimSpace(*in.ShortDescription)
	}
	if in.Description != nil {
		course.Description = strings.TrimSpace(*in.Description)
	}
	if in.IsFree != nil {
		course.IsFree = *in.IsFree
	}
	if err := validateCourse(course); err != nil {
		return nil, err
	}

	if in.ExamID != nil {
		examExists, err := s.courseRepo.ExamExists(ctx, course.ExamID)
		if err != nil {
			return nil, err
		}
		if !examExists {
			return nil, fmt.Errorf("%w: exam not found", ErrInvalidInput)
		}
	}

	err = s.courseRepo.UpdateCourse(ctx, course, user.ID)
	if isUniqueViolation(err) {
		return nil, ErrSlugTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	return course, nil
}

// DeleteCourse lets the course owner (instructor or admin) soft-delete it. Enrollments are kept as
// they are, but every content and feed query skips deleted courses, so students lose access.
func (s *CourseService) DeleteCourse(ctx context.Context, user *models.User, courseID string) error {
	if !user.CanTeach() {
		return ErrForbidden
	}
	if !uuidPattern.MatchString(courseID) {
		return ErrCourseNotFound
	}
	err := s.courseRepo.SoftDelete(ctx, courseID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCourseNotFound
	}
	return err
}

// ownedCourse loads a course the user owns. Anything else looks like a missing course.
func (s *CourseService) ownedCourse(ctx context.Context, user *models.User, courseID string) (*models.Course, error) {
	if !uuidPattern.MatchString(courseID) {
		return nil, ErrCourseNotFound
	}
	course, err := s.courseRepo.GetCourse(ctx, courseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	if course.InstructorID != user.ID {
		return nil, ErrCourseNotFound
	}
	return course, nil
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

// UpdateCourseStatus lets the course owner (instructor or admin) change its status.
func (s *CourseService) UpdateCourseStatus(ctx context.Context, user *models.User, courseID, status string) (*models.Course, error) {
	if !user.CanTeach() {
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

// UpdateCourseVisibility lets the course owner (instructor or admin) make a course public or private.
// Making it private hides it from everyone who is not enrolled yet; enrolled students keep their access
// until their enrollment ends.
func (s *CourseService) UpdateCourseVisibility(ctx context.Context, user *models.User, courseID, visibility string) (*models.Course, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(courseID) {
		return nil, ErrCourseNotFound
	}
	visibility = strings.TrimSpace(visibility)
	if visibility != models.VisibilityPublic && visibility != models.VisibilityPrivate {
		return nil, fmt.Errorf("%w: visibility must be public or private", ErrInvalidInput)
	}

	course, err := s.courseRepo.UpdateVisibility(ctx, courseID, user.ID, visibility)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	return course, nil
}

// NewBatchInput names the next batch of a course. Empty fields get a default: the source's title, and
// the source's slug ending in "-batch-<number>".
type NewBatchInput struct {
	Title string
	Slug  string
}

// CreateNextBatch lets the course owner (instructor or admin) start the next batch of their course: a
// draft copy with the same lessons, PDF notes, offline recordings, tests and flashcards, but no Meet
// recordings and no students. The source course is left as it is; the owner usually makes it private next.
func (s *CourseService) CreateNextBatch(ctx context.Context, user *models.User, courseID string, in NewBatchInput) (*models.Course, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if _, err := s.ownedCourse(ctx, user, courseID); err != nil {
		return nil, err
	}

	in.Title = strings.TrimSpace(in.Title)
	in.Slug = strings.TrimSpace(in.Slug)
	switch {
	case utf8.RuneCountInString(in.Title) > maxTitleLength:
		return nil, fmt.Errorf("%w: title must be at most %d characters", ErrInvalidInput, maxTitleLength)
	case in.Slug != "" && (len(in.Slug) > maxSlugLength || !slugPattern.MatchString(in.Slug)):
		return nil, fmt.Errorf("%w: slug must be lowercase letters, numbers and dashes, like \"neet-physics\"", ErrInvalidInput)
	}

	course, err := s.courseRepo.CopyAsNextBatch(ctx, courseID, models.NewBatch{Title: in.Title, Slug: in.Slug})
	if isUniqueViolation(err) {
		return nil, ErrSlugTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, err
	}
	return course, nil
}

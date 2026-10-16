package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

var (
	// ErrAlreadyEnrolled means the student already has an enrollment for this course.
	ErrAlreadyEnrolled = errors.New("student is already enrolled in this course")
	// ErrEnrollmentNotFound means no enrollment has this id.
	ErrEnrollmentNotFound = errors.New("enrollment not found")
)

const maxEnrollmentMonths = 60

// CreateEnrollmentInput is what an admin sends to enroll a student.
type CreateEnrollmentInput struct {
	UserID   string
	CourseID string
	Months   int    // how long the enrollment lasts
	Tier     string // plus, premium or ultra
}

type EnrollmentService struct {
	enrollmentRepo *models.EnrollmentRepository
}

func NewEnrollmentService(enrollmentRepo *models.EnrollmentRepository) *EnrollmentService {
	return &EnrollmentService{
		enrollmentRepo: enrollmentRepo,
	}
}

// CreateEnrollment lets an admin enroll a student in a published course.
func (s *EnrollmentService) CreateEnrollment(ctx context.Context, user *models.User, input CreateEnrollmentInput) (*models.Enrollment, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}

	e := &models.Enrollment{
		UserID:   strings.TrimSpace(input.UserID),
		CourseID: strings.TrimSpace(input.CourseID),
	}
	if !uuidPattern.MatchString(e.UserID) {
		return nil, fmt.Errorf("%w: userId must be a valid id", ErrInvalidInput)
	}
	if !uuidPattern.MatchString(e.CourseID) {
		return nil, fmt.Errorf("%w: courseId must be a valid id", ErrInvalidInput)
	}
	e.Tier = models.Tier(strings.TrimSpace(input.Tier))
	if !e.Tier.IsEnrollmentTier() {
		return nil, fmt.Errorf("%w: tier must be plus, premium or ultra", ErrInvalidInput)
	}
	if input.Months < 1 || input.Months > maxEnrollmentMonths {
		return nil, fmt.Errorf("%w: months must be between 1 and %d", ErrInvalidInput, maxEnrollmentMonths)
	}
	// The server decides the expiry date, never the client.
	expiresAt := time.Now().AddDate(0, input.Months, 0)
	e.ExpiresAt = &expiresAt

	studentExists, err := s.enrollmentRepo.StudentExists(ctx, e.UserID)
	if err != nil {
		return nil, err
	}
	if !studentExists {
		return nil, fmt.Errorf("%w: student not found", ErrInvalidInput)
	}

	published, err := s.enrollmentRepo.CoursePublished(ctx, e.CourseID)
	if err != nil {
		return nil, err
	}
	if !published {
		return nil, fmt.Errorf("%w: course not found or not published", ErrInvalidInput)
	}

	err = s.enrollmentRepo.CreateEnrollment(ctx, e)
	if isUniqueViolation(err) {
		return nil, ErrAlreadyEnrolled
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEnrollments lets an admin see enrollments, optionally for one course or one student.
func (s *EnrollmentService) ListEnrollments(ctx context.Context, user *models.User, courseID, userID string, limit, offset int) ([]models.Enrollment, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if courseID != "" && !uuidPattern.MatchString(courseID) {
		return nil, fmt.Errorf("%w: courseId must be a valid id", ErrInvalidInput)
	}
	if userID != "" && !uuidPattern.MatchString(userID) {
		return nil, fmt.Errorf("%w: userId must be a valid id", ErrInvalidInput)
	}
	return s.enrollmentRepo.ListEnrollments(ctx, courseID, userID, limit, offset)
}

// UpdateEnrollmentInput holds the enrollment fields to change. Nil fields are left as they are.
type UpdateEnrollmentInput struct {
	Status *string // active, completed, expired or cancelled
	Tier   *string // plus, premium or ultra
}

// UpdateEnrollment lets an admin change the status (for example cancel) and/or the tier of an enrollment.
func (s *EnrollmentService) UpdateEnrollment(ctx context.Context, user *models.User, enrollmentID string, in UpdateEnrollmentInput) (*models.Enrollment, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(enrollmentID) {
		return nil, ErrEnrollmentNotFound
	}
	if in.Status == nil && in.Tier == nil {
		return nil, fmt.Errorf("%w: send a status or a tier to change", ErrInvalidInput)
	}
	var status *string
	if in.Status != nil {
		st := strings.TrimSpace(*in.Status)
		if st != "active" && st != "completed" && st != "expired" && st != "cancelled" {
			return nil, fmt.Errorf("%w: status must be active, completed, expired or cancelled", ErrInvalidInput)
		}
		status = &st
	}
	var tier *models.Tier
	if in.Tier != nil {
		t := models.Tier(strings.TrimSpace(*in.Tier))
		if !t.IsEnrollmentTier() {
			return nil, fmt.Errorf("%w: tier must be plus, premium or ultra", ErrInvalidInput)
		}
		tier = &t
	}

	e, err := s.enrollmentRepo.Update(ctx, enrollmentID, status, tier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

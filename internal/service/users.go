package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

type UserService struct {
	userRepo *models.UserRepository
}

func NewUserService(userRepo *models.UserRepository) *UserService {
	return &UserService{
		userRepo: userRepo,
	}
}

// ListUsers lets an admin list active users, optionally of one role, for instructor and student pickers.
func (s *UserService) ListUsers(ctx context.Context, user *models.User, role string, limit, offset int) ([]models.UserSummary, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	role = strings.TrimSpace(role)
	switch models.Role(role) {
	case "", models.RoleStudent, models.RoleInstructor, models.RoleAdmin:
	default:
		return nil, fmt.Errorf("%w: role must be student, instructor or admin", ErrInvalidInput)
	}
	return s.userRepo.ListUsers(ctx, role, limit, offset)
}

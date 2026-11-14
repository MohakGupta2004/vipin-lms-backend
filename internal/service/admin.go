package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

const (
	defaultAdminDays  = 3
	minAdminDays      = 1
	maxAdminDays      = 30
	defaultAdminLimit = 50
	maxAdminLimit     = 200
)

// adminPeopleViews are the lists of students the admin students page can show.
var adminPeopleViews = []string{"new", "never_logged_in", "not_enrolled", "recently_enrolled"}

type AdminService struct {
	repo *models.AdminRepository
}

func NewAdminService(repo *models.AdminRepository) *AdminService {
	return &AdminService{repo: repo}
}

// PeopleFilter is what the admin asks for: which list of students, how many days back, which page.
type PeopleFilter struct {
	View   string
	Days   int
	Limit  int
	Offset int
}

// ListPeople lets an admin list students for one view, each with all of their enrollments.
func (s *AdminService) ListPeople(ctx context.Context, user *models.User, filter PeopleFilter) (*models.PeoplePage, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	filter.View = strings.TrimSpace(filter.View)
	if !isValidPeopleView(filter.View) {
		return nil, fmt.Errorf("%w: view must be one of new, never_logged_in, not_enrolled or recently_enrolled", ErrInvalidInput)
	}
	if filter.Days == 0 {
		filter.Days = defaultAdminDays
	}
	if filter.Days < minAdminDays || filter.Days > maxAdminDays {
		return nil, fmt.Errorf("%w: days must be between %d and %d", ErrInvalidInput, minAdminDays, maxAdminDays)
	}
	if filter.Limit == 0 {
		filter.Limit = defaultAdminLimit
	}
	if filter.Limit < 1 || filter.Limit > maxAdminLimit {
		return nil, fmt.Errorf("%w: limit must be between %d and %d", ErrInvalidInput, 1, maxAdminLimit)
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must be 0 or more", ErrInvalidInput)
	}
	return s.repo.ListPeople(ctx, filter.View, filter.Days, filter.Limit, filter.Offset, time.Now().UTC())
}

// GetDashboard lets an admin read the home screen numbers of the last days.
func (s *AdminService) GetDashboard(ctx context.Context, user *models.User, days int) (*models.AdminDashboard, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if days == 0 {
		days = defaultAdminDays
	}
	if days < minAdminDays || days > maxAdminDays {
		return nil, fmt.Errorf("%w: days must be between %d and %d", ErrInvalidInput, minAdminDays, maxAdminDays)
	}
	return s.repo.GetDashboard(ctx, days, time.Now().UTC())
}

func isValidPeopleView(view string) bool {
	for _, allowed := range adminPeopleViews {
		if view == allowed {
			return true
		}
	}
	return false
}

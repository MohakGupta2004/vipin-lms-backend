package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

var ErrSiteVisitorNotFound = errors.New("visitor not found")

const (
	defaultSiteDays        = 30
	minSiteDays            = 1
	maxSiteDays            = 365
	defaultSiteLimit       = 50
	maxSiteLimit           = 200
	defaultSiteEventsLimit = 100
	maxSiteEventsLimit     = 500
)

var siteLeadKinds = []string{"masterclass", "diagnostic", "contact", "firms", "newsletter", "interest"}

type SiteService struct {
	repo *models.SiteRepository
}

func NewSiteService(repo *models.SiteRepository) *SiteService {
	return &SiteService{repo: repo}
}

func (s *SiteService) GetOverview(ctx context.Context, user *models.User, days int) (*models.SiteOverview, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if days == 0 {
		days = defaultSiteDays
	}
	if days < minSiteDays || days > maxSiteDays {
		return nil, fmt.Errorf("%w: days must be between %d and %d", ErrInvalidInput, minSiteDays, maxSiteDays)
	}
	overview, err := s.repo.GetOverview(ctx, days, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	overview.LeadsByKind = fillLeadKinds(overview.LeadsByKind)
	return overview, nil
}

type SiteLeadsFilter struct {
	Kind   string
	Course string
	Q      string
	Limit  int
	Offset int
}

func (s *SiteService) ListLeads(ctx context.Context, user *models.User, filter SiteLeadsFilter) (*models.SiteLeadsPage, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	filter.Kind = strings.TrimSpace(filter.Kind)
	filter.Course = strings.TrimSpace(filter.Course)
	filter.Q = strings.TrimSpace(filter.Q)
	if filter.Limit == 0 {
		filter.Limit = defaultSiteLimit
	}
	if filter.Limit < 1 || filter.Limit > maxSiteLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxSiteLimit)
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must be 0 or more", ErrInvalidInput)
	}
	if filter.Kind != "" && !isValidLeadKind(filter.Kind) {
		return nil, fmt.Errorf("%w: kind must be one of masterclass, diagnostic, contact, firms, newsletter or interest", ErrInvalidInput)
	}
	return s.repo.ListLeads(ctx, filter.Kind, filter.Course, filter.Q, filter.Limit, filter.Offset)
}

func (s *SiteService) ListLeadEmails(ctx context.Context, user *models.User, kind, course string) (*models.SiteLeadEmails, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	kind = strings.TrimSpace(kind)
	course = strings.TrimSpace(course)
	if !isValidLeadKind(kind) {
		return nil, fmt.Errorf("%w: kind must be one of masterclass, diagnostic, contact, firms, newsletter or interest", ErrInvalidInput)
	}
	return s.repo.ListLeadEmails(ctx, kind, course)
}

func (s *SiteService) ListLeadPhones(ctx context.Context, user *models.User, kind, course string) (*models.SiteLeadPhones, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	kind = strings.TrimSpace(kind)
	course = strings.TrimSpace(course)
	if kind != "" && !isValidLeadKind(kind) {
		return nil, fmt.Errorf("%w: kind must be one of masterclass, diagnostic, contact, firms, newsletter or interest", ErrInvalidInput)
	}
	return s.repo.ListLeadPhones(ctx, kind, course)
}

type SiteVisitorsFilter struct {
	Returning bool
	Limit     int
	Offset    int
}

func (s *SiteService) ListVisitors(ctx context.Context, user *models.User, filter SiteVisitorsFilter) (*models.SiteVisitorsPage, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if filter.Limit == 0 {
		filter.Limit = defaultSiteLimit
	}
	if filter.Limit < 1 || filter.Limit > maxSiteLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxSiteLimit)
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must be 0 or more", ErrInvalidInput)
	}
	return s.repo.ListVisitors(ctx, filter.Returning, filter.Limit, filter.Offset)
}

func (s *SiteService) ListVisitorEvents(ctx context.Context, user *models.User, visitorID string, limit int) ([]models.SiteEvent, error) {
	if user.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(visitorID) {
		return nil, ErrSiteVisitorNotFound
	}
	if limit == 0 {
		limit = defaultSiteEventsLimit
	}
	if limit < 1 || limit > maxSiteEventsLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxSiteEventsLimit)
	}
	exists, err := s.repo.VisitorExists(ctx, visitorID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrSiteVisitorNotFound
	}
	return s.repo.ListVisitorEvents(ctx, visitorID, limit)
}

func isValidLeadKind(kind string) bool {
	for _, allowed := range siteLeadKinds {
		if kind == allowed {
			return true
		}
	}
	return false
}

func fillLeadKinds(items []models.SiteKeyCount) []models.SiteKeyCount {
	byKey := map[string]int{}
	for _, item := range items {
		byKey[item.Key] = item.Count
	}
	result := make([]models.SiteKeyCount, 0, len(siteLeadKinds))
	for _, kind := range siteLeadKinds {
		result = append(result, models.SiteKeyCount{Key: kind, Count: byKey[kind]})
	}
	return result
}

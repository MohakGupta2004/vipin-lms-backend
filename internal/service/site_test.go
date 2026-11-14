package service

import (
	"context"
	"errors"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestSiteServiceRejectsNonAdmin(t *testing.T) {
	s := NewSiteService(nil)
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}

	if _, err := s.GetOverview(context.Background(), student, 30); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetOverview err = %v, want ErrForbidden", err)
	}
	if _, err := s.ListLeads(context.Background(), student, SiteLeadsFilter{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListLeads err = %v, want ErrForbidden", err)
	}
	if _, err := s.ListLeadEmails(context.Background(), student, "masterclass", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListLeadEmails err = %v, want ErrForbidden", err)
	}
	if _, err := s.ListVisitors(context.Background(), student, SiteVisitorsFilter{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListVisitors err = %v, want ErrForbidden", err)
	}
	if _, err := s.ListVisitorEvents(context.Background(), student, student.ID, 100); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListVisitorEvents err = %v, want ErrForbidden", err)
	}
}

func TestSiteServiceValidatesDaysAndKinds(t *testing.T) {
	s := NewSiteService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000002", Role: models.RoleAdmin}

	if _, err := s.GetOverview(context.Background(), admin, -1); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("GetOverview err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.GetOverview(context.Background(), admin, 366); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("GetOverview err = %v, want ErrInvalidInput", err)
	}

	if _, err := s.ListLeads(context.Background(), admin, SiteLeadsFilter{Kind: "bad", Limit: 50}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListLeads err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListLeadEmails(context.Background(), admin, "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListLeadEmails err = %v, want ErrInvalidInput", err)
	}
}

func TestSiteServiceValidatesLimitAndOffsets(t *testing.T) {
	s := NewSiteService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000003", Role: models.RoleAdmin}

	if _, err := s.ListLeads(context.Background(), admin, SiteLeadsFilter{Limit: 201}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListLeads limit err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListLeads(context.Background(), admin, SiteLeadsFilter{Limit: 50, Offset: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListLeads offset err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListVisitors(context.Background(), admin, SiteVisitorsFilter{Limit: 201}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListVisitors limit err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListVisitors(context.Background(), admin, SiteVisitorsFilter{Limit: 50, Offset: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListVisitors offset err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListVisitorEvents(context.Background(), admin, admin.ID, 501); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListVisitorEvents err = %v, want ErrInvalidInput", err)
	}
}

func TestFillLeadKinds(t *testing.T) {
	got := fillLeadKinds([]models.SiteKeyCount{{Key: "masterclass", Count: 3}, {Key: "contact", Count: 1}})
	if len(got) != 6 {
		t.Fatalf("got %d kinds, want 6", len(got))
	}
	if got[0].Key != "masterclass" || got[0].Count != 3 {
		t.Errorf("masterclass = %#v, want count 3", got[0])
	}
	if got[2].Key != "contact" || got[2].Count != 1 {
		t.Errorf("contact = %#v, want count 1", got[2])
	}
	if got[1].Count != 0 {
		t.Errorf("diagnostic count = %d, want 0", got[1].Count)
	}
}

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestAdminServiceRejectsNonAdmin(t *testing.T) {
	s := NewAdminService(nil)
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}

	if _, err := s.ListPeople(context.Background(), student, PeopleFilter{View: "new", Days: 3}); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListPeople err = %v, want ErrForbidden", err)
	}
	if _, err := s.GetDashboard(context.Background(), student, 3); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetDashboard err = %v, want ErrForbidden", err)
	}
}

func TestAdminServiceValidatesPeopleView(t *testing.T) {
	s := NewAdminService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000002", Role: models.RoleAdmin}

	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "", Days: 3}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople empty view err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "bad", Days: 3}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople bad view err = %v, want ErrInvalidInput", err)
	}
}

func TestAdminServiceValidatesDays(t *testing.T) {
	s := NewAdminService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000003", Role: models.RoleAdmin}

	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "new", Days: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople days err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "new", Days: 31}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople days err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.GetDashboard(context.Background(), admin, -1); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("GetDashboard days err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.GetDashboard(context.Background(), admin, 31); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("GetDashboard days err = %v, want ErrInvalidInput", err)
	}
}

func TestAdminServiceValidatesLimitAndOffset(t *testing.T) {
	s := NewAdminService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000004", Role: models.RoleAdmin}

	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "new", Days: 3, Limit: 201}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople limit err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListPeople(context.Background(), admin, PeopleFilter{View: "new", Days: 3, Limit: 50, Offset: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ListPeople offset err = %v, want ErrInvalidInput", err)
	}
}

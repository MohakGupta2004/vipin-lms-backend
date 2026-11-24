package service

import (
	"context"
	"errors"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func validDiagnosticInput() SaveDiagnosticResultInput {
	elements := []models.DiagnosticElementResult{}
	for i := 1; i <= 9; i++ {
		elements = append(elements, models.DiagnosticElementResult{
			Element:   i,
			Correct:   2,
			Total:     3,
			Points:    20,
			MaxPoints: 30,
		})
	}
	return SaveDiagnosticResultInput{
		Correct:  18,
		Total:    27,
		Elements: elements,
		Answers: []models.DiagnosticAnswer{
			{QuestionID: 1, Picked: 2, Confidence: "high"},
			{QuestionID: 2, Picked: -1, Confidence: "none"},
		},
	}
}

func TestDiagnosticServiceRejectsNonAdmin(t *testing.T) {
	s := NewDiagnosticService(nil)
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}

	if _, err := s.ListResults(context.Background(), student, DiagnosticResultsFilter{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListResults err = %v, want ErrForbidden", err)
	}
	if _, err := s.GetResult(context.Background(), student, "00000000-0000-0000-0000-000000000002"); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetResult err = %v, want ErrForbidden", err)
	}
}

func TestDiagnosticServiceValidatesResult(t *testing.T) {
	s := NewDiagnosticService(nil)
	user := &models.User{ID: "00000000-0000-0000-0000-000000000003", Role: models.RoleStudent}

	// Invalid inputs return before the repository is used, so a nil repo is fine here.
	badTotal := validDiagnosticInput()
	badTotal.Total = 0
	if _, err := s.SaveResult(context.Background(), user, badTotal); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("total 0 err = %v, want ErrInvalidInput", err)
	}

	badTotal = validDiagnosticInput()
	badTotal.Total = 101
	if _, err := s.SaveResult(context.Background(), user, badTotal); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("total 101 err = %v, want ErrInvalidInput", err)
	}

	badCorrect := validDiagnosticInput()
	badCorrect.Correct = -1
	if _, err := s.SaveResult(context.Background(), user, badCorrect); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("correct -1 err = %v, want ErrInvalidInput", err)
	}

	badCorrect = validDiagnosticInput()
	badCorrect.Correct = badCorrect.Total + 1
	if _, err := s.SaveResult(context.Background(), user, badCorrect); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("correct above total err = %v, want ErrInvalidInput", err)
	}

	fewElements := validDiagnosticInput()
	fewElements.Elements = fewElements.Elements[:8]
	if _, err := s.SaveResult(context.Background(), user, fewElements); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("8 elements err = %v, want ErrInvalidInput", err)
	}

	repeated := validDiagnosticInput()
	repeated.Elements[1].Element = repeated.Elements[0].Element
	if _, err := s.SaveResult(context.Background(), user, repeated); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("repeated element err = %v, want ErrInvalidInput", err)
	}

	outOfRange := validDiagnosticInput()
	outOfRange.Elements[0].Element = 10
	if _, err := s.SaveResult(context.Background(), user, outOfRange); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("element 10 err = %v, want ErrInvalidInput", err)
	}

	badElementScore := validDiagnosticInput()
	badElementScore.Elements[0].Correct = badElementScore.Elements[0].Total + 1
	if _, err := s.SaveResult(context.Background(), user, badElementScore); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("element correct above total err = %v, want ErrInvalidInput", err)
	}

	badMaxPoints := validDiagnosticInput()
	badMaxPoints.Elements[0].MaxPoints = -1
	if _, err := s.SaveResult(context.Background(), user, badMaxPoints); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("element maxPoints -1 err = %v, want ErrInvalidInput", err)
	}

	badPicked := validDiagnosticInput()
	badPicked.Answers[0].Picked = 4
	if _, err := s.SaveResult(context.Background(), user, badPicked); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("picked 4 err = %v, want ErrInvalidInput", err)
	}

	badConfidence := validDiagnosticInput()
	badConfidence.Answers[0].Confidence = "sure"
	if _, err := s.SaveResult(context.Background(), user, badConfidence); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad confidence err = %v, want ErrInvalidInput", err)
	}

	tooManyAnswers := validDiagnosticInput()
	tooManyAnswers.Answers = []models.DiagnosticAnswer{}
	for i := 0; i < 101; i++ {
		tooManyAnswers.Answers = append(tooManyAnswers.Answers, models.DiagnosticAnswer{QuestionID: i, Picked: 0, Confidence: "low"})
	}
	if _, err := s.SaveResult(context.Background(), user, tooManyAnswers); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("101 answers err = %v, want ErrInvalidInput", err)
	}
}

func TestDiagnosticServiceAcceptsValidResult(t *testing.T) {
	err := validateDiagnosticResult(validDiagnosticInput())
	if err != nil {
		t.Errorf("valid result err = %v, want nil", err)
	}
}

func TestDiagnosticServiceValidatesListFilters(t *testing.T) {
	s := NewDiagnosticService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000004", Role: models.RoleAdmin}

	if _, err := s.ListResults(context.Background(), admin, DiagnosticResultsFilter{Limit: 201}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("limit 201 err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListResults(context.Background(), admin, DiagnosticResultsFilter{Limit: 50, Offset: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("offset -1 err = %v, want ErrInvalidInput", err)
	}
}

func TestDiagnosticServiceRejectsUnknownID(t *testing.T) {
	s := NewDiagnosticService(nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000005", Role: models.RoleAdmin}

	if _, err := s.GetResult(context.Background(), admin, "not-a-uuid"); !errors.Is(err, ErrDiagnosticResultNotFound) {
		t.Errorf("GetResult bad id err = %v, want ErrDiagnosticResultNotFound", err)
	}
}

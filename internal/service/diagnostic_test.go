package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/email"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func validDiagnosticInput() SaveDiagnosticResultInput {
	return SaveDiagnosticResultInput{
		Answers: []models.DiagnosticAnswer{
			{QuestionID: 1, Picked: 2, Confidence: "high"},
			{QuestionID: 2, Picked: -1, Confidence: "none"},
		},
	}
}

// diagnosticBank is a small test: question i is in element (i+1)/2 and its answer is option i%4.
func diagnosticBank(n int) []models.DiagnosticQuestionKey {
	questions := []models.DiagnosticQuestionKey{}
	for i := 1; i <= n; i++ {
		questions = append(questions, models.DiagnosticQuestionKey{
			ID:      i,
			Element: (i + 1) / 2,
			Options: []string{"a", "b", "c", "d"},
			Answer:  i % 4,
		})
	}
	return questions
}

func TestDiagnosticServiceRejectsNonAdmin(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}

	if _, err := s.ListResults(context.Background(), student, DiagnosticResultsFilter{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListResults err = %v, want ErrForbidden", err)
	}
	if _, err := s.GetResult(context.Background(), student, "00000000-0000-0000-0000-000000000002"); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetResult err = %v, want ErrForbidden", err)
	}
}

func TestDiagnosticServiceValidatesAnswers(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	user := &models.User{ID: "00000000-0000-0000-0000-000000000003", Role: models.RoleStudent}

	// Invalid answers return before the repository is used, so a nil repo is fine here.
	badPicked := validDiagnosticInput()
	badPicked.Answers[0].Picked = -2
	if _, err := s.SaveResult(context.Background(), user, badPicked); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("picked -2 err = %v, want ErrInvalidInput", err)
	}

	badPicked = validDiagnosticInput()
	badPicked.Answers[0].Picked = 10
	if _, err := s.SaveResult(context.Background(), user, badPicked); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("picked 10 err = %v, want ErrInvalidInput", err)
	}

	badConfidence := validDiagnosticInput()
	badConfidence.Answers[0].Confidence = "sure"
	if _, err := s.SaveResult(context.Background(), user, badConfidence); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad confidence err = %v, want ErrInvalidInput", err)
	}

	repeated := validDiagnosticInput()
	repeated.Answers[1].QuestionID = repeated.Answers[0].QuestionID
	if _, err := s.SaveResult(context.Background(), user, repeated); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("repeated question err = %v, want ErrInvalidInput", err)
	}

	tooManyAnswers := validDiagnosticInput()
	tooManyAnswers.Answers = []models.DiagnosticAnswer{}
	for i := 0; i < 101; i++ {
		tooManyAnswers.Answers = append(tooManyAnswers.Answers, models.DiagnosticAnswer{QuestionID: i, Picked: 0, Confidence: "low"})
	}
	if _, err := s.SaveResult(context.Background(), user, tooManyAnswers); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("101 answers err = %v, want ErrInvalidInput", err)
	}

	if err := validateDiagnosticAnswers(validDiagnosticInput().Answers); err != nil {
		t.Errorf("valid answers err = %v, want nil", err)
	}
}

func TestDiagnosticServiceRejectsUnknownTestSlug(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	user := &models.User{ID: "00000000-0000-0000-0000-000000000006", Role: models.RoleStudent}

	for _, slug := range []string{"../admin", "CIRE", "cire_advanced", "-cire", "cire-"} {
		if _, err := s.GetTest(context.Background(), user, slug); !errors.Is(err, ErrDiagnosticTestNotFound) {
			t.Errorf("GetTest(%q) err = %v, want ErrDiagnosticTestNotFound", slug, err)
		}
	}
}

func TestScoreDiagnostic(t *testing.T) {
	bank := diagnosticBank(4) // answers: q1=1, q2=2, q3=3, q4=0; elements: q1,q2 -> 1, q3,q4 -> 2
	score, err := scoreDiagnostic(bank, []models.DiagnosticAnswer{
		{QuestionID: 1, Picked: 1, Confidence: "high"},   // right, confident: +2
		{QuestionID: 2, Picked: 2, Confidence: "medium"}, // right: +1
		{QuestionID: 3, Picked: 0, Confidence: "high"},   // wrong: -1
		// q4 left out: skipped, 0
	})
	if err != nil {
		t.Fatalf("scoreDiagnostic err = %v", err)
	}
	if score.Correct != 2 || score.Total != 4 {
		t.Errorf("correct/total = %d/%d, want 2/4", score.Correct, score.Total)
	}
	want := []models.DiagnosticElementResult{
		{Element: 1, Correct: 2, Total: 2, Points: 3, MaxPoints: 4},
		{Element: 2, Correct: 0, Total: 2, Points: -1, MaxPoints: 4},
	}
	if len(score.Elements) != len(want) {
		t.Fatalf("elements = %+v, want %+v", score.Elements, want)
	}
	for i := range want {
		if score.Elements[i] != want[i] {
			t.Errorf("element %d = %+v, want %+v", i+1, score.Elements[i], want[i])
		}
	}
	if len(score.Answers) != 4 || score.Answers[3] != (models.DiagnosticAnswer{QuestionID: 4, Picked: -1, Confidence: "none"}) {
		t.Errorf("answers = %+v, want 4 with q4 skipped", score.Answers)
	}
}

func TestScoreDiagnosticRejectsAnswersOutsideTheTest(t *testing.T) {
	bank := diagnosticBank(4)
	if _, err := scoreDiagnostic(bank, []models.DiagnosticAnswer{{QuestionID: 5, Picked: 0, Confidence: "low"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown question err = %v, want ErrInvalidInput", err)
	}
	if _, err := scoreDiagnostic(bank, []models.DiagnosticAnswer{{QuestionID: 1, Picked: 4, Confidence: "low"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("picked past the options err = %v, want ErrInvalidInput", err)
	}
}

func TestDiagnosticServiceValidatesListFilters(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000004", Role: models.RoleAdmin}

	if _, err := s.ListResults(context.Background(), admin, DiagnosticResultsFilter{Limit: 201}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("limit 201 err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ListResults(context.Background(), admin, DiagnosticResultsFilter{Limit: 50, Offset: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("offset -1 err = %v, want ErrInvalidInput", err)
	}
}

func TestDiagnosticServiceRejectsUnknownID(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	admin := &models.User{ID: "00000000-0000-0000-0000-000000000005", Role: models.RoleAdmin}

	if _, err := s.GetResult(context.Background(), admin, "not-a-uuid"); !errors.Is(err, ErrDiagnosticResultNotFound) {
		t.Errorf("GetResult bad id err = %v, want ErrDiagnosticResultNotFound", err)
	}
}

func TestDiagnosticServiceEmailResultChecks(t *testing.T) {
	s := NewDiagnosticService(nil, nil)
	id := "00000000-0000-0000-0000-000000000002"
	student := &models.User{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleStudent}
	if _, err := s.EmailResult(context.Background(), student, id, EmailDiagnosticResultInput{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("student err = %v, want ErrForbidden", err)
	}

	admin := &models.User{ID: "00000000-0000-0000-0000-000000000003", Role: models.RoleAdmin}
	for _, to := range []string{"not-an-email", "Asha <a@b.co>", "a@b.co, c@d.co", "a@b.co\r\nBcc: x@y.co"} {
		if _, err := s.EmailResult(context.Background(), admin, id, EmailDiagnosticResultInput{To: to}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("to %q err = %v, want ErrInvalidInput", to, err)
		}
	}
	long := strings.Repeat("é", maxDiagnosticNoteRunes+1)
	if _, err := s.EmailResult(context.Background(), admin, id, EmailDiagnosticResultInput{Note: long}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("long note err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.EmailResult(context.Background(), admin, "not-a-uuid", EmailDiagnosticResultInput{}); !errors.Is(err, ErrDiagnosticResultNotFound) {
		t.Errorf("bad id err = %v, want ErrDiagnosticResultNotFound", err)
	}

	to, note, err := validateDiagnosticEmailInput(EmailDiagnosticResultInput{To: "  a@b.co ", Note: "  hi \n"})
	if err != nil || to != "a@b.co" || note != "hi" {
		t.Errorf("trimmed = %q, %q, %v", to, note, err)
	}
}

func TestDiagnosticEmailData(t *testing.T) {
	result := &models.AdminDiagnosticResultDetail{
		AdminDiagnosticResult: models.AdminDiagnosticResult{
			FirstName: "Asha",
			TestSlug:  DefaultDiagnosticTestSlug,
			TestTitle: "CIRE skill diagnostic",
			Correct:   5,
			Total:     12,
			Elements: []models.DiagnosticElementResult{
				{Element: 1, Correct: 1, Total: 4, Points: 0, MaxPoints: 8},  // 0%
				{Element: 2, Correct: 4, Total: 4, Points: 6, MaxPoints: 8},  // 75%
				{Element: 9, Correct: 0, Total: 4, Points: -4, MaxPoints: 8}, // clamped to 0%, fewer points
			},
		},
	}
	data := diagnosticEmailData(result, "note")
	if data.Strongest != "Prospective client relationships" || data.Weakest != "Ethics and conflicts" {
		t.Errorf("strongest/weakest = %q/%q", data.Strongest, data.Weakest)
	}
	if got := data.Elements[1]; got.Percent != 75 || got.Level != email.LevelStrong {
		t.Errorf("element 2 = %+v", got)
	}
	if got := data.Elements[2]; got.Percent != 0 || got.Level != email.LevelWeak {
		t.Errorf("element 9 = %+v", got)
	}
	if data.Note != "note" || data.FirstName != "Asha" {
		t.Errorf("data = %+v", data)
	}

	result.TestSlug = "unknown-test"
	if data := diagnosticEmailData(result, ""); data.Elements[0].Name != "Element 1" {
		t.Errorf("fallback name = %q", data.Elements[0].Name)
	}
	if got := diagnosticLevel(50); got != email.LevelSteady {
		t.Errorf("level 50 = %q", got)
	}
}

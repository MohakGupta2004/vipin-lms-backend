package service

import (
	"errors"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func intPtr(v int) *int { return &v }

func testQuestions() []CreateQuestionInput {
	return []CreateQuestionInput{{
		QuestionText: "2+2?",
		Explanation:  "basic maths",
		Options:      []CreateOptionInput{{OptionText: "3"}, {OptionText: "4", IsCorrect: true}},
	}}
}

func TestBuildQuizDefaultsToMockTest(t *testing.T) {
	q, err := buildQuiz(CreateQuizInput{Title: "t", Questions: testQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != models.QuizTypeMockTest || q.PassPercent == nil || *q.PassPercent != defaultPassPercent {
		t.Errorf("got type=%q pass=%v", q.Type, q.PassPercent)
	}
}

func TestBuildQuizPractice(t *testing.T) {
	q, err := buildQuiz(CreateQuizInput{Title: "t", Type: models.QuizTypePractice, Questions: testQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if q.TimeLimitSec != nil || q.PassPercent != nil {
		t.Errorf("practice must have no time limit or pass percent: %v %v", q.TimeLimitSec, q.PassPercent)
	}
}

func TestBuildQuizRejects(t *testing.T) {
	cases := map[string]CreateQuizInput{
		"practice with time limit":   {Title: "t", Type: models.QuizTypePractice, TimeLimitSec: intPtr(60), Questions: testQuestions()},
		"practice with pass percent": {Title: "t", Type: models.QuizTypePractice, PassPercent: intPtr(50), Questions: testQuestions()},
		"bad type":                   {Title: "t", Type: "bogus", Questions: testQuestions()},
		"pass percent over 100":      {Title: "t", PassPercent: intPtr(101), Questions: testQuestions()},
	}
	for name, in := range cases {
		if _, err := buildQuiz(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func gradingFixture() []models.Question {
	yes, no := true, false
	return []models.Question{
		{ID: "q1", Explanation: "e1", Options: []models.QuestionOption{{ID: "a1", IsCorrect: &no}, {ID: "b1", IsCorrect: &yes}}},
		{ID: "q2", Explanation: "e2", Options: []models.QuestionOption{{ID: "a2", IsCorrect: &yes}, {ID: "b2", IsCorrect: &no}}},
	}
}

func TestGradeAttemptMockTest(t *testing.T) {
	quiz := &models.Quiz{ID: "z", Type: models.QuizTypeMockTest, PassPercent: intPtr(50)}
	// q1 right, q2 skipped (counts as wrong).
	a := gradeAttempt(quiz, gradingFixture(), map[string]string{"q1": "b1"})
	if len(a.Answers) != 2 {
		t.Fatalf("answers = %d, want 2", len(a.Answers))
	}
	if a.Score == nil || *a.Score != 1 || a.Total == nil || *a.Total != 2 || a.Passed == nil || !*a.Passed {
		t.Errorf("bad marks: %v %v %v", a.Score, a.Total, a.Passed)
	}
	if a.Answers[1].SelectedOptionID != nil || a.Answers[1].IsCorrect {
		t.Error("skipped question must be unselected and wrong")
	}

	quiz.PassPercent = intPtr(51)
	if a := gradeAttempt(quiz, gradingFixture(), map[string]string{"q1": "b1"}); *a.Passed {
		t.Error("1/2 must fail a 51% pass mark")
	}
}

func TestGradeAttemptPractice(t *testing.T) {
	quiz := &models.Quiz{ID: "z", Type: models.QuizTypePractice}
	a := gradeAttempt(quiz, gradingFixture(), map[string]string{"q2": "b2"})
	if a.Score != nil || a.Total != nil || a.Passed != nil {
		t.Errorf("practice has no marks: %v %v %v", a.Score, a.Total, a.Passed)
	}
	if len(a.Answers) != 1 {
		t.Fatalf("answers = %d, want only the answered one", len(a.Answers))
	}
	got := a.Answers[0]
	if got.QuestionID != "q2" || got.IsCorrect || got.CorrectOptionID != "a2" || got.Explanation != "e2" {
		t.Errorf("bad answer: %+v", got)
	}
}

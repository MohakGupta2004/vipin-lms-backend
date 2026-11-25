package email

import (
	"strings"
	"testing"
)

func TestRenderAllKinds(t *testing.T) {
	for kind := range contents {
		msg, err := Render(kind, "a@b.co", Data{FirstName: "Asha", Code: "123456", ExpiresInMins: 10})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if msg.Subject == "" || !strings.Contains(msg.HTML, SenderName) || !strings.Contains(msg.Text, SenderName) {
			t.Errorf("%s: missing subject or sender name", kind)
		}
		if contents[kind].HasCode && (!strings.Contains(msg.HTML, "123456") || !strings.Contains(msg.Text, "123456")) {
			t.Errorf("%s: code not rendered", kind)
		}
	}
}

func TestRenderEscapesHTML(t *testing.T) {
	msg, err := Render(KindVerifyEmail, "a@b.co", Data{FirstName: `<script>alert(1)</script>`, Code: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "<script>") {
		t.Error("first name was not escaped")
	}
}

func TestRenderUnknownKind(t *testing.T) {
	if _, err := Render("nope", "a@b.co", Data{}); err == nil {
		t.Error("want error")
	}
}

func TestRenderDiagnosticResult(t *testing.T) {
	msg, err := RenderDiagnosticResult("a@b.co", DiagnosticResultData{
		FirstName: "Asha",
		TestTitle: "CIRE skill diagnostic",
		Correct:   9,
		Total:     36,
		Strongest: "Derivatives",
		Weakest:   "Complaint handling",
		Note:      `Great call <b>today</b>`,
		Elements: []DiagnosticElementLine{
			{Name: "Derivatives", Correct: 3, Total: 4, Percent: 75, Level: LevelStrong},
			{Name: "Complaint handling", Correct: 0, Total: 4, Percent: 0, Level: LevelWeak},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.To != "a@b.co" || msg.Subject != "Your CIRE skill diagnostic result" {
		t.Errorf("to/subject = %q/%q", msg.To, msg.Subject)
	}
	for _, want := range []string{"Asha", "9", "/36", "Derivatives", "75% Strong", "0% Weak", "width:75%", SenderName} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("html missing %q", want)
		}
	}
	if strings.Contains(msg.HTML, "<b>today</b>") || strings.Contains(msg.HTML, "ZgotmplZ") {
		t.Error("note not escaped or a style value was rejected")
	}
	for _, want := range []string{"Score: 9/36", "Derivatives: 3/4 correct, 75% (Strong)", "Great call <b>today</b>"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("text missing %q", want)
		}
	}
}

func TestRenderDiagnosticResultWithoutNote(t *testing.T) {
	msg, err := RenderDiagnosticResult("a@b.co", DiagnosticResultData{TestTitle: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.HTML, "Hi there,") {
		t.Error("missing default first name")
	}
}

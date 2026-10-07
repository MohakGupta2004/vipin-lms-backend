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

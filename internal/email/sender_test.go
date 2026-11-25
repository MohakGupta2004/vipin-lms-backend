package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestResendRequestHasUniqueThreadingHeader(t *testing.T) {
	msg := Message{To: "a@b.co", Subject: "Your CIRE skill diagnostic result", HTML: "<p>hi</p>", Text: "hi"}
	first := resendRequest("Vipin Garg <no-reply@example.com>", msg)
	second := resendRequest("Vipin Garg <no-reply@example.com>", msg)

	a, b := first.Headers[threadingHeader], second.Headers[threadingHeader]
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("ref ids = %q, %q, want 32 hex chars each", a, b)
	}
	if a == b {
		t.Error("two emails with the same subject got the same ref id, so Gmail would thread them")
	}
	if first.Subject != msg.Subject || len(first.To) != 1 || first.To[0] != msg.To || first.Html != msg.HTML || first.Text != msg.Text {
		t.Errorf("request = %+v", first)
	}
}

// TestResendSenderSendsHeaders checks the real request the Resend client sends, against a local server.
func TestResendSenderSendsHeaders(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test"}`))
	}))
	defer srv.Close()

	s := NewResendSender("re_test", "no-reply@example.com", 100)
	s.client.BaseURL, _ = url.Parse(srv.URL + "/")
	msg := Message{
		To:          "a@b.co",
		Subject:     "Your CIRE skill diagnostic result",
		HTML:        "<p>hi</p>",
		Attachments: []Attachment{{Filename: "c.png", ContentType: "image/png", Content: []byte{1, 2}, ContentID: "chart"}},
	}
	for range 2 {
		if err := s.Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}

	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	refs := [2]string{}
	for i, body := range bodies {
		headers, _ := body["headers"].(map[string]any)
		refs[i], _ = headers[threadingHeader].(string)
		attachments, _ := body["attachments"].([]any)
		if len(attachments) != 1 || attachments[0].(map[string]any)["content_id"] != "chart" {
			t.Errorf("request %d attachments = %v", i, body["attachments"])
		}
	}
	if refs[0] == "" || refs[0] == refs[1] {
		t.Errorf("%s values = %q, want two different non-empty values", threadingHeader, refs)
	}
}

package email

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"

	"github.com/resend/resend-go/v4"
	"go.uber.org/ratelimit"
)

// Sender delivers one rendered email.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// ResendSender sends through the Resend API. The shared limiter paces calls across all workers
// so a burst of jobs cannot exceed the Resend account's requests-per-second limit.
type ResendSender struct {
	client  *resend.Client
	from    string
	limiter ratelimit.Limiter
}

var _ Sender = (*ResendSender)(nil)

// NewResendSender builds a sender for the given API key and address (bare address; the display name is SenderName).
func NewResendSender(apiKey, fromAddress string, perSecond int) *ResendSender {
	return &ResendSender{
		client:  resend.NewClient(apiKey),
		from:    (&mail.Address{Name: SenderName, Address: fromAddress}).String(),
		limiter: ratelimit.New(perSecond, ratelimit.WithoutSlack),
	}
}

func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.limiter.Take() // blocks until the next slot; bounded by 1/perSecond per queued email
	_, err := s.client.Emails.SendWithContext(ctx, resendRequest(s.from, msg))
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}

// threadingHeader set to a value unique to each email stops Gmail from grouping emails with the same
// subject into one conversation. In a conversation Gmail hides whatever a later email repeats from an
// earlier one behind a "•••" button, which would hide most of a second results email.
// See https://resend.com/docs/dashboard/emails/custom-headers.
const threadingHeader = "X-Entity-Ref-ID"

// resendRequest builds the Resend API request for one email.
func resendRequest(from string, msg Message) *resend.SendEmailRequest {
	var attachments []*resend.Attachment
	for _, a := range msg.Attachments {
		attachments = append(attachments, &resend.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Content:     a.Content,
			ContentId:   a.ContentID,
		})
	}
	return &resend.SendEmailRequest{
		From:        from,
		To:          []string{msg.To},
		Subject:     msg.Subject,
		Html:        msg.HTML,
		Text:        msg.Text,
		Attachments: attachments,
		Headers:     map[string]string{threadingHeader: uniqueRefID()},
	}
}

func uniqueRefID() string {
	b := make([]byte, 16)
	rand.Read(b) // never returns an error; it panics if the system's random source fails
	return hex.EncodeToString(b)
}

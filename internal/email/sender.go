package email

import (
	"context"
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
	_, err := s.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Html:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}

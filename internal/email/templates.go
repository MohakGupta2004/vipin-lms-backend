package email

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

// SenderName is the display name on every outgoing email.
const SenderName = "Vipin Garg"

// Kind identifies the purpose of an email. Each kind has one static template below.
type Kind string

const (
	KindVerifyEmail     Kind = "verify_email"
	KindEmailVerified   Kind = "email_verified"
	KindPasswordReset   Kind = "password_reset"
	KindPasswordChanged Kind = "password_changed"
)

// content is the static copy of one email. Code is shown prominently when set.
type content struct {
	Subject   string
	Preheader string
	Heading   string
	Intro     string
	HasCode   bool
	Outro     string
}

// Data is what a template is filled with. Every field is HTML-escaped when rendered.
type Data struct {
	FirstName     string
	Code          string
	ExpiresInMins int
}

var contents = map[Kind]content{
	KindVerifyEmail: {
		Subject:   "Verify your email address",
		Preheader: "Use this code to verify your email address.",
		Heading:   "Verify your email",
		Intro:     "Welcome to the platform! Enter the code below to confirm this is your email address.",
		HasCode:   true,
		Outro:     "If you did not create an account, you can safely ignore this email.",
	},
	KindEmailVerified: {
		Subject:   "Your email address is verified",
		Preheader: "Your email address has been verified.",
		Heading:   "Email verified",
		Intro:     "Thanks for confirming your email address. Your account is all set.",
		Outro:     "If this was not you, please reset your password right away.",
	},
	KindPasswordReset: {
		Subject:   "Your password reset code",
		Preheader: "Use this code to reset your password.",
		Heading:   "Reset your password",
		Intro:     "We received a request to reset your password. Enter the code below to continue.",
		HasCode:   true,
		Outro:     "If you did not request this, you can safely ignore this email. Your password will not change.",
	},
	KindPasswordChanged: {
		Subject:   "Your password was changed",
		Preheader: "Your password was just changed.",
		Heading:   "Password changed",
		Intro:     "The password for your account was just changed.",
		Outro:     "If this was not you, reset your password immediately and contact us.",
	},
}

// layout is the shared HTML shell. html/template escapes every field.
var layout = htmltemplate.Must(htmltemplate.New("layout").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#1f2937;">
<span style="display:none;max-height:0;overflow:hidden;opacity:0;">{{.Preheader}}</span>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:8px;padding:32px;">
<tr><td>
<h1 style="margin:0 0 16px;font-size:22px;">{{.Heading}}</h1>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;">Hi {{.FirstName}},</p>
<p style="margin:0 0 24px;font-size:15px;line-height:1.6;">{{.Intro}}</p>
{{if .HasCode}}
<p style="margin:0 0 24px;text-align:center;font-size:32px;font-weight:700;letter-spacing:8px;font-family:Menlo,Consolas,monospace;">{{.Code}}</p>
<p style="margin:0 0 24px;font-size:13px;color:#6b7280;text-align:center;">This code expires in {{.ExpiresInMins}} minutes. Never share it with anyone.</p>
{{end}}
<p style="margin:0 0 24px;font-size:15px;line-height:1.6;">{{.Outro}}</p>
<p style="margin:0;font-size:15px;line-height:1.6;">Regards,<br>` + SenderName + `</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`))

var textLayout = texttemplate.Must(texttemplate.New("text").Parse(`{{.Heading}}

Hi {{.FirstName}},

{{.Intro}}
{{if .HasCode}}
Your code: {{.Code}}
This code expires in {{.ExpiresInMins}} minutes. Never share it with anyone.
{{end}}
{{.Outro}}

Regards,
` + SenderName + `
`))

// Message is a rendered email ready to send.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Render fills the static template for kind with data.
func Render(kind Kind, to string, data Data) (Message, error) {
	c, ok := contents[kind]
	if !ok {
		return Message{}, fmt.Errorf("unknown email kind %q", kind)
	}
	view := struct {
		content
		Data
	}{c, data}
	if view.FirstName == "" {
		view.FirstName = "there"
	}

	var html, text bytes.Buffer
	if err := layout.Execute(&html, view); err != nil {
		return Message{}, fmt.Errorf("render html: %w", err)
	}
	if err := textLayout.Execute(&text, view); err != nil {
		return Message{}, fmt.Errorf("render text: %w", err)
	}
	return Message{To: to, Subject: c.Subject, HTML: html.String(), Text: text.String()}, nil
}

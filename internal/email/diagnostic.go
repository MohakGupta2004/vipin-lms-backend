package email

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strconv"
	texttemplate "text/template"
)

// DiagnosticLevel labels an element's percent the same way the admin result page does.
type DiagnosticLevel string

const (
	LevelStrong DiagnosticLevel = "Strong"
	LevelSteady DiagnosticLevel = "Steady"
	LevelWeak   DiagnosticLevel = "Weak"
)

// DiagnosticElementLine is one row of the element breakdown in a results email.
type DiagnosticElementLine struct {
	Name    string
	Correct int
	Total   int
	Percent int
	Level   DiagnosticLevel
}

// DiagnosticResultData fills the diagnostic results email. Every field is HTML-escaped when rendered.
type DiagnosticResultData struct {
	FirstName string
	TestTitle string
	Correct   int
	Total     int
	Strongest string
	Weakest   string
	Elements  []DiagnosticElementLine
	// Note is an optional personal message from the admin, shown above the results.
	Note string
}

var diagnosticLayout = htmltemplate.Must(htmltemplate.New("diagnostic").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#1f2937;">
<span style="display:none;max-height:0;overflow:hidden;opacity:0;">You scored {{.Correct}} out of {{.Total}}. See how you did in each chapter.</span>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border-radius:8px;padding:32px;">
<tr><td>
<p style="margin:0 0 4px;font-size:12px;font-weight:600;letter-spacing:0.06em;text-transform:uppercase;color:#6b7280;">{{.TestTitle}}</p>
<h1 style="margin:0 0 16px;font-size:22px;color:#0a2540;">Your diagnostic result</h1>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;">Hi {{.FirstName}},</p>
{{if .Note}}<p style="margin:0 0 16px;font-size:15px;line-height:1.6;white-space:pre-line;">{{.Note}}</p>{{end}}
<p style="margin:0 0 24px;font-size:15px;line-height:1.6;">Here is how you did on the {{.TestTitle}}, chapter by chapter.</p>

<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 24px;border:1px solid #e5e7eb;border-radius:8px;">
<tr>
<td style="padding:16px;border-right:1px solid #e5e7eb;" width="34%" valign="top">
<p style="margin:0 0 4px;font-size:12px;color:#6b7280;">Score</p>
<p style="margin:0;font-size:28px;font-weight:700;color:#0a2540;">{{.Correct}}<span style="font-size:16px;font-weight:400;color:#9ca3af;">/{{.Total}}</span></p>
</td>
<td style="padding:16px;" valign="top">
<p style="margin:0 0 4px;font-size:12px;color:#6b7280;">Strongest</p>
<p style="margin:0 0 12px;font-size:14px;font-weight:600;color:#15803d;">{{.Strongest}}</p>
<p style="margin:0 0 4px;font-size:12px;color:#6b7280;">Needs the most work</p>
<p style="margin:0;font-size:14px;font-weight:600;color:#b91c1c;">{{.Weakest}}</p>
</td>
</tr>
</table>

{{if .HasChart}}<img src="cid:` + radarContentID + `" width="` + strconv.Itoa(radarDisplayWidth) + `" height="` + strconv.Itoa(radarDisplayHeight) + `" alt="Chart of your score in each chapter" style="display:block;width:100%;max-width:` + strconv.Itoa(radarDisplayWidth) + `px;height:auto;margin:0 auto 24px;border:0;">{{end}}

<h2 style="margin:0 0 12px;font-size:16px;color:#0a2540;">Chapter breakdown</h2>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:0 0 24px;">
{{range .Elements}}
<tr><td style="padding:10px 0;border-top:1px solid #f0f1f3;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
<tr>
<td style="font-size:14px;font-weight:500;color:#1f2937;">{{.Name}}</td>
<td align="right" style="font-size:13px;color:#6b7280;white-space:nowrap;">{{.Correct}}/{{.Total}} correct &middot; <span style="font-weight:600;{{if eq .Level "Strong"}}color:#15803d;{{else if eq .Level "Weak"}}color:#b91c1c;{{else}}color:#475569;{{end}}">{{.Percent}}% {{.Level}}</span></td>
</tr>
<tr><td colspan="2" style="padding-top:6px;">
<div style="background:#eef0f3;border-radius:3px;height:6px;line-height:6px;font-size:0;">{{if gt .Percent 0}}<div style="width:{{.Percent}}%;height:6px;border-radius:3px;{{if eq .Level "Strong"}}background:#15803d;{{else if eq .Level "Weak"}}background:#b91c1c;{{else}}background:#64748b;{{end}}"></div>{{end}}</div>
</td></tr>
</table>
</td></tr>
{{end}}
</table>

<p style="margin:0 0 24px;font-size:13px;line-height:1.6;color:#6b7280;">Each chapter's percent weighs how confident you were: a confident right answer counts double, and a wrong answer takes a point away.</p>
<p style="margin:0;font-size:15px;line-height:1.6;">Regards,<br>` + SenderName + `</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`))

var diagnosticTextLayout = texttemplate.Must(texttemplate.New("diagnostic-text").Parse(`Your diagnostic result: {{.TestTitle}}

Hi {{.FirstName}},
{{if .Note}}
{{.Note}}
{{end}}
Here is how you did on the {{.TestTitle}}, chapter by chapter.

Score: {{.Correct}}/{{.Total}}
Strongest: {{.Strongest}}
Needs the most work: {{.Weakest}}

Chapter breakdown
{{range .Elements}}- {{.Name}}: {{.Correct}}/{{.Total}} correct, {{.Percent}}% ({{.Level}})
{{end}}
Each chapter's percent weighs how confident you were: a confident right answer counts double, and a wrong answer takes a point away.

Regards,
` + SenderName + `
`))

// RenderDiagnosticResult builds the email that shares a diagnostic result with its recipient.
// With 3 or more elements it carries the chapter radar chart as an inline PNG, like the admin result page.
func RenderDiagnosticResult(to string, data DiagnosticResultData) (Message, error) {
	if data.FirstName == "" {
		data.FirstName = "there"
	}

	var attachments []Attachment
	if len(data.Elements) >= 3 {
		labels := make([]string, len(data.Elements))
		values := make([]int, len(data.Elements))
		for i, element := range data.Elements {
			labels[i] = element.Name
			values[i] = element.Percent
		}
		chart, err := radarPNG(labels, values)
		if err != nil {
			return Message{}, fmt.Errorf("render chart: %w", err)
		}
		attachments = append(attachments, Attachment{
			Filename:    "chapter-chart.png",
			ContentType: "image/png",
			Content:     chart,
			ContentID:   radarContentID,
		})
	}

	view := struct {
		DiagnosticResultData
		Subject  string
		HasChart bool
	}{data, fmt.Sprintf("Your %s result", data.TestTitle), len(attachments) > 0}

	var html, text bytes.Buffer
	if err := diagnosticLayout.Execute(&html, view); err != nil {
		return Message{}, fmt.Errorf("render html: %w", err)
	}
	if err := diagnosticTextLayout.Execute(&text, view); err != nil {
		return Message{}, fmt.Errorf("render text: %w", err)
	}
	return Message{To: to, Subject: view.Subject, HTML: html.String(), Text: text.String(), Attachments: attachments}, nil
}

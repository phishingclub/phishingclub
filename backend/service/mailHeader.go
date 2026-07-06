package service

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/phishingclub/phishingclub/model"
	"github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

// sanitizeSMTPHeaderValue removes characters that are illegal in a single header
// line so a header value can never be used to inject additional headers, even if
// the underlying mail library stops encoding them. It strips all C0 control
// characters (including carriage return, line feed and NUL) and DEL.
func sanitizeSMTPHeaderValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}

// applyCustomSMTPHeaders sets the configured custom headers on the message.
//
// When templateData is not nil each header value is rendered with the same per
// recipient variables available in the email body and subject (for example
// {{.rID}} or {{.Email}}), using funcs as the template function map. Paths without
// a recipient context, such as the SMTP connectivity test, pass nil to send the
// values verbatim. Every value is sanitized before being set on the message.
func applyCustomSMTPHeaders(
	msg *mail.Msg,
	headers []*model.SMTPHeader,
	templateData *map[string]any,
	funcs template.FuncMap,
	logger *zap.SugaredLogger,
) {
	for _, header := range headers {
		key := header.Key.MustGet().String()
		value := header.Value.MustGet().String()
		if templateData != nil {
			value = renderSMTPHeaderValue(key, value, templateData, funcs, logger)
		}
		msg.SetGenHeader(mail.Header(key), sanitizeSMTPHeaderValue(value))
	}
}

// renderSMTPHeaderValue renders a single header value as a template. On any parse
// or execution error it logs and falls back to the raw value so a broken header
// template never blocks a send.
func renderSMTPHeaderValue(
	key string,
	value string,
	templateData *map[string]any,
	funcs template.FuncMap,
	logger *zap.SugaredLogger,
) string {
	tmpl, err := template.New("smtpHeader").Funcs(funcs).Parse(value)
	if err != nil {
		logger.Warnw("failed to parse custom smtp header template, sending raw value",
			"header", key,
			"error", err,
		)
		return value
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		logger.Warnw("failed to execute custom smtp header template, sending raw value",
			"header", key,
			"error", err,
		)
		return value
	}
	return buf.String()
}

package platform

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"strings"
)

// The HTML part of every mail (ADR 0082, spec #870), rendered beside the plain
// text from the same message value. The message stays the seam: each message
// type with Subject() and Text() gains an HTML(MailChrome) built from the same
// fields, and the providers only call it.
//
// The templates are html/template, for its contextual escaping: mail
// interpolates names and titles people typed, and an Event called <b>Fest</b>
// must reach an inbox as text. They are embedded and parsed ONCE, when this
// package is initialised, and every kind is executed once against its zero
// value there too, so a template that fails to parse or to escape fails the
// process at boot and never a send.
//
// Styles are written inline in the templates by hand rather than produced by
// a CSS inliner, so nothing runs at build or send time but the template.

// DefaultSupportEmail is where every footer sends a reader with a question.
const DefaultSupportEmail = "info@redplanettribe.org"

// MailChrome is the sender-wide configuration every HTML mail's chrome reads:
// resolved once, when the sender is built, and never per call.
type MailChrome struct {
	// MarkURL is the absolute URL of the Multiticketing mark shown beside the
	// name above the card, one of the platform's own assets. Empty omits the
	// image and leaves the name.
	MarkURL string
	// OperatorName and OperatorCountry make the footer's legal line: the
	// Issuer's razón social exactly as registered (today in capitals) and its
	// country's name. With either empty, which is a deployment with no Issuer
	// configured, the legal line is omitted rather than invented.
	OperatorName    string
	OperatorCountry string
	// SupportEmail is the footer's support contact; empty reads as
	// DefaultSupportEmail.
	SupportEmail string
}

func (c MailChrome) supportEmail() string {
	if strings.TrimSpace(c.SupportEmail) == "" {
		return DefaultSupportEmail
	}
	return c.SupportEmail
}

// The copy every HTML mail's chrome carries, beside each kind's own preheader
// and footer reason (which live with that kind's copy).
var (
	mailLinkFallbackCopy = translated(
		"Or paste this link into your browser:",
		"O pegue este enlace en su navegador:",
	)
	mailSupportLeadCopy = translated(
		"Questions? Write to",
		"¿Preguntas? Escriba a",
	)
	// The Issuer's razón social, then its country, both printed as stored.
	mailLegalLineCopy = translated(
		"Multiticketing is operated by %s, %s.",
		"Multiticketing es operado por %s, %s.",
	)
)

// mailPage is what the layout template renders: the chrome around one kind's
// card.
type mailPage struct {
	Lang      string
	Title     string
	Preheader string
	Chrome    mailChromeView
	Footer    mailFooter
	Card      any
}

type mailChromeView struct {
	MarkURL string
}

type mailFooter struct {
	Reason       string
	SupportLead  string
	SupportEmail string
	Legal        string
}

// mailButton is the primary button and its raw-link fallback. A kind builds
// one with newMailButton so the fallback sentence is always there.
type mailButton struct {
	Label    string
	URL      string
	Fallback string
}

func newMailButton(locale Locale, label, url string) mailButton {
	return mailButton{Label: label, URL: url, Fallback: mailLinkFallbackCopy.in(locale)}
}

// mailKind names one kind's template file, mailtemplates/kinds/<kind>.html.
type mailKind string

const (
	mailKindOTP               mailKind = "otp"
	mailKindConsentWithdrawal mailKind = "consentwithdrawal"
)

// mailKinds names every kind's template file (mailtemplates/kinds/<name>.html)
// and the card value it is executed with. The zero value is what the boot-time
// dry run executes, which is what turns a template referring to a field its
// card does not have into a failed boot rather than a failed send.
var mailKinds = map[mailKind]any{
	mailKindOTP:               otpCard{},
	mailKindConsentWithdrawal: consentWithdrawalCard{},
}

//go:embed mailtemplates
var mailTemplateFiles embed.FS

// mailTemplates is every kind's template set, parsed at init. A broken
// template panics here, which is the process failing at boot.
var mailTemplates = mustParseMailTemplates(mailTemplateFiles, mailKinds)

func mustParseMailTemplates(fsys fs.FS, kinds map[mailKind]any) map[mailKind]*template.Template {
	templates, err := parseMailTemplates(fsys, kinds)
	if err != nil {
		panic(fmt.Sprintf("mail templates: %v", err))
	}
	return templates
}

// mailTemplateFuncs emit the markup html/template would otherwise refuse or
// strip. Every one is a constant: none of them ever carries a value.
var mailTemplateFuncs = template.FuncMap{
	// The padding after the preheader, so a client does not pull body text
	// into the inbox preview after it.
	"preheaderPad": func() template.HTML {
		return template.HTML(strings.Repeat("&#847;&zwnj;&nbsp;", 60))
	},
	// html/template strips comments, and desktop Outlook's fixed-width wrapper
	// is a conditional comment.
	"msoOpen": func() template.HTML {
		return `<!--[if mso]><table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->`
	},
	"msoClose": func() template.HTML {
		return `<!--[if mso]></td></tr></table><![endif]-->`
	},
}

// parseMailTemplates parses the shared layout and components once, then one
// clone of them per kind with that kind's card, and executes each kind against
// its zero card so escaping and field errors surface now. A kind with no file,
// or a file with no kind, is an error too.
func parseMailTemplates(fsys fs.FS, kinds map[mailKind]any) (map[mailKind]*template.Template, error) {
	base, err := template.New("mail").Funcs(mailTemplateFuncs).ParseFS(fsys, "mailtemplates/layout.html", "mailtemplates/components.html")
	if err != nil {
		return nil, err
	}
	files, err := fs.Glob(fsys, "mailtemplates/kinds/*.html")
	if err != nil {
		return nil, err
	}
	templates := make(map[mailKind]*template.Template, len(files))
	for _, file := range files {
		name := mailKind(strings.TrimSuffix(path.Base(file), ".html"))
		card, known := kinds[name]
		if !known {
			return nil, fmt.Errorf("%s: no mail kind is declared for it", file)
		}
		kind, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := kind.ParseFS(fsys, file); err != nil {
			return nil, err
		}
		if kind.Lookup("card") == nil {
			return nil, fmt.Errorf("%s: defines no card", file)
		}
		if err := kind.ExecuteTemplate(io.Discard, "layout", mailPage{Card: card}); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		templates[name] = kind
	}
	for name := range kinds {
		if templates[name] == nil {
			return nil, fmt.Errorf("mail kind %q has no template file", name)
		}
	}
	return templates, nil
}

// renderMail renders one kind's card inside the chrome. subject is the
// <title>; preheader and reason are the kind's own copy, already in locale.
func renderMail(kind mailKind, locale Locale, chrome MailChrome, subject, preheader, reason string, card any) (string, error) {
	tpl, ok := mailTemplates[kind]
	if !ok {
		return "", fmt.Errorf("no mail template for %q", kind)
	}
	footer := mailFooter{
		Reason:       reason,
		SupportLead:  mailSupportLeadCopy.in(locale),
		SupportEmail: chrome.supportEmail(),
	}
	if strings.TrimSpace(chrome.OperatorName) != "" && strings.TrimSpace(chrome.OperatorCountry) != "" {
		footer.Legal = fmt.Sprintf(mailLegalLineCopy.in(locale), chrome.OperatorName, chrome.OperatorCountry)
	}
	page := mailPage{
		Lang:      mailLang(locale),
		Title:     subject,
		Preheader: preheader,
		Chrome:    mailChromeView{MarkURL: chrome.MarkURL},
		Footer:    footer,
		Card:      card,
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout", page); err != nil {
		return "", fmt.Errorf("render %s mail: %w", kind, err)
	}
	return buf.String(), nil
}

// mailLang is the page's lang attribute: the locale the copy was chosen in,
// which is English for anything this platform does not write (mailCopy.in).
func mailLang(locale Locale) string {
	if locale == LocaleES {
		return string(LocaleES)
	}
	return string(LocaleEN)
}

// renderedHTML is what a provider puts beside the text: the message's HTML,
// or nothing when rendering failed. A failed render is logged and the mail
// goes out as text alone, because the HTML part is never worth a passcode
// that does not arrive. Boot has already executed every template, so this is
// not expected to happen.
func renderedHTML(ctx context.Context, logger Logger, messageType string, render func(MailChrome) (string, error), chrome MailChrome) string {
	html, err := render(chrome)
	if err != nil {
		logger.Error(ctx, "render "+messageType+" html failed; sending text only", "error", err)
		return ""
	}
	return html
}

// paragraphs splits a plain-text body into its paragraphs, so the HTML part
// carries exactly the copy the text part does.
func paragraphs(text string) []string {
	return strings.Split(text, "\n\n")
}

// otpCard is the One-time Passcode's card.
type otpCard struct {
	Heading string
	Code    string
	Expiry  string
}

// HTML is the passcode's HTML part: the code large and monospace, the same
// expiry sentence the text carries, and no button.
func (o OTPMessage) HTML(chrome MailChrome) (string, error) {
	return renderMail(mailKindOTP, o.Locale, chrome, o.Subject(),
		fmt.Sprintf(otpPreheaderCopy.in(o.Locale), o.Code),
		otpFooterReasonCopy.in(o.Locale),
		otpCard{Heading: o.Subject(), Code: o.Code, Expiry: otpExpiryCopy.in(o.Locale)},
	)
}

// consentWithdrawalCard is the Consent Withdrawal Confirmation's card.
type consentWithdrawalCard struct {
	Heading    string
	Paragraphs []string
}

// HTML is the confirmation's HTML part: the subject as its heading and the
// text's own paragraphs, chosen by what the act took away exactly as Text()
// chooses them.
func (c ConsentWithdrawalConfirmation) HTML(chrome MailChrome) (string, error) {
	return renderMail(mailKindConsentWithdrawal, c.Locale, chrome, c.Subject(),
		consentWithdrawalPreheaderCopy.in(c.Locale),
		consentWithdrawalFooterReasonCopy.in(c.Locale),
		consentWithdrawalCard{Heading: c.Subject(), Paragraphs: paragraphs(c.Text())},
	)
}

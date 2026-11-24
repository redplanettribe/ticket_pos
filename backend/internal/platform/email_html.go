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
	// mailKindNotice is the plain notice card in the platform chrome, with no
	// Event imagery: the fiscal kinds (Tax Document Delivery, Annulment
	// Notice) and the staff and operator kinds (the Certificate Expiry
	// Warning, the five Payout Request notices, both Question Review notices
	// and Ticket Question Revoked) are all written in it.
	mailKindNotice mailKind = "notice"
	// mailKindEventRow is the card that shows which Event a mail concerns in a
	// compact Event row: the Answer Reminder, the Assignment Reminder, Sale
	// Voided, Sale Reversal Refused and No Longer Holding.
	mailKindEventRow mailKind = "eventrow"
)

// mailKinds names every kind's template file (mailtemplates/kinds/<name>.html)
// and the card value it is executed with. That value (the zero value, unless a
// card's optional branches are filled so each is run) is what the boot-time
// dry run executes, which is what turns a template referring to a field its
// card does not have into a failed boot rather than a failed send.
var mailKinds = map[mailKind]any{
	mailKindOTP:               otpCard{},
	mailKindConsentWithdrawal: consentWithdrawalCard{},
	mailKindNotice:            noticeCard{},
	// Every optional branch filled, so the dry run executes each one.
	mailKindEventRow: eventRowCard{
		Intro:  [][]string{{""}},
		Rows:   []eventRowView{{SquareURL: "#", Meta: "-", Detail: "-", Organization: &orgRowView{LogoURL: "#"}}},
		Button: &mailButton{},
		After:  [][]string{{""}},
	},
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
	// lines makes one string the []string the "lines" component takes, so a
	// paragraph is a paragraph of one line and both share one style.
	"lines": func(line string) []string { return []string{line} },
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

// noticeCard is the card of the fiscal and the staff and operator kinds: a
// heading, the text part's paragraphs before its link, each split into the
// lines the text breaks it into, and the link as the one button with the
// sentence that leads to it. Button is nil when the mail has no link, which
// drops the lead too.
type noticeCard struct {
	Heading  string
	Blocks   [][]string
	LinkLead string
	Button   *mailButton
}

func newNoticeCard(locale Locale, heading string, blocks []string, link mailLink) noticeCard {
	card := noticeCard{Heading: heading}
	for _, block := range blocks {
		card.Blocks = append(card.Blocks, strings.Split(block, "\n"))
	}
	if link.url != "" {
		button := newMailButton(locale, link.label.in(locale), link.url)
		card.LinkLead, card.Button = link.lead.in(locale), &button
	}
	return card
}

// HTML is the Tax Document Delivery's HTML part, in the notice card. The
// documents travel as attachments beside both parts, never in the markup.
func (d TaxDocumentDelivery) HTML(chrome MailChrome) (string, error) {
	return renderMail(mailKindNotice, d.Locale, chrome, d.Subject(),
		fmt.Sprintf(taxDocumentPreheaderCopy.in(d.Locale), d.Reference),
		fmt.Sprintf(taxDocumentFooterReasonCopy.in(d.Locale), d.EventName),
		newNoticeCard(d.Locale, d.Subject(), d.blocks(), d.link()),
	)
}

// HTML is the Annulment Notice's HTML part, in the same notice card.
func (n AnnulmentNotice) HTML(chrome MailChrome) (string, error) {
	return renderMail(mailKindNotice, n.Locale, chrome, n.Subject(),
		fmt.Sprintf(annulmentNoticePreheaderCopy.in(n.Locale), n.Number),
		fmt.Sprintf(annulmentNoticeFooterReasonCopy.in(n.Locale), n.EventName),
		newNoticeCard(n.Locale, n.Subject(), n.blocks(), n.link()),
	)
}

// The staff and operator kinds (#875): notices to an Organization's Members and
// to Platform Operators in the notice card, each in its reader's Staff Locale exactly as
// its text is. Only the Certificate Expiry Warning carries a link today, so it
// is the only one with a button. The rest are the subject as heading and the
// text's own paragraphs, so the two parts cannot drift.

// staffNotice renders a staff or operator kind with no link: the subject as
// heading and the text part's paragraphs.
func staffNotice(locale Locale, chrome MailChrome, subject, text, preheader, reason string) (string, error) {
	return renderMail(mailKindNotice, locale, chrome, subject, preheader, reason,
		newNoticeCard(locale, subject, paragraphs(text), mailLink{}))
}

// HTML is the Certificate Expiry Warning's HTML part: the heading its text
// opens with, the date and what lapsing costs, and the Issuer page as the
// one button.
func (w CertificateExpiryWarning) HTML(chrome MailChrome) (string, error) {
	return renderMail(mailKindNotice, w.Locale, chrome, w.Subject(),
		fmt.Sprintf(certificateExpiryPreheaderCopy.in(w.Locale), w.RUC),
		operatorFooterReasonCopy.in(w.Locale),
		newNoticeCard(w.Locale, certificateExpiryHeadingCopy.in(w.Locale), w.blocks(), w.link()),
	)
}

// HTML is the operators' Payout Request submission notice's HTML part.
func (p PayoutRequestSubmitted) HTML(chrome MailChrome) (string, error) {
	return staffNotice(p.Locale, chrome, p.Subject(), p.Text(),
		fmt.Sprintf(payoutSubmittedPreheaderCopy.in(p.Locale), p.RequestedBy),
		operatorFooterReasonCopy.in(p.Locale))
}

// HTML is the paid notice's HTML part.
func (p PayoutRequestPaid) HTML(chrome MailChrome) (string, error) {
	return staffNotice(p.Locale, chrome, p.Subject(), p.Text(),
		payoutPaidPreheaderCopy.in(p.Locale),
		fmt.Sprintf(payoutAskerFooterReasonCopy.in(p.Locale), p.OrganizationName))
}

// HTML is the decline notice's HTML part. The operator's reason is in the
// card, quoted whole as the text quotes it, and never in the preheader.
func (p PayoutRequestDeclined) HTML(chrome MailChrome) (string, error) {
	return staffNotice(p.Locale, chrome, p.Subject(), p.Text(),
		payoutDeclinedPreheaderCopy.in(p.Locale),
		fmt.Sprintf(payoutAskerFooterReasonCopy.in(p.Locale), p.OrganizationName))
}

// HTML is the transfer-sent notice's HTML part.
func (p PayoutRequestTransferSent) HTML(chrome MailChrome) (string, error) {
	return staffNotice(p.Locale, chrome, p.Subject(), p.Text(),
		fmt.Sprintf(payoutTransferSentPreheaderCopy.in(p.Locale), formatEcuadorDate(p.SubmittedAt, p.Locale)),
		fmt.Sprintf(payoutAskerFooterReasonCopy.in(p.Locale), p.OrganizationName))
}

// HTML is the transfer-failed notice's HTML part.
func (p PayoutRequestTransferFailed) HTML(chrome MailChrome) (string, error) {
	return staffNotice(p.Locale, chrome, p.Subject(), p.Text(),
		payoutTransferFailedPreheaderCopy.in(p.Locale),
		fmt.Sprintf(payoutAskerFooterReasonCopy.in(p.Locale), p.OrganizationName))
}

// HTML is the operators' Question Review submission notice's HTML part.
func (q QuestionReviewSubmitted) HTML(chrome MailChrome) (string, error) {
	return staffNotice(q.Locale, chrome, q.Subject(), q.Text(),
		fmt.Sprintf(questionReviewSubmittedPreheaderCopy.in(q.Locale), q.SubmittedBy),
		operatorFooterReasonCopy.in(q.Locale))
}

// HTML is the verdict notice's HTML part: the verdicts are one paragraph,
// one line each, as the text lists them.
func (q QuestionReviewAnswered) HTML(chrome MailChrome) (string, error) {
	return staffNotice(q.Locale, chrome, q.Subject(), q.Text(),
		questionReviewAnsweredPreheaderCopy.in(q.Locale),
		fmt.Sprintf(questionReviewAnsweredFooterReasonCopy.in(q.Locale), q.EventName))
}

// HTML is the Revocation notice's HTML part.
func (r TicketQuestionRevoked) HTML(chrome MailChrome) (string, error) {
	return staffNotice(r.Locale, chrome, r.Subject(), r.Text(),
		fmt.Sprintf(questionRevokedPreheaderCopy.in(r.Locale), r.QuestionLabel),
		fmt.Sprintf(questionRevokedFooterReasonCopy.in(r.Locale), r.OrganizationName))
}

// orgRowView is the Organization row: its name, and its Logo rendition on a
// white tile Tile pixels square.
type orgRowView struct {
	Name    string
	LogoURL string
	Tile    int
}

// Logo is the Logo's width inside its tile.
func (o orgRowView) Logo() int { return o.Tile - 8 }

// compactOrgTile is the Logo tile's size in the compact Event row.
const compactOrgTile = 24

// newOrgRow is the Organization row of an Event, or nil when the mail does not
// know the Organization's name: the Logo's alt is empty on the promise that
// the name is printed beside it, so a Logo never stands alone.
func newOrgRow(event MailEvent, tile int) *orgRowView {
	if strings.TrimSpace(event.OrganizationName) == "" {
		return nil
	}
	return &orgRowView{Name: event.OrganizationName, LogoURL: event.LogoURL, Tile: tile}
}

// eventRowView is one compact Event row: the square rendition (none when
// empty), the Event's name, the date and venue line, an optional detail line
// and the Organization row.
type eventRowView struct {
	Name         string
	SquareURL    string
	Meta         string
	Detail       string
	Organization *orgRowView
}

func newEventRow(locale Locale, name, detail string, event MailEvent) eventRowView {
	var meta []string
	if when := formatEventDate(event.StartsAt, event.Timezone, locale); when != "" {
		meta = append(meta, when)
	}
	if venue := strings.TrimSpace(event.VenueName); venue != "" {
		meta = append(meta, venue)
	}
	return eventRowView{
		Name:         name,
		SquareURL:    event.SquareURL,
		Meta:         strings.Join(meta, " \u00b7 "),
		Detail:       detail,
		Organization: newOrgRow(event, compactOrgTile),
	}
}

// eventRowCard is the compact Event row card: a heading, the paragraphs before
// the rows, the rows, the paragraphs after them, then the mail's one link as
// the button with the sentence that leads to it (nil when the mail has no
// link), and the paragraphs after the button. Each paragraph is split into
// the lines the text breaks it into.
type eventRowCard struct {
	Heading  string
	Intro    [][]string
	Rows     []eventRowView
	Blocks   [][]string
	LinkLead string
	Button   *mailButton
	After    [][]string
}

// splitLines splits each of a text's paragraphs into its lines.
func splitLines(blocks []string) [][]string {
	lines := make([][]string, 0, len(blocks))
	for _, block := range blocks {
		lines = append(lines, strings.Split(block, "\n"))
	}
	return lines
}

// The compact Event row kinds (#878). Each shows which Event it concerns in
// the Event row, and otherwise carries the text part's own paragraphs, so the
// two parts cannot drift; the text's "Event: ..." lines are what the row
// shows instead.

// HTML is the Answer Reminder's HTML part: one Event row per listed Ticket,
// with its Ticket Type under the Event's name, and the Customer Area as the
// one button.
func (r HolderAnswerReminder) HTML(chrome MailChrome) (string, error) {
	opening, closing := holderAnswerReminderOpeningCopy, holderAnswerReminderClosingCopy
	if len(r.Tickets) != 1 {
		opening, closing = holderAnswerReminderOpeningPluralCopy, holderAnswerReminderClosingPluralCopy
	}
	rows := make([]eventRowView, 0, len(r.Tickets))
	for _, ticket := range r.Tickets {
		rows = append(rows, newEventRow(r.Locale, ticket.EventName,
			fmt.Sprintf(holderAnswerReminderTicketLineCopy.in(r.Locale), ticket.TicketTypeName), ticket.Event))
	}
	button := newMailButton(r.Locale, holderAnswerReminderButtonCopy.in(r.Locale), r.CustomerAreaURL)
	return renderMail(mailKindEventRow, r.Locale, chrome, r.Subject(),
		holderAnswerReminderPreheaderCopy.in(r.Locale),
		holderAnswerReminderFooterReasonCopy.in(r.Locale),
		eventRowCard{
			Heading:  r.Subject(),
			Intro:    splitLines([]string{opening.in(r.Locale)}),
			Rows:     rows,
			LinkLead: holderAnswerReminderActionLeadCopy.in(r.Locale),
			Button:   &button,
			After:    splitLines([]string{holderAnswerReminderSignInCopy.in(r.Locale), closing.in(r.Locale)}),
		},
	)
}

// HTML is the Assignment Reminder's HTML part: the Event row, with the date
// Text() prints, in place of its "Event:" and "Starts:" lines, and the Sale's
// page as the one button with the disclosure after it, as the text has it.
func (r AssignmentReminder) HTML(chrome MailChrome) (string, error) {
	event := r.Event
	event.StartsAt, event.Timezone = r.EventStartsAt, r.EventTimezone
	tally := assignmentReminderTallyCopy
	if r.UnassignedTickets == 1 {
		tally = assignmentReminderTallyOneCopy
	}
	blocks := []string{
		fmt.Sprintf(assignmentReminderGreetingCopy.in(r.Locale), r.FirstName),
		fmt.Sprintf(tally.in(r.Locale), r.UnassignedTickets, r.TotalTickets),
	}
	if r.SaleCreatedAt.Before(TicketAssignmentWentLiveAt) {
		blocks = append(blocks, assignmentReminderGoLiveCopy.in(r.Locale))
	}
	button := newMailButton(r.Locale, assignmentReminderButtonCopy.in(r.Locale), r.ConfirmationLink)
	return renderMail(mailKindEventRow, r.Locale, chrome, r.Subject(),
		assignmentReminderPreheaderCopy.in(r.Locale),
		fmt.Sprintf(buyerFooterReasonCopy.in(r.Locale), r.EventName),
		eventRowCard{
			Heading:  r.Subject(),
			Rows:     []eventRowView{newEventRow(r.Locale, r.EventName, "", event)},
			Blocks:   splitLines(blocks),
			LinkLead: assignmentReminderActionLeadCopy.in(r.Locale),
			Button:   &button,
			After:    splitLines([]string{assignmentReminderDisclosureCopy.in(r.Locale), assignmentReminderClosingCopy.in(r.Locale)}),
		},
	)
}

// eventRowNotice renders a compact Event row kind with no link: the subject
// as heading, the Event row, and the text part's paragraphs.
func eventRowNotice(locale Locale, chrome MailChrome, subject, text, preheader, reason, eventName string, event MailEvent) (string, error) {
	return renderMail(mailKindEventRow, locale, chrome, subject, preheader, reason,
		eventRowCard{
			Heading: subject,
			Rows:    []eventRowView{newEventRow(locale, eventName, "", event)},
			Blocks:  splitLines(paragraphs(text)),
		})
}

// HTML is the void notice's HTML part. It has no button: the text has no link.
func (v SaleVoided) HTML(chrome MailChrome) (string, error) {
	return eventRowNotice(v.Locale, chrome, v.Subject(), v.Text(),
		fmt.Sprintf(saleVoidedPreheaderCopy.in(v.Locale), v.Reference),
		fmt.Sprintf(buyerFooterReasonCopy.in(v.Locale), v.EventName),
		v.EventName, v.Event)
}

// HTML is the refused-reversal notice's HTML part, with no button.
func (r SaleReversalRefused) HTML(chrome MailChrome) (string, error) {
	return eventRowNotice(r.Locale, chrome, r.Subject(), r.Text(),
		fmt.Sprintf(saleReversalRefusedPreheaderCopy.in(r.Locale), r.Reference),
		fmt.Sprintf(buyerFooterReasonCopy.in(r.Locale), r.EventName),
		r.EventName, r.Event)
}

// HTML is No Longer Holding's HTML part. It has no button, deliberately: there
// is nothing left for the reader to do (see NoLongerHolding).
func (n NoLongerHolding) HTML(chrome MailChrome) (string, error) {
	return eventRowNotice(n.Locale, chrome, n.Subject(), n.Text(),
		noLongerHoldingPreheaderCopy.in(n.Locale),
		fmt.Sprintf(noLongerHoldingFooterReasonCopy.in(n.Locale), n.EventName),
		n.EventName, n.Event)
}

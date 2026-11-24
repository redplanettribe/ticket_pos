package platform

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/net/html"
)

// The HTML mail harness (ADR 0082, spec #870), reused by every kind that gains
// an HTML part: one golden file per kind, fixture and locale under
// testdata/mail, and the structural assertions run over every one of them.
//
//	go test ./internal/platform -run TestMailHTMLGoldens -update
//
// rewrites the goldens; review the diff before committing it.

var updateMailGoldens = flag.Bool("update", false, "rewrite the golden HTML mail files")

// Invented throughout: the platform's own asset origin, a media bucket, and an
// Issuer nobody registered.
const (
	goldenAssetOrigin = "https://tickets.example.com"
	goldenMediaOrigin = "https://media.example.com"
)

var goldenChrome = MailChrome{
	MarkURL:         goldenAssetOrigin + "/icon-192.png",
	OperatorName:    "FUNDACION EJEMPLO",
	OperatorCountry: "Ecuador",
	SupportEmail:    DefaultSupportEmail,
}

// mailFixture is one rendered mail: a golden file name, the locale it was
// written in, and both parts.
type mailFixture struct {
	name   string
	locale Locale
	html   func(MailChrome) (string, error)
	text   string
}

// mailFixtures is every kind with an HTML part, in both languages. A kind that
// gains one belongs here, and its goldens with it.
func mailFixtures() []mailFixture {
	var fixtures []mailFixture
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		otp := OTPMessage{Code: "482913", Locale: locale}
		fixtures = append(fixtures, mailFixture{"otp-" + string(locale), locale, otp.HTML, otp.Text()})

		for _, variant := range []struct {
			name                  string
			marketing, networking bool
		}{
			{"marketing", true, false},
			{"networking", false, true},
			{"both", true, true},
		} {
			c := ConsentWithdrawalConfirmation{To: "ana@example.com", Locale: locale, MarketingConsent: variant.marketing, NetworkingConsent: variant.networking}
			fixtures = append(fixtures, mailFixture{"consentwithdrawal-" + variant.name + "-" + string(locale), locale, c.HTML, c.Text()})
		}

		for _, variant := range []struct {
			name string
			edit func(*TaxDocumentDelivery)
		}{
			{"invoice", func(*TaxDocumentDelivery) {}},
			{"invoice-replaces-credited", func(d *TaxDocumentDelivery) { d.Replaces = TaxDocumentReplacesCredited }},
			{"invoice-replaces-annulled", func(d *TaxDocumentDelivery) { d.Replaces = TaxDocumentReplacesAnnulled }},
			{"invoice-reinstated", func(d *TaxDocumentDelivery) { d.Reinstated = true }},
			{"creditnote", func(d *TaxDocumentDelivery) { d.Kind, d.Reason = TaxDocumentKindCreditNote, "refund" }},
			{"creditnote-reissue", func(d *TaxDocumentDelivery) {
				d.Kind, d.Reason = TaxDocumentKindCreditNote, TaxDocumentReasonReissue
			}},
		} {
			d := goldenTaxDocumentDelivery(locale)
			variant.edit(&d)
			fixtures = append(fixtures, mailFixture{"taxdocument-" + variant.name + "-" + string(locale), locale, d.HTML, d.Text()})
		}

		for _, variant := range []struct {
			name     string
			reversed bool
		}{
			{"standing", false},
			{"reversed", true},
		} {
			n := goldenAnnulmentNotice(locale)
			n.SaleReversed = variant.reversed
			fixtures = append(fixtures, mailFixture{"annulmentnotice-" + variant.name + "-" + string(locale), locale, n.HTML, n.Text()})
		}

		fixtures = append(fixtures, staffMailFixtures(locale)...)
		fixtures = append(fixtures, eventRowMailFixtures(locale)...)
		fixtures = append(fixtures, coverMailFixtures(locale)...)
		fixtures = append(fixtures, digestMailFixtures(locale)...)
	}
	return fixtures
}

// digestMailFixtures is the Follow Digest in one locale (#880): both sections
// with an Event the reader holds, one that registers elsewhere and one with
// no renditions beside the rendered ones; every section capped, with a link
// and without ("-capped"); no renditions at all ("-noimages"); and only the
// agenda, with no Storefront origin and no unsubscribe link ("-nolinks").
func digestMailFixtures(locale Locale) []mailFixture {
	suffix := "-" + string(locale)
	var fixtures []mailFixture
	add := func(name string, d FollowDigest) {
		fixtures = append(fixtures, mailFixture{name + suffix, locale, d.HTML, d.Text()})
	}
	add("followdigest", goldenFollowDigest(locale))

	capped := goldenFollowDigest(locale)
	capped.NewOverflow = FollowDigestOverflow{Count: 4, URL: goldenAssetOrigin + "/?tags=jazz"}
	capped.HappeningOverflow = FollowDigestOverflow{Count: 2}
	add("followdigest-capped", capped)

	noImages := goldenFollowDigest(locale)
	for _, section := range [][]FollowDigestEvent{noImages.New, noImages.Happening} {
		for i := range section {
			section[i].SquareURL, section[i].LogoURL = "", ""
		}
	}
	add("followdigest-noimages", noImages)

	noLinks := goldenFollowDigest(locale)
	noLinks.New = nil
	noLinks.ExploreURL, noLinks.UnsubscribeURL = "", ""
	for i := range noLinks.Happening {
		noLinks.Happening[i].URL, noLinks.Happening[i].TicketSaleURL = "", ""
	}
	add("followdigest-nolinks", noLinks)
	return fixtures
}

// goldenFollowDigest is an invented reader's week: two new Events (one
// rendered, one registering elsewhere with no renditions yet) and two this
// week (one the reader holds Tickets to, one matched by two Tags).
func goldenFollowDigest(locale Locale) FollowDigest {
	jazz := goldenMailEvent()
	return FollowDigest{
		To: "ana@example.com", CustomerName: "Ana", Locale: locale,
		New: []FollowDigestEvent{
			{
				Name: "Noche de Jazz", StartsAt: jazz.StartsAt, Timezone: jazz.Timezone,
				OrganizationName: jazz.OrganizationName, Venue: jazz.VenueName,
				URL: goldenAssetOrigin + "/club-de-jazz/events/noche-de-jazz", MatchedOrganization: true,
				SquareURL: jazz.SquareURL, LogoURL: jazz.LogoURL,
			},
			{
				Name: "Feria del Libro", StartsAt: jazz.StartsAt.AddDate(0, 0, 7), Timezone: jazz.Timezone,
				OrganizationName: "Libros EC", Venue: "Casa de la Cultura",
				URL: goldenAssetOrigin + "/libros-ec/events/feria-del-libro", ExternallyRegistered: true,
				MatchedTagNames: []string{"Literatura"},
			},
		},
		Happening: []FollowDigestEvent{
			{
				Name: "Cine de Barrio", StartsAt: jazz.StartsAt.AddDate(0, 0, -3), Timezone: jazz.Timezone,
				OrganizationName: jazz.OrganizationName, Venue: "Ochoymedio",
				URL: goldenAssetOrigin + "/club-de-jazz/events/cine-de-barrio", MatchedOrganization: true,
				MatchedTagNames: []string{"Cine"}, Attending: true, TicketSaleURL: goldenAssetOrigin + "/tickets",
				SquareURL: goldenMediaOrigin + "/email-square/org-1/event-3/square.jpg", LogoURL: jazz.LogoURL,
			},
			{
				Name: "Rock al Parque", StartsAt: jazz.StartsAt.AddDate(0, 0, -2), Timezone: jazz.Timezone,
				OrganizationName: "Ruido Colectivo",
				URL:              goldenAssetOrigin + "/ruido/events/rock-al-parque",
				MatchedTagNames:  []string{"Música", "Rock"},
				SquareURL:        goldenMediaOrigin + "/email-square/org-2/event-4/square.jpg",
			},
		},
		UnsubscribeURL: goldenAssetOrigin + "/unsubscribe?token=abc",
		ExploreURL:     goldenAssetOrigin + "/",
	}
}

// goldenMailEvent is an invented Event and Organization with every rendition;
// goldenMailEventNoImages is the same with none rendered yet.
func goldenMailEvent() MailEvent {
	return MailEvent{
		OrganizationName: "Club de Jazz",
		LogoURL:          goldenMediaOrigin + "/email-logo/org-1/logo.png",
		WideURL:          goldenMediaOrigin + "/email-wide/org-1/event-1/wide.jpg",
		SquareURL:        goldenMediaOrigin + "/email-square/org-1/event-1/square.jpg",
		StartsAt:         time.Date(2026, 12, 13, 1, 0, 0, 0, time.UTC),
		Timezone:         "America/Guayaquil",
		VenueName:        "Teatro Sucre",
	}
}

func goldenMailEventNoImages() MailEvent {
	event := goldenMailEvent()
	event.LogoURL, event.WideURL, event.SquareURL = "", "", ""
	return event
}

// eventRowMailFixtures is the compact Event row kinds in one locale (#878),
// each with every rendition and again with none ("-noimages").
func eventRowMailFixtures(locale Locale) []mailFixture {
	suffix := "-" + string(locale)
	var fixtures []mailFixture
	add := func(name string, html func(MailChrome) (string, error), text string) {
		fixtures = append(fixtures, mailFixture{name + suffix, locale, html, text})
	}
	for _, images := range []struct {
		suffix string
		event  MailEvent
	}{
		{"", goldenMailEvent()},
		{"-noimages", goldenMailEventNoImages()},
	} {
		answer := goldenHolderAnswerReminder(locale, images.event)
		add("answerreminder"+images.suffix, answer.HTML, answer.Text())

		assign := goldenAssignmentReminder(locale, images.event)
		add("assignmentreminder"+images.suffix, assign.HTML, assign.Text())

		voided := goldenSaleVoided(locale, images.event)
		add("salevoided"+images.suffix, voided.HTML, voided.Text())

		refused := goldenSaleReversalRefused(locale, images.event)
		add("salereversalrefused"+images.suffix, refused.HTML, refused.Text())

		gone := goldenNoLongerHolding(locale, images.event)
		add("nolongerholding"+images.suffix, gone.HTML, gone.Text())
	}

	// A Holder of Tickets for two Events: one row each, and the plural copy.
	plural := goldenHolderAnswerReminder(locale, goldenMailEvent())
	other := goldenMailEventNoImages()
	other.OrganizationName, other.VenueName = "Teatro Abierto", "Casa de la Cultura"
	plural.Tickets = append(plural.Tickets, HolderAnswerReminderTicket{EventName: "Feria del Libro", TicketTypeName: "VIP", Event: other})
	add("answerreminder-plural", plural.HTML, plural.Text())

	// A Sale older than the go-live, which says assigning is new.
	golive := goldenAssignmentReminder(locale, goldenMailEvent())
	golive.SaleCreatedAt = TicketAssignmentWentLiveAt.Add(-time.Hour)
	golive.UnassignedTickets = 1
	add("assignmentreminder-golive", golive.HTML, golive.Text())
	return fixtures
}

func goldenHolderAnswerReminder(locale Locale, event MailEvent) HolderAnswerReminder {
	return HolderAnswerReminder{
		To:              "ana@example.com",
		Tickets:         []HolderAnswerReminderTicket{{EventName: "Noche de Jazz", TicketTypeName: "General", Event: event}},
		CustomerAreaURL: goldenAssetOrigin + "/" + string(locale) + "/tickets",
		Locale:          locale,
	}
}

func goldenAssignmentReminder(locale Locale, event MailEvent) AssignmentReminder {
	return AssignmentReminder{
		To: "ana@example.com", FirstName: "Ana", EventName: "Noche de Jazz",
		EventStartsAt: event.StartsAt, EventTimezone: event.Timezone, Event: event,
		UnassignedTickets: 2, TotalTickets: 3,
		ConfirmationLink: goldenAssetOrigin + "/" + string(locale) + "/c/sale-1?token=abc",
		SaleCreatedAt:    TicketAssignmentWentLiveAt.Add(24 * time.Hour),
		Locale:           locale,
	}
}

func goldenSaleVoided(locale Locale, event MailEvent) SaleVoided {
	return SaleVoided{To: "ana@example.com", CustomerName: "Ana Lopez", EventName: "Noche de Jazz", Reference: "ABC123", Event: event, Locale: locale}
}

func goldenSaleReversalRefused(locale Locale, event MailEvent) SaleReversalRefused {
	return SaleReversalRefused{To: "ana@example.com", CustomerName: "Ana Lopez", EventName: "Noche de Jazz", Reference: "ABC123", Event: event, Locale: locale}
}

func goldenNoLongerHolding(locale Locale, event MailEvent) NoLongerHolding {
	return NoLongerHolding{To: "ana@example.com", EventName: "Noche de Jazz", Event: event, Locale: locale}
}

// coverMailFixtures is the Cover Image kinds in one locale (#879), each with
// every rendition and again with none ("-noimages"), plus a Sale Confirmation
// carrying every optional part, the consent block among them ("-full").
func coverMailFixtures(locale Locale) []mailFixture {
	suffix := "-" + string(locale)
	var fixtures []mailFixture
	add := func(name string, html func(MailChrome) (string, error), text string) {
		fixtures = append(fixtures, mailFixture{name + suffix, locale, html, text})
	}
	for _, images := range []struct {
		suffix string
		event  MailEvent
	}{
		{"", goldenMailEvent()},
		{"-noimages", goldenMailEventNoImages()},
	} {
		confirmation := goldenSaleConfirmation(locale, images.event)
		add("saleconfirmation"+images.suffix, confirmation.HTML, confirmation.Text())

		assignment := goldenTicketAssignment(locale, images.event)
		add("ticketassignment"+images.suffix, assignment.HTML, assignment.Text())

		reAddressing := goldenSaleReAddressing(locale, images.event)
		add("salereaddressing"+images.suffix, reAddressing.HTML, reAddressing.Text())
	}

	full := goldenSaleConfirmation(locale, goldenMailEvent())
	full.TaxID = SaleTaxID{Type: TaxIDTypeCedula, Number: "1712345675"}
	full.SaleInvoiceFollows = true
	full.HasOutstandingAnswers = true
	full.ConsentConfirmationLink = goldenAssetOrigin + "/" + string(locale) + "/consent/confirm?token=def"
	add("saleconfirmation-full", full.HTML, full.Text())
	return fixtures
}

func goldenSaleConfirmation(locale Locale, event MailEvent) SaleConfirmation {
	return SaleConfirmation{
		To: "ana@example.com", CustomerName: "Ana Lopez", EventName: "Noche de Jazz", Reference: "ABC123",
		AmountCents: 2500, Currency: "USD",
		ConfirmationLink: goldenAssetOrigin + "/" + string(locale) + "/c/sale-1?token=abc",
		Event:            event, Locale: locale,
	}
}

func goldenTicketAssignment(locale Locale, event MailEvent) TicketAssignment {
	return TicketAssignment{
		To: "luis@example.com", EventName: "Noche de Jazz", TicketTypeName: "General",
		AcceptURL: goldenAssetOrigin + "/" + string(locale) + "/a?token=abc", Event: event, Locale: locale,
	}
}

func goldenSaleReAddressing(locale Locale, event MailEvent) SaleReAddressing {
	return SaleReAddressing{
		To: "ana@example.com", EventName: "Noche de Jazz", Reference: "ABC123",
		AcceptURL: goldenAssetOrigin + "/" + string(locale) + "/r?token=abc", Event: event, Locale: locale,
	}
}

// staffMailFixtures is the staff and operator kinds in one locale (#875):
// an invented Organization, Event, Issuer and staff.
func staffMailFixtures(locale Locale) []mailFixture {
	suffix := "-" + string(locale)
	var fixtures []mailFixture
	add := func(name string, html func(MailChrome) (string, error), text string) {
		fixtures = append(fixtures, mailFixture{name + suffix, locale, html, text})
	}

	for _, variant := range []struct {
		name      string
		threshold int
	}{
		{"expiring", 7},
		{"expired", 0},
	} {
		w := goldenCertificateExpiryWarning(locale)
		w.Threshold = variant.threshold
		add("certificateexpiry-"+variant.name, w.HTML, w.Text())
	}

	submitted := goldenPayoutRequestSubmitted(locale)
	add("payoutrequest-submitted", submitted.HTML, submitted.Text())

	paid := goldenPayoutRequestPaid(locale)
	add("payoutrequest-paid", paid.HTML, paid.Text())
	paid.AmountCents = 100000
	add("payoutrequest-paid-shortfall", paid.HTML, paid.Text())

	declined := goldenPayoutRequestDeclined(locale)
	add("payoutrequest-declined", declined.HTML, declined.Text())

	sent := goldenPayoutRequestTransferSent(locale)
	add("payoutrequest-transfersent", sent.HTML, sent.Text())

	failed := goldenPayoutRequestTransferFailed(locale)
	add("payoutrequest-transferfailed", failed.HTML, failed.Text())

	reviewSubmitted := goldenQuestionReviewSubmitted(locale)
	add("questionreview-submitted", reviewSubmitted.HTML, reviewSubmitted.Text())

	answered := goldenQuestionReviewAnswered(locale)
	add("questionreview-answered", answered.HTML, answered.Text())

	revoked := goldenTicketQuestionRevoked(locale)
	add("ticketquestionrevoked", revoked.HTML, revoked.Text())
	return fixtures
}

func goldenCertificateExpiryWarning(locale Locale) CertificateExpiryWarning {
	return CertificateExpiryWarning{
		To: "ops@example.com", Locale: locale, Threshold: 7,
		NotAfter: time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC), RUC: "1790012345001",
		IssuerURL: "https://staff.example.com/operator/invoicing/issuer",
	}
}

func goldenPayoutRequestSubmitted(locale Locale) PayoutRequestSubmitted {
	return PayoutRequestSubmitted{
		To: "ops@example.com", Locale: locale, OrganizationName: "Club de Jazz",
		AmountCents: 120000, Currency: "USD", RequestedBy: "luis@example.com", Note: "We owe the venue on Monday",
	}
}

func goldenPayoutRequestPaid(locale Locale) PayoutRequestPaid {
	return PayoutRequestPaid{To: "luis@example.com", Locale: locale, OrganizationName: "Club de Jazz", AmountCents: 120000, RequestedCents: 120000, Currency: "USD"}
}

func goldenPayoutRequestTransferSent(locale Locale) PayoutRequestTransferSent {
	return PayoutRequestTransferSent{
		To: "luis@example.com", Locale: locale, OrganizationName: "Club de Jazz", AmountCents: 120000, Currency: "USD",
		SubmittedAt: time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC),
	}
}

func goldenPayoutRequestTransferFailed(locale Locale) PayoutRequestTransferFailed {
	return PayoutRequestTransferFailed{To: "luis@example.com", Locale: locale, OrganizationName: "Club de Jazz", AmountCents: 120000, Currency: "USD", Reason: "Account closed"}
}

func goldenPayoutRequestDeclined(locale Locale) PayoutRequestDeclined {
	return PayoutRequestDeclined{
		To: "luis@example.com", Locale: locale, OrganizationName: "Club de Jazz",
		AmountCents: 120000, Currency: "USD", Reason: "The balance does not cover it yet",
	}
}

func goldenQuestionReviewSubmitted(locale Locale) QuestionReviewSubmitted {
	return QuestionReviewSubmitted{
		To: "ops@example.com", Locale: locale, OrganizationName: "Club de Jazz", EventName: "Noche de Jazz",
		SubmittedBy: "luis@example.com", QuestionCount: 3, Note: "Doors open early",
	}
}

func goldenQuestionReviewAnswered(locale Locale) QuestionReviewAnswered {
	return QuestionReviewAnswered{
		To: "luis@example.com", Locale: locale, OrganizationName: "Club de Jazz", EventName: "Noche de Jazz",
		AnsweredBy: "ops@example.com",
		Items: []QuestionReviewAnsweredItem{
			{QuestionLabel: "Shirt size", Verdict: "approved"},
			{QuestionLabel: "Meal", OptionLabel: "Chicken", Verdict: "approved"},
			{QuestionLabel: "ID number", Verdict: "refused", Reason: "Not needed for entry"},
		},
	}
}

func goldenTicketQuestionRevoked(locale Locale) TicketQuestionRevoked {
	return TicketQuestionRevoked{
		To: "admin@example.com", Locale: locale, OrganizationName: "Club de Jazz", EventName: "Noche de Jazz",
		QuestionLabel: "Shirt size", Reason: "Asks for a body measurement",
	}
}

// The fiscal fixtures: an invented buyer, Event and factura.
func goldenTaxDocumentDelivery(locale Locale) TaxDocumentDelivery {
	return TaxDocumentDelivery{
		To: "ana@example.com", Kind: TaxDocumentKindSaleInvoice, Number: "001-001-000000042",
		CustomerName: "Ana Lopez", EventName: "Noche de Jazz", Reference: "ABC123",
		CustomerAreaURL: goldenAssetOrigin + "/tickets#sale-1",
		Attachments: []EmailAttachment{
			{Filename: "0707202601.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura/>")},
			{Filename: "0707202601.pdf", ContentType: "application/pdf", Body: []byte("%PDF-1.3")},
		},
		Locale: locale,
	}
}

func goldenAnnulmentNotice(locale Locale) AnnulmentNotice {
	return AnnulmentNotice{
		To: "ana@example.com", CustomerName: "Ana Lopez", EventName: "Noche de Jazz", Reference: "ABC123",
		Number: "001-001-000000042", CustomerAreaURL: goldenAssetOrigin + "/tickets#sale-1", Locale: locale,
	}
}

func TestMailHTMLGoldens(t *testing.T) {
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			path := filepath.Join("testdata", "mail", f.name+".html")
			if *updateMailGoldens {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			if got != string(want) {
				t.Fatalf("%s differs from its golden; rerun with -update and review the diff\n%s", f.name, firstDifference(string(want), got))
			}
		})
	}
}

// Every golden fixture, read the way a client does.
func TestMailHTMLStructure(t *testing.T) {
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertMailStructure(t, body, f.text, f.locale, goldenAssetOrigin, goldenMediaOrigin)
		})
	}
}

// The button component, which no kind in this first set uses, is held to the
// same structure: one primary button, its raw link printed below it.
func TestMailButtonCarriesItsRawLinkFallback(t *testing.T) {
	const link = "https://tickets.example.com/c/abc?token=x&y=1"
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			templates, err := parseMailTemplates(withKind(t, "buttontest", `{{define "card"}}{{template "cardOpen"}}{{template "button" .}}{{template "cardClose"}}{{end}}`), withCard(mailKinds, "buttontest", mailButton{}))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			saved := mailTemplates
			mailTemplates = templates
			t.Cleanup(func() { mailTemplates = saved })

			body, err := renderMail("buttontest", locale, goldenChrome, "Subject", "Preheader", "Reason", newMailButton(locale, "View your tickets", link))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertMailStructure(t, body, "", locale, goldenAssetOrigin, goldenMediaOrigin)
			doc := parseMail(t, body)
			if n := len(findAll(doc, isPrimaryButton)); n != 1 {
				t.Fatalf("found %d primary buttons, want 1", n)
			}
			if !strings.Contains(textOf(doc), mailLinkFallbackCopy.in(locale)) {
				t.Fatalf("the fallback sentence %q is missing", mailLinkFallbackCopy.in(locale))
			}
		})
	}
}

// The footer's legal line names the Issuer as registered, in the mail's
// language, and is absent when no Issuer is configured.
func TestMailFooterLegalLine(t *testing.T) {
	cases := []struct {
		locale Locale
		want   string
	}{
		{LocaleEN, "Multiticketing is operated by FUNDACION EJEMPLO, Ecuador."},
		{LocaleES, "Multiticketing es operado por FUNDACION EJEMPLO, Ecuador."},
	}
	for _, tc := range cases {
		t.Run(string(tc.locale), func(t *testing.T) {
			withIssuer, err := OTPMessage{Code: "1", Locale: tc.locale}.HTML(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(textOf(parseMail(t, withIssuer)), tc.want) {
				t.Fatalf("legal line %q missing from the footer", tc.want)
			}

			noIssuer := goldenChrome
			noIssuer.OperatorName, noIssuer.OperatorCountry = "", ""
			without, err := ConsentWithdrawalConfirmation{Locale: tc.locale, MarketingConsent: true}.HTML(noIssuer)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			text := textOf(parseMail(t, without))
			for _, absent := range []string{"operated by", "operado por", "FUNDACION"} {
				if strings.Contains(text, absent) {
					t.Fatalf("with no Issuer the footer still says %q", absent)
				}
			}
			if !strings.Contains(text, DefaultSupportEmail) {
				t.Fatalf("with no Issuer the support contact went missing too")
			}
		})
	}
}

// Half an Issuer is no Issuer: a name without a country, or the reverse,
// omits the legal line rather than printing half of it.
func TestMailFooterLegalLineNeedsBothNameAndCountry(t *testing.T) {
	for _, chrome := range []MailChrome{
		{OperatorName: "FUNDACION EJEMPLO"},
		{OperatorCountry: "Ecuador"},
	} {
		body, err := OTPMessage{Code: "1", Locale: LocaleEN}.HTML(chrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(textOf(parseMail(t, body)), "operated by") {
			t.Fatalf("chrome %+v printed a legal line", chrome)
		}
	}
}

// The support contact defaults when the chrome names none, as a mailto link.
func TestMailFooterSupportLink(t *testing.T) {
	body, err := OTPMessage{Code: "1", Locale: LocaleEN}.HTML(MailChrome{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	links := findAll(parseMail(t, body), func(n *html.Node) bool {
		return n.Data == "a" && attr(n, "href") == "mailto:"+DefaultSupportEmail
	})
	if len(links) != 1 || textOf(links[0]) != DefaultSupportEmail {
		t.Fatalf("want one mailto:%s link reading the address, got %d", DefaultSupportEmail, len(links))
	}
}

// What people and operators typed reaches the inbox as text. Of the fields
// these kinds interpolate, the Issuer's razón social is the one somebody
// entered by hand.
func TestMailHTMLEscapesEnteredText(t *testing.T) {
	const hostile = `<b>"ACME" & 'Co'</b><img src=x onerror=alert(1)>`
	chrome := goldenChrome
	chrome.OperatorName = hostile
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(chrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(body, "<b>") || strings.Contains(body, "<img src=x") || strings.Contains(body, `"ACME"`) {
				t.Fatalf("entered markup reached the HTML unescaped")
			}
			doc := parseMail(t, body)
			if !strings.Contains(textOf(doc), hostile) {
				t.Fatalf("the entered name does not read as the text that was entered")
			}
			if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "b" })); n != 0 {
				t.Fatalf("found %d <b> elements; entered markup became markup", n)
			}
			assertMailStructure(t, body, f.text, f.locale, goldenAssetOrigin, goldenMediaOrigin)
		})
	}
}

// A template that does not parse, or that names a field its card lacks, is
// refused when the templates are parsed, which happens at package init: the
// process fails at boot, never at a send.
func TestBrokenMailTemplateFailsAtBoot(t *testing.T) {
	cases := map[string]string{
		"does not parse":         `{{define "card"}}<tr><td>{{.Heading</td></tr>{{end}}`,
		"names a missing field":  `{{define "card"}}<tr><td>{{.NoSuchField}}</td></tr>{{end}}`,
		"breaks the escaping":    `{{define "card"}}<tr><td><a href="{{.Heading}}></td></tr>{{end}}`,
		"defines no card at all": `{{define "notacard"}}{{end}}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := withKind(t, "otp", source)
			if _, err := parseMailTemplates(fsys, mailKinds); err == nil {
				t.Fatal("parseMailTemplates accepted a broken template")
			}
			defer func() {
				if recover() == nil {
					t.Fatal("mustParseMailTemplates did not panic on a broken template")
				}
			}()
			mustParseMailTemplates(fsys, mailKinds)
		})
	}

	t.Run("a declared kind with no file", func(t *testing.T) {
		kinds := withCard(mailKinds, "missing", otpCard{})
		if _, err := parseMailTemplates(mailTemplateFiles, kinds); err == nil {
			t.Fatal("parseMailTemplates accepted a kind with no template file")
		}
	})
	t.Run("the shipped templates parse", func(t *testing.T) {
		if _, err := parseMailTemplates(mailTemplateFiles, mailKinds); err != nil {
			t.Fatalf("parse: %v", err)
		}
	})
}

// withCard is kinds with one more declared.
func withCard(kinds map[mailKind]any, kind mailKind, card any) map[mailKind]any {
	all := map[mailKind]any{kind: card}
	for name, c := range kinds {
		all[name] = c
	}
	return all
}

// withKind is the shipped templates with one kind's file replaced (or added).
func withKind(t *testing.T, kind mailKind, source string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	err := fs.WalkDir(mailTemplateFiles, "mailtemplates", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := mailTemplateFiles.ReadFile(name)
		if err != nil {
			return err
		}
		fsys[name] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("read the shipped templates: %v", err)
	}
	fsys["mailtemplates/kinds/"+string(kind)+".html"] = &fstest.MapFile{Data: []byte(source)}
	return fsys
}

// assertMailStructure holds one rendered body (and its text part, when the
// fixture has one) to the spec's structural rules.
func assertMailStructure(t *testing.T, body, text string, locale Locale, assetOrigin, mediaOrigin string) {
	t.Helper()
	doc := parseMail(t, body)

	htmlElements := findAll(doc, func(n *html.Node) bool { return n.Data == "html" })
	if len(htmlElements) != 1 || attr(htmlElements[0], "lang") != string(locale) {
		t.Errorf("<html lang> is not %q", locale)
	}

	for _, table := range findAll(doc, func(n *html.Node) bool { return n.Data == "table" }) {
		if attr(table, "role") != "presentation" {
			t.Errorf("a layout table has no role=\"presentation\"")
		}
	}

	for _, img := range findAll(doc, func(n *html.Node) bool { return n.Data == "img" }) {
		if !hasAttr(img, "alt") {
			t.Errorf("<img src=%q> has no alt", attr(img, "src"))
		}
		src := attr(img, "src")
		if !strings.HasPrefix(src, assetOrigin+"/") && !strings.HasPrefix(src, mediaOrigin+"/") {
			t.Errorf("<img src=%q> points outside the media bucket and the platform's assets", src)
		}
	}

	buttons := findAll(doc, isPrimaryButton)
	if len(buttons) > 1 {
		t.Errorf("found %d primary buttons, want at most one", len(buttons))
	}
	for _, button := range buttons {
		href := attr(button, "href")
		fallback := findAll(doc, func(n *html.Node) bool {
			return n.Data == "a" && !isPrimaryButton(n) && attr(n, "href") == href && textOf(n) == href
		})
		if len(fallback) == 0 {
			t.Errorf("the button to %q has no raw-link fallback", href)
			continue
		}
		if !strings.Contains(textOf(fallback[0].Parent), mailLinkFallbackCopy.in(locale)) {
			t.Errorf("the raw link to %q is not introduced by %q", href, mailLinkFallbackCopy.in(locale))
		}
	}

	for name, part := range map[string]string{"html": body, "text": text} {
		if strings.ContainsRune(part, '\u2014') {
			t.Errorf("the %s part contains an em dash", name)
		}
	}

	preheaders := findAll(doc, func(n *html.Node) bool { return n.Data == "div" && hasClass(n, "preheader") })
	if len(preheaders) != 1 {
		t.Fatalf("found %d preheaders, want 1", len(preheaders))
	}
	preheader := preheaders[0]
	if !strings.Contains(attr(preheader, "style"), "display:none") {
		t.Errorf("the preheader is not hidden")
	}
	if strings.TrimSpace(strings.Trim(textOf(preheader), "\u034f\u200c\u00a0 ")) == "" {
		t.Errorf("the preheader is empty")
	}
	if !strings.Contains(textOf(preheader), strings.Repeat("\u034f\u200c\u00a0", 60)) {
		t.Errorf("the preheader is not padded")
	}
	if first := firstElementChild(findAll(doc, func(n *html.Node) bool { return n.Data == "body" })[0]); first != preheader {
		t.Errorf("the preheader is not the first thing in the body")
	}
}

func isPrimaryButton(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "button")
}

func parseMail(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func firstElementChild(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// textOf is the text a reader sees in n, with whitespace runs collapsed.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' }), " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// firstDifference shows where a render left its golden, which a full dump of
// two pages of markup would bury.
func firstDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	from := max(0, i-80)
	return "golden: ..." + want[from:min(len(want), i+80)] + "...\nrender: ..." + got[from:min(len(got), i+80)] + "..."
}

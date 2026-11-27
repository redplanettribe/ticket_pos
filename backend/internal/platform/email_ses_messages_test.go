package platform

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Every transactional message through SES (#693, ADR 0077), at the same seam
// as the passcode: the sender's real request path against the fake SES v2
// endpoint. Each message's subject and text must be the shared copy from
// email_content.go, so both providers say exactly the same words.

// messageCase is one mail kind: its name as the
// sender's errors and logs give it, the recipient, the shared copy it must
// carry, any attachments, and the call that sends it. The SES and the SMTP
// sender tests (email_smtp_test.go) both run every case.
type messageCase struct {
	name    string
	to      string
	subject string
	text    string
	// html is the HTML part the message must carry beside the text; every
	// kind has one (ADR 0082).
	html        string
	attachments []EmailAttachment
	send        func(ctx context.Context, s EmailSender) error
}

// testHTML is a message's HTML part as the senders render it, with the chrome
// a test sender is built with (none).
func testHTML(render func(MailChrome) (string, error)) string {
	html, err := render(MailChrome{})
	if err != nil {
		panic(err)
	}
	return html
}

// transactionalMessageCases lists every message on the transactional
// interface except the passcode (covered in email_ses_test.go) and the Follow
// Digest (the Digest identity's, #696). A method added to EmailSender without
// an SES or SMTP implementation fails to compile at the assertions in
// email_ses_test.go and email_smtp.go; one added with an implementation
// belongs in this list.
func transactionalMessageCases() []messageCase {
	startsAt := time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC)

	saleConfirmation := SaleConfirmation{
		To: "Ana@Example.com", CustomerName: "Ana", EventName: "Noche de Jazz",
		Reference: "TS-1001", AmountCents: 2500, Currency: "USD",
		ConfirmationLink: "https://shop.example.com/c/abc", Locale: LocaleES,
	}
	// The Event row kinds carry their imagery, so the HTML part the providers
	// send shows it.
	event := MailEvent{
		OrganizationName: "Jazz Club", LogoURL: "https://media.example.com/email-logo/o/l.png",
		SquareURL: "https://media.example.com/email-square/o/e/s.jpg", StartsAt: startsAt, Timezone: "America/Guayaquil", VenueName: "Teatro",
	}
	saleConfirmation.Event = event
	saleVoided := SaleVoided{To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1002", Event: event, Locale: LocaleEN}
	reversalRefused := SaleReversalRefused{To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1003", Event: event, Locale: LocaleES}
	taxDocument := TaxDocumentDelivery{
		To: "ana@example.com", Kind: "invoice", CustomerName: "Ana", EventName: "Noche de Jazz",
		Reference: "TS-1004", CustomerAreaURL: "https://shop.example.com/me",
		Attachments: []EmailAttachment{
			{Filename: "0707202601.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura>ñ</factura>")},
			{Filename: "0707202601.pdf", ContentType: "application/pdf", Body: []byte("%PDF-1.3\x00\xff\x10binary")},
		},
		Locale: LocaleES,
	}
	annulmentNotice := AnnulmentNotice{
		To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1004",
		Number: "001-001-000000007", CustomerAreaURL: "https://shop.example.com/me", Locale: LocaleES,
	}
	withdrawal := ConsentWithdrawalConfirmation{To: "ana@example.com", Locale: LocaleEN, MarketingConsent: true}
	answerReminder := HolderAnswerReminder{
		To:              "ana@example.com",
		Tickets:         []HolderAnswerReminderTicket{{EventName: "Noche de Jazz", TicketTypeName: "General", Event: event}},
		CustomerAreaURL: "https://shop.example.com/me", Locale: LocaleES,
	}
	assignmentReminder := AssignmentReminder{
		To: "ana@example.com", FirstName: "Ana", EventName: "Noche de Jazz",
		EventStartsAt: startsAt, EventTimezone: "America/Guayaquil", Event: event,
		UnassignedTickets: 2, TotalTickets: 3, ConfirmationLink: "https://shop.example.com/c/abc",
		SaleCreatedAt: startsAt.AddDate(0, -1, 0), Locale: LocaleEN,
	}
	assignment := TicketAssignment{To: "ana@example.com", EventName: "Noche de Jazz", TicketTypeName: "General", AcceptURL: "https://shop.example.com/a/xyz", Event: event, Locale: LocaleES}
	noLongerHolding := NoLongerHolding{To: "ana@example.com", EventName: "Noche de Jazz", Event: event, Locale: LocaleEN}
	reAddressing := SaleReAddressing{To: "ana@example.com", EventName: "Noche de Jazz", Reference: "TS-1005", AcceptURL: "https://shop.example.com/r/xyz", Event: event, Locale: LocaleES}
	questionRevoked := TicketQuestionRevoked{To: "admin@example.com", Locale: LocaleEN, OrganizationName: "Jazz Club", EventName: "Noche de Jazz", QuestionLabel: "Talla", Reason: "No longer needed"}
	payoutSubmitted := PayoutRequestSubmitted{To: "ops@example.com", Locale: LocaleES, OrganizationName: "Jazz Club", AmountCents: 120000, Currency: "USD", RequestedBy: "Luis"}
	reviewSubmitted := QuestionReviewSubmitted{To: "ops@example.com", Locale: LocaleEN, OrganizationName: "Jazz Club", EventName: "Noche de Jazz", SubmittedBy: "Luis", QuestionCount: 2}
	reviewAnswered := QuestionReviewAnswered{
		To: "admin@example.com", Locale: LocaleES, OrganizationName: "Jazz Club", EventName: "Noche de Jazz", AnsweredBy: "Marta",
		Items: []QuestionReviewAnsweredItem{{QuestionLabel: "Talla", Verdict: "approved"}},
	}
	payoutPaid := PayoutRequestPaid{To: "admin@example.com", Locale: LocaleEN, OrganizationName: "Jazz Club", AmountCents: 120000, RequestedCents: 120000, Currency: "USD"}
	payoutDeclined := PayoutRequestDeclined{To: "admin@example.com", Locale: LocaleES, OrganizationName: "Jazz Club", AmountCents: 120000, Currency: "USD", Reason: "Saldo insuficiente"}
	transferSent := PayoutRequestTransferSent{To: "admin@example.com", Locale: LocaleEN, OrganizationName: "Jazz Club", AmountCents: 120000, Currency: "USD", SubmittedAt: startsAt}
	transferFailed := PayoutRequestTransferFailed{To: "admin@example.com", Locale: LocaleES, OrganizationName: "Jazz Club", AmountCents: 120000, Currency: "USD", Reason: "Cuenta cerrada"}
	expiry := CertificateExpiryWarning{To: "ops@example.com", Locale: LocaleEN, Threshold: 7, NotAfter: startsAt, RUC: "1790012345001", IssuerURL: "https://staff.example.com/issuer"}
	recap := goldenEventRecap(LocaleES)

	return []messageCase{
		{"sale confirmation", saleConfirmation.To, saleConfirmation.Subject(), saleConfirmation.Text(), testHTML(saleConfirmation.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendSaleConfirmation(ctx, saleConfirmation)
			}},
		{"sale voided", saleVoided.To, saleVoided.Subject(), saleVoided.Text(), testHTML(saleVoided.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendSaleVoided(ctx, saleVoided) }},
		{"sale reversal refused", reversalRefused.To, reversalRefused.Subject(), reversalRefused.Text(), testHTML(reversalRefused.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendSaleReversalRefused(ctx, reversalRefused)
			}},
		{"tax document delivery", taxDocument.To, taxDocument.Subject(), taxDocument.Text(), testHTML(taxDocument.HTML), taxDocument.Attachments,
			func(ctx context.Context, s EmailSender) error { return s.SendTaxDocumentDelivery(ctx, taxDocument) }},
		{"annulment notice", annulmentNotice.To, annulmentNotice.Subject(), annulmentNotice.Text(), testHTML(annulmentNotice.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendAnnulmentNotice(ctx, annulmentNotice) }},
		{"consent withdrawal confirmation", withdrawal.To, withdrawal.Subject(), withdrawal.Text(), testHTML(withdrawal.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendConsentWithdrawalConfirmation(ctx, withdrawal)
			}},
		{"holder answer reminder", answerReminder.To, answerReminder.Subject(), answerReminder.Text(), testHTML(answerReminder.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendHolderAnswerReminder(ctx, answerReminder)
			}},
		{"assignment reminder", assignmentReminder.To, assignmentReminder.Subject(), assignmentReminder.Text(), testHTML(assignmentReminder.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendAssignmentReminder(ctx, assignmentReminder)
			}},
		{"ticket assignment", assignment.To, assignment.Subject(), assignment.Text(), testHTML(assignment.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendTicketAssignment(ctx, assignment) }},
		{"no longer holding", noLongerHolding.To, noLongerHolding.Subject(), noLongerHolding.Text(), testHTML(noLongerHolding.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendNoLongerHolding(ctx, noLongerHolding) }},
		{"sale re-addressing", reAddressing.To, reAddressing.Subject(), reAddressing.Text(), testHTML(reAddressing.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendSaleReAddressing(ctx, reAddressing) }},
		{"ticket question revoked", questionRevoked.To, questionRevoked.Subject(), questionRevoked.Text(), testHTML(questionRevoked.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendTicketQuestionRevoked(ctx, questionRevoked)
			}},
		{"payout request submitted", payoutSubmitted.To, payoutSubmitted.Subject(), payoutSubmitted.Text(), testHTML(payoutSubmitted.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendPayoutRequestSubmitted(ctx, payoutSubmitted)
			}},
		{"question review submitted", reviewSubmitted.To, reviewSubmitted.Subject(), reviewSubmitted.Text(), testHTML(reviewSubmitted.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendQuestionReviewSubmitted(ctx, reviewSubmitted)
			}},
		{"question review answered", reviewAnswered.To, reviewAnswered.Subject(), reviewAnswered.Text(), testHTML(reviewAnswered.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendQuestionReviewAnswered(ctx, reviewAnswered)
			}},
		{"payout request paid", payoutPaid.To, payoutPaid.Subject(), payoutPaid.Text(), testHTML(payoutPaid.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendPayoutRequestPaid(ctx, payoutPaid) }},
		{"payout request declined", payoutDeclined.To, payoutDeclined.Subject(), payoutDeclined.Text(), testHTML(payoutDeclined.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendPayoutRequestDeclined(ctx, payoutDeclined)
			}},
		{"payout request transfer sent", transferSent.To, transferSent.Subject(), transferSent.Text(), testHTML(transferSent.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendPayoutRequestTransferSent(ctx, transferSent)
			}},
		{"payout request transfer failed", transferFailed.To, transferFailed.Subject(), transferFailed.Text(), testHTML(transferFailed.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendPayoutRequestTransferFailed(ctx, transferFailed)
			}},
		{"certificate expiry warning", expiry.To, expiry.Subject(), expiry.Text(), testHTML(expiry.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendCertificateExpiryWarning(ctx, expiry) }},
		{"event recap", recap.To, recap.Subject(), recap.Text(), testHTML(recap.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendEventRecap(ctx, recap) }},
	}
}

// One message, one SES request: the transactional From, the recipient alone,
// the shared copy as UTF-8 text with the HTML part beside it, the configured
// configuration set, and
// attachments on the Tax Document delivery and nowhere else.
func TestSESSendsEveryTransactionalMessageWithTheSharedCopy(t *testing.T) {
	for _, tc := range transactionalMessageCases() {
		t.Run(tc.name, func(t *testing.T) {
			sender, fake := newTestSESSender(t, noopLogger{})

			if err := tc.send(context.Background(), sender); err != nil {
				t.Fatalf("send: %v", err)
			}

			sent := fake.sent()
			if len(sent) != 1 {
				t.Fatalf("fake SES saw %d requests, want 1", len(sent))
			}
			req := sent[0]
			if req.Method != http.MethodPost || req.Path != "/v2/email/outbound-emails" {
				t.Fatalf("request = %s %s, want POST /v2/email/outbound-emails", req.Method, req.Path)
			}
			body := req.Body
			if body.FromEmailAddress != sesTestFrom {
				t.Fatalf("From = %q, want %q", body.FromEmailAddress, sesTestFrom)
			}
			if got := body.Destination.ToAddresses; len(got) != 1 || got[0] != tc.to {
				t.Fatalf("To = %v, want [%s]", got, tc.to)
			}
			if len(body.Destination.CcAddresses) != 0 || len(body.Destination.BccAddresses) != 0 {
				t.Fatalf("unexpected Cc/Bcc: %+v", body.Destination)
			}
			if body.ConfigurationSetName != "ticket-pos-transactional" {
				t.Fatalf("ConfigurationSetName = %q, want ticket-pos-transactional", body.ConfigurationSetName)
			}
			simple := body.Content.Simple
			if simple == nil {
				t.Fatal("no Simple content in the request")
			}
			if tc.subject == "" || tc.text == "" {
				t.Fatalf("the fixture's shared copy is empty: subject %q, text %q", tc.subject, tc.text)
			}
			if simple.Subject.Data != tc.subject || simple.Subject.Charset != "UTF-8" {
				t.Fatalf("Subject = %+v, want %q in UTF-8", simple.Subject, tc.subject)
			}
			if simple.Body.Text == nil || simple.Body.Text.Data != tc.text || simple.Body.Text.Charset != "UTF-8" {
				t.Fatalf("Text = %+v, want %q in UTF-8", simple.Body.Text, tc.text)
			}
			if tc.html == "" {
				t.Fatal("the fixture has no HTML part")
			}
			if simple.Body.Html == nil || simple.Body.Html.Data != tc.html || simple.Body.Html.Charset != "UTF-8" {
				t.Fatalf("Html is not the message's HTML part in UTF-8 (%d bytes want)", len(tc.html))
			}

			if len(simple.Attachments) != len(tc.attachments) {
				t.Fatalf("request carries %d attachments, want %d", len(simple.Attachments), len(tc.attachments))
			}
			for i, want := range tc.attachments {
				got := simple.Attachments[i]
				if got.FileName != want.Filename {
					t.Errorf("attachment %d FileName = %q, want %q", i, got.FileName, want.Filename)
				}
				if got.ContentType != want.ContentType {
					t.Errorf("attachment %d ContentType = %q, want %q", i, got.ContentType, want.ContentType)
				}
				if got.ContentDisposition != "ATTACHMENT" {
					t.Errorf("attachment %d ContentDisposition = %q, want ATTACHMENT", i, got.ContentDisposition)
				}
				// Left unset, SES sends the part as 7bit and drops a binary body (#702).
				if got.ContentTransferEncoding != "BASE64" {
					t.Errorf("attachment %d ContentTransferEncoding = %q, want BASE64", i, got.ContentTransferEncoding)
				}
				if !bytes.Equal(got.RawContent, want.Body) {
					t.Errorf("attachment %d bytes = %q, want %q", i, got.RawContent, want.Body)
				}
			}
		})
	}
}

// A rejected message reaches its caller, which decides what the failure means.
// The error and the one failure log name the message type and the provider's
// error and never the recipient (#377), even when SES quotes it back.
func TestSESReturnsEveryMessagesFailureAndNeverLogsTheRecipient(t *testing.T) {
	for _, tc := range transactionalMessageCases() {
		t.Run(tc.name, func(t *testing.T) {
			logger := &lineLogger{}
			sender, fake := newTestSESSender(t, logger)
			fake.respond = func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Amzn-ErrorType", "MessageRejected")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"Email address is not verified. The following identities failed the check in region US-EAST-1: ` + strings.ToUpper(tc.to) + `"}`))
			}

			err := tc.send(context.Background(), sender)
			if err == nil {
				t.Fatal("send returned nil on a rejected message")
			}
			recipient := strings.ToLower(tc.to)
			if !strings.Contains(err.Error(), "ses send "+tc.name) {
				t.Fatalf("error %q does not name the message type %q", err, tc.name)
			}
			if !strings.Contains(err.Error(), "MessageRejected") {
				t.Fatalf("error %q does not carry the provider's error", err)
			}
			if strings.Contains(strings.ToLower(err.Error()), recipient) {
				t.Fatalf("error %q names the recipient", err)
			}

			lines := logger.errors()
			if len(lines) != 1 {
				t.Fatalf("logged %d error lines, want 1: %v", len(lines), lines)
			}
			line := lines[0].String()
			if !strings.Contains(line, "ses send "+tc.name+" failed") {
				t.Fatalf("failure log %q does not name the message type", line)
			}
			if !strings.Contains(line, "MessageRejected") {
				t.Fatalf("failure log %q does not carry the provider's error", line)
			}
			if strings.Contains(strings.ToLower(line), recipient) {
				t.Fatalf("failure log %q names the recipient", line)
			}
		})
	}
}

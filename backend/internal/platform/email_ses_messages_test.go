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

// sesMessageCase is one transactional message: its name as the sender's
// errors and logs give it, the recipient, the shared copy it must carry, any
// attachments, and the call that sends it.
type sesMessageCase struct {
	name        string
	to          string
	subject     string
	text        string
	attachments []EmailAttachment
	send        func(ctx context.Context, s *SESEmailSender) error
}

// sesTransactionalMessages lists every message on the transactional
// interface except the passcode (covered in email_ses_test.go) and the Follow
// Digest (the Digest identity's, #696). A method added to EmailSender without
// an SES implementation fails to compile at the assertion in email_ses_test.go;
// one added with an implementation belongs in this list.
func sesTransactionalMessages() []sesMessageCase {
	startsAt := time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC)

	saleConfirmation := SaleConfirmation{
		To: "Ana@Example.com", CustomerName: "Ana", EventName: "Noche de Jazz",
		Reference: "TS-1001", AmountCents: 2500, Currency: "USD",
		ConfirmationLink: "https://shop.example.com/c/abc", Locale: LocaleES,
	}
	saleVoided := SaleVoided{To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1002", Locale: LocaleEN}
	reversalRefused := SaleReversalRefused{To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1003", Locale: LocaleES}
	taxDocument := TaxDocumentDelivery{
		To: "ana@example.com", Kind: "invoice", CustomerName: "Ana", EventName: "Noche de Jazz",
		Reference: "TS-1004", CustomerAreaURL: "https://shop.example.com/me",
		Attachments: []EmailAttachment{
			{Filename: "0707202601.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura>ñ</factura>")},
			{Filename: "0707202601.pdf", ContentType: "application/pdf", Body: []byte("%PDF-1.3\x00\xff\x10binary")},
		},
		Locale: LocaleES,
	}
	withdrawal := ConsentWithdrawalConfirmation{To: "ana@example.com", Locale: LocaleEN, MarketingConsent: true}
	answerReminder := HolderAnswerReminder{
		To:              "ana@example.com",
		Tickets:         []HolderAnswerReminderTicket{{EventName: "Noche de Jazz", TicketTypeName: "General"}},
		CustomerAreaURL: "https://shop.example.com/me", Locale: LocaleES,
	}
	assignmentReminder := AssignmentReminder{
		To: "ana@example.com", FirstName: "Ana", EventName: "Noche de Jazz",
		EventStartsAt: startsAt, EventTimezone: "America/Guayaquil",
		UnassignedTickets: 2, TotalTickets: 3, ConfirmationLink: "https://shop.example.com/c/abc",
		SaleCreatedAt: startsAt.AddDate(0, -1, 0), Locale: LocaleEN,
	}
	assignment := TicketAssignment{To: "ana@example.com", EventName: "Noche de Jazz", TicketTypeName: "General", AcceptURL: "https://shop.example.com/a/xyz", Locale: LocaleES}
	noLongerHolding := NoLongerHolding{To: "ana@example.com", EventName: "Noche de Jazz", Locale: LocaleEN}
	reAddressing := SaleReAddressing{To: "ana@example.com", EventName: "Noche de Jazz", Reference: "TS-1005", AcceptURL: "https://shop.example.com/r/xyz", Locale: LocaleES}
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

	return []sesMessageCase{
		{"sale confirmation", saleConfirmation.To, saleConfirmation.Subject(), saleConfirmation.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendSaleConfirmation(ctx, saleConfirmation)
			}},
		{"sale voided", saleVoided.To, saleVoided.Subject(), saleVoided.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendSaleVoided(ctx, saleVoided) }},
		{"sale reversal refused", reversalRefused.To, reversalRefused.Subject(), reversalRefused.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendSaleReversalRefused(ctx, reversalRefused)
			}},
		{"tax document delivery", taxDocument.To, taxDocument.Subject(), taxDocument.Text(), taxDocument.Attachments,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendTaxDocumentDelivery(ctx, taxDocument) }},
		{"consent withdrawal confirmation", withdrawal.To, withdrawal.Subject(), withdrawal.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendConsentWithdrawalConfirmation(ctx, withdrawal)
			}},
		{"holder answer reminder", answerReminder.To, answerReminder.Subject(), answerReminder.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendHolderAnswerReminder(ctx, answerReminder)
			}},
		{"assignment reminder", assignmentReminder.To, assignmentReminder.Subject(), assignmentReminder.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendAssignmentReminder(ctx, assignmentReminder)
			}},
		{"ticket assignment", assignment.To, assignment.Subject(), assignment.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendTicketAssignment(ctx, assignment) }},
		{"no longer holding", noLongerHolding.To, noLongerHolding.Subject(), noLongerHolding.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendNoLongerHolding(ctx, noLongerHolding) }},
		{"sale re-addressing", reAddressing.To, reAddressing.Subject(), reAddressing.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendSaleReAddressing(ctx, reAddressing) }},
		{"ticket question revoked", questionRevoked.To, questionRevoked.Subject(), questionRevoked.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendTicketQuestionRevoked(ctx, questionRevoked)
			}},
		{"payout request submitted", payoutSubmitted.To, payoutSubmitted.Subject(), payoutSubmitted.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendPayoutRequestSubmitted(ctx, payoutSubmitted)
			}},
		{"question review submitted", reviewSubmitted.To, reviewSubmitted.Subject(), reviewSubmitted.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendQuestionReviewSubmitted(ctx, reviewSubmitted)
			}},
		{"question review answered", reviewAnswered.To, reviewAnswered.Subject(), reviewAnswered.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendQuestionReviewAnswered(ctx, reviewAnswered)
			}},
		{"payout request paid", payoutPaid.To, payoutPaid.Subject(), payoutPaid.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendPayoutRequestPaid(ctx, payoutPaid) }},
		{"payout request declined", payoutDeclined.To, payoutDeclined.Subject(), payoutDeclined.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendPayoutRequestDeclined(ctx, payoutDeclined)
			}},
		{"payout request transfer sent", transferSent.To, transferSent.Subject(), transferSent.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendPayoutRequestTransferSent(ctx, transferSent)
			}},
		{"payout request transfer failed", transferFailed.To, transferFailed.Subject(), transferFailed.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error {
				return s.SendPayoutRequestTransferFailed(ctx, transferFailed)
			}},
		{"certificate expiry warning", expiry.To, expiry.Subject(), expiry.Text(), nil,
			func(ctx context.Context, s *SESEmailSender) error { return s.SendCertificateExpiryWarning(ctx, expiry) }},
	}
}

// One message, one SES request: the transactional From, the recipient alone,
// the shared copy as plain UTF-8 text, the configured configuration set, and
// attachments on the Tax Document delivery and nowhere else.
func TestSESSendsEveryTransactionalMessageWithTheSharedCopy(t *testing.T) {
	for _, tc := range sesTransactionalMessages() {
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
			if simple.Body.Html != nil {
				t.Fatalf("the message carries HTML; SES mail is plain text only: %+v", simple.Body.Html)
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
	for _, tc := range sesTransactionalMessages() {
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

package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The logging sender, which local development and the manual checks read,
// says that a kind's HTML part exists and how big it is, and never prints the
// markup (ADR 0082). The passcode line keeps the code, which the e2e helpers
// read from it.
func TestLoggingSenderLogsTheHTMLSizeAndNotTheMarkup(t *testing.T) {
	otp := OTPMessage{Code: "482913", Locale: LocaleES}
	withdrawal := ConsentWithdrawalConfirmation{To: "ana@example.com", Locale: LocaleEN, MarketingConsent: true}
	taxDocument := goldenTaxDocumentDelivery(LocaleES)
	annulment := goldenAnnulmentNotice(LocaleEN)
	expiry := goldenCertificateExpiryWarning(LocaleES)
	payoutSubmitted := goldenPayoutRequestSubmitted(LocaleEN)
	payoutPaid := goldenPayoutRequestPaid(LocaleES)
	payoutDeclined := goldenPayoutRequestDeclined(LocaleEN)
	transferSent := goldenPayoutRequestTransferSent(LocaleEN)
	transferFailed := goldenPayoutRequestTransferFailed(LocaleES)
	reviewSubmitted := goldenQuestionReviewSubmitted(LocaleES)
	reviewAnswered := goldenQuestionReviewAnswered(LocaleEN)
	revoked := goldenTicketQuestionRevoked(LocaleES)
	cases := []struct {
		name   string
		msg    string
		render func(MailChrome) (string, error)
		send   func(context.Context, *LoggingEmailSender) error
	}{
		{"otp", "otp sent", otp.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendOTP(ctx, "ana@example.com", otp.Code, otp.Locale)
		}},
		{"consent withdrawal confirmation", "consent withdrawal confirmation sent", withdrawal.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendConsentWithdrawalConfirmation(ctx, withdrawal)
		}},
		{"tax document delivery", "tax document delivery sent", taxDocument.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendTaxDocumentDelivery(ctx, taxDocument)
		}},
		{"annulment notice", "annulment notice sent", annulment.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendAnnulmentNotice(ctx, annulment)
		}},
		{"certificate expiry warning", "certificate expiry warning sent", expiry.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendCertificateExpiryWarning(ctx, expiry)
		}},
		{"payout request submitted", "payout request submitted notice sent", payoutSubmitted.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendPayoutRequestSubmitted(ctx, payoutSubmitted)
		}},
		{"payout request paid", "payout request paid notice sent", payoutPaid.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendPayoutRequestPaid(ctx, payoutPaid)
		}},
		{"payout request declined", "payout request declined notice sent", payoutDeclined.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendPayoutRequestDeclined(ctx, payoutDeclined)
		}},
		{"payout request transfer sent", "payout request transfer sent notice sent", transferSent.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendPayoutRequestTransferSent(ctx, transferSent)
		}},
		{"payout request transfer failed", "payout request transfer failed notice sent", transferFailed.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendPayoutRequestTransferFailed(ctx, transferFailed)
		}},
		{"question review submitted", "question review submitted notice sent", reviewSubmitted.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendQuestionReviewSubmitted(ctx, reviewSubmitted)
		}},
		{"question review answered", "question review answered notice sent", reviewAnswered.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendQuestionReviewAnswered(ctx, reviewAnswered)
		}},
		{"ticket question revoked", "ticket question revoked notice sent", revoked.HTML, func(ctx context.Context, s *LoggingEmailSender) error {
			return s.SendTicketQuestionRevoked(ctx, revoked)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &lineLogger{}
			sender := &LoggingEmailSender{Logger: logger, Chrome: goldenChrome}
			if err := tc.send(context.Background(), sender); err != nil {
				t.Fatalf("send: %v", err)
			}
			if len(logger.lines) != 1 || logger.lines[0].msg != tc.msg || logger.lines[0].level != "info" {
				t.Fatalf("logged %v, want one %q info line", logger.lines, tc.msg)
			}
			html, err := tc.render(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			attrs := map[string]any{}
			args := logger.lines[0].args
			for i := 0; i+1 < len(args); i += 2 {
				attrs[fmt.Sprint(args[i])] = args[i+1]
			}
			if attrs["html"] != true {
				t.Errorf("html = %v, want true", attrs["html"])
			}
			if attrs["html_bytes"] != len(html) {
				t.Errorf("html_bytes = %v, want %d", attrs["html_bytes"], len(html))
			}
			if line := logger.lines[0].String(); strings.Contains(line, "<") || strings.Contains(line, "doctype") {
				t.Errorf("the log line carries markup: %s", line)
			}
			if tc.name == "otp" && attrs["code"] != otp.Code {
				t.Errorf("code = %v, want the passcode the e2e helpers read", attrs["code"])
			}
		})
	}
}

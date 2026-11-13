package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// ErrNotYetOnSES is what the SES sender returns for a message it does not
// send yet. Only the One-time Passcode travels through SES so far (#692);
// every other transactional message lands with its own ticket under #690.
// Until then a deployment that selects SES for the transactional identity is
// told, on every such send, that the message did not leave.
var ErrNotYetOnSES = errors.New("not yet implemented on SES")

// SESEmailSender delivers transactional email through Amazon SES, beside the
// Resend sender and selected only by an explicit EMAIL_PROVIDER=ses (ADR
// 0077). It is the Resend sender's twin: one private send path every public
// method funnels through, the copy taken from the message's own content
// (email_content.go) and never written here, plain text only.
//
// It sends with the SES v2 API through a named configuration set, so the
// delivery settings live in one place in Terraform. Credentials come from the
// SDK's default credential chain; the Workload Identity Federation exchange
// Cloud Run will use is its own ticket under #690.
//
// Delivery is synchronous and its truth goes to the caller, exactly as with
// Resend: a failed passcode fails the sign-in, and best-effort callers decide
// for themselves what to discard.
//
// A failure log names the message type and the provider's error, never the
// recipient (#377). SES quotes recipients back in some of its errors (its
// sandbox rejection lists the unverified address), so the recipient is
// redacted from the error before it is logged or returned.
type SESEmailSender struct {
	from             string
	configurationSet string
	client           *sesv2.Client
	logger           Logger
}

// sesHTTPTimeout bounds one SES request attempt, and sesSendTimeout the
// whole send, retries included. The Resend sender stops at 10s; the standard
// retryer makes up to three attempts, so without the overall deadline a
// stalled SES could hold a sign-in for three timeouts plus backoff. An
// unresponsive provider must not hang a sign-in until the Cloud Run request
// timeout.
const (
	sesHTTPTimeout = 10 * time.Second
	sesSendTimeout = 15 * time.Second
)

// NewSESEmailSender builds an SES sender sending as from, the full RFC 5322
// From header of the identity it serves; the address must live on a domain
// verified in SES or delivery is rejected. settings is the SES configuration
// LoadConfig validated: the region and configuration set, and an endpoint
// override that only tests set.
//
// Nothing reaches AWS here. The default credential chain resolves on the
// first send, so building the sender at startup costs no network call.
func NewSESEmailSender(ctx context.Context, settings SESConfig, from string, logger Logger) (*SESEmailSender, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(settings.Region),
		awsconfig.WithHTTPClient(awshttp.NewBuildableClient().WithTimeout(sesHTTPTimeout)),
		// The standard retryer retries throttling and transient faults with
		// backoff, so a brief SES throttle does not drop a message. A
		// rejection is not retried.
		awsconfig.WithRetryer(func() aws.Retryer { return retry.NewStandard() }),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws configuration for ses: %w", err)
	}
	client := sesv2.NewFromConfig(awsCfg, func(o *sesv2.Options) {
		if settings.Endpoint != "" {
			o.BaseEndpoint = aws.String(settings.Endpoint)
		}
	})
	return &SESEmailSender{
		from:             from,
		configurationSet: settings.ConfigurationSet,
		client:           client,
		logger:           logger,
	}, nil
}

// From reports the RFC 5322 From header every message from this sender
// carries, which is to say which sending identity it is. Startup logging and
// the wiring tests ask, as they ask the Resend sender.
func (s *SESEmailSender) From() string { return s.from }

// send performs the one SendEmail call every public method funnels through.
// messageType names the message in the returned error; the recipient is
// redacted from it.
func (s *SESEmailSender) send(ctx context.Context, messageType, to, subject, text string) error {
	ctx, cancel := context.WithTimeout(ctx, sesSendTimeout)
	defer cancel()
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress:     aws.String(s.from),
		Destination:          &sestypes.Destination{ToAddresses: []string{to}},
		ConfigurationSetName: aws.String(s.configurationSet),
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject: &sestypes.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
				Body: &sestypes.Body{
					Text: &sestypes.Content{Data: aws.String(text), Charset: aws.String("UTF-8")},
				},
			},
		},
	})
	if err != nil {
		return redactRecipient(fmt.Errorf("ses send %s: %w", messageType, err), to)
	}
	return nil
}

// notYet refuses a message SES does not send yet, logging it the way a
// failed send is logged so the refusal is visible beside real failures.
func (s *SESEmailSender) notYet(messageType string) error {
	err := fmt.Errorf("ses send %s: %w", messageType, ErrNotYetOnSES)
	s.logger.Error("ses send "+messageType+" failed", "error", err)
	return err
}

// SendOTP delivers a One-time Passcode in the recipient's Mail Locale. A
// failure is returned, which fails the sign-in rather than pretending a code
// was sent.
func (s *SESEmailSender) SendOTP(ctx context.Context, to string, code string, locale Locale) error {
	message := OTPMessage{Code: code, Locale: locale}
	if err := s.send(ctx, "otp", to, message.Subject(), message.Text()); err != nil {
		s.logger.Error("ses send otp failed", "locale", string(locale), "error", err)
		return err
	}
	return nil
}

// The methods below are the messages SES does not send yet (#692). Each
// refuses with ErrNotYetOnSES until the "every transactional message" ticket
// under #690 routes it through send, with its copy from email_content.go.

// SendSaleConfirmation is not yet sent through SES.
func (s *SESEmailSender) SendSaleConfirmation(context.Context, SaleConfirmation) error {
	return s.notYet("sale confirmation")
}

// SendSaleVoided is not yet sent through SES.
func (s *SESEmailSender) SendSaleVoided(context.Context, SaleVoided) error {
	return s.notYet("sale voided")
}

// SendSaleReversalRefused is not yet sent through SES.
func (s *SESEmailSender) SendSaleReversalRefused(context.Context, SaleReversalRefused) error {
	return s.notYet("sale reversal refused")
}

// SendTaxDocumentDelivery is not yet sent through SES.
func (s *SESEmailSender) SendTaxDocumentDelivery(context.Context, TaxDocumentDelivery) error {
	return s.notYet("tax document delivery")
}

// SendConsentWithdrawalConfirmation is not yet sent through SES.
func (s *SESEmailSender) SendConsentWithdrawalConfirmation(context.Context, ConsentWithdrawalConfirmation) error {
	return s.notYet("consent withdrawal confirmation")
}

// SendHolderAnswerReminder is not yet sent through SES.
func (s *SESEmailSender) SendHolderAnswerReminder(context.Context, HolderAnswerReminder) error {
	return s.notYet("holder answer reminder")
}

// SendAssignmentReminder is not yet sent through SES.
func (s *SESEmailSender) SendAssignmentReminder(context.Context, AssignmentReminder) error {
	return s.notYet("assignment reminder")
}

// SendTicketAssignment is not yet sent through SES.
func (s *SESEmailSender) SendTicketAssignment(context.Context, TicketAssignment) error {
	return s.notYet("ticket assignment")
}

// SendNoLongerHolding is not yet sent through SES.
func (s *SESEmailSender) SendNoLongerHolding(context.Context, NoLongerHolding) error {
	return s.notYet("no longer holding")
}

// SendSaleReAddressing is not yet sent through SES.
func (s *SESEmailSender) SendSaleReAddressing(context.Context, SaleReAddressing) error {
	return s.notYet("sale re-addressing")
}

// SendTicketQuestionRevoked is not yet sent through SES.
func (s *SESEmailSender) SendTicketQuestionRevoked(context.Context, TicketQuestionRevoked) error {
	return s.notYet("ticket question revoked")
}

// SendPayoutRequestSubmitted is not yet sent through SES.
func (s *SESEmailSender) SendPayoutRequestSubmitted(context.Context, PayoutRequestSubmitted) error {
	return s.notYet("payout request submitted")
}

// SendQuestionReviewSubmitted is not yet sent through SES.
func (s *SESEmailSender) SendQuestionReviewSubmitted(context.Context, QuestionReviewSubmitted) error {
	return s.notYet("question review submitted")
}

// SendQuestionReviewAnswered is not yet sent through SES.
func (s *SESEmailSender) SendQuestionReviewAnswered(context.Context, QuestionReviewAnswered) error {
	return s.notYet("question review answered")
}

// SendPayoutRequestPaid is not yet sent through SES.
func (s *SESEmailSender) SendPayoutRequestPaid(context.Context, PayoutRequestPaid) error {
	return s.notYet("payout request paid")
}

// SendPayoutRequestDeclined is not yet sent through SES.
func (s *SESEmailSender) SendPayoutRequestDeclined(context.Context, PayoutRequestDeclined) error {
	return s.notYet("payout request declined")
}

// SendPayoutRequestTransferSent is not yet sent through SES.
func (s *SESEmailSender) SendPayoutRequestTransferSent(context.Context, PayoutRequestTransferSent) error {
	return s.notYet("payout request transfer sent")
}

// SendPayoutRequestTransferFailed is not yet sent through SES.
func (s *SESEmailSender) SendPayoutRequestTransferFailed(context.Context, PayoutRequestTransferFailed) error {
	return s.notYet("payout request transfer failed")
}

// SendCertificateExpiryWarning is not yet sent through SES.
func (s *SESEmailSender) SendCertificateExpiryWarning(context.Context, CertificateExpiryWarning) error {
	return s.notYet("certificate expiry warning")
}

// SendFollowDigest is not yet sent through SES.
func (s *SESEmailSender) SendFollowDigest(context.Context, FollowDigest) error {
	return s.notYet("follow digest")
}

// redactedError carries an error's text with the recipient taken out, while
// errors.Is and errors.As still see the provider's error underneath.
type redactedError struct {
	text string
	err  error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.err }

// redactRecipient removes every case-insensitive occurrence of the recipient
// address from err's text. Case-insensitive because SES does not quote the
// address back in the case it was given.
func redactRecipient(err error, to string) error {
	if to == "" {
		return err
	}
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(to))
	return &redactedError{text: pattern.ReplaceAllString(err.Error(), "[recipient]"), err: err}
}

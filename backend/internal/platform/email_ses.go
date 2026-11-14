package platform

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// SESEmailSender delivers email through Amazon SES, beside the Resend sender
// and selected only by an explicit EMAIL_PROVIDER=ses for the transactional
// identity or DIGEST_EMAIL_PROVIDER=ses for the Follow Digest's (ADR 0077).
// Which identity a sender is depends only on the From it was built with: the
// wiring builds one per identity that selects SES, as it does with Resend. It is the Resend sender's twin: one private send path every public
// method funnels through, the copy taken from the message's own content
// (email_content.go) and never written here, plain text only.
//
// It sends with the SES v2 API through a named configuration set, so the
// delivery settings live in one place in Terraform. On Cloud Run its
// credentials come from Workload Identity Federation (aws_web_identity.go);
// elsewhere, with no role to assume, from the SDK's default credential chain.
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
// LoadConfig validated: the region and configuration set, the role to assume
// and its token audience, and endpoint overrides that only tests set.
//
// Nothing reaches AWS here. Credentials, federated or from the default chain,
// resolve on the first send, so building the sender at startup costs no
// network call.
func NewSESEmailSender(ctx context.Context, settings SESConfig, from string, logger Logger) (*SESEmailSender, error) {
	httpClient := awshttp.NewBuildableClient().WithTimeout(sesHTTPTimeout)
	// The standard retryer retries throttling and transient faults with
	// backoff, so a brief SES throttle does not drop a message. A rejection is
	// not retried.
	retryer := func() aws.Retryer { return retry.NewStandard() }
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(settings.Region),
		awsconfig.WithHTTPClient(httpClient),
		awsconfig.WithRetryer(retryer),
	}
	// With a role to assume, the credentials come from Workload Identity
	// Federation and nothing else: the default chain is never consulted, so a
	// Cloud Run service cannot pick up some other AWS credential. Without one
	// (never in production, which LoadConfig refuses) the default chain is
	// used, which is how the send check runs on an Identity Center profile.
	if settings.RoleARN != "" {
		options = append(options, awsconfig.WithCredentialsProvider(newWebIdentityCredentials(settings, sts.Options{
			Region:     settings.Region,
			HTTPClient: httpClient,
			Retryer:    retryer(),
		})))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
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
//
// Attachments travel on the Simple message, which SES v2 builds into the MIME
// mail itself: the bytes as stored, under their filename and media type, as
// attachments rather than inline parts. Only the Tax Document delivery passes
// any (#475); every other message's request carries no attachment key at all.
func (s *SESEmailSender) send(ctx context.Context, messageType, to, subject, text string, attachments ...EmailAttachment) error {
	ctx, cancel := context.WithTimeout(ctx, sesSendTimeout)
	defer cancel()
	message := &sestypes.Message{
		Subject: &sestypes.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
		Body: &sestypes.Body{
			Text: &sestypes.Content{Data: aws.String(text), Charset: aws.String("UTF-8")},
		},
	}
	for _, a := range attachments {
		message.Attachments = append(message.Attachments, sestypes.Attachment{
			FileName:           aws.String(a.Filename),
			ContentType:        aws.String(a.ContentType),
			ContentDisposition: sestypes.AttachmentContentDispositionAttachment,
			RawContent:         a.Body,
		})
	}
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress:     aws.String(s.from),
		Destination:          &sestypes.Destination{ToAddresses: []string{to}},
		ConfigurationSetName: aws.String(s.configurationSet),
		Content:              &sestypes.EmailContent{Simple: message},
	})
	if err != nil {
		return redactRecipient(fmt.Errorf("ses send %s: %w", messageType, err), to)
	}
	return nil
}

// fail logs a failed send under the message type, with the attributes the
// Resend sender logs for the same message and never the recipient (the error
// has it redacted already), and hands the error back for the caller.
func (s *SESEmailSender) fail(messageType string, err error, attrs ...any) error {
	s.logger.Error("ses send "+messageType+" failed", append(attrs, "error", err)...)
	return err
}

// SendOTP delivers a One-time Passcode in the recipient's Mail Locale. A
// failure is returned, which fails the sign-in rather than pretending a code
// was sent.
func (s *SESEmailSender) SendOTP(ctx context.Context, to string, code string, locale Locale) error {
	message := OTPMessage{Code: code, Locale: locale}
	if err := s.send(ctx, "otp", to, message.Subject(), message.Text()); err != nil {
		return s.fail("otp", err, "locale", string(locale))
	}
	return nil
}

// Every transactional message below funnels through send with the copy from
// email_content.go, so SES and Resend say exactly the same words (#693). Each
// failure is logged with the attributes the Resend sender logs for the same
// message, never the recipient, and returned: callers decide what it means.

// SendSaleConfirmation delivers a Customer's receipt. Best-effort for its caller, which logs and
// discards the error: a delivery hiccup never reverses a recorded Ticket Sale.
func (s *SESEmailSender) SendSaleConfirmation(ctx context.Context, c SaleConfirmation) error {
	if err := s.send(ctx, "sale confirmation", c.To, c.Subject(), c.Text()); err != nil {
		return s.fail("sale confirmation", err, "reference", c.Reference)
	}
	return nil
}

// SendSaleVoided delivers a cancellation notice referencing the original Sale
// Confirmation. Best-effort, as above.
func (s *SESEmailSender) SendSaleVoided(ctx context.Context, v SaleVoided) error {
	if err := s.send(ctx, "sale voided", v.To, v.Subject(), v.Text()); err != nil {
		return s.fail("sale voided", err, "reference", v.Reference)
	}
	return nil
}

// SendSaleReversalRefused delivers the notice that a refund the Customer was
// told was being processed could not be made. Best-effort, as above.
func (s *SESEmailSender) SendSaleReversalRefused(ctx context.Context, r SaleReversalRefused) error {
	if err := s.send(ctx, "sale reversal refused", r.To, r.Subject(), r.Text()); err != nil {
		return s.fail("sale reversal refused", err, "reference", r.Reference)
	}
	return nil
}

// SendTaxDocumentDelivery hands the Customer their authorized Tax Document with
// the signed XML and its RIDE attached (#475, #496), the only message that
// carries attachments. The Drainer reads the error and retries on its ladder.
func (s *SESEmailSender) SendTaxDocumentDelivery(ctx context.Context, d TaxDocumentDelivery) error {
	if err := s.send(ctx, "tax document delivery", d.To, d.Subject(), d.Text(), d.Attachments...); err != nil {
		return s.fail("tax document delivery", err, "kind", d.Kind, "reference", d.Reference)
	}
	return nil
}

// SendConsentWithdrawalConfirmation delivers the confirmation that a Consent
// Withdrawal took something away (#267). The error is returned because the
// confirmation-sent stamp is written only where the provider took the message.
func (s *SESEmailSender) SendConsentWithdrawalConfirmation(ctx context.Context, c ConsentWithdrawalConfirmation) error {
	if err := s.send(ctx, "consent withdrawal confirmation", c.To, c.Subject(), c.Text()); err != nil {
		return s.fail("consent withdrawal confirmation", err)
	}
	return nil
}

// SendHolderAnswerReminder delivers the Answer Reminder (#328). The sweep writes
// its ledger row only once this returns nil. The Event is not logged.
func (s *SESEmailSender) SendHolderAnswerReminder(ctx context.Context, r HolderAnswerReminder) error {
	if err := s.send(ctx, "holder answer reminder", r.To, r.Subject(), r.Text()); err != nil {
		return s.fail("holder answer reminder", err)
	}
	return nil
}

// SendAssignmentReminder delivers the Assignment Reminder (#362). The sweep
// writes its ledger row only once this returns nil. The Event and the link are
// not logged.
func (s *SESEmailSender) SendAssignmentReminder(ctx context.Context, r AssignmentReminder) error {
	if err := s.send(ctx, "assignment reminder", r.To, r.Subject(), r.Text()); err != nil {
		return s.fail("assignment reminder", err)
	}
	return nil
}

// SendTicketAssignment delivers the Assignment mail carrying an Assignment
// Link (#325). Neither the link, a credential that mints an identity, nor the
// Event is logged.
func (s *SESEmailSender) SendTicketAssignment(ctx context.Context, a TicketAssignment) error {
	if err := s.send(ctx, "ticket assignment", a.To, a.Subject(), a.Text()); err != nil {
		return s.fail("ticket assignment", err)
	}
	return nil
}

// SendNoLongerHolding delivers the notice that a Ticket a Holder accepted is
// no longer theirs (#327). The Event is not logged.
func (s *SESEmailSender) SendNoLongerHolding(ctx context.Context, n NoLongerHolding) error {
	if err := s.send(ctx, "no longer holding", n.To, n.Subject(), n.Text()); err != nil {
		return s.fail("no longer holding", err)
	}
	return nil
}

// SendSaleReAddressing delivers the mail carrying a Re-addressing Link
// (#420). Neither the link nor the Event is logged.
func (s *SESEmailSender) SendSaleReAddressing(ctx context.Context, r SaleReAddressing) error {
	if err := s.send(ctx, "sale re-addressing", r.To, r.Subject(), r.Text()); err != nil {
		return s.fail("sale re-addressing", err)
	}
	return nil
}

// SendTicketQuestionRevoked delivers one Org Admin's notice that an approved
// Ticket Question was revoked (#410). The reason is not logged.
func (s *SESEmailSender) SendTicketQuestionRevoked(ctx context.Context, r TicketQuestionRevoked) error {
	if err := s.send(ctx, "ticket question revoked", r.To, r.Subject(), r.Text()); err != nil {
		return s.fail("ticket question revoked", err, "organization", r.OrganizationName)
	}
	return nil
}

// SendPayoutRequestSubmitted delivers one Platform Operator's notice that an
// Organization asked to be paid. Best-effort.
func (s *SESEmailSender) SendPayoutRequestSubmitted(ctx context.Context, p PayoutRequestSubmitted) error {
	if err := s.send(ctx, "payout request submitted", p.To, p.Subject(), p.Text()); err != nil {
		return s.fail("payout request submitted", err, "organization", p.OrganizationName)
	}
	return nil
}

// SendQuestionReviewSubmitted delivers one Platform Operator's notice that an
// Event's questions are waiting for review (#406). Best-effort.
func (s *SESEmailSender) SendQuestionReviewSubmitted(ctx context.Context, q QuestionReviewSubmitted) error {
	if err := s.send(ctx, "question review submitted", q.To, q.Subject(), q.Text()); err != nil {
		return s.fail("question review submitted", err, "organization", q.OrganizationName)
	}
	return nil
}

// SendQuestionReviewAnswered delivers the submitter's notice of the Operator's
// verdicts (#407). Best-effort.
func (s *SESEmailSender) SendQuestionReviewAnswered(ctx context.Context, q QuestionReviewAnswered) error {
	if err := s.send(ctx, "question review answered", q.To, q.Subject(), q.Text()); err != nil {
		return s.fail("question review answered", err, "organization", q.OrganizationName)
	}
	return nil
}

// SendPayoutRequestPaid delivers the asker's notice that the transfer was
// made. Best-effort.
func (s *SESEmailSender) SendPayoutRequestPaid(ctx context.Context, p PayoutRequestPaid) error {
	if err := s.send(ctx, "payout request paid", p.To, p.Subject(), p.Text()); err != nil {
		return s.fail("payout request paid", err, "organization", p.OrganizationName)
	}
	return nil
}

// SendPayoutRequestDeclined delivers the asker's notice that the ask was
// refused. The reason is not logged.
func (s *SESEmailSender) SendPayoutRequestDeclined(ctx context.Context, p PayoutRequestDeclined) error {
	if err := s.send(ctx, "payout request declined", p.To, p.Subject(), p.Text()); err != nil {
		return s.fail("payout request declined", err, "organization", p.OrganizationName)
	}
	return nil
}

// SendPayoutRequestTransferSent delivers the asker's notice that the transfer
// has been submitted to the bank. Best-effort.
func (s *SESEmailSender) SendPayoutRequestTransferSent(ctx context.Context, p PayoutRequestTransferSent) error {
	if err := s.send(ctx, "payout request transfer sent", p.To, p.Subject(), p.Text()); err != nil {
		return s.fail("payout request transfer sent", err, "organization", p.OrganizationName)
	}
	return nil
}

// SendPayoutRequestTransferFailed delivers the asker's notice that the bank
// sent the transfer back. The reason is not logged.
func (s *SESEmailSender) SendPayoutRequestTransferFailed(ctx context.Context, p PayoutRequestTransferFailed) error {
	if err := s.send(ctx, "payout request transfer failed", p.To, p.Subject(), p.Text()); err != nil {
		return s.fail("payout request transfer failed", err, "organization", p.OrganizationName)
	}
	return nil
}

// SendCertificateExpiryWarning delivers one Platform Operator's notice that
// the signing certificate is about to lapse, or has (#502). The Drainer writes
// its ledger row only once an address accepted the mail.
func (s *SESEmailSender) SendCertificateExpiryWarning(ctx context.Context, w CertificateExpiryWarning) error {
	if err := s.send(ctx, "certificate expiry warning", w.To, w.Subject(), w.Text()); err != nil {
		return s.fail("certificate expiry warning", err, "threshold_days", w.Threshold, "ruc", w.RUC)
	}
	return nil
}

// SendFollowDigest delivers the weekly Follow Digest (#696). The error is
// returned because the drain retries a failed Digest rather than recording it
// as sent (ADR 0030). This method is only ever reached on the sender built
// with the Digest identity's From: the wiring routes the Digest there through
// SplitEmailSender, so it never leaves on the transactional domain.
func (s *SESEmailSender) SendFollowDigest(ctx context.Context, d FollowDigest) error {
	if err := s.send(ctx, "follow digest", d.To, d.Subject(), d.Text()); err != nil {
		return s.fail("follow digest", err, "locale", string(d.Locale), "new", len(d.New), "happening", len(d.Happening))
	}
	return nil
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

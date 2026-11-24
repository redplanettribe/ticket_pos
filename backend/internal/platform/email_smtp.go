package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// SMTPEmailSender delivers every mail kind over plain SMTP to the local
// stack's Mailpit (#872, spec #870), so a developer can read each one the way
// a reader does: headers, parts and attachments.
//
// It is a DEVELOPMENT sender. It is selected only by EMAIL_PROVIDER=smtp and
// DIGEST_EMAIL_PROVIDER=smtp, which docker-compose.yml sets and nothing else
// does, and LoadConfig refuses either in production. It speaks no TLS and no
// authentication because Mailpit asks for neither; pointing it at a real mail
// server is not a supported use.
//
// It is the SES sender's twin in every way that matters to a reader: one
// private send path every public method funnels through, the copy taken from
// the message's own content (email_content.go) and never written here, the
// HTML part beside the text as multipart/alternative (ADR 0082), and the Tax Document's attachments carried as attachments. Which identity a
// sender is depends only on the From it was built with, as with SES and
// Resend: the wiring builds one per identity.
//
// A failure is logged under the message type and returned, never with the
// recipient (#377), the same rule the production senders keep.
type SMTPEmailSender struct {
	addr   string
	host   string
	from   string
	logger Logger
	chrome MailChrome
}

// smtpSendTimeout bounds one whole delivery, dial to QUIT. Mailpit answers in
// milliseconds; the bound only keeps a stopped container from holding a
// sign-in open.
const smtpSendTimeout = 10 * time.Second

// NewSMTPEmailSender builds a sender delivering to settings' host and port as
// from, the full RFC 5322 From header of the identity it serves. Nothing is
// dialled here; the first send connects.
func NewSMTPEmailSender(settings SMTPConfig, from string, logger Logger) *SMTPEmailSender {
	return &SMTPEmailSender{addr: settings.Addr(), host: settings.Host, from: from, logger: logger}
}

// From reports the From header every message from this sender carries, which
// is to say which sending identity it is.
func (s *SMTPEmailSender) From() string { return s.from }

// WithChrome sets the sender-wide configuration every HTML part's chrome
// reads, as on the SES sender.
func (s *SMTPEmailSender) WithChrome(chrome MailChrome) *SMTPEmailSender {
	s.chrome = chrome
	return s
}

// Chrome reports the chrome this sender renders HTML parts with, as From
// reports its identity: the wiring tests ask.
func (s *SMTPEmailSender) Chrome() MailChrome { return s.chrome }

// send performs the one SMTP transaction every public method funnels through:
// the From's address as the envelope sender, the recipient alone, and the
// message composeSMTPMessage writes.
func (s *SMTPEmailSender) send(ctx context.Context, messageType, to, subject, text, html string, attachments ...EmailAttachment) error {
	if err := s.deliver(ctx, to, subject, text, html, attachments); err != nil {
		err = redactRecipient(fmt.Errorf("smtp send %s: %w", messageType, err), to)
		s.logger.Error(ctx, "smtp send "+messageType+" failed", "error", err)
		return err
	}
	return nil
}

func (s *SMTPEmailSender) deliver(ctx context.Context, to, subject, text, html string, attachments []EmailAttachment) error {
	from, err := mail.ParseAddress(s.from)
	if err != nil {
		return fmt.Errorf("parse from %q: %w", s.from, err)
	}
	message, err := composeSMTPMessage(from, to, subject, text, html, attachments, time.Now())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// composeSMTPMessage writes the RFC 5322 message: the headers, then the text
// part alone; or, with an HTML part, a multipart/alternative of the text and
// then the HTML, the order in which a client prefers the last it can show;
// or, when there are attachments, a multipart/mixed body with that text (or
// that alternative) first and each attachment after it under its own filename
// and media type, its bytes base64-encoded exactly as stored.
//
// The text and the HTML are quoted-printable so UTF-8 copy and long lines
// survive any relay. Subject and header values come from user-entered fields
// (an Event name), so line breaks in them are folded to spaces before they
// reach a header: no Event name may add a header.
func composeSMTPMessage(from *mail.Address, to, subject, text, html string, attachments []EmailAttachment, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	header := func(name, value string) {
		fmt.Fprintf(&buf, "%s: %s\r\n", name, value)
	}
	header("From", from.String())
	header("To", (&mail.Address{Address: singleLine(to)}).String())
	header("Subject", mime.QEncoding.Encode("utf-8", singleLine(subject)))
	header("Date", now.Format(time.RFC1123Z))
	messageID, err := newMessageID(from.Address)
	if err != nil {
		return nil, err
	}
	header("Message-ID", messageID)
	header("MIME-Version", "1.0")

	contentType, content, err := smtpContent(text, html)
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		header("Content-Type", contentType)
		if html == "" {
			header("Content-Transfer-Encoding", "quoted-printable")
		}
		buf.WriteString("\r\n")
		buf.Write(content)
		// SMTP carries a message as lines, so the body ends in a line break
		// either way; writing it here keeps the message well formed as composed.
		buf.WriteString("\r\n")
		return buf.Bytes(), nil
	}

	body := multipart.NewWriter(&buf)
	header("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": body.Boundary()}))
	buf.WriteString("\r\n")
	contentHeader := textproto.MIMEHeader{"Content-Type": {contentType}}
	if html == "" {
		contentHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	}
	contentPart, err := body.CreatePart(contentHeader)
	if err != nil {
		return nil, err
	}
	if _, err := contentPart.Write(content); err != nil {
		return nil, err
	}
	for _, a := range attachments {
		part, err := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {singleLine(a.ContentType)},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeBase64Lines(part, a.Body); err != nil {
			return nil, err
		}
	}
	if err := body.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// smtpContent is the readable body: the quoted-printable text alone when there
// is no HTML, or a multipart/alternative of the text and then the HTML. It
// returns the Content-Type the body goes under and the encoded body.
func smtpContent(text, html string) (string, []byte, error) {
	var buf bytes.Buffer
	if html == "" {
		if err := writeQuotedPrintable(&buf, text); err != nil {
			return "", nil, err
		}
		return "text/plain; charset=utf-8", buf.Bytes(), nil
	}
	alternative := multipart.NewWriter(&buf)
	for _, part := range []struct{ mediaType, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", html},
	} {
		w, err := alternative.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.mediaType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return "", nil, err
		}
		if err := writeQuotedPrintable(w, part.body); err != nil {
			return "", nil, err
		}
	}
	if err := alternative.Close(); err != nil {
		return "", nil, err
	}
	return mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternative.Boundary()}), buf.Bytes(), nil
}

// singleLine folds every line break in a header value to a space.
func singleLine(value string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(value)
}

func writeQuotedPrintable(w io.Writer, text string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(text)); err != nil {
		return err
	}
	return qp.Close()
}

// writeBase64Lines writes body as base64 in 76-character lines, the length
// RFC 2045 sets for an encoded body.
func writeBase64Lines(w io.Writer, body []byte) error {
	encoded := base64.StdEncoding.EncodeToString(body)
	for len(encoded) > 76 {
		if _, err := fmt.Fprintf(w, "%s\r\n", encoded[:76]); err != nil {
			return err
		}
		encoded = encoded[76:]
	}
	_, err := fmt.Fprintf(w, "%s\r\n", encoded)
	return err
}

// newMessageID mints a random Message-ID on the From's domain, so Mailpit
// lists every send as its own message.
func newMessageID(fromAddress string) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	domain := "localhost"
	if at := strings.LastIndex(fromAddress, "@"); at >= 0 && at < len(fromAddress)-1 {
		domain = fromAddress[at+1:]
	}
	return "<" + hex.EncodeToString(random) + "@" + domain + ">", nil
}

// Every message below funnels through send with the copy from
// email_content.go, so Mailpit shows exactly the words SES and Resend send.

// SendOTP delivers a One-time Passcode in the recipient's Mail Locale.
func (s *SMTPEmailSender) SendOTP(ctx context.Context, to string, code string, locale Locale) error {
	message := OTPMessage{Code: code, Locale: locale}
	return s.send(ctx, "otp", to, message.Subject(), message.Text(), renderedHTML(ctx, s.logger, "otp", message.HTML, s.chrome))
}

// SendSaleConfirmation delivers a Customer's receipt.
func (s *SMTPEmailSender) SendSaleConfirmation(ctx context.Context, c SaleConfirmation) error {
	return s.send(ctx, "sale confirmation", c.To, c.Subject(), c.Text(), renderedHTML(ctx, s.logger, "sale confirmation", c.HTML, s.chrome))
}

// SendSaleVoided delivers a cancellation notice.
func (s *SMTPEmailSender) SendSaleVoided(ctx context.Context, v SaleVoided) error {
	return s.send(ctx, "sale voided", v.To, v.Subject(), v.Text(), renderedHTML(ctx, s.logger, "sale voided", v.HTML, s.chrome))
}

// SendSaleReversalRefused delivers the notice that a refund could not be made.
func (s *SMTPEmailSender) SendSaleReversalRefused(ctx context.Context, r SaleReversalRefused) error {
	return s.send(ctx, "sale reversal refused", r.To, r.Subject(), r.Text(), renderedHTML(ctx, s.logger, "sale reversal refused", r.HTML, s.chrome))
}

// SendTaxDocumentDelivery delivers the Tax Document with its signed XML and
// RIDE attached, the only message that carries attachments, beside both
// parts.
func (s *SMTPEmailSender) SendTaxDocumentDelivery(ctx context.Context, d TaxDocumentDelivery) error {
	return s.send(ctx, "tax document delivery", d.To, d.Subject(), d.Text(), renderedHTML(ctx, s.logger, "tax document delivery", d.HTML, s.chrome), d.Attachments...)
}

// SendAnnulmentNotice tells the buyer their factura was annulled at the SRI.
func (s *SMTPEmailSender) SendAnnulmentNotice(ctx context.Context, n AnnulmentNotice) error {
	return s.send(ctx, "annulment notice", n.To, n.Subject(), n.Text(), renderedHTML(ctx, s.logger, "annulment notice", n.HTML, s.chrome))
}

// SendConsentWithdrawalConfirmation delivers the Consent Withdrawal
// Confirmation.
func (s *SMTPEmailSender) SendConsentWithdrawalConfirmation(ctx context.Context, c ConsentWithdrawalConfirmation) error {
	return s.send(ctx, "consent withdrawal confirmation", c.To, c.Subject(), c.Text(), renderedHTML(ctx, s.logger, "consent withdrawal confirmation", c.HTML, s.chrome))
}

// SendHolderAnswerReminder delivers the Answer Reminder.
func (s *SMTPEmailSender) SendHolderAnswerReminder(ctx context.Context, r HolderAnswerReminder) error {
	return s.send(ctx, "holder answer reminder", r.To, r.Subject(), r.Text(), renderedHTML(ctx, s.logger, "holder answer reminder", r.HTML, s.chrome))
}

// SendAssignmentReminder delivers the Assignment Reminder.
func (s *SMTPEmailSender) SendAssignmentReminder(ctx context.Context, r AssignmentReminder) error {
	return s.send(ctx, "assignment reminder", r.To, r.Subject(), r.Text(), renderedHTML(ctx, s.logger, "assignment reminder", r.HTML, s.chrome))
}

// SendTicketAssignment delivers the Assignment mail carrying an Assignment
// Link.
func (s *SMTPEmailSender) SendTicketAssignment(ctx context.Context, a TicketAssignment) error {
	return s.send(ctx, "ticket assignment", a.To, a.Subject(), a.Text(), renderedHTML(ctx, s.logger, "ticket assignment", a.HTML, s.chrome))
}

// SendNoLongerHolding delivers the notice that a Ticket is no longer the
// Holder's.
func (s *SMTPEmailSender) SendNoLongerHolding(ctx context.Context, n NoLongerHolding) error {
	return s.send(ctx, "no longer holding", n.To, n.Subject(), n.Text(), renderedHTML(ctx, s.logger, "no longer holding", n.HTML, s.chrome))
}

// SendSaleReAddressing delivers the mail carrying a Re-addressing Link.
func (s *SMTPEmailSender) SendSaleReAddressing(ctx context.Context, r SaleReAddressing) error {
	return s.send(ctx, "sale re-addressing", r.To, r.Subject(), r.Text(), renderedHTML(ctx, s.logger, "sale re-addressing", r.HTML, s.chrome))
}

// SendTicketQuestionRevoked delivers an Org Admin's revocation notice.
func (s *SMTPEmailSender) SendTicketQuestionRevoked(ctx context.Context, r TicketQuestionRevoked) error {
	return s.send(ctx, "ticket question revoked", r.To, r.Subject(), r.Text(), renderedHTML(ctx, s.logger, "ticket question revoked", r.HTML, s.chrome))
}

// SendPayoutRequestSubmitted delivers a Platform Operator's Payout Request
// notice.
func (s *SMTPEmailSender) SendPayoutRequestSubmitted(ctx context.Context, p PayoutRequestSubmitted) error {
	return s.send(ctx, "payout request submitted", p.To, p.Subject(), p.Text(), renderedHTML(ctx, s.logger, "payout request submitted", p.HTML, s.chrome))
}

// SendQuestionReviewSubmitted delivers a Platform Operator's Question Review
// notice.
func (s *SMTPEmailSender) SendQuestionReviewSubmitted(ctx context.Context, q QuestionReviewSubmitted) error {
	return s.send(ctx, "question review submitted", q.To, q.Subject(), q.Text(), renderedHTML(ctx, s.logger, "question review submitted", q.HTML, s.chrome))
}

// SendQuestionReviewAnswered delivers the submitter's verdicts notice.
func (s *SMTPEmailSender) SendQuestionReviewAnswered(ctx context.Context, q QuestionReviewAnswered) error {
	return s.send(ctx, "question review answered", q.To, q.Subject(), q.Text(), renderedHTML(ctx, s.logger, "question review answered", q.HTML, s.chrome))
}

// SendPayoutRequestPaid delivers the asker's paid notice.
func (s *SMTPEmailSender) SendPayoutRequestPaid(ctx context.Context, p PayoutRequestPaid) error {
	return s.send(ctx, "payout request paid", p.To, p.Subject(), p.Text(), renderedHTML(ctx, s.logger, "payout request paid", p.HTML, s.chrome))
}

// SendPayoutRequestDeclined delivers the asker's declined notice.
func (s *SMTPEmailSender) SendPayoutRequestDeclined(ctx context.Context, p PayoutRequestDeclined) error {
	return s.send(ctx, "payout request declined", p.To, p.Subject(), p.Text(), renderedHTML(ctx, s.logger, "payout request declined", p.HTML, s.chrome))
}

// SendPayoutRequestTransferSent delivers the asker's transfer-sent notice.
func (s *SMTPEmailSender) SendPayoutRequestTransferSent(ctx context.Context, p PayoutRequestTransferSent) error {
	return s.send(ctx, "payout request transfer sent", p.To, p.Subject(), p.Text(), renderedHTML(ctx, s.logger, "payout request transfer sent", p.HTML, s.chrome))
}

// SendPayoutRequestTransferFailed delivers the asker's transfer-failed notice.
func (s *SMTPEmailSender) SendPayoutRequestTransferFailed(ctx context.Context, p PayoutRequestTransferFailed) error {
	return s.send(ctx, "payout request transfer failed", p.To, p.Subject(), p.Text(), renderedHTML(ctx, s.logger, "payout request transfer failed", p.HTML, s.chrome))
}

// SendCertificateExpiryWarning delivers a Platform Operator's Certificate
// Expiry Warning.
func (s *SMTPEmailSender) SendCertificateExpiryWarning(ctx context.Context, w CertificateExpiryWarning) error {
	return s.send(ctx, "certificate expiry warning", w.To, w.Subject(), w.Text(), renderedHTML(ctx, s.logger, "certificate expiry warning", w.HTML, s.chrome))
}

// SendFollowDigest delivers the weekly Follow Digest. It is only reached on
// the sender built with the Digest identity's From: the wiring routes the
// Digest there through SplitEmailSender.
func (s *SMTPEmailSender) SendFollowDigest(ctx context.Context, d FollowDigest) error {
	return s.send(ctx, "follow digest", d.To, d.Subject(), d.Text(), renderedHTML(ctx, s.logger, "follow digest", d.HTML, s.chrome))
}

var _ EmailSender = (*SMTPEmailSender)(nil)

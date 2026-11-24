package platform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The SMTP sender seam (#872): the sender's real SMTP transaction against a
// fake SMTP server on a loopback port, which records the envelope and the
// message exactly as Mailpit would receive them. The tests read that message
// back the way a mail client does and assert what a reader gets: recipients,
// From, Subject, the text part and the attachments. Every fixture is invented.

const smtpTestFrom = "Multiticketing <noreply@send.multiticketing.com>"

// fakeSMTP is the smallest SMTP server the sender's transaction needs. It
// records every accepted mail; rejectRcpt, when set, is the reply every RCPT
// gets instead of 250.
type fakeSMTP struct {
	addr       string
	mu         sync.Mutex
	mails      []fakeSMTPMail
	rejectRcpt string
}

type fakeSMTPMail struct {
	from string
	to   []string
	data []byte
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	fake := &fakeSMTP{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fake.serve(conn)
		}
	}()
	return fake
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	reply := func(line string) { _ = tp.PrintfLine("%s", line) }
	reply("220 fake ESMTP")
	var current fakeSMTPMail
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(verb, "EHLO"), strings.HasPrefix(verb, "HELO"):
			reply("250 fake")
		case strings.HasPrefix(verb, "MAIL FROM:"):
			current = fakeSMTPMail{from: strings.Trim(line[len("MAIL FROM:"):], "<> ")}
			reply("250 ok")
		case strings.HasPrefix(verb, "RCPT TO:"):
			if f.rejectRcpt != "" {
				reply(f.rejectRcpt)
				continue
			}
			current.to = append(current.to, strings.Trim(line[len("RCPT TO:"):], "<> "))
			reply("250 ok")
		case verb == "DATA":
			reply("354 go ahead")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			current.data = data
			f.mu.Lock()
			f.mails = append(f.mails, current)
			f.mu.Unlock()
			reply("250 queued")
		case verb == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (f *fakeSMTP) received() []fakeSMTPMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSMTPMail(nil), f.mails...)
}

func newTestSMTPSender(t *testing.T, fake *fakeSMTP, logger Logger) *SMTPEmailSender {
	t.Helper()
	host, port, err := net.SplitHostPort(fake.addr)
	if err != nil {
		t.Fatalf("split %q: %v", fake.addr, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("port %q: %v", port, err)
	}
	return NewSMTPEmailSender(SMTPConfig{Host: host, Port: portNumber}, smtpTestFrom, logger)
}

// readMail is a received message as a reader's client shows it: its headers,
// the decoded text part and every attachment with its disposition.
type readMail struct {
	header      mail.Header
	subject     string
	text        string
	html        string
	attachments []readAttachment
}

type readAttachment struct {
	disposition string
	filename    string
	contentType string
	body        []byte
}

func readSMTPMail(t *testing.T, data []byte) readMail {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("read message: %v\n%s", err, data)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	got := readMail{header: message.Header, subject: subject}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	if mediaType == "text/plain" || mediaType == "multipart/alternative" {
		got.text, got.html = readContent(t, mediaType, params, message.Header.Get("Content-Transfer-Encoding"), message.Body)
		return got
	}
	if mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want text/plain, multipart/alternative or multipart/mixed", mediaType)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	for i := 0; ; i++ {
		part, err := parts.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		partType, partParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("part %d content type: %v", i, err)
		}
		if i == 0 {
			if (partType != "text/plain" && partType != "multipart/alternative") || part.Header.Get("Content-Disposition") != "" {
				t.Fatalf("first part is %q (%q), want the inline text or alternative part", partType, part.Header.Get("Content-Disposition"))
			}
			got.text, got.html = readContent(t, partType, partParams, part.Header.Get("Content-Transfer-Encoding"), part)
			continue
		}
		disposition, dispositionParams, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil {
			t.Fatalf("part %d disposition: %v", i, err)
		}
		if enc := part.Header.Get("Content-Transfer-Encoding"); enc != "base64" {
			t.Fatalf("part %d Content-Transfer-Encoding = %q, want base64", i, enc)
		}
		body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("part %d body: %v", i, err)
		}
		got.attachments = append(got.attachments, readAttachment{
			disposition: disposition,
			filename:    dispositionParams["filename"],
			contentType: part.Header.Get("Content-Type"),
			body:        body,
		})
	}
	return got
}

// readContent reads the readable body: a text part alone, or a
// multipart/alternative of exactly the text and then the HTML.
func readContent(t *testing.T, mediaType string, params map[string]string, encoding string, body io.Reader) (text, html string) {
	t.Helper()
	if mediaType == "text/plain" {
		return decodeTextPart(t, encoding, params, body), ""
	}
	parts := multipart.NewReader(body, params["boundary"])
	var types []string
	for {
		part, err := parts.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("alternative part: %v", err)
		}
		partType, partParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("alternative part content type: %v", err)
		}
		types = append(types, partType)
		switch partType {
		case "text/plain":
			text = decodeTextPart(t, part.Header.Get("Content-Transfer-Encoding"), partParams, part)
		case "text/html":
			html = decodeTextPart(t, part.Header.Get("Content-Transfer-Encoding"), partParams, part)
		}
	}
	// A client shows the last alternative it can, so the HTML comes last.
	if len(types) != 2 || types[0] != "text/plain" || types[1] != "text/html" {
		t.Fatalf("alternative parts = %v, want [text/plain text/html]", types)
	}
	return text, html
}

func decodeTextPart(t *testing.T, encoding string, params map[string]string, body io.Reader) string {
	t.Helper()
	if !strings.EqualFold(params["charset"], "utf-8") {
		t.Fatalf("text part charset = %q, want utf-8", params["charset"])
	}
	if encoding != "quoted-printable" {
		t.Fatalf("text part Content-Transfer-Encoding = %q, want quoted-printable", encoding)
	}
	text, err := io.ReadAll(quotedprintable.NewReader(body))
	if err != nil {
		t.Fatalf("decode text part: %v", err)
	}
	// A single-part body ends in the line break SMTP carries every message's
	// last line with; a reader cannot see it, so it is not part of the copy.
	return strings.TrimSuffix(string(text), "\n")
}

// everyMessageCases is every mail kind: the transactional table the SES
// sender is held to, plus the passcode and the Follow Digest.
func everyMessageCases() []messageCase {
	otp := OTPMessage{Code: "482913", Locale: LocaleES}
	digest := FollowDigest{
		To: "ana@example.com", CustomerName: "Ana", Locale: LocaleEN,
		New: []FollowDigestEvent{{Name: "Noche de Jazz", StartsAt: time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC), Timezone: "America/Guayaquil", URL: "https://shop.example.com/e/jazz"}},
	}
	return append(transactionalMessageCases(),
		messageCase{"otp", "ana@example.com", otp.Subject(), otp.Text(), testHTML(otp.HTML), nil,
			func(ctx context.Context, s EmailSender) error {
				return s.SendOTP(ctx, "ana@example.com", otp.Code, otp.Locale)
			}},
		messageCase{"follow digest", digest.To, digest.Subject(), digest.Text(), testHTML(digest.HTML), nil,
			func(ctx context.Context, s EmailSender) error { return s.SendFollowDigest(ctx, digest) }},
	)
}

// One message, one SMTP transaction: the From's address as the envelope
// sender, the recipient alone, the From header, the shared copy as the
// subject and the UTF-8 text part, and the Tax Document's attachments under
// their own names, media types and bytes, on that kind and no other.
func TestSMTPSendsEveryMessageWithTheSharedCopy(t *testing.T) {
	for _, tc := range everyMessageCases() {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeSMTP(t)
			sender := newTestSMTPSender(t, fake, noopLogger{})

			if err := tc.send(context.Background(), sender); err != nil {
				t.Fatalf("send: %v", err)
			}

			received := fake.received()
			if len(received) != 1 {
				t.Fatalf("fake SMTP received %d mails, want 1", len(received))
			}
			envelope := received[0]
			if envelope.from != "noreply@send.multiticketing.com" {
				t.Fatalf("MAIL FROM = %q, want the From's address", envelope.from)
			}
			if len(envelope.to) != 1 || envelope.to[0] != tc.to {
				t.Fatalf("RCPT TO = %v, want [%s]", envelope.to, tc.to)
			}

			got := readSMTPMail(t, envelope.data)
			from, err := got.header.AddressList("From")
			if err != nil || len(from) != 1 || from[0].Name != "Multiticketing" || from[0].Address != "noreply@send.multiticketing.com" {
				t.Fatalf("From = %v (%v), want %q", from, err, smtpTestFrom)
			}
			to, err := got.header.AddressList("To")
			if err != nil || len(to) != 1 || to[0].Address != tc.to {
				t.Fatalf("To = %v (%v), want %s", to, err, tc.to)
			}
			for _, absent := range []string{"Cc", "Bcc"} {
				if got.header.Get(absent) != "" {
					t.Fatalf("message carries a %s header: %q", absent, got.header.Get(absent))
				}
			}
			if got.header.Get("Message-Id") == "" || got.header.Get("Date") == "" {
				t.Fatalf("message lacks a Message-ID or Date: %v", got.header)
			}
			if tc.subject == "" || tc.text == "" {
				t.Fatalf("the fixture's shared copy is empty: subject %q, text %q", tc.subject, tc.text)
			}
			if got.subject != tc.subject {
				t.Fatalf("Subject = %q, want %q", got.subject, tc.subject)
			}
			if got.text != tc.text {
				t.Fatalf("text part = %q, want %q", got.text, tc.text)
			}
			if tc.html == "" || got.html != tc.html {
				t.Fatalf("html part (%d bytes) is not the message's HTML (%d bytes)", len(got.html), len(tc.html))
			}

			if len(got.attachments) != len(tc.attachments) {
				t.Fatalf("message carries %d attachments, want %d", len(got.attachments), len(tc.attachments))
			}
			for i, want := range tc.attachments {
				a := got.attachments[i]
				if a.disposition != "attachment" || a.filename != want.Filename {
					t.Errorf("attachment %d disposition = %q filename %q, want attachment %q", i, a.disposition, a.filename, want.Filename)
				}
				if a.contentType != want.ContentType {
					t.Errorf("attachment %d Content-Type = %q, want %q", i, a.contentType, want.ContentType)
				}
				if !bytes.Equal(a.body, want.Body) {
					t.Errorf("attachment %d bytes = %q, want %q", i, a.body, want.Body)
				}
			}
		})
	}
}

// An attachment larger than one encoded line, the shape of a real RIDE PDF,
// survives byte for byte, and no encoded line exceeds RFC 2045's 76.
func TestSMTPCarriesALargeBinaryAttachmentIntact(t *testing.T) {
	pdf := make([]byte, 64*1024)
	for i := range pdf {
		pdf[i] = byte(i * 7)
	}
	from := &mail.Address{Name: "Multiticketing", Address: "noreply@send.multiticketing.com"}
	data, err := composeSMTPMessage(from, "ana@example.com", "Factura", "Adjunta.", "", []EmailAttachment{
		{Filename: "0707202601.pdf", ContentType: "application/pdf", Body: pdf},
	}, time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if len(scanner.Text()) > 998 {
			t.Fatalf("a line is %d bytes long, over SMTP's 998", len(scanner.Text()))
		}
	}
	got := readSMTPMail(t, data)
	if len(got.attachments) != 1 || !bytes.Equal(got.attachments[0].body, pdf) {
		t.Fatal("the PDF did not survive the round trip")
	}
}

// A line break in a user-entered field that reaches a header (an Event name
// in a Subject) is folded to a space: it can never add a header.
func TestSMTPSubjectCannotAddAHeader(t *testing.T) {
	from := &mail.Address{Name: "Multiticketing", Address: "noreply@send.multiticketing.com"}
	data, err := composeSMTPMessage(from, "ana@example.com", "Fest\r\nBcc: mallory@example.com\nX-Evil: 1", "Hola", "", nil, time.Now())
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	got := readSMTPMail(t, data)
	if got.header.Get("Bcc") != "" || got.header.Get("X-Evil") != "" {
		t.Fatalf("the subject added headers: %v", got.header)
	}
	if got.subject != "Fest Bcc: mallory@example.com X-Evil: 1" {
		t.Fatalf("Subject = %q, want the folded line", got.subject)
	}
}

// A refused delivery reaches its caller, and neither the error nor the one
// failure log names the recipient (#377), even when the server quotes it.
func TestSMTPReturnsAFailureAndNeverLogsTheRecipient(t *testing.T) {
	fake := newFakeSMTP(t)
	fake.rejectRcpt = "550 <ANA@EXAMPLE.COM> mailbox unavailable"
	logger := &lineLogger{}
	sender := newTestSMTPSender(t, fake, logger)

	err := sender.SendSaleVoided(context.Background(), SaleVoided{To: "ana@example.com", CustomerName: "Ana", EventName: "Noche de Jazz", Reference: "TS-1002", Locale: LocaleEN})
	if err == nil {
		t.Fatal("send returned nil on a refused recipient")
	}
	if !strings.Contains(err.Error(), "smtp send sale voided") || !strings.Contains(err.Error(), "mailbox unavailable") {
		t.Fatalf("error %q does not name the message type and the server's reply", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "ana@example.com") {
		t.Fatalf("error %q names the recipient", err)
	}
	lines := logger.errors()
	if len(lines) != 1 {
		t.Fatalf("logged %d error lines, want 1", len(lines))
	}
	if line := lines[0].String(); strings.Contains(strings.ToLower(line), "ana@example.com") || !strings.Contains(line, "smtp send sale voided failed") {
		t.Fatalf("failure log %q names the recipient or not the message type", line)
	}
	if len(fake.received()) != 0 {
		t.Fatal("a refused recipient was delivered anyway")
	}
}

// Nothing listening is a failure the caller sees, never a silent drop.
func TestSMTPWithNothingListeningFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	sender := NewSMTPEmailSender(SMTPConfig{Host: "127.0.0.1", Port: addr.Port}, smtpTestFrom, noopLogger{})
	if err := sender.SendOTP(context.Background(), "ana@example.com", "482913", LocaleEN); err == nil {
		t.Fatal("send returned nil with no SMTP server")
	}
}

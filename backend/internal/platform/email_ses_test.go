package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The SES sender seam (#692, ADR 0077). The sender runs its real request path
// against a fake SES v2 HTTP endpoint, the way the Resend sender runs against
// an httptest server, so these tests need no AWS account. They assert what
// reaches the provider and what the caller gets back, never how the sender is
// built.

const sesTestFrom = "Multiticketing <noreply@send.multiticketing.com>"

// fakeSES stands in for the SES v2 SendEmail endpoint. It records every
// request it receives and answers each with the handler's response.
type fakeSES struct {
	mu       sync.Mutex
	requests []fakeSESRequest
	respond  func(w http.ResponseWriter)
}

type fakeSESRequest struct {
	Method        string
	Path          string
	Authorization string
	SecurityToken string
	Body          sesSendEmailBody
}

// sesSendEmailBody is the slice of the SES v2 SendEmail JSON body the
// sender is responsible for.
type sesSendEmailBody struct {
	FromEmailAddress string
	Destination      struct {
		ToAddresses  []string
		CcAddresses  []string
		BccAddresses []string
	}
	ConfigurationSetName string
	Content              struct {
		Simple *struct {
			Subject struct{ Data, Charset string }
			Body    struct {
				Text *struct{ Data, Charset string }
				Html *struct{ Data, Charset string }
			}
			Attachments []struct {
				FileName           string
				ContentType        string
				ContentDisposition string
				RawContent         []byte
			}
		}
	}
}

func (f *fakeSES) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body sesSendEmailBody
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, fakeSESRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		SecurityToken: r.Header.Get("X-Amz-Security-Token"),
		Body:          body,
	})
	respond := f.respond
	f.mu.Unlock()
	if respond != nil {
		respond(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"MessageId":"fake-message-id"}`))
}

func (f *fakeSES) sent() []fakeSESRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSESRequest(nil), f.requests...)
}

// sesTestCredentials pins the SDK's default credential chain to static test
// keys and away from anything on the machine running the tests, so no test
// can pick up a developer's real AWS profile.
func sesTestCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDSESTEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ses-test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

// newTestSESSender points an SES sender at a fresh fake SES endpoint.
func newTestSESSender(t *testing.T, logger Logger) (*SESEmailSender, *fakeSES) {
	t.Helper()
	sesTestCredentials(t)
	fake := &fakeSES{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	sender, err := NewSESEmailSender(context.Background(), SESConfig{
		Region:           "us-east-1",
		ConfigurationSet: "ticket-pos-transactional",
		Endpoint:         srv.URL,
	}, sesTestFrom, logger)
	if err != nil {
		t.Fatalf("NewSESEmailSender: %v", err)
	}
	return sender, fake
}

// recordedLog is one line a lineLogger saw.
type recordedLog struct {
	level string
	msg   string
	args  []any
}

func (l recordedLog) String() string { return fmt.Sprint(l.level, " ", l.msg, " ", l.args) }

// lineLogger keeps every line with its level and attributes, so a test can
// read what a failure log said. recordingLogger (email_digest_test.go) only
// counts lines per level.
type lineLogger struct {
	mu    sync.Mutex
	lines []recordedLog
}

func (l *lineLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, recordedLog{level: level, msg: msg, args: args})
}

func (l *lineLogger) Info(msg string, args ...any)  { l.record("info", msg, args) }
func (l *lineLogger) Warn(msg string, args ...any)  { l.record("warn", msg, args) }
func (l *lineLogger) Error(msg string, args ...any) { l.record("error", msg, args) }

func (l *lineLogger) errors() []recordedLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []recordedLog
	for _, line := range l.lines {
		if line.level == "error" {
			out = append(out, line)
		}
	}
	return out
}

func TestSESSenderReportsItsFrom(t *testing.T) {
	sender, _ := newTestSESSender(t, noopLogger{})
	if sender.From() != sesTestFrom {
		t.Fatalf("From() = %q, want %q", sender.From(), sesTestFrom)
	}
}

// One passcode, one SES request: the transactional From, the recipient alone,
// the shared OTP copy in the recipient's Mail Locale as plain text, and the
// configured configuration set.
func TestSESSendOTPSendsOneRequestWithTheSharedCopy(t *testing.T) {
	for _, locale := range []Locale{LocaleES, LocaleEN} {
		t.Run(string(locale), func(t *testing.T) {
			sender, fake := newTestSESSender(t, noopLogger{})

			if err := sender.SendOTP(context.Background(), "ana@example.com", "482913", locale); err != nil {
				t.Fatalf("SendOTP: %v", err)
			}

			sent := fake.sent()
			if len(sent) != 1 {
				t.Fatalf("fake SES saw %d requests, want 1", len(sent))
			}
			req := sent[0]
			if req.Method != http.MethodPost || req.Path != "/v2/email/outbound-emails" {
				t.Fatalf("request = %s %s, want POST /v2/email/outbound-emails", req.Method, req.Path)
			}
			// Signed with the default chain's key, which is what proves the
			// credentials came from the SDK rather than anywhere else.
			if !strings.Contains(req.Authorization, "Credential=AKIDSESTEST/") {
				t.Fatalf("Authorization %q is not signed with the default chain's key", req.Authorization)
			}
			body := req.Body
			if body.FromEmailAddress != sesTestFrom {
				t.Fatalf("From = %q, want %q", body.FromEmailAddress, sesTestFrom)
			}
			if got := body.Destination.ToAddresses; len(got) != 1 || got[0] != "ana@example.com" {
				t.Fatalf("To = %v, want [ana@example.com]", got)
			}
			if len(body.Destination.CcAddresses) != 0 || len(body.Destination.BccAddresses) != 0 {
				t.Fatalf("unexpected Cc/Bcc: %+v", body.Destination)
			}
			if body.ConfigurationSetName != "ticket-pos-transactional" {
				t.Fatalf("ConfigurationSetName = %q, want ticket-pos-transactional", body.ConfigurationSetName)
			}
			want := OTPMessage{Code: "482913", Locale: locale}
			simple := body.Content.Simple
			if simple == nil {
				t.Fatal("no Simple content in the request")
			}
			if simple.Subject.Data != want.Subject() {
				t.Fatalf("Subject = %q, want %q", simple.Subject.Data, want.Subject())
			}
			if simple.Body.Text == nil || simple.Body.Text.Data != want.Text() {
				t.Fatalf("Text = %+v, want %q", simple.Body.Text, want.Text())
			}
			if simple.Body.Html != nil {
				t.Fatalf("the message carries HTML; SES mail is plain text only: %+v", simple.Body.Html)
			}
		})
	}
}

// A rejected send reaches the caller, so a sign-in whose passcode never left
// fails instead of claiming a code was sent. The failure log names the message
// type and the provider's error and never the recipient (#377), even when SES
// quotes the recipient back in its error, as its sandbox rejection does.
func TestSESSendOTPReturnsTheProviderErrorAndNeverLogsTheRecipient(t *testing.T) {
	logger := &lineLogger{}
	sender, fake := newTestSESSender(t, logger)
	fake.respond = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-ErrorType", "MessageRejected")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Email address is not verified. The following identities failed the check in region US-EAST-1: Ana@Example.com"}`))
	}

	err := sender.SendOTP(context.Background(), "ana@example.com", "482913", LocaleES)
	if err == nil {
		t.Fatal("SendOTP returned nil on a rejected send; a failed passcode must fail sign-in")
	}
	if !strings.Contains(err.Error(), "MessageRejected") {
		t.Fatalf("error %q does not carry the provider's error", err)
	}
	if !strings.Contains(err.Error(), "otp") {
		t.Fatalf("error %q does not name the message type", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "ana@example.com") {
		t.Fatalf("error %q names the recipient", err)
	}

	lines := logger.errors()
	if len(lines) != 1 {
		t.Fatalf("logged %d error lines, want 1: %v", len(lines), lines)
	}
	line := lines[0].String()
	if !strings.Contains(line, "otp") {
		t.Fatalf("failure log %q does not name the message type", line)
	}
	if !strings.Contains(line, "MessageRejected") {
		t.Fatalf("failure log %q does not carry the provider's error", line)
	}
	if strings.Contains(strings.ToLower(line), "ana@example.com") {
		t.Fatalf("failure log %q names the recipient", line)
	}
	if len(fake.sent()) != 1 {
		t.Fatalf("a rejected message was retried: %d requests", len(fake.sent()))
	}
}

// The Follow Digest waits for its own ticket (#696). Until then it is refused
// out loud, never sent through some other provider and never reported as sent.
func TestSESSenderRefusesTheFollowDigestItDoesNotYetSend(t *testing.T) {
	sender, fake := newTestSESSender(t, noopLogger{})
	err := sender.SendFollowDigest(context.Background(), FollowDigest{To: "a@example.com"})
	if !errors.Is(err, ErrNotYetOnSES) {
		t.Fatalf("error = %v, want ErrNotYetOnSES", err)
	}
	if !strings.Contains(err.Error(), "follow digest") {
		t.Fatalf("error %q does not name the message", err)
	}
	if n := len(fake.sent()); n != 0 {
		t.Fatalf("fake SES saw %d requests for a message SES does not send yet", n)
	}
}

// SESEmailSender is a full transactional sender, so the wiring can select it
// for the transactional identity.
var _ EmailSender = (*SESEmailSender)(nil)

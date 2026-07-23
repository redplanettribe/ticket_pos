package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// noopLogger satisfies Logger without asserting on output.
type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// newTestSender points a ResendEmailSender at a stub server standing in for
// Resend, so the tests exercise the real request-building and response-handling
// path without touching the network.
func newTestSender(t *testing.T, handler http.HandlerFunc) *ResendEmailSender {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	s := NewResendEmailSender("test-key", "Multiticketing <noreply@send.multiticketing.com>", noopLogger{})
	// Rewrite every request to the stub, so the real request-building path runs
	// against the httptest server instead of api.resend.com.
	s.client = &http.Client{
		Timeout:   2 * time.Second,
		Transport: rewriteHost{to: srv.URL, base: srv.Client().Transport},
	}
	return s
}

// rewriteHost sends every request to the stub server regardless of its URL.
type rewriteHost struct {
	to   string
	base http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := req.URL.Parse(r.to)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func TestResendSendOTP_Success(t *testing.T) {
	var gotAuth, gotBody string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})

	if err := sender.SendOTP(context.Background(), "user@example.com", "123456"); err != nil {
		t.Fatalf("SendOTP returned error: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}

	var payload resendRequest
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
	if len(payload.To) != 1 || payload.To[0] != "user@example.com" {
		t.Errorf("To = %v, want [user@example.com]", payload.To)
	}
	if payload.From != "Multiticketing <noreply@send.multiticketing.com>" {
		t.Errorf("From = %q", payload.From)
	}
	if payload.Text == "" || payload.Subject == "" {
		t.Errorf("empty subject or text: %+v", payload)
	}
	// The code must reach the recipient; a silent template drop would be worse
	// than a send error because the caller would report success.
	if want := "123456"; !contains(payload.Text, want) {
		t.Errorf("Text %q does not contain the passcode %q", payload.Text, want)
	}
}

func TestResendSendOTP_ProviderErrorIsReturned(t *testing.T) {
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"domain not verified"}`))
	})

	err := sender.SendOTP(context.Background(), "user@example.com", "123456")
	if err == nil {
		t.Fatal("SendOTP returned nil on a 422; a failed OTP send must surface so login fails")
	}
	if !contains(err.Error(), "422") {
		t.Errorf("error %q should carry the status code for diagnosis", err.Error())
	}
}

func TestResendSaleConfirmation_Success(t *testing.T) {
	var payload resendRequest
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusOK)
	})

	err := sender.SendSaleConfirmation(context.Background(), SaleConfirmation{
		To:           "buyer@example.com",
		CustomerName: "Ada",
		EventName:    "Midnight Synth",
		Reference:    "REF-1",
	})
	if err != nil {
		t.Fatalf("SendSaleConfirmation error: %v", err)
	}
	if !contains(payload.Text, "REF-1") || !contains(payload.Text, "Midnight Synth") {
		t.Errorf("confirmation missing reference or event: %q", payload.Text)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (needle == "" || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

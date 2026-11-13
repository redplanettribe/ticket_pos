package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// The SES tracer bullet (#692, ADR 0077): a deployment that set
// EMAIL_PROVIDER=ses delivers a One-time Passcode through Amazon SES, end to end
// through the same sign-in path Resend serves today. Nothing here injects a
// sender: the app wires SES from its configuration exactly as production would,
// and only the SES endpoint is a fake, so no AWS account is needed.

const sesTransactionalFrom = "Multiticketing <noreply@send.multiticketing.com>"

// fakeSESEndpoint records every SES v2 SendEmail request the app makes.
type fakeSESEndpoint struct {
	mu       sync.Mutex
	requests []fakeSESEmail
	reject   bool
}

// fakeSESEmail is the slice of a SendEmail body this test reads.
type fakeSESEmail struct {
	FromEmailAddress     string
	ConfigurationSetName string
	Destination          struct{ ToAddresses []string }
	Content              struct {
		Simple struct {
			Subject struct{ Data string }
			Body    struct{ Text struct{ Data string } }
		}
	}
}

func (f *fakeSESEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var email fakeSESEmail
	_ = json.Unmarshal(raw, &email)
	f.mu.Lock()
	f.requests = append(f.requests, email)
	reject := f.reject
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if reject {
		w.Header().Set("X-Amzn-ErrorType", "MessageRejected")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Email address is not verified."}`))
		return
	}
	_, _ = w.Write([]byte(`{"MessageId":"fake-message-id"}`))
}

func (f *fakeSESEndpoint) sent() []fakeSESEmail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSESEmail(nil), f.requests...)
}

// startAppWithSES boots an app over the shared database whose transactional
// identity selected SES, pointed at a fake SES endpoint. Credentials come from
// the SDK's default chain, pinned here to static test keys so nothing on the
// machine running the suite is read.
func startAppWithSES(t *testing.T) (*testEnv, *fakeSESEndpoint) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDINTEGRATION")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "integration-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	fake := &fakeSESEndpoint{}
	sesServer := httptest.NewServer(fake)
	t.Cleanup(sesServer.Close)

	cfg := platform.Config{
		DatabaseURL:       sharedConnStr,
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		StaffBaseURL:      "http://staff.example",
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
		EmailProvider: platform.EmailProviderSES,
		EmailFrom:     sesTransactionalFrom,
		SES: platform.SESConfig{
			Region:           "us-east-1",
			ConfigurationSet: "ticket-pos-transactional",
			Endpoint:         sesServer.URL,
		},
	}
	app, err := server.NewApp(context.Background(), cfg,
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		t.Fatalf("new app with ses: %v", err)
	}
	app.CustomersService.WithClock(func() time.Time { return fixedClock })
	srv := httptest.NewServer(server.NewHandler(app))
	t.Cleanup(func() {
		srv.Close()
		_ = app.Close()
	})
	return &testEnv{
		server:     srv,
		db:         app.DB.Pool,
		email:      sharedEmail,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}, fake
}

var passcodeInText = regexp.MustCompile(`\b[0-9]{6}\b`)

// A Customer asking for a passcode from a Spanish page gets exactly one SES
// request: the transactional From, the recipient, the Spanish subject and text
// from the shared email content, through the configured configuration set.
func TestPasscodeSignInSendsOneSESRequest(t *testing.T) {
	setupTest(t)
	env, fake := startAppWithSES(t)

	resp, body := requestPasscodeInLocale(t, env, "ana@example.com", "es")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	sent := fake.sent()
	if len(sent) != 1 {
		t.Fatalf("fake SES saw %d requests, want 1", len(sent))
	}
	email := sent[0]
	if email.FromEmailAddress != sesTransactionalFrom {
		t.Fatalf("From = %q, want %q", email.FromEmailAddress, sesTransactionalFrom)
	}
	if to := email.Destination.ToAddresses; len(to) != 1 || to[0] != "ana@example.com" {
		t.Fatalf("To = %v, want [ana@example.com]", to)
	}
	if email.ConfigurationSetName != "ticket-pos-transactional" {
		t.Fatalf("ConfigurationSetName = %q, want ticket-pos-transactional", email.ConfigurationSetName)
	}
	text := email.Content.Simple.Body.Text.Data
	code := passcodeInText.FindString(text)
	if code == "" {
		t.Fatalf("text %q carries no passcode", text)
	}
	want := platform.OTPMessage{Code: code, Locale: platform.LocaleES}
	if got := email.Content.Simple.Subject.Data; got != want.Subject() {
		t.Fatalf("subject = %q, want the Spanish subject %q", got, want.Subject())
	}
	if text != want.Text() {
		t.Fatalf("text = %q, want the shared Spanish copy %q", text, want.Text())
	}
}

// A passcode SES rejected was never sent, so the sign-in request fails rather
// than telling the Customer to check a mailbox nothing will arrive in.
func TestPasscodeSignInFailsWhenSESRejectsTheMessage(t *testing.T) {
	setupTest(t)
	env, fake := startAppWithSES(t)
	fake.mu.Lock()
	fake.reject = true
	fake.mu.Unlock()

	resp, _ := requestPasscodeInLocale(t, env, "ana@example.com", "es")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("request passcode answered 200 although SES rejected the message")
	}
	if n := len(fake.sent()); n != 1 {
		t.Fatalf("fake SES saw %d requests, want 1", n)
	}
}

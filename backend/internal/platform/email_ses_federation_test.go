package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Workload Identity Federation at the SES sender seam (#694, ADR 0077). The
// sender runs against fake GCP metadata, STS and SES endpoints; the tests
// assert what each fake is asked and which credentials sign the SES request,
// never how the exchange is built.

// fakeMetadata stands in for the GCP metadata server's identity endpoint. It
// hands out a numbered token per request, or fails when told to.
type fakeMetadata struct {
	mu       sync.Mutex
	requests []*http.Request
	fail     bool
}

func (f *fakeMetadata) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	n, fail := len(f.requests), f.fail
	f.mu.Unlock()
	if fail {
		http.Error(w, "metadata server unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = fmt.Fprintf(w, "fake-id-token-%d", n)
}

func (f *fakeMetadata) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

// fakeSTSExchange is one AssumeRoleWithWebIdentity call the fake STS saw.
type fakeSTSExchange struct {
	Action, RoleArn, WebIdentityToken, RoleSessionName string
}

// fakeSTS answers AssumeRoleWithWebIdentity with credentials derived from the
// token it was handed (token "fake-id-token-1" earns key "ASIA-fake-id-token-1"),
// so a test can tell which exchange signed which request. lifetimes sets how
// long each successive issue is valid; past its end the last one repeats.
type fakeSTS struct {
	mu        sync.Mutex
	exchanges []fakeSTSExchange
	lifetimes []time.Duration
	deny      bool
}

func (f *fakeSTS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.exchanges = append(f.exchanges, fakeSTSExchange{
		Action:           r.PostForm.Get("Action"),
		RoleArn:          r.PostForm.Get("RoleArn"),
		WebIdentityToken: r.PostForm.Get("WebIdentityToken"),
		RoleSessionName:  r.PostForm.Get("RoleSessionName"),
	})
	lifetime := time.Hour
	if len(f.lifetimes) > 0 {
		i := min(len(f.exchanges)-1, len(f.lifetimes)-1)
		lifetime = f.lifetimes[i]
	}
	deny := f.deny
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/xml")
	if deny {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>Not authorized to perform sts:AssumeRoleWithWebIdentity</Message></Error><RequestId>fake</RequestId></ErrorResponse>`))
		return
	}
	token := r.PostForm.Get("WebIdentityToken")
	_, _ = fmt.Fprintf(w, `<AssumeRoleWithWebIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleWithWebIdentityResult>
    <Credentials>
      <AccessKeyId>ASIA-%[1]s</AccessKeyId>
      <SecretAccessKey>secret-%[1]s</SecretAccessKey>
      <SessionToken>session-%[1]s</SessionToken>
      <Expiration>%[2]s</Expiration>
    </Credentials>
  </AssumeRoleWithWebIdentityResult>
  <ResponseMetadata><RequestId>fake</RequestId></ResponseMetadata>
</AssumeRoleWithWebIdentityResponse>`, token, time.Now().Add(lifetime).UTC().Format(time.RFC3339))
}

func (f *fakeSTS) seen() []fakeSTSExchange {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSTSExchange(nil), f.exchanges...)
}

// newFederatedSESSender builds an SES sender with a role to assume, pointed
// at fresh fake metadata, STS and SES endpoints. The default credential chain
// is pinned to static keys (AKIDSESTEST) on purpose: a request signed with
// them would mean the federation was bypassed.
func newFederatedSESSender(t *testing.T, logger Logger) (*SESEmailSender, *fakeMetadata, *fakeSTS, *fakeSES) {
	t.Helper()
	sesTestCredentials(t)
	metadata, stsFake, ses := &fakeMetadata{}, &fakeSTS{}, &fakeSES{}
	metadataSrv, stsSrv, sesSrv := httptest.NewServer(metadata), httptest.NewServer(stsFake), httptest.NewServer(ses)
	t.Cleanup(metadataSrv.Close)
	t.Cleanup(stsSrv.Close)
	t.Cleanup(sesSrv.Close)
	sender, err := NewSESEmailSender(context.Background(), SESConfig{
		Region:              "us-east-1",
		ConfigurationSet:    "ticket-pos-transactional",
		RoleARN:             sesTestRoleARN,
		WebIdentityAudience: sesTestAudience,
		Endpoint:            sesSrv.URL,
		STSEndpoint:         stsSrv.URL,
		MetadataEndpoint:    metadataSrv.URL,
	}, sesTestFrom, logger)
	if err != nil {
		t.Fatalf("NewSESEmailSender: %v", err)
	}
	if n := len(metadata.seen()) + len(stsFake.seen()); n != 0 {
		t.Fatalf("building the sender made %d federation calls, want none before the first send", n)
	}
	return sender, metadata, stsFake, ses
}

// assertSignedWith fails unless the SES request is signed with the access key
// and session token the fake STS issued for token.
func assertSignedWith(t *testing.T, req fakeSESRequest, token string) {
	t.Helper()
	if !strings.Contains(req.Authorization, "Credential=ASIA-"+token+"/") {
		t.Fatalf("SES request Authorization %q is not signed with the key STS issued for %s", req.Authorization, token)
	}
	if req.SecurityToken != "session-"+token {
		t.Fatalf("SES request session token = %q, want session-%s", req.SecurityToken, token)
	}
}

// With a role set, a send fetches an ID token for the configured audience
// from the metadata server, exchanges it at STS for the configured role, and
// signs the SES request with what STS returned.
func TestSESSenderSignsWithCredentialsFederatedFromTheMetadataToken(t *testing.T) {
	sender, metadata, sts, ses := newFederatedSESSender(t, noopLogger{})

	if err := sender.SendOTP(context.Background(), "ana@example.com", "482913", LocaleES); err != nil {
		t.Fatalf("SendOTP: %v", err)
	}

	tokenRequests := metadata.seen()
	if len(tokenRequests) != 1 {
		t.Fatalf("metadata server saw %d requests, want 1", len(tokenRequests))
	}
	tr := tokenRequests[0]
	if tr.Method != http.MethodGet || tr.URL.Path != "/computeMetadata/v1/instance/service-accounts/default/identity" {
		t.Fatalf("metadata request = %s %s, want GET of the default service account's identity", tr.Method, tr.URL.Path)
	}
	if got := tr.URL.Query().Get("audience"); got != sesTestAudience {
		t.Fatalf("ID token audience = %q, want %q", got, sesTestAudience)
	}
	if got := tr.Header.Get("Metadata-Flavor"); got != "Google" {
		t.Fatalf("Metadata-Flavor = %q, want Google", got)
	}

	exchanges := sts.seen()
	if len(exchanges) != 1 {
		t.Fatalf("STS saw %d exchanges, want 1", len(exchanges))
	}
	ex := exchanges[0]
	if ex.Action != "AssumeRoleWithWebIdentity" {
		t.Fatalf("STS action = %q, want AssumeRoleWithWebIdentity", ex.Action)
	}
	if ex.RoleArn != sesTestRoleARN {
		t.Fatalf("RoleArn = %q, want %q", ex.RoleArn, sesTestRoleARN)
	}
	if ex.WebIdentityToken != "fake-id-token-1" {
		t.Fatalf("WebIdentityToken = %q, want the metadata server's fake-id-token-1", ex.WebIdentityToken)
	}
	if ex.RoleSessionName == "" {
		t.Fatal("no RoleSessionName; STS requires one")
	}

	sent := ses.sent()
	if len(sent) != 1 {
		t.Fatalf("fake SES saw %d requests, want 1", len(sent))
	}
	assertSignedWith(t, sent[0], "fake-id-token-1")
}

// Credentials are reused across sends while they are good, and replaced
// before they expire: an issue already inside the refresh window is used for
// the send that fetched it and exchanged anew on the next.
func TestSESSenderReusesFederatedCredentialsAndRefreshesThemBeforeExpiry(t *testing.T) {
	t.Run("reused while good", func(t *testing.T) {
		sender, metadata, sts, ses := newFederatedSESSender(t, noopLogger{})
		for range 3 {
			if err := sender.SendOTP(context.Background(), "ana@example.com", "482913", LocaleES); err != nil {
				t.Fatalf("SendOTP: %v", err)
			}
		}
		if n := len(metadata.seen()); n != 1 {
			t.Fatalf("metadata server saw %d requests over three sends, want 1", n)
		}
		if n := len(sts.seen()); n != 1 {
			t.Fatalf("STS saw %d exchanges over three sends, want 1", n)
		}
		for _, req := range ses.sent() {
			assertSignedWith(t, req, "fake-id-token-1")
		}
	})
	t.Run("refreshed before expiry", func(t *testing.T) {
		sender, metadata, sts, ses := newFederatedSESSender(t, noopLogger{})
		// The first issue still has minutes to run but is inside the refresh
		// window; the second is good for an hour.
		sts.lifetimes = []time.Duration{webIdentityExpiryWindow - time.Minute, time.Hour}
		for range 3 {
			if err := sender.SendOTP(context.Background(), "ana@example.com", "482913", LocaleES); err != nil {
				t.Fatalf("SendOTP: %v", err)
			}
		}
		if n := len(metadata.seen()); n != 2 {
			t.Fatalf("metadata server saw %d requests, want 2 (one refresh)", n)
		}
		exchanges := sts.seen()
		if len(exchanges) != 2 || exchanges[1].WebIdentityToken != "fake-id-token-2" {
			t.Fatalf("STS exchanges = %+v, want a second exchange of a fresh token", exchanges)
		}
		sent := ses.sent()
		if len(sent) != 3 {
			t.Fatalf("fake SES saw %d requests, want 3", len(sent))
		}
		assertSignedWith(t, sent[0], "fake-id-token-1")
		assertSignedWith(t, sent[1], "fake-id-token-2")
		assertSignedWith(t, sent[2], "fake-id-token-2")
	})
}

// A metadata or STS failure is the send's failure: it reaches the caller (a
// passcode that could not be sent fails the sign-in), nothing reaches SES,
// and the one failure log names the message type and the cause but never the
// recipient (#377).
func TestSESSenderReturnsAFederationFailureAndNeverLogsTheRecipient(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(*fakeMetadata, *fakeSTS)
		cause   string
	}{
		{"metadata", func(m *fakeMetadata, _ *fakeSTS) { m.fail = true }, "metadata"},
		{"sts", func(_ *fakeMetadata, s *fakeSTS) { s.deny = true }, "AccessDenied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &lineLogger{}
			sender, metadata, sts, ses := newFederatedSESSender(t, logger)
			tc.breakIt(metadata, sts)

			err := sender.SendOTP(context.Background(), "Ana@Example.com", "482913", LocaleES)
			if err == nil {
				t.Fatal("SendOTP returned nil when no credentials could be obtained")
			}
			for _, want := range []string{"otp", tc.cause} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not carry %q", err, want)
				}
			}
			if strings.Contains(strings.ToLower(err.Error()), "ana@example.com") {
				t.Fatalf("error %q names the recipient", err)
			}
			if n := len(ses.sent()); n != 0 {
				t.Fatalf("fake SES saw %d requests without credentials", n)
			}

			lines := logger.errors()
			if len(lines) != 1 {
				t.Fatalf("logged %d error lines, want 1: %v", len(lines), lines)
			}
			line := lines[0].String()
			if !strings.Contains(line, "otp") || !strings.Contains(line, tc.cause) {
				t.Fatalf("failure log %q does not name the message type and the cause", line)
			}
			if strings.Contains(strings.ToLower(line), "ana@example.com") {
				t.Fatalf("failure log %q names the recipient", line)
			}
		})
	}
}

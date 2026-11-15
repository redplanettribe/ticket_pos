package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The sender-selection seam (#225, ADR 0030). Everything here is about one
// property: the Follow Digest and the One-time Passcode must never be able to
// leave on the same sending identity, and a half-configured deployment must
// choose "no Digest" over "a Digest from the transactional domain".
//
// This is tested at newEmailSender rather than through an HTTP request for the
// same reason the config rules are unit-tested: which identity a message leaves
// on is decided once, at startup, and is a property of wiring rather than of any
// request.

const (
	transactionalFrom = "Multiticketing <noreply@send.multiticketing.com>"
	digestFrom        = "Multiticketing <digest@digest.multiticketing.com>"
)

// quietLogger satisfies platform.Logger and counts errors, so a test can assert
// that a refusal was announced rather than swallowed.
type quietLogger struct{ errors int }

func (*quietLogger) Info(string, ...any)    {}
func (*quietLogger) Warn(string, ...any)    {}
func (l *quietLogger) Error(string, ...any) { l.errors++ }

// sendingConfig is a Config carrying a real transactional identity, which is
// what makes the split apply at all.
func sendingConfig(digest platform.DigestEmailConfig) platform.Config {
	return platform.Config{
		ResendAPIKey: "re_transactional",
		EmailFrom:    transactionalFrom,
		DigestEmail:  digest,
	}
}

// aFollowDigest is a Digest with the one field the refusing sender logs.
func aFollowDigest() platform.FollowDigest {
	return platform.FollowDigest{
		To:           "ana@example.com",
		CustomerName: "Ana",
		Locale:       platform.DefaultLocale,
		New:          []platform.FollowDigestEvent{{Name: "Followed Fest"}},
	}
}

func TestEmailSendersSplitWhenTheDigestHasItsOwnIdentity(t *testing.T) {
	sender := mustEmailSender(t, sendingConfig(platform.DigestEmailConfig{
		ResendAPIKey: "re_digest",
		From:         digestFrom,
	}), &quietLogger{})

	split, ok := sender.(*platform.SplitEmailSender)
	if !ok {
		t.Fatalf("sender is %T, want a split sender: one identity for both kinds of mail is the coupling #225 removes", sender)
	}

	// The Digest leaves on the Digest identity.
	digestSender, ok := split.Digest.(*platform.ResendEmailSender)
	if !ok {
		t.Fatalf("digest sender is %T, want a Resend sender", split.Digest)
	}
	if digestSender.From() != digestFrom {
		t.Fatalf("digest From = %q, want %q", digestSender.From(), digestFrom)
	}

	// Passcodes, Sale Confirmations and Payout notices do not move.
	txSender, ok := split.EmailSender.(*platform.ResendEmailSender)
	if !ok {
		t.Fatalf("transactional sender is %T, want a Resend sender", split.EmailSender)
	}
	if txSender.From() != transactionalFrom {
		t.Fatalf("transactional From = %q, want %q; it must be untouched by the Digest", txSender.From(), transactionalFrom)
	}
	if digestSender == txSender {
		t.Fatal("both kinds of mail share one sender object; there is no domain separation at all")
	}
}

// The acceptance criterion this whole issue turns on: unconfigured degrades to
// SILENCE, never to the transactional domain.
func TestAnUnconfiguredDigestSendsNothingAndLeavesTransactionalMailAlone(t *testing.T) {
	cases := []struct {
		name   string
		digest platform.DigestEmailConfig
	}{
		{"nothing set at all", platform.DigestEmailConfig{}},
		{"a key with no from", platform.DigestEmailConfig{ResendAPIKey: "re_digest"}},
		{"a from with no key", platform.DigestEmailConfig{From: digestFrom}},
		// Configured onto the transactional domain is the same failure wearing a
		// different variable name, so it earns the same refusal.
		{"pointed back at the transactional domain", platform.DigestEmailConfig{
			ResendAPIKey: "re_digest",
			From:         "Multiticketing <digest@send.multiticketing.com>",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &quietLogger{}
			sender := mustEmailSender(t, sendingConfig(tc.digest), logger)

			// Nothing is sent, and the caller can tell why.
			err := sender.SendFollowDigest(context.Background(), aFollowDigest())
			if err == nil {
				t.Fatal("a Digest was accepted with no Digest identity; it would have gone out on the transactional domain")
			}
			if !errors.Is(err, platform.ErrDigestSenderUnconfigured) {
				t.Fatalf("error = %v, want ErrDigestSenderUnconfigured", err)
			}
			if logger.errors == 0 {
				t.Fatal("the misconfiguration was never announced at error level")
			}

			// Transactional mail is entirely unaffected: still Resend, still the
			// transactional From. A broken Digest must not cost anyone a passcode.
			split, ok := sender.(*platform.SplitEmailSender)
			if !ok {
				t.Fatalf("sender is %T, want a split sender", sender)
			}
			txSender, ok := split.EmailSender.(*platform.ResendEmailSender)
			if !ok {
				t.Fatalf("transactional sender is %T, want a Resend sender", split.EmailSender)
			}
			if txSender.From() != transactionalFrom {
				t.Fatalf("transactional From = %q, want %q", txSender.From(), transactionalFrom)
			}
		})
	}
}

// A deployment with no provider key at all sends no mail of either kind, so
// there is no reputation to protect and nothing to refuse. Local development and
// the manual verification docs read Digests out of the container log, and #225
// must not take that away.
func TestWithoutAnyProviderKeyEverythingKeepsLogging(t *testing.T) {
	sender := mustEmailSender(t, platform.Config{}, &quietLogger{})

	if _, ok := sender.(*platform.LoggingEmailSender); !ok {
		t.Fatalf("sender is %T, want the logging sender", sender)
	}
	if err := sender.SendFollowDigest(context.Background(), aFollowDigest()); err != nil {
		t.Fatalf("SendFollowDigest: %v; local development must keep logging Digests", err)
	}
}

// recordingLogger keeps every Info line, so a test can read what startup said
// about each sending identity.
type recordingLogger struct {
	quietLogger
	infos []loggedLine
}

type loggedLine struct {
	msg  string
	args map[string]any
}

func (l *recordingLogger) Info(msg string, args ...any) {
	line := loggedLine{msg: msg, args: map[string]any{}}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			line.args[key] = args[i+1]
		}
	}
	l.infos = append(l.infos, line)
}

// line returns the first Info line whose message starts with prefix.
func (l *recordingLogger) line(t *testing.T, prefix string) loggedLine {
	t.Helper()
	for _, line := range l.infos {
		if strings.HasPrefix(line.msg, prefix) {
			return line
		}
	}
	t.Fatalf("no startup log line starting %q; got %v", prefix, l.infos)
	return loggedLine{}
}

// The provider switch (ADR 0077): each identity names its provider explicitly,
// and Resend is the default. An unset provider (the zero Config) and an
// explicit "resend" must wire exactly the same senders as before the switch
// existed, and startup must say, per identity, which provider and From it is
// using so the operator can confirm a switch took effect.
func TestBothIdentitiesUseResendByDefaultAndWhenNamed(t *testing.T) {
	cases := []struct {
		name     string
		provider platform.EmailProvider
	}{
		{"unset", ""},
		{"resend", platform.EmailProviderResend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sendingConfig(platform.DigestEmailConfig{
				Provider:     tc.provider,
				ResendAPIKey: "re_digest",
				From:         digestFrom,
			})
			cfg.EmailProvider = tc.provider
			logger := &recordingLogger{}

			split, ok := mustEmailSender(t, cfg, logger).(*platform.SplitEmailSender)
			if !ok {
				t.Fatal("sender is not a split sender")
			}
			if tx, ok := split.EmailSender.(*platform.ResendEmailSender); !ok || tx.From() != transactionalFrom {
				t.Fatalf("transactional sender = %T, want Resend from %q", split.EmailSender, transactionalFrom)
			}
			if digest, ok := split.Digest.(*platform.ResendEmailSender); !ok || digest.From() != digestFrom {
				t.Fatalf("digest sender = %T, want Resend from %q", split.Digest, digestFrom)
			}

			for prefix, from := range map[string]string{
				"email sender:":        transactionalFrom,
				"digest email sender:": digestFrom,
			} {
				line := logger.line(t, prefix)
				if fmt.Sprint(line.args["provider"]) != string(platform.EmailProviderResend) {
					t.Fatalf("%q logged provider %v, want resend", line.msg, line.args["provider"])
				}
				if fmt.Sprint(line.args["from"]) != from {
					t.Fatalf("%q logged from %v, want %q", line.msg, line.args["from"], from)
				}
			}
		})
	}
}

// Naming Resend explicitly does not conjure a key. With no provider credentials
// at all the deployment still logs mail, whatever the switch says, and startup
// says so in the same shape.
func TestNamingResendWithoutAKeyStillLogs(t *testing.T) {
	logger := &recordingLogger{}
	sender := mustEmailSender(t, platform.Config{
		EmailProvider: platform.EmailProviderResend,
		EmailFrom:     transactionalFrom,
		DigestEmail:   platform.DigestEmailConfig{Provider: platform.EmailProviderResend},
	}, logger)

	if _, ok := sender.(*platform.LoggingEmailSender); !ok {
		t.Fatalf("sender is %T, want the logging sender", sender)
	}
	for prefix, from := range map[string]string{
		"email sender:":        transactionalFrom,
		"digest email sender:": "",
	} {
		line := logger.line(t, prefix)
		if fmt.Sprint(line.args["provider"]) != "logging" {
			t.Fatalf("%q logged provider %v, want logging", line.msg, line.args["provider"])
		}
		if fmt.Sprint(line.args["from"]) != from {
			t.Fatalf("%q logged from %v, want %q", line.msg, line.args["from"], from)
		}
	}
}

// mustEmailSender wires the sender the way NewApp does and fails the test on a
// wiring error.
func mustEmailSender(t *testing.T, cfg platform.Config, logger platform.Logger) platform.EmailSender {
	t.Helper()
	sender, err := newEmailSender(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("newEmailSender: %v", err)
	}
	return sender
}

// hermeticAWS keeps the SDK's default credential chain away from anything on
// the machine running the tests. Building an SES sender reaches no network,
// but it does read the shared AWS config files.
func hermeticAWS(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDWIRINGTEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wiring-test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

// sesConfig is a deployment that selected SES for the transactional identity
// with complete SES settings, as LoadConfig would hand it over.
func sesConfig(digest platform.DigestEmailConfig) platform.Config {
	return platform.Config{
		EmailProvider: platform.EmailProviderSES,
		EmailFrom:     transactionalFrom,
		SES: platform.SESConfig{
			Region:           "us-east-1",
			ConfigurationSet: "ticket-pos-transactional",
		},
		DigestEmail: digest,
	}
}

// EMAIL_PROVIDER=ses selects the SES sender for the transactional identity, on
// the transactional From, and startup says so. The Digest identity keeps its own
// provider: the two switch independently (ADR 0077).
func TestSESIsSelectedForTheTransactionalIdentity(t *testing.T) {
	hermeticAWS(t)
	logger := &recordingLogger{}
	cfg := sesConfig(platform.DigestEmailConfig{
		Provider:     platform.EmailProviderResend,
		ResendAPIKey: "re_digest",
		From:         digestFrom,
	})

	split, ok := mustEmailSender(t, cfg, logger).(*platform.SplitEmailSender)
	if !ok {
		t.Fatal("sender is not a split sender")
	}
	if tx, ok := split.EmailSender.(*platform.SESEmailSender); !ok || tx.From() != transactionalFrom {
		t.Fatalf("transactional sender = %T, want SES from %q", split.EmailSender, transactionalFrom)
	}
	if digest, ok := split.Digest.(*platform.ResendEmailSender); !ok || digest.From() != digestFrom {
		t.Fatalf("digest sender = %T, want Resend from %q", split.Digest, digestFrom)
	}

	line := logger.line(t, "email sender:")
	if fmt.Sprint(line.args["provider"]) != string(platform.EmailProviderSES) {
		t.Fatalf("%q logged provider %v, want ses", line.msg, line.args["provider"])
	}
	if fmt.Sprint(line.args["from"]) != transactionalFrom {
		t.Fatalf("%q logged from %v, want %q", line.msg, line.args["from"], transactionalFrom)
	}
	digestLine := logger.line(t, "digest email sender:")
	if fmt.Sprint(digestLine.args["provider"]) != string(platform.EmailProviderResend) {
		t.Fatalf("%q logged provider %v, want resend", digestLine.msg, digestLine.args["provider"])
	}
}

// Selecting SES is the decision to send real mail, so a missing Resend key does
// not demote it to the logging sender. And the Digest's own rules still hold
// beside it: an unconfigured Digest identity refuses to send rather than
// borrowing the transactional one.
func TestSESWithoutAnyResendKeyStillSendsAndTheDigestStillRefuses(t *testing.T) {
	hermeticAWS(t)
	logger := &recordingLogger{}

	split, ok := mustEmailSender(t, sesConfig(platform.DigestEmailConfig{}), logger).(*platform.SplitEmailSender)
	if !ok {
		t.Fatal("sender is not a split sender; selecting SES must not fall back to logging")
	}
	if _, ok := split.EmailSender.(*platform.SESEmailSender); !ok {
		t.Fatalf("transactional sender = %T, want SES", split.EmailSender)
	}
	if err := split.SendFollowDigest(context.Background(), aFollowDigest()); err == nil {
		t.Fatal("an unconfigured Digest sent anyway")
	}
	if logger.errors == 0 {
		t.Fatal("the unconfigured Digest was not announced at error level")
	}
}

// fakeSESFroms is an SES v2 endpoint that answers every SendEmail and keeps
// the From of each, so a wiring test can see which identity a message left on.
type fakeSESFroms struct {
	mu    sync.Mutex
	froms []string
}

func (f *fakeSESFroms) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct{ FromEmailAddress string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.froms = append(f.froms, body.FromEmailAddress)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"MessageId":"fake-message-id"}`))
}

func (f *fakeSESFroms) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.froms...)
}

// providerConfig is a sending deployment with each identity on the given
// provider, complete settings for both, and SES pointed at endpoint. The
// Digest carries a Resend key only when it is on Resend, as LoadConfig hands
// it over.
func providerConfig(transactional, digest platform.EmailProvider, endpoint string) platform.Config {
	cfg := sendingConfig(platform.DigestEmailConfig{Provider: digest, From: digestFrom})
	cfg.EmailProvider = transactional
	if digest == platform.EmailProviderResend {
		cfg.DigestEmail.ResendAPIKey = "re_digest"
	}
	cfg.SES = platform.SESConfig{
		Region:           "us-east-1",
		ConfigurationSet: "ticket-pos-transactional",
		Endpoint:         endpoint,
	}
	return cfg
}

// senderFrom reports a wired sender's provider and From.
func senderFrom(t *testing.T, sender any) (platform.EmailProvider, string) {
	t.Helper()
	switch s := sender.(type) {
	case *platform.ResendEmailSender:
		return platform.EmailProviderResend, s.From()
	case *platform.SESEmailSender:
		return platform.EmailProviderSES, s.From()
	}
	t.Fatalf("sender is %T, want a Resend or SES sender", sender)
	return "", ""
}

// The two identities switch independently (ADR 0077): every combination
// wires each identity to its own provider on its own From, the two are never
// one object, and startup names each identity's provider and From.
func TestEveryCombinationOfProvidersWiresEachIdentityOnItsOwn(t *testing.T) {
	resend, ses := platform.EmailProviderResend, platform.EmailProviderSES
	for _, tc := range []struct{ transactional, digest platform.EmailProvider }{
		{resend, resend}, {resend, ses}, {ses, resend}, {ses, ses},
	} {
		t.Run(string(tc.transactional)+"/"+string(tc.digest), func(t *testing.T) {
			hermeticAWS(t)
			fake := &fakeSESFroms{}
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)
			logger := &recordingLogger{}

			split, ok := mustEmailSender(t, providerConfig(tc.transactional, tc.digest, srv.URL), logger).(*platform.SplitEmailSender)
			if !ok {
				t.Fatal("sender is not a split sender")
			}
			if logger.errors != 0 {
				t.Fatalf("a fully configured deployment logged %d errors at startup", logger.errors)
			}
			if any(split.EmailSender) == any(split.Digest) {
				t.Fatal("both identities share one sender object; there is no domain separation at all")
			}

			for _, identity := range []struct {
				prefix   string
				sender   any
				provider platform.EmailProvider
				from     string
			}{
				{"email sender: ", split.EmailSender, tc.transactional, transactionalFrom},
				{"digest email sender: ", split.Digest, tc.digest, digestFrom},
			} {
				provider, from := senderFrom(t, identity.sender)
				if provider != identity.provider || from != identity.from {
					t.Fatalf("%q wired %s from %q, want %s from %q", identity.prefix, provider, from, identity.provider, identity.from)
				}
				line := logger.line(t, identity.prefix+string(identity.provider))
				if fmt.Sprint(line.args["provider"]) != string(identity.provider) {
					t.Fatalf("%q logged provider %v, want %s", line.msg, line.args["provider"], identity.provider)
				}
				if fmt.Sprint(line.args["from"]) != identity.from {
					t.Fatalf("%q logged from %v, want %q", line.msg, line.args["from"], identity.from)
				}
			}

			// A Digest on SES leaves through SES on the Digest's From and
			// nothing else.
			if tc.digest == ses {
				if err := split.SendFollowDigest(context.Background(), aFollowDigest()); err != nil {
					t.Fatalf("SendFollowDigest: %v", err)
				}
				if got := fake.sent(); len(got) != 1 || got[0] != digestFrom {
					t.Fatalf("fake SES saw Froms %v, want exactly [%q]", got, digestFrom)
				}
			}
		})
	}
}

// A Digest on SES never needs a Digest Resend key, nor any Resend key at all:
// selecting SES is the decision to send real mail, so a deployment whose only
// Resend setting is missing still sends Digests through SES, while its
// transactional identity, on Resend with no key, keeps logging.
func TestADigestOnSESNeedsNoResendKeyAtAll(t *testing.T) {
	hermeticAWS(t)
	logger := &recordingLogger{}
	cfg := providerConfig(platform.EmailProviderResend, platform.EmailProviderSES, "")
	cfg.ResendAPIKey = ""

	split, ok := mustEmailSender(t, cfg, logger).(*platform.SplitEmailSender)
	if !ok {
		t.Fatal("sender is not a split sender; a Digest on SES must not fall back to logging")
	}
	if _, ok := split.EmailSender.(*platform.LoggingEmailSender); !ok {
		t.Fatalf("transactional sender = %T, want the logging sender: Resend has no key", split.EmailSender)
	}
	if digest, ok := split.Digest.(*platform.SESEmailSender); !ok || digest.From() != digestFrom {
		t.Fatalf("digest sender = %T, want SES from %q", split.Digest, digestFrom)
	}
	if line := logger.line(t, "email sender: logging"); fmt.Sprint(line.args["provider"]) != "logging" {
		t.Fatalf("%q logged provider %v, want logging", line.msg, line.args["provider"])
	}
	if line := logger.line(t, "digest email sender: ses"); fmt.Sprint(line.args["from"]) != digestFrom {
		t.Fatalf("%q logged from %v, want %q", line.msg, line.args["from"], digestFrom)
	}
}

// The Digest's rules hold under SES: no From, or a From on the transactional
// domain without the sharing declared, refuses every Digest loudly and leaves
// the transactional identity alone; declared sharing is obeyed.
func TestADigestOnSESKeepsTheDigestRules(t *testing.T) {
	sharedFrom := "Multiticketing <digest@send.multiticketing.com>"
	for _, tc := range []struct {
		name string
		from string
	}{
		{"no from", ""},
		{"the transactional domain, undeclared", sharedFrom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hermeticAWS(t)
			logger := &recordingLogger{}
			cfg := providerConfig(platform.EmailProviderSES, platform.EmailProviderSES, "")
			cfg.DigestEmail.From = tc.from

			split, ok := mustEmailSender(t, cfg, logger).(*platform.SplitEmailSender)
			if !ok {
				t.Fatal("sender is not a split sender")
			}
			err := split.SendFollowDigest(context.Background(), aFollowDigest())
			if !errors.Is(err, platform.ErrDigestSenderUnconfigured) {
				t.Fatalf("error = %v, want ErrDigestSenderUnconfigured", err)
			}
			if logger.errors == 0 {
				t.Fatal("the unconfigured Digest was not announced at error level")
			}
			if tx, ok := split.EmailSender.(*platform.SESEmailSender); !ok || tx.From() != transactionalFrom {
				t.Fatalf("transactional sender = %T, want SES from %q", split.EmailSender, transactionalFrom)
			}
		})
	}

	t.Run("the transactional domain, declared", func(t *testing.T) {
		hermeticAWS(t)
		cfg := providerConfig(platform.EmailProviderSES, platform.EmailProviderSES, "")
		cfg.DigestEmail.From = sharedFrom
		cfg.DigestEmail.AllowSharedSendingDomain = true

		split, ok := mustEmailSender(t, cfg, &quietLogger{}).(*platform.SplitEmailSender)
		if !ok {
			t.Fatal("sender is not a split sender")
		}
		if digest, ok := split.Digest.(*platform.SESEmailSender); !ok || digest.From() != sharedFrom {
			t.Fatalf("digest sender = %T, want SES from %q", split.Digest, sharedFrom)
		}
	})
}

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	sender := newEmailSender(sendingConfig(platform.DigestEmailConfig{
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
			sender := newEmailSender(sendingConfig(tc.digest), logger)

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
	sender := newEmailSender(platform.Config{}, &quietLogger{})

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

			split, ok := newEmailSender(cfg, logger).(*platform.SplitEmailSender)
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
	sender := newEmailSender(platform.Config{
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

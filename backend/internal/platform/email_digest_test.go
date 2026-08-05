package platform

import (
	"context"
	"errors"
	"testing"
)

// UnconfiguredDigestSender is what a deployment gets when it has a
// transactional sender and no Digest identity (#225, ADR 0030). It is the
// safe degradation: no marketing mail goes out at all, rather than going out
// from the domain the One-time Passcodes depend on.

func TestUnconfiguredDigestSenderSendsNothingAndSaysWhy(t *testing.T) {
	logger := &recordingLogger{}
	sender := NewUnconfiguredDigestSender(logger, "DIGEST_RESEND_API_KEY is not set")

	err := sender.SendFollowDigest(context.Background(), FollowDigest{
		To:           "ana@example.com",
		CustomerName: "Ana",
		Locale:       DefaultLocale,
		Events:       []FollowDigestEvent{{Name: "Followed Fest"}},
	})
	if err == nil {
		t.Fatal("SendFollowDigest succeeded with no Digest identity configured; a caller would record a Digest that was never sent")
	}
	if !errors.Is(err, ErrDigestSenderUnconfigured) {
		t.Fatalf("error = %v, want it to wrap ErrDigestSenderUnconfigured so callers can tell a misconfiguration from a provider outage", err)
	}
	// Loudly, not quietly: a silently dropped Digest is indistinguishable from a
	// feature nobody uses.
	if logger.errors == 0 {
		t.Fatal("a refused Digest was not logged at error level")
	}
}

// recordingLogger counts what was logged at each level.
type recordingLogger struct {
	infos  int
	warns  int
	errors int
}

func (l *recordingLogger) Info(string, ...any)  { l.infos++ }
func (l *recordingLogger) Warn(string, ...any)  { l.warns++ }
func (l *recordingLogger) Error(string, ...any) { l.errors++ }
func (l *recordingLogger) Debug(string, ...any) {}

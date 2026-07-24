package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The global outbound OTP ceiling (PRD #55 decision 12, ADR 0009).
//
// The per-email and per-IP limits assume the requester can be identified. On a
// public sign-in form that assumption is weak, so the ceiling is deliberately
// blind to who is asking: it caps passcode emails platform-wide per window,
// across both purposes, because what it protects is one shared sending domain.
//
// These tests lower the ceiling rather than sending thousands of requests. The
// number is configuration; the behaviour under test is what happens at it.

const (
	staffOTPRequestPath = "/api/v1/auth/otp/request"
	testGlobalCeiling   = 4
)

// withGlobalCeiling lowers the platform-wide ceiling for one test and restores
// the default afterwards. The OTP service is shared by the whole package, and
// integration tests run serially, so this mirrors how the harness swaps clocks.
func withGlobalCeiling(t *testing.T, ceiling int) {
	t.Helper()
	original := sharedApp.OTPService.GlobalCeiling()
	sharedApp.OTPService.WithGlobalCeiling(ceiling)
	t.Cleanup(func() {
		sharedApp.OTPService.WithGlobalCeiling(original)
	})
}

// fromClientIP addresses a request as the BFF would, so the per-IP limit counts
// a fresh bucket per caller and leaves the ceiling as the only control in play.
func fromClientIP(ip string) map[string]string {
	return map[string]string{platform.ClientIPHeader: ip}
}

// TestOTPGlobalCeilingBlocksSendsOnceReached is the acceptance criterion: once
// the platform has sent its window's worth of passcodes, it stops sending — to
// anyone, on any surface.
//
// Every request below uses a distinct email and a distinct client IP, so no
// per-key limit is anywhere near tripping. The requests are split across the
// staff and Customer sign-in surfaces on purpose: the ceiling spans purposes,
// because its job is protecting one shared sending domain rather than fairness
// between surfaces.
func TestOTPGlobalCeilingBlocksSendsOnceReached(t *testing.T) {
	env := setupTest(t)
	withGlobalCeiling(t, testGlobalCeiling)

	for i := 0; i < testGlobalCeiling; i++ {
		path := staffOTPRequestPath
		if i%2 == 1 {
			path = customerOTPRequestPath
		}
		resp, body := env.post(t, path, map[string]string{
			"email": fmt.Sprintf("ceiling%d@example.com", i),
		}, fromClientIP(fmt.Sprintf("198.51.100.%d", i)))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d to %s status=%d error=%+v", i, path, resp.StatusCode, body.Error)
		}
	}

	sentBeforeCeiling := env.email.OTPSendCount()

	// A brand-new email from a brand-new address: refused only because the
	// platform as a whole has sent enough this window.
	resp, body := env.post(t, staffOTPRequestPath, map[string]string{
		"email": "over-the-ceiling@example.com",
	}, fromClientIP("198.51.100.200"))
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_GLOBAL_CEILING_REACHED")

	// The Customer surface is refused by the same ceiling, not a separate one.
	resp, body = env.post(t, customerOTPRequestPath, map[string]string{
		"email": "over-the-ceiling-customer@example.com",
	}, fromClientIP("198.51.100.201"))
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_GLOBAL_CEILING_REACHED")

	// The point of the control is that no email leaves the building.
	if got := env.email.OTPSendCount(); got != sentBeforeCeiling {
		t.Fatalf("passcode emails sent after the ceiling: %d, want %d", got, sentBeforeCeiling)
	}
}

// TestOTPGlobalCeilingErrorIsDistinctFromPerKeyRateLimit protects the operator's
// ability to tell "this user is being throttled" apart from "the platform is
// under attack". Both are 429s; they must never share an error code.
//
// It also pins the order the two are evaluated in: a caller who has exhausted
// its own allowance hears the ordinary per-key error even while the ceiling is
// also reached, so an individual's throttling is never misreported as an
// outage.
func TestOTPGlobalCeilingErrorIsDistinctFromPerKeyRateLimit(t *testing.T) {
	env := setupTest(t)
	withGlobalCeiling(t, testGlobalCeiling)

	// Exhaust one email's own allowance (3 per window), from one address.
	const throttled = "throttled@example.com"
	for i := 0; i < 3; i++ {
		resp, body := env.post(t, staffOTPRequestPath, map[string]string{
			"email": throttled,
		}, fromClientIP("203.0.113.10"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}
	// One more send takes the platform to its ceiling.
	resp, body := env.post(t, staffOTPRequestPath, map[string]string{
		"email": "filler@example.com",
	}, fromClientIP("203.0.113.11"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filler request status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The throttled caller: still its own per-key problem.
	resp, body = env.post(t, staffOTPRequestPath, map[string]string{
		"email": throttled,
	}, fromClientIP("203.0.113.10"))
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_RATE_LIMITED")

	// An unrelated caller with a clean allowance: refused by the ceiling, and
	// told so with a different code.
	resp, body = env.post(t, staffOTPRequestPath, map[string]string{
		"email": "innocent@example.com",
	}, fromClientIP("203.0.113.12"))
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_GLOBAL_CEILING_REACHED")
}

// TestOTPGlobalCeilingLeavesNormalTrafficUnaffected is the other half of the
// bargain: at real volumes the ceiling must be invisible. A full staff sign-in
// and a full Customer sign-in both complete under the shipped default.
func TestOTPGlobalCeilingLeavesNormalTrafficUnaffected(t *testing.T) {
	env := setupTest(t)

	if ceiling := sharedApp.OTPService.GlobalCeiling(); ceiling < 10 {
		t.Fatalf("default global ceiling = %d, too low to be invisible to normal traffic", ceiling)
	}

	sessionID := verifyOTP(t, env, "operator@example.com")
	if sessionID == "" {
		t.Fatal("expected a Staff Session under the ceiling")
	}

	token := customerSignIn(t, env, "customer@example.com")
	if token == "" {
		t.Fatal("expected a Customer Session under the ceiling")
	}
}

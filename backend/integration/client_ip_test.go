package integration

import (
	"fmt"
	"net/http"
	"testing"
)

// maxOTPRequestsPerIP mirrors the per-IP allowance in internal/platform/otp.
const maxOTPRequestsPerIP = 10

// TestOTPPerIPLimitIgnoresForgedForwardingHeaders is the security property: a
// caller-supplied forwarding header must not buy a fresh per-IP OTP allowance.
//
// Every request below arrives from the same transport peer but claims a
// different X-Forwarded-For (and X-Real-IP) address, which is exactly the
// rotation an email-bombing attacker would use. Distinct emails keep the
// per-email cap out of the way, so the only control under test is the per-IP
// one. If the API were to believe any forwarding header, every request would
// land in its own bucket and none would ever be refused.
func TestOTPPerIPLimitIgnoresForgedForwardingHeaders(t *testing.T) {
	env := setupTest(t)

	forged := func(i int) map[string]string {
		return map[string]string{
			"X-Forwarded-For": fmt.Sprintf("203.0.113.%d, 198.51.100.%d", i, i),
			"X-Real-IP":       fmt.Sprintf("203.0.113.%d", i),
		}
	}

	for i := 0; i < maxOTPRequestsPerIP; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
			"email": fmt.Sprintf("forged%d@example.com", i),
		}, forged(i))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "forged-extra@example.com",
	}, forged(99))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("forged forwarding header raised the per-IP allowance: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "OTP_RATE_LIMITED" {
		t.Fatalf("expected OTP_RATE_LIMITED, got %+v", body.Error)
	}
	if data := string(body.Data); data != "" && data != "null" {
		t.Fatalf("expected null data on error, got %s", data)
	}
	if body.RequestID == "" {
		t.Fatal("expected a request_id in the error envelope")
	}
}

// TestOTPPerIPLimitCountsBFFSuppliedClientIP proves the other half: the address
// the BFF derives is what the limit counts, and a forged forwarding header
// alongside it changes nothing. A second, different BFF-supplied address still
// has its own allowance, so real traffic from distinct clients is unaffected.
func TestOTPPerIPLimitCountsBFFSuppliedClientIP(t *testing.T) {
	env := setupTest(t)

	headers := func(clientIP string, i int) map[string]string {
		return map[string]string{
			"X-BFF-Client-IP": clientIP,
			"X-Forwarded-For": fmt.Sprintf("203.0.113.%d", i),
		}
	}

	for i := 0; i < maxOTPRequestsPerIP; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
			"email": fmt.Sprintf("member%d@example.com", i),
		}, headers("198.51.100.20", i))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "member-extra@example.com",
	}, headers("198.51.100.20", 99))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for the exhausted client IP, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "OTP_RATE_LIMITED" {
		t.Fatalf("expected OTP_RATE_LIMITED, got %+v", body.Error)
	}

	resp, body = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "other-client@example.com",
	}, headers("198.51.100.21", 100))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a different client IP should have its own allowance: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
}

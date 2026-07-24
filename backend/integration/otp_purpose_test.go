package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
)

// The staff sign-in surface is the only OTP purpose exposed over HTTP today, so
// these tests drive the customer purpose through the platform OTP service the
// application is wired with. They assert the privilege boundary ADR 0010 draws
// between staff and Customer identity: a passcode minted for one surface is
// worthless on the other, and neither surface can spend the other's allowance.

func TestOTPStaffPasscodeIsNotVerifiableForCustomerPurpose(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	email := "boundary@example.com"

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": email,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request staff otp status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffCode := env.email.LastCode
	if staffCode == "" {
		t.Fatal("expected captured staff passcode")
	}

	err := sharedApp.OTPService.Verify(ctx, otp.PurposeCustomer, email, staffCode)
	assertOTPErrorCode(t, err, "OTP_INVALID")

	// The rejected cross-purpose attempt must not have consumed the staff
	// challenge: it is still redeemable for a Staff Session.
	resp, body = env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": email,
		"code":  staffCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify staff otp status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestOTPCustomerPasscodeIsNotVerifiableForStaffPurpose(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	email := "boundary@example.com"

	if err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, email, "203.0.113.7"); err != nil {
		t.Fatalf("issue customer otp: %v", err)
	}
	customerCode := env.email.LastCode
	if customerCode == "" {
		t.Fatal("expected captured customer passcode")
	}

	resp, body := env.post(t, "/api/v1/auth/otp/verify", map[string]string{
		"email": email,
		"code":  customerCode,
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 verifying a customer passcode on the staff surface, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "OTP_INVALID" {
		t.Fatalf("expected OTP_INVALID, got %+v", body.Error)
	}

	// The customer challenge survives the staff attempt untouched.
	if err := sharedApp.OTPService.Verify(ctx, otp.PurposeCustomer, email, customerCode); err != nil {
		t.Fatalf("verify customer otp after staff attempt: %v", err)
	}
}

func TestOTPPerEmailRateLimitIsScopedByPurpose(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	email := "shared-allowance@example.com"

	// Exhaust the customer allowance for this email: 3 per 15 minutes.
	for i := 0; i < 3; i++ {
		if err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, email, "203.0.113.20"); err != nil {
			t.Fatalf("issue customer otp %d: %v", i, err)
		}
	}
	err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, email, "203.0.113.20")
	assertOTPErrorCode(t, err, "OTP_RATE_LIMITED")

	// The staff allowance for the same email is untouched.
	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": email,
	}, map[string]string{"X-Forwarded-For": "203.0.113.20"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff otp request after customer allowance exhausted: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestOTPPerIPRateLimitIsScopedByPurpose(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	clientIP := "198.51.100.42"

	// Exhaust the customer allowance for this IP: 10 per 15 minutes.
	for i := 0; i < 10; i++ {
		if err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, fmt.Sprintf("customer%d@example.com", i), clientIP); err != nil {
			t.Fatalf("issue customer otp %d: %v", i, err)
		}
	}
	err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, "customer-over@example.com", clientIP)
	assertOTPErrorCode(t, err, "OTP_RATE_LIMITED")

	// A staff request from the same IP still has its full allowance.
	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": "operator@example.com",
	}, map[string]string{"X-Forwarded-For": clientIP})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff otp request after customer IP allowance exhausted: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestOTPStaffTrafficDoesNotExhaustCustomerAllowance(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	email := "shared-allowance@example.com"

	for i := 0; i < 3; i++ {
		resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
			"email": email,
		}, map[string]string{"X-Forwarded-For": "198.51.100.77"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("staff otp request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}
	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": email,
	}, map[string]string{"X-Forwarded-For": "198.51.100.77"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected staff allowance exhausted, got %d error=%+v", resp.StatusCode, body.Error)
	}

	if err := sharedApp.OTPService.Issue(ctx, otp.PurposeCustomer, email, "198.51.100.77"); err != nil {
		t.Fatalf("customer otp issue after staff allowance exhausted: %v", err)
	}
}

func assertOTPErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil error", want)
	}
	var domainErr apperror.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error %s, got %v", want, err)
	}
	if domainErr.Code() != want {
		t.Fatalf("expected %s, got %s", want, domainErr.Code())
	}
}

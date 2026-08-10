package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The One-time Passcode is the first message this platform writes in the
// language the reader asked in (#244, ADR 0033), and it is the tracer bullet for
// every message that follows: it touches no sale, so it proves the whole journey
// from a field in a request body to the words in a delivered email on its own.
//
// The assertions below are on the RENDERED message rather than on the Locale
// having been carried. A test that only checked a language was passed would pass
// just as happily against an email that was never translated.

// requestPasscodeInLocale asks for a Customer passcode from a Storefront page
// served in the given Locale. An empty locale sends no field at all, which is a
// caller with no page to name one.
func requestPasscodeInLocale(t *testing.T, env *testEnv, email, locale string) (*http.Response, envelope) {
	t.Helper()
	payload := map[string]string{"email": email}
	if locale != "" {
		payload["locale"] = locale
	}
	return env.post(t, customerOTPRequestPath, payload, nil)
}

// lastPasscodeSent is the passcode email as its recipient reads it. The capture
// sender keeps the message whole rather than a rendered string precisely so a
// test can render it here (platform.CaptureEmailSender).
func lastPasscodeSent(t *testing.T, env *testEnv) platform.OTPMessage {
	t.Helper()
	sent := env.email.OTPsSent()
	if len(sent) == 0 {
		t.Fatal("no passcode email was delivered")
	}
	return sent[len(sent)-1]
}

// TestCustomerPasscodeIsWrittenInSpanishWhenAskedFromASpanishPage is the story
// the ticket tells: a visitor reading the Storefront in Spanish asks for a
// passcode and the email arrives in Spanish.
func TestCustomerPasscodeIsWrittenInSpanishWhenAskedFromASpanishPage(t *testing.T) {
	env := setupTest(t)

	resp, body := requestPasscodeInLocale(t, env, "ana@example.com", "es")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	message := lastPasscodeSent(t, env)
	if got := message.Subject(); got != "Su código de acceso de Multiticketing" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	if got := message.Text(); !strings.Contains(got, "Su código de acceso es "+env.email.LastCode) {
		t.Fatalf("text = %q, want the Spanish body carrying the delivered code", got)
	}
}

// TestCustomerPasscodeIsEnglishWhenTheRequestNamesNoLocale proves the floor of
// the chain holds for a caller that has no page to speak for: English is what
// every mail this platform sends was written in before this feature.
func TestCustomerPasscodeIsEnglishWhenTheRequestNamesNoLocale(t *testing.T) {
	env := setupTest(t)

	resp, body := requestPasscodeInLocale(t, env, "bruno@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if got := lastPasscodeSent(t, env).Subject(); got != "Your Multiticketing passcode" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
}

// TestCustomerPasscodeIgnoresALanguageItCannotWrite proves the
// ignore-don't-refuse rule the sign-in doors already use. A passcode is how a
// person gets back into their tickets: a language this platform does not serve,
// or one that is not a language at all, must never be the reason the email did
// not go out.
func TestCustomerPasscodeIgnoresALanguageItCannotWrite(t *testing.T) {
	env := setupTest(t)

	for _, locale := range []string{"fr", "not-a-locale", "  "} {
		resp, body := requestPasscodeInLocale(t, env, "carla@example.com", locale)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request passcode with locale %q status=%d error=%+v; an unserved language must never fail the request",
				locale, resp.StatusCode, body.Error)
		}
		if got := lastPasscodeSent(t, env).Subject(); got != "Your Multiticketing passcode" {
			t.Fatalf("locale %q produced subject %q, want the English subject", locale, got)
		}
	}
}

// TestRequestingAPasscodeLeavesAStoredMailLocaleUntouched is the security
// property of this ticket rather than a detail of it (ADR 0033).
//
// This route is anonymous: anybody may name anybody's address on it. If the
// locale it carried were remembered, a stranger could rewrite the language a
// Customer's receipts arrive in without ever proving they own the address. The
// language words one email, and only a completed sign-in writes what is stored.
func TestRequestingAPasscodeLeavesAStoredMailLocaleUntouched(t *testing.T) {
	env := setupTest(t)

	customerSignInWithLocale(t, env, "diego@example.com", "es")
	if got := mailLocale(t, env, "diego@example.com"); got != "es" {
		t.Fatalf("mail locale after sign-in=%q, want es", got)
	}

	resp, body := requestPasscodeInLocale(t, env, "diego@example.com", "en")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if got := mailLocale(t, env, "diego@example.com"); got != "es" {
		t.Fatalf("mail locale=%q after a passcode request naming en; asking for a passcode must not rewrite a remembered language", got)
	}
	// And the passcode itself still followed the request, not the record: the
	// two are independent, which is the whole point of the rule above.
	if got := lastPasscodeSent(t, env).Subject(); got != "Your Multiticketing passcode" {
		t.Fatalf("subject = %q, want the English subject the request named", got)
	}
}

// TestRequestingAPasscodeForAnUnknownAddressStoresNothing is the same rule at
// the other end: an address the platform has never seen must not gain a record
// — let alone a remembered language — because somebody typed it into a public
// form. SQL because there is no API that could show the absence of a Customer.
func TestRequestingAPasscodeForAnUnknownAddressStoresNothing(t *testing.T) {
	env := setupTest(t)

	resp, body := requestPasscodeInLocale(t, env, "elena@example.com", "es")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var customers int
	if err := env.db.QueryRow(`SELECT count(*) FROM customers WHERE email = $1`, "elena@example.com").Scan(&customers); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	if customers != 0 {
		t.Fatalf("a passcode request created %d Customer row(s); it proves nothing about who owns the address", customers)
	}
}

// TestStaffPasscodeIsAlwaysEnglish proves the boundary ADR 0033 draws rather
// than an omission: no Member has a language, apps/staff has no i18n, and the
// staff caller names DefaultLocale at its own call site. The staff route carries
// no locale field to name anything else with.
func TestStaffPasscodeIsAlwaysEnglish(t *testing.T) {
	env := setupTest(t)

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email":  "staff@example.com",
		"locale": "es",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request staff otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	message := lastPasscodeSent(t, env)
	if message.Locale != platform.DefaultLocale {
		t.Fatalf("staff passcode locale=%q, want %q", message.Locale, platform.DefaultLocale)
	}
	if got := message.Subject(); got != "Your Multiticketing passcode" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
}

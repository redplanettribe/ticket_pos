package platform

import (
	"strings"
	"testing"
)

// TestOTPMessageIsWrittenInEnglish is what a Member reads, and what a Customer
// reads when nothing named a language. Staff mail is English by decision
// (ADR 0033), so this is also the assertion that the staff door is unchanged.
func TestOTPMessageIsWrittenInEnglish(t *testing.T) {
	message := OTPMessage{Code: "123456", Locale: DefaultLocale}

	if got := message.Subject(); got != "Your Multiticketing passcode" {
		t.Fatalf("subject = %q", got)
	}
	if got := message.Text(); !strings.Contains(got, "Your one-time passcode is 123456.") {
		t.Fatalf("text = %q, want the English passcode line carrying the code", got)
	}
}

// TestOTPMessageIsWrittenInSpanish asserts on the words themselves rather than
// on the Locale having been carried, because the words are the deliverable: a
// visitor reading a Spanish page and receiving an English email is the whole
// bug this feature closes (#244).
//
// It also pins the REGISTER. Spanish here is usted, matching the Storefront
// catalog ("compruebe su conexión e inténtelo de nuevo"), and a tú-form
// rewrite would pass any test that only checked the language had branched.
func TestOTPMessageIsWrittenInSpanish(t *testing.T) {
	message := OTPMessage{Code: "654321", Locale: LocaleES}

	if got := message.Subject(); got != "Su código de acceso de Multiticketing" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}

	text := message.Text()
	if !strings.Contains(text, "Su código de acceso es 654321.") {
		t.Fatalf("text = %q, want the Spanish passcode line carrying the code", text)
	}
	if !strings.Contains(text, "Si no lo solicitó") {
		t.Fatalf("text = %q, want the usted form; ADR 0033 settles the register", text)
	}
	if strings.Contains(text, "Tu ") || strings.Contains(text, "solicitaste") {
		t.Fatalf("text = %q is written in tú; Spanish mail is usted (ADR 0033)", text)
	}
}

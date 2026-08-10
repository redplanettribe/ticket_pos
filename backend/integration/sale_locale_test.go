package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Sale Locale end to end (#245, ADR 0033).
//
// This is the ONE test that proves the whole journey: a language named in a
// checkout request body survives the Payment the checkout begins, the confirm
// leg that arrives on the provider's return redirect carrying nothing but a
// transaction id, the transaction that commits the Ticket Sale, the column it is
// stored in, and the composition of the receipt at send time. Every other
// property of localized mail is cheaper to test at its own seam — the chain in
// platform.ResolveMailLocale, the words in the copy tests — and repeating this
// journey per message would buy nothing but minutes.
//
// It uses the PAID legs deliberately. A free checkout settles inside the begin
// request, where the language is still in living memory; only the paid path
// forces it through storage to be read back by a second request.

// saleLocale reads the Sale Locale recorded on the Ticket Sale behind a
// confirmation reference. SQL because nothing publishes this column: it is a
// fact about how the sale is written to, not about the sale, and no API surface
// shows it to anybody.
func saleLocale(t *testing.T, env *testEnv, confirmationRef string) sql.NullString {
	t.Helper()
	var locale sql.NullString
	if err := env.db.QueryRow(
		`SELECT locale FROM ticket_sales WHERE confirmation_ref = $1`, confirmationRef,
	).Scan(&locale); err != nil {
		t.Fatalf("read sale locale: %v", err)
	}
	return locale
}

// TestOnlineSaleMadeOnASpanishPageIsConfirmedInSpanish is the story the ticket
// tells: a guest reads the Storefront in Spanish, checks out without ever
// signing in, and the receipt arrives in Spanish. Nothing about their record is
// consulted and no earlier visit is needed — the page they bought on is the only
// thing that knew, and the sale kept it.
func TestOnlineSaleMadeOnASpanishPageIsConfirmedInSpanish(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Noche de Jazz", "noche-de-jazz", 1500, 10)

	body := checkoutBody("ana@example.com", "Ana", "Ruiz", map[string]any{
		"ticket_type_id": gaID, "quantity": 1,
	})
	body["locale"] = "es"

	begun := beginCheckoutOK(t, env, "test-org", "noche-de-jazz", body)
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if settled.Status != "approved" {
		t.Fatalf("confirm status=%q, want approved", settled.Status)
	}

	// Stored on the sale, not merely carried through the request: this is what
	// makes every later mail about this sale Spanish too, including one sent
	// days afterwards from no page at all.
	if got := saleLocale(t, env, settled.ConfirmationRef); !got.Valid || got.String != "es" {
		t.Fatalf("ticket_sales.locale = %+v, want es", got)
	}

	// The assertion is on the WORDS the buyer reads. A test that only checked a
	// Locale had been carried would pass just as happily against a receipt that
	// was never translated.
	confirmations := env.email.Confirmations()
	if len(confirmations) != 1 {
		t.Fatalf("captured %d Sale Confirmations, want exactly 1", len(confirmations))
	}
	confirmation := confirmations[0]
	if got := confirmation.Subject(); got != "Sus entradas para Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := confirmation.Text()
	for _, want := range []string{"Hola Ana Ruiz:", "Su compra de Noche de Jazz está confirmada.", "Referencia: " + settled.ConfirmationRef} {
		if !strings.Contains(text, want) {
			t.Fatalf("receipt = %q, want it to contain %q", text, want)
		}
	}
}

// TestCheckoutRecordsNoLocaleWhenItCannotBeWrittenIn is the rule that must never
// be traded away: a missing or malformed language is dropped, the purchase
// completes, and the sale records nothing rather than asserting English.
//
// Recording 'en' here would be the bug ADR 0033 spends a section on — the Sale
// Locale sits at the top of the chain, so an asserted English would outrank a
// Spanish-speaking Customer's own remembered language forever.
func TestCheckoutRecordsNoLocaleWhenItCannotBeWrittenIn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Locale Fest", "locale-fest", 1500, 10)

	for _, locale := range []any{nil, "", "fr", "not-a-locale", "   "} {
		body := checkoutBody("bruno@example.com", "Bruno", "Vera", map[string]any{
			"ticket_type_id": gaID, "quantity": 1,
		})
		if locale != nil {
			body["locale"] = locale
		}

		resp, envelope := beginCheckout(t, env, "test-org", "locale-fest", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("begin checkout with locale %v status=%d error=%+v; a language must never fail a purchase",
				locale, resp.StatusCode, envelope.Error)
		}
		var begun beginCheckoutResult
		if err := json.Unmarshal(envelope.Data, &begun); err != nil {
			t.Fatalf("decode begin checkout result: %v", err)
		}
		settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

		if got := saleLocale(t, env, settled.ConfirmationRef); got.Valid {
			t.Fatalf("locale %v recorded %q on the sale, want none: absence is not English", locale, got.String)
		}
		if got := lastConfirmationText(t, env); !strings.Contains(got, "Your purchase for Locale Fest is confirmed.") {
			t.Fatalf("locale %v produced receipt %q, want the English receipt", locale, got)
		}
	}
}

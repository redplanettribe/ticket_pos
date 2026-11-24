package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// What the Cover Image kinds carry of their Event (#879, spec #870): the Sale
// Confirmation, the Ticket Assignment and the Sale Re-addressing. Each is
// driven through the path that really sends it, once with the renditions
// recorded and once with them null, and the captured message is read for the
// Organization's name and the rendition URLs (assertMailEvent, beside the
// compact Event row's tests).

// theOneConfirmationTo is the one Sale Confirmation captured for an address.
func theOneConfirmationTo(t *testing.T, env *testEnv, address string) platform.SaleConfirmation {
	t.Helper()
	var found []platform.SaleConfirmation
	for _, c := range env.email.Confirmations() {
		if c.To == address {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Sale Confirmations to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// An Online Sale's receipt, from the checkout's own send. Its text names the
// Organization the buyer paid.
func TestTheSaleConfirmationCarriesItsCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, freeID := publishFreeEvent(t, env, sessionID, "Cover Fest", "cover-fest", 10)
		rendered := record(eventID)
		env.email.Reset()

		claimFree(t, env, "cover-fest", freeID, "ana@example.com", 1)

		confirmation := theOneConfirmationTo(t, env, "ana@example.com")
		assertMailEvent(t, confirmation.Event, eventID, rendered)
		if want := "\nOrganizer: " + eventOrganizationName(t, eventID) + "\n"; !strings.Contains(confirmation.Text(), want) {
			t.Errorf("the receipt's text does not name the Organization: want %q in\n%s", want, confirmation.Text())
		}
	})
}

// A Sale Import batch's receipts, which read the Event once for the batch.
func TestAnImportedSaleConfirmationCarriesItsCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, gaID := publishEventStarting(t, env, sessionID, "Import Cover Fest", "import-cover-fest",
			env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
		rendered := record(eventID)
		env.email.Reset()

		commitBatch(t, env, sessionID, eventID, "cover-1", []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
				"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
				"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		})

		for _, address := range []string{"ana@example.com", "bob@example.com"} {
			assertMailEvent(t, theOneConfirmationTo(t, env, address).Event, eventID, rendered)
		}
	})
}

// The Assignment mail, from the catalog's own path.
func TestTheTicketAssignmentCarriesItsCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		f := newAssignmentFixture(t, env)
		rendered := record(f.eventID)

		assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")

		assertMailEvent(t, assignmentMailFor(t, env, "carla@example.com").Event, f.eventID, rendered)
	})
}

// The Re-addressing mail, from a Platform Operator's act.
func TestTheSaleReAddressingCarriesItsCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, gaID := publishEventStarting(t, env, sessionID, "Readdress Cover Fest", "readdress-cover-fest",
			env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
		rendered := record(eventID)
		ref, _ := buyOnlineThroughPayPhone(t, "readdress-cover-fest", gaID, "ana.lopes@example.com", 1)
		operatorSessionID := operatorSession(t, env, "operator@example.com")
		env.email.Reset()

		reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})

		assertMailEvent(t, reAddressingMailFor(t, env, "ana.lopez@example.com").Event, eventID, rendered)
	})
}

// A sale a Member records by hand, and the replacement a Sale Correction
// records, are mailed through the import channel's one receipt, each reading
// the Event for itself.
func TestManualAndCorrectedSaleConfirmationsCarryTheirCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, gaID := publishEventStarting(t, env, sessionID, "Manual Cover Fest", "manual-cover-fest",
			env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
		rendered := record(eventID)
		commitBatch(t, env, sessionID, eventID, "manual-cover-1", []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
				"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		})
		imported := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
		env.email.Reset()

		recordManualSaleOK(t, env, sessionID, eventID,
			manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
		correction := correctionBody("anna@example.com", "Anna", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z")
		correction["send_confirmation"] = true
		correctImportedSaleOK(t, env, sessionID, eventID, imported, correction)

		for _, address := range []string{"bob@example.com", "anna@example.com"} {
			assertMailEvent(t, theOneConfirmationTo(t, env, address).Event, eventID, rendered)
		}
	})
}

// Accepting a Re-addressing mails the corrected address the Sale's receipt.
func TestTheReAddressedSaleConfirmationCarriesItsCoverImage(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		const wrong, corrected = "pablo.mistyped@example.com", "pablo.meant@example.com"
		s := strandSale(t, env, "Moved Cover Fest", "moved-cover-fest", wrong, corrected, 1)
		rendered := record(s.eventID)
		env.email.Reset()

		acceptReAddressingLinkOK(t, payphoneEnv, s.token)

		assertMailEvent(t, theOneConfirmationTo(t, env, corrected).Event, s.eventID, rendered)
	})
}

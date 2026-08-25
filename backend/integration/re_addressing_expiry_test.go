package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// A pending Sale Re-addressing does not outlive its Sale or its Event (issue
// #424, parent #419, ADR 0058).
//
// TWO ENDINGS, NEITHER OF WHICH WRITES TO THE RECORD. A Sale Reversal by any
// route — the Customer's own undo, the drain finishing one the platform half
// did, an Operator Reversal — leaves the row exactly as it was in storage and
// the derived state reads `expired`, because the Sale is no longer active. The
// Event's start is the other: from that instant nothing can be recorded and
// nothing pending can be accepted, and the Holder Address Purge that already
// takes unaccepted Assignment addresses takes the unaccepted corrected address
// too, keeping the row and its operator/time facts. In both endings the
// corrected address is told nothing, having never become party to anything.
//
// Modelled on the Assignment Link's "dies on a reversed Sale and expires at the
// doors" test and on the Holder Address Purge tests. The Operator Reversal case
// lives with the link's other refusals in re_addressing_link_test.go; what is
// here is the Customer's routes to a reversal, and the doors.

// readdressClocksAt walks EVERY clock a stranded Sale is read through. The
// Sale is bought and its link opened through the PayPhone app, the purge and
// the Operator lookup run on the shared one, and a doors-open moment that only
// one of them saw would have the link and the lookup disagreeing about whether
// the Event has started. Both are put back by cleanup.
func readdressClocksAt(t *testing.T, at time.Time) {
	t.Helper()
	holdClocksAt(at)
	atReversalClock(t, at)
	payphoneApp.CatalogService.WithClock(func() time.Time { return at })
	t.Cleanup(func() {
		payphoneApp.CatalogService.WithClock(func() time.Time { return fixedClock })
	})
}

// strandAnotherSale is strandSale for a SECOND Sale in the same test: the
// Organization and the Operator already exist, so it publishes another Event
// under the same sessions rather than minting them again.
func strandAnotherSale(t *testing.T, env *testEnv, first strandedSale, name, slug, wrong, corrected string) strandedSale {
	t.Helper()
	eventID, gaID := publishEventStarting(t, env, first.orgSession, name, slug,
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref, ctid := buyOnlineThroughPayPhone(t, slug, gaID, wrong, 1)
	env.email.Reset()
	reAddressOK(t, payphoneEnv, first.operator, ref, reAddressBody{Email: corrected})
	token := reAddressingTokenFrom(t, reAddressingMailFor(t, env, corrected))
	env.email.Reset()
	return strandedSale{
		eventID: eventID, gaID: gaID, ref: ref,
		saleID:              saleIDOfPayment(t, env, ctid),
		clientTransactionID: ctid,
		orgSession:          first.orgSession,
		operator:            first.operator,
		token:               token,
	}
}

// reAddressingRow is the stored record, read through SQL for the one fact no
// surface hands anybody: that the corrected address is GONE from the database
// rather than merely hidden from a page, and that everything beside it stayed.
type reAddressingRow struct {
	operatorEmail, previousEmail string
	correctedEmail               sql.NullString
	requestedAt                  time.Time
	acceptedAt, withdrawnAt      sql.NullTime
}

func readReAddressingRow(t *testing.T, env *testEnv, saleID string) reAddressingRow {
	t.Helper()
	var row reAddressingRow
	if err := env.db.QueryRow(`
		SELECT operator_email, previous_email, corrected_email, requested_at, accepted_at, withdrawn_at
		FROM sale_re_addressings WHERE ticket_sale_id = $1
	`, saleID).Scan(&row.operatorEmail, &row.previousEmail, &row.correctedEmail, &row.requestedAt, &row.acceptedAt, &row.withdrawnAt); err != nil {
		t.Fatalf("read the re-addressing of Sale %s: %v", saleID, err)
	}
	return row
}

// assertReAddressingLinkNoLongerValid: the link's GET and POST both refuse with
// the buyer-facing code and name the reason, and the click wrote nothing —
// no Confirmation, no Customer, nothing to the corrected address.
func assertReAddressingLinkNoLongerValid(t *testing.T, env *testEnv, s strandedSale, corrected, reason string) {
	t.Helper()
	resp, body := viewReAddressingLink(t, payphoneEnv, s.token)
	assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
	if got := answerDetail(body, "reason"); got != reason {
		t.Errorf("view details.reason = %q, want %q", got, reason)
	}
	resp, body = acceptReAddressingLink(t, payphoneEnv, s.token)
	assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
	if got := answerDetail(body, "reason"); got != reason {
		t.Errorf("accept details.reason = %q, want %q", got, reason)
	}
	if _, exists := readHolderCustomer(t, env, corrected); exists {
		t.Error("a refused click minted a Customer for the corrected address")
	}
	// THE CORRECTED ADDRESS IS TOLD NOTHING: not a void notice, not a
	// Confirmation, not a second link. They never became party to anything.
	assertNothingWentToTheWrongAddress(t, env, corrected)
}

// assertNothingPendingInTheLookup: the Operator sees no pending record and
// (unless one was accepted earlier) no history, and the Sale still reads under
// the address it was sold to.
func assertNothingPendingInTheLookup(t *testing.T, env *testEnv, s strandedSale, wrong string) {
	t.Helper()
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.ReAddressing.Pending != nil {
		t.Errorf("the lookup still shows a pending re-addressing %+v; the record expired with the Sale", *found.ReAddressing.Pending)
	}
	if len(found.ReAddressing.Accepted) != 0 {
		t.Errorf("the lookup lists %d accepted re-addressings, want none", len(found.ReAddressing.Accepted))
	}
	if found.Sale.Customer.Email != wrong {
		t.Errorf("the Sale reads under %q, want it left with %q", found.Sale.Customer.Email, wrong)
	}
}

// TestAPendingReAddressingExpiresWhenTheCustomerUndoesTheSale: the buyer at the
// wrong address — the ghost — can still undo their own purchase within the
// Reversal Window, and when they do the pending re-addressing dies with the
// Sale. Nothing is written to the record: it reads `expired` because the Sale
// is no longer active.
func TestAPendingReAddressingExpiresWhenTheCustomerUndoesTheSale(t *testing.T) {
	env := setupTest(t)
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	const wrong, corrected = "ana.lopes@example.com", "ana.lopez@example.com"
	s := strandSale(t, env, "Undone Fest", "readdress-undone-fest", wrong, corrected, 1)
	viewReAddressingLinkOK(t, payphoneEnv, s.token)

	ghost := customerSignIn(t, payphoneEnv, wrong)
	env.email.Reset()
	if result := reverseSaleOK(t, payphoneEnv, ghost, s.saleID); result.Status != "reversed" {
		t.Fatalf("undo = %+v, want the Sale reversed", result)
	}
	// The undo's own mail goes where the Sale still pointed: the wrong address,
	// which is the buyer of record until somebody accepts.
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].To != wrong {
		t.Fatalf("void notices = %+v, want exactly one to the address the Sale was sold to", voided)
	}

	assertReAddressingLinkNoLongerValid(t, env, s, corrected, "sale_reversed")
	assertNothingPendingInTheLookup(t, env, s, wrong)

	// Nothing was stamped on the record. The state is derived from the Sale, and
	// a reversal writes to the Sale alone.
	row := readReAddressingRow(t, env, s.saleID)
	if row.withdrawnAt.Valid || row.acceptedAt.Valid {
		t.Errorf("the reversal ended the record in storage (accepted_at=%v withdrawn_at=%v); its state is derived, never stored", row.acceptedAt, row.withdrawnAt)
	}
	if !row.correctedEmail.Valid || row.correctedEmail.String != corrected {
		t.Errorf("the reversal took the corrected address (%+v); that is the Event-start purge's job, not a reversal's", row.correctedEmail)
	}
}

// TestAPendingReAddressingExpiresWhenTheDrainFinishesTheCustomersUndo: the
// same ending reached the slow way. The buyer's undo went through at PayPhone
// and the local commit failed (ADR 0024's incident); the Reversal Reconciler
// finishes it a tick later. Until it does the Sale is active and the record
// is still pending; once it does, the record is expired — the drain needs no
// hook into re-addressing to make that so.
func TestAPendingReAddressingExpiresWhenTheDrainFinishesTheCustomersUndo(t *testing.T) {
	env := setupTest(t)
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	const wrong, corrected = "ana.lopes@example.com", "ana.lopez@example.com"
	s := strandSale(t, env, "Half Undone Fest", "readdress-half-undone-fest", wrong, corrected, 1)

	ghost := customerSignIn(t, payphoneEnv, wrong)
	repair := failTheLocalCommit(t, payphoneEnv, s.saleID)
	if resp, body := reverseSaleRequest(t, payphoneEnv, ghost, s.saleID); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("undo whose local commit failed: status=%d error=%+v, want 500", resp.StatusCode, body.Error)
	}
	// The money has gone back and the Sale has not yet followed it. The record
	// is still pending, because the Sale is still active — and the link still
	// opens. A click here would move a Sale that is about to be voided, which
	// the repair below then voids under whoever holds it; that is the residual
	// risk ADR 0024 already accepted for every other fact about a half-done
	// reversal, and it lasts one tick.
	if found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref); found.ReAddressing.Pending == nil {
		t.Fatal("the record is not pending while the Sale is still active; the drain has not run yet")
	}

	repair()
	readdressClocksAt(t, fixedClock.Add(time.Minute))
	env.email.Reset()
	if result := drainReversals(t, payphoneEnv); result.Reversed != 1 {
		t.Fatalf("drain = %+v, want the half-done undo completed", result)
	}

	assertReAddressingLinkNoLongerValid(t, env, s, corrected, "sale_reversed")
	assertNothingPendingInTheLookup(t, env, s, wrong)
}

// TestAPendingReAddressingExpiresAtTheDoorsAndItsAddressIsPurged is the
// Event-start half, and it walks the clock rather than moving the Event: the
// link was recorded while the doors were ahead, and it is TIME that closes it.
//
// Two stranded Sales, both for Events starting in 72 hours. One is accepted
// before the doors; the other never is. Past the start nothing can be recorded
// on either, the unaccepted link refuses with `event_started`, and the purge
// takes the unaccepted corrected address — and only that one — keeping the row
// and its operator/time facts. The accepted record is history and keeps its
// address: once proven, it is the Customer's own.
func TestAPendingReAddressingExpiresAtTheDoorsAndItsAddressIsPurged(t *testing.T) {
	env := setupTest(t)
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	const wrong, corrected = "ana.lopes@example.com", "ana.lopez@example.com"
	unaccepted := strandSale(t, env, "Doors Fest", "readdress-doors-fest", wrong, corrected, 1)
	accepted := strandAnotherSale(t, env, unaccepted, "Doors Fest Two", "readdress-doors-fest-two", "bea.torres@example.com", "bea.torrez@example.com")
	acceptAndSignIn(t, env, accepted.token)
	env.email.Reset()

	// An hour before the doors: nothing is due, and the purge says so without
	// having touched the record.
	readdressClocksAt(t, fixedClock.Add(72*time.Hour-time.Hour))
	if early := purgeHolderAddresses(t, env); early.CorrectedAddressesPurged != 0 {
		t.Fatalf("purge took %d corrected addresses an hour before the doors, want 0", early.CorrectedAddressesPurged)
	}
	viewReAddressingLinkOK(t, payphoneEnv, unaccepted.token)

	// The doors open.
	readdressClocksAt(t, fixedClock.Add(72*time.Hour+time.Hour))

	// Nothing can be recorded, even a resend of the same address.
	resp, body, _ := reAddressRequest(t, env, unaccepted.operator, unaccepted.ref, reAddressBody{Email: corrected})
	assertRefused(t, resp, body, http.StatusConflict, "RE_ADDRESSING_EVENT_STARTED")
	assertNoReAddressingMail(t, env)

	// A link recorded before the start refuses after it, before any purge has
	// run: the state is derived from the Event, not from the purge's write.
	assertReAddressingLinkNoLongerValid(t, env, unaccepted, corrected, "event_started")
	assertNothingPendingInTheLookup(t, env, unaccepted, wrong)

	result := purgeHolderAddresses(t, env)
	if result.CorrectedAddressesPurged != 1 {
		t.Fatalf("purge = %+v, want exactly 1 corrected address taken — the unaccepted one, and not the accepted one", result)
	}

	// THE ADDRESS IS GONE FROM THE DATABASE, and everything beside it stayed:
	// who recorded it, what the Sale's address was, and when. A row that says
	// "somebody tried" with no address is the record the ADR promises.
	row := readReAddressingRow(t, env, unaccepted.saleID)
	if row.correctedEmail.Valid {
		t.Errorf("the purged record still carries corrected_email=%q — this is the whole job", row.correctedEmail.String)
	}
	if row.operatorEmail != "operator@example.com" || row.previousEmail != wrong {
		t.Errorf("the purge took the operator/previous facts with the address: %+v", row)
	}
	if row.requestedAt.IsZero() || row.acceptedAt.Valid || row.withdrawnAt.Valid {
		t.Errorf("the purge rewrote the record's time facts: %+v", row)
	}

	// The accepted record is untouched at any age.
	kept := readReAddressingRow(t, env, accepted.saleID)
	if !kept.correctedEmail.Valid || kept.correctedEmail.String != "bea.torrez@example.com" || !kept.acceptedAt.Valid {
		t.Errorf("the accepted record lost something to the purge: %+v", kept)
	}
	history, _ := lookUpSaleWithReAddressing(t, env, accepted.operator, accepted.ref)
	if len(history.ReAddressing.Accepted) != 1 || history.ReAddressing.Pending != nil {
		t.Errorf("the accepted Sale's lookup reads pending=%v accepted=%d after the purge, want its one accepted record listed", history.ReAddressing.Pending, len(history.ReAddressing.Accepted))
	}

	// The link still refuses after the purge, for the same reason as before it,
	// and nothing about the purged row leaks to the reader.
	assertReAddressingLinkNoLongerValid(t, env, unaccepted, corrected, "event_started")
	assertNothingPendingInTheLookup(t, env, unaccepted, wrong)

	// IDEMPOTENT: the second run finds nothing.
	if again := purgeHolderAddresses(t, env); again.CorrectedAddressesPurged != 0 {
		t.Fatalf("second purge = %+v, want no corrected addresses taken", again)
	}
}

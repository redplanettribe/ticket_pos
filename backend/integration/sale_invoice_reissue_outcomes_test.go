package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// A reissue that does not go to plan, and a reissue crossed by a reversal
// (#484, parent #478, ADR 0061). The reissue owes a Credit Note against the
// old factura and a corrected factura that waits, unsigned, for that Credit
// Note's authorization (#483). What this file pins down is everything
// around that wait:
//
//   - The reissue's Credit Note dies (refused by the SRI, then marked
//     annulled by the operator): the corrected factura, never sent, is
//     withdrawn in the same act; the old factura is the Sale's current one
//     again, credited by nothing live; the operator may reissue it again.
//   - A Sale Reversal lands while the corrected factura waits. The reversal
//     is never refused or delayed, and ONE Credit Note ends up crediting
//     the old factura at the SRI:
//       reissue Credit Note unsent (owed)      → withdrawn with the corrected
//                                               factura; the reversal's own
//                                               Credit Note credits the old
//                                               factura and is worked at once;
//       reissue Credit Note signed, undecided  → the reversal's Credit Note is
//       (pending, needs_attention)              owed and WAITS for the reissue
//                                               Credit Note's answer: authorized
//                                               → the reversal's is withdrawn,
//                                               the factura is credited already;
//                                               annulled → the reversal's is
//                                               issued;
//       reissue Credit Note authorized         → nothing owed: credited already.
//   - A Sale Reversal after both are authorized credits the corrected
//     (current) factura alone; the superseded one is never credited twice.
//   - A second reissue on the corrected factura chains five documents, each
//     linked to its neighbours, the third factura current.
//   - Supersession clears the Recipient Warning on the superseded factura
//     at the moment the corrected factura is AUTHORIZED — the moment the
//     old one can never be current again — and the dashboard count drops
//     with it. A reissue whose Credit Note dies clears nothing.
//
// Everything is asserted through the operator API, the reversal endpoints,
// the internal Drainer endpoint, what the fake SRI received, and the
// captured mail; the sequence table is read directly where no surface says
// "no number was consumed".

// creditNoteWithdrawnRedundantCode is the platform message a Credit Note
// withdrawn because another already credits its factura carries.
const creditNoteWithdrawnRedundantCode = "CREDIT_NOTE_FACTURA_ALREADY_CREDITED"

// correctedInvoiceWithdrawnCode is the platform message a corrected Sale
// Invoice withdrawn because its Credit Note died carries.
const correctedInvoiceWithdrawnCode = "SALE_INVOICE_CREDIT_NOTE_NOT_AUTHORIZED"

// statusOf is one document's status among the Sale's, by id; "" when it
// is not there. The two documents one reissue owes share an instant, so
// their order in the list is not something to assert on.
func statusOf(docs []saleDocumentRow, id string) string {
	for _, d := range docs {
		if d.ID == id {
			return d.Status
		}
	}
	return ""
}

// theOtherCreditNote is the Sale's one Credit Note that is not `known`.
func theOtherCreditNote(t *testing.T, docs []saleDocumentRow, known string) saleDocumentRow {
	t.Helper()
	var out *saleDocumentRow
	for i := range docs {
		if docs[i].Kind == "credit_note" && docs[i].ID != known {
			if out != nil {
				t.Fatalf("more than one other Credit Note: %+v", docs)
			}
			out = &docs[i]
		}
	}
	if out == nil {
		t.Fatalf("no other Credit Note among %+v", docs)
	}
	return *out
}

// creditNotesOfSale is the Sale's Credit Notes, oldest first, through the
// operator's Sale lookup surface.
func creditNotesOfSale(t *testing.T, operatorSessionID, ref string) []saleDocumentRow {
	t.Helper()
	var out []saleDocumentRow
	for _, d := range documentsOfSale(t, operatorSessionID, ref) {
		if d.Kind == "credit_note" {
			out = append(out, d)
		}
	}
	return out
}

// reissueParkedByTheSRI reissues an authorized House factura and drains
// once with the SRI refusing the nota de crédito: the reissue's Credit Note
// is parked needs_attention, signed, and the corrected factura still waits
// unsigned. Returns the corrected factura's id and the Credit Note's.
func reissueParkedByTheSRI(t *testing.T, operatorSessionID, facturaID string) (correctedID, noteID string) {
	t.Helper()
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID = creditNoteOf(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.NeedsAttention != 1 {
		t.Fatalf("drain with the SRI refusing = %+v; want the Credit Note alone, parked", result)
	}
	if waiting := getReissuedInvoice(t, operatorSessionID, corrected.ID); waiting.Status != "owed" || waiting.Number != nil {
		t.Fatalf("corrected factura = %s %v; want still owed and unsigned while its Credit Note is parked", waiting.Status, waiting.Number)
	}
	return corrected.ID, noteID
}

// TestAnnulledReissueCreditNoteWithdrawsTheCorrectedSaleInvoiceAndReopensTheReissue:
// the SRI refuses the reissue's nota de crédito and the operator marks it
// annulled at the portal. In that same act the corrected factura — never
// sent, no number consumed — is withdrawn with a platform message saying
// why; the old factura is current again: authorized, superseded by nothing,
// credited by nothing live, its Recipient Warning still standing (nothing
// was corrected), and the dashboard still counts it. Nothing is due. A
// second reissue on the old factura is allowed, chains after the dead one,
// and settles as the first should have; the buyer's Sale is untouched.
func TestAnnulledReissueCreditNoteWithdrawsTheCorrectedSaleInvoiceAndReopensTheReissue(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID := houseSaleOwed(t, env, 1000, 1)
	ref := lastConfirmation(t, env).Reference
	issuerReady(t, operatorSessionID)
	authorizeWithAdvertencia("59", "IDENTIFICACION NO EXISTE")
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("setup drain = %+v; want the factura authorized", result)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("warning count before the reissue = %d; want 1", n)
	}
	correctedID, noteID := reissueParkedByTheSRI(t, operatorSessionID, facturaID)

	// While the Credit Note is parked the reissue is in flight: nothing
	// else may be started on the Sale.
	resp, body := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
	expectRefusal(t, "reissue while the first is parked", resp, body, http.StatusConflict, "REISSUE_IN_FLIGHT")

	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if view := annulOK(t, operatorSessionID, noteID); view.Status != "annulled" {
		t.Fatalf("annul = %s", view.Status)
	}

	corrected := getDrainedInvoice(t, operatorSessionID, correctedID)
	if corrected.Status != "withdrawn" || corrected.Number != nil || corrected.EcuadorFull != nil || corrected.NextAttemptAt != nil || len(corrected.AttemptRows) != 0 {
		t.Fatalf("corrected factura after the annulment = %s number %v ecuador %v next %v attempts %d; want withdrawn, unsigned, off the queue, nothing asked of the SRI",
			corrected.Status, corrected.Number, corrected.EcuadorFull, corrected.NextAttemptAt, len(corrected.AttemptRows))
	}
	if len(corrected.Messages) != 1 || corrected.Messages[0].Type != "PLATFORM" || corrected.Messages[0].Identifier != correctedInvoiceWithdrawnCode {
		t.Fatalf("corrected factura messages = %+v; want one PLATFORM message saying why", corrected.Messages)
	}
	old := getRecipientWarningDetail(t, sriEnv, operatorSessionID, facturaID)
	if old.Status != "authorized" || old.CreditedByInvoiceID != nil {
		t.Fatalf("old factura = %s credited by %v; want authorized and credited by nothing live — a dead Credit Note credits nothing", old.Status, old.CreditedByInvoiceID)
	}
	if superseded := getReissuedInvoice(t, operatorSessionID, facturaID); superseded.SupersededByInvoiceID != nil {
		t.Fatalf("old factura superseded by %v; want nothing: the corrected factura was withdrawn", *superseded.SupersededByInvoiceID)
	}
	if !old.RecipientWarning {
		t.Fatal("the Recipient Warning was cleared by a reissue that died; the wrong Tax ID still stands")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("warning count after the dead reissue = %d; want still 1", n)
	}
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("drain after the annulment = %+v; want nothing due", result)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the factura and the refused nota de crédito alone", n)
	}
	var last int64
	if err := env.db.QueryRow(`SELECT last_secuencial FROM invoicing_sequences_ec WHERE cod_doc = '01'`).Scan(&last); err != nil || last != 1 {
		t.Fatalf("01 sequence = %d (%v); want still at 1: the withdrawn corrected factura consumed no number", last, err)
	}

	// The operator reissues again; the old factura is the one to correct.
	resp, body = reissueInvoice(t, operatorSessionID, correctedID, companyRecipient())
	expectRefusal(t, "reissue of the withdrawn corrected factura", resp, body, http.StatusConflict, "INVOICE_NOT_AUTHORIZED")
	sriStub.answerAsUsual()
	env.email.Reset()
	second := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	if second.SupersedesInvoiceID == nil || *second.SupersedesInvoiceID != facturaID {
		t.Fatalf("second reissue supersedes %v; want the old factura", second.SupersedesInvoiceID)
	}
	docs := documentsOfSale(t, operatorSessionID, ref)
	if len(docs) != 5 || statusOf(docs, noteID) != "annulled" || statusOf(docs, correctedID) != "withdrawn" || statusOf(docs, second.ID) != "owed" || theOtherCreditNote(t, docs, noteID).Status != "owed" {
		t.Fatalf("the Sale's documents = %+v; want the factura, the annulled Credit Note, the withdrawn corrected factura, a fresh Credit Note owed, the fresh corrected factura", docs)
	}
	result := drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("drain of the second reissue = %+v; want the Credit Note and then the corrected factura authorized and delivered", result)
	}
	received := sriStub.allReceived()
	if len(received) != 4 || !strings.Contains(string(received[2].signedXML), "<notaCredito") || !strings.Contains(string(received[3].signedXML), "<factura") {
		t.Fatalf("the SRI received %d documents; want the second nota de crédito before the second corrected factura", len(received))
	}
	after := getReissuedInvoice(t, operatorSessionID, facturaID)
	if after.SupersededByInvoiceID == nil || *after.SupersededByInvoiceID != second.ID {
		t.Fatalf("old factura superseded by %v; want the second corrected factura", after.SupersededByInvoiceID)
	}
	docs = documentsOfSale(t, operatorSessionID, ref)
	secondNote := theOtherCreditNote(t, docs, noteID)
	if statusOf(docs, noteID) != "annulled" || secondNote.Status != "authorized" {
		t.Fatalf("credit notes = %+v; want the dead one and the authorized one", creditNotesOfSale(t, operatorSessionID, ref))
	}
	if credited := getDrainedInvoice(t, operatorSessionID, facturaID); credited.CreditedByInvoiceID == nil || *credited.CreditedByInvoiceID != secondNote.ID {
		t.Fatalf("old factura credited by %v; want the second, authorized Credit Note", credited.CreditedByInvoiceID)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("warning count once the second reissue authorized = %d; want 0", n)
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Fatalf("sale status = %s; want active", status)
	}
}

// TestDrainerWithdrawsACorrectedSaleInvoiceWhoseCreditNoteDied: the
// Drainer's own safety net for the same fact. Mark annulled withdraws the
// corrected factura in its own act, so an owed corrected factura whose
// Credit Note is dead is a row nothing produces through the API any more;
// put back by hand, the next drain claims it and withdraws it unsigned
// rather than issuing a factura the SRI never saw cancelled.
func TestDrainerWithdrawsACorrectedSaleInvoiceWhoseCreditNoteDied(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	correctedID, noteID := reissueParkedByTheSRI(t, operatorSessionID, facturaID)
	annulOK(t, operatorSessionID, noteID)
	// SQL: the annulment withdrew the corrected factura in the same act; no
	// API leaves a corrected factura owed behind a dead Credit Note, and the
	// Drainer's claim is what this test is about.
	if _, err := env.db.Exec(`UPDATE invoicing_invoices SET status = 'owed', next_attempt_at = $2, last_messages = '[]' WHERE id = $1`, correctedID, fixedClock); err != nil {
		t.Fatalf("put the corrected factura back on the queue: %v", err)
	}

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Withdrawn != 1 || result.Authorized != 0 {
		t.Fatalf("drain = %+v; want the corrected factura claimed and withdrawn", result)
	}
	corrected := getDrainedInvoice(t, operatorSessionID, correctedID)
	if corrected.Status != "withdrawn" || corrected.Number != nil || corrected.NextAttemptAt != nil || len(corrected.AttemptRows) != 0 {
		t.Fatalf("corrected factura = %s number %v next %v attempts %d; want withdrawn, unsigned, off the queue", corrected.Status, corrected.Number, corrected.NextAttemptAt, len(corrected.AttemptRows))
	}
	if len(corrected.Messages) != 1 || corrected.Messages[0].Identifier != correctedInvoiceWithdrawnCode {
		t.Fatalf("messages = %+v; want the platform's reason", corrected.Messages)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want no corrected factura", n)
	}
}

// TestReversalDuringAReissueWithdrawsTheUnsentCreditNoteAndCreditsTheOldSaleInvoice:
// the buyer reverses before the Drainer has touched the reissue. Both of
// the reissue's documents were never sent, and both are withdrawn: the SRI
// hears nothing of a correction to a sale that no longer stands. The
// reversal's own Credit Note credits the old factura — the Sale's current
// one — with the reversal's reason, and is worked at once.
func TestReversalDuringAReissueWithdrawsTheUnsentCreditNoteAndCreditsTheOldSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	env.email.Reset()
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	reissueNoteID := creditNoteOf(t, operatorSessionID)

	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)

	docs := documentsOfSale(t, operatorSessionID, ref)
	reversalNoteID := theOtherCreditNote(t, docs, reissueNoteID).ID
	if len(docs) != 4 || statusOf(docs, reissueNoteID) != "withdrawn" || statusOf(docs, corrected.ID) != "withdrawn" || statusOf(docs, reversalNoteID) != "owed" {
		t.Fatalf("the Sale's documents = %+v; want the factura, the reissue's Credit Note withdrawn, the corrected factura withdrawn, the reversal's Credit Note owed", docs)
	}
	reversalNote := getDrainedInvoice(t, operatorSessionID, reversalNoteID)
	if reversalNote.CreditsInvoiceID == nil || *reversalNote.CreditsInvoiceID != facturaID || reversalNote.CreditNoteReason == nil || *reversalNote.CreditNoteReason != "customer" {
		t.Fatalf("reversal credit note credits %v for %v; want the old factura, the customer's route", reversalNote.CreditsInvoiceID, reversalNote.CreditNoteReason)
	}
	if old := getReissuedInvoice(t, operatorSessionID, facturaID); old.SupersededByInvoiceID != nil {
		t.Fatalf("old factura superseded by %v; want nothing", *old.SupersededByInvoiceID)
	}

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain = %+v; want the reversal's Credit Note alone, authorized and delivered", result)
	}
	received := sriStub.allReceived()
	if len(received) != 2 || !strings.Contains(string(received[1].signedXML), "<motivo>"+reversalRoutes[0].motivo+"</motivo>") {
		t.Fatalf("the SRI received %d documents; want the factura and one nota de crédito for the reversal", len(received))
	}
	if old := getDrainedInvoice(t, operatorSessionID, facturaID); old.CreditedByInvoiceID == nil || *old.CreditedByInvoiceID != reversalNoteID {
		t.Fatalf("old factura credited by %v; want the reversal's Credit Note", old.CreditedByInvoiceID)
	}
	if sent := deliveriesSent(t, env); len(sent) != 1 || sent[0].Kind != "credit_note" || sent[0].Reason != "customer" {
		t.Fatalf("deliveries = %+v; want the reversal's Credit Note alone", sent)
	}
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing due", again)
	}
}

// TestReversalDuringAReissueWaitsForThePendingReissueCreditNoteThenCreditsOnce:
// the SRI holds the reissue's nota de crédito EN PROCESAMIENTO when the
// buyer reverses. The reversal succeeds at once: the corrected factura,
// unsent, is withdrawn; the reversal's Credit Note is owed against the old
// factura and waits — the pending nota is polled alone. When it authorizes,
// the old factura is credited, and the reversal's Credit Note is withdrawn
// rather than crediting it twice.
func TestReversalDuringAReissueWaitsForThePendingReissueCreditNoteThenCreditsOnce(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	reissueNoteID := creditNoteOf(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("first drain = %+v; want the Credit Note alone, pending", result)
	}

	atInvoicingClock(t, fixedClock.Add(30*time.Second))
	reversalRoutes[1].reverse(t, env, operatorSessionID, ref)

	docs := documentsOfSale(t, operatorSessionID, ref)
	reversalNoteID := theOtherCreditNote(t, docs, reissueNoteID).ID
	if len(docs) != 4 || statusOf(docs, reissueNoteID) != "pending" || statusOf(docs, corrected.ID) != "withdrawn" || statusOf(docs, reversalNoteID) != "owed" {
		t.Fatalf("the Sale's documents = %+v; want the factura, the reissue's Credit Note still pending, the corrected factura withdrawn, the reversal's Credit Note owed", docs)
	}
	if note := getDrainedInvoice(t, operatorSessionID, reversalNoteID); note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID || note.CreditNoteReason == nil || *note.CreditNoteReason != "platform" {
		t.Fatalf("reversal credit note credits %v for %v; want the old factura, the platform's route", note.CreditsInvoiceID, note.CreditNoteReason)
	}

	// Still in processing: only the reissue's nota is polled; the reversal's waits.
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("drain with the reissue's Credit Note pending = %+v; want it polled alone, the reversal's Credit Note waiting", result)
	}
	if note := getDrainedInvoice(t, operatorSessionID, reversalNoteID); note.Status != "owed" || note.Number != nil {
		t.Fatalf("reversal credit note = %s %v; want still owed and unsigned", note.Status, note.Number)
	}

	// The reissue's nota authorizes: the factura is credited, once.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(6*time.Minute))
	result := drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 1 || result.Withdrawn != 1 {
		t.Fatalf("drain with a late AUTORIZADO = %+v; want the reissue's Credit Note authorized and the reversal's withdrawn", result)
	}
	if note := getDrainedInvoice(t, operatorSessionID, reissueNoteID); note.Status != "authorized" {
		t.Fatalf("reissue credit note = %s; want authorized", note.Status)
	}
	withdrawn := getDrainedInvoice(t, operatorSessionID, reversalNoteID)
	if withdrawn.Status != "withdrawn" || withdrawn.Number != nil || withdrawn.NextAttemptAt != nil || len(withdrawn.AttemptRows) != 0 {
		t.Fatalf("reversal credit note = %s number %v next %v attempts %d; want withdrawn, unsigned, off the queue", withdrawn.Status, withdrawn.Number, withdrawn.NextAttemptAt, len(withdrawn.AttemptRows))
	}
	if len(withdrawn.Messages) != 1 || withdrawn.Messages[0].Type != "PLATFORM" || withdrawn.Messages[0].Identifier != creditNoteWithdrawnRedundantCode {
		t.Fatalf("reversal credit note messages = %+v; want one PLATFORM message: the factura is credited already", withdrawn.Messages)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the factura and ONE nota de crédito", n)
	}
	if old := getDrainedInvoice(t, operatorSessionID, facturaID); old.CreditedByInvoiceID == nil || *old.CreditedByInvoiceID != reissueNoteID {
		t.Fatalf("old factura credited by %v; want the reissue's authorized Credit Note", old.CreditedByInvoiceID)
	}
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain after everything settled = %+v; want nothing due", again)
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "reversed" {
		t.Fatalf("sale status = %s; want reversed", status)
	}

	// The reissue's nota is the only Credit Note the buyer will ever receive
	// for this Sale, and by now the Sale is reversed: no corrected factura
	// follows. The mail says the purchase was reversed, not that a corrected
	// factura is on its way.
	sent := deliveriesSent(t, env)
	if len(sent) != 2 || sent[1].Kind != "credit_note" {
		t.Fatalf("deliveries = %d (%v); want the factura's and then the Credit Note's", len(sent), sent)
	}
	if text := sent[1].Text(); strings.Contains(text, "corrected tax invoice") || !strings.Contains(text, "reversed purchase") {
		t.Fatalf("credit note mail after the reversal reads:\n%s\nwant the reversed-purchase wording, never a corrected factura to follow", text)
	}
}

// TestReversalDuringAReissueCreditsTheOldSaleInvoiceWhenTheReissueCreditNoteDies:
// the SRI refused the reissue's nota de crédito when the buyer reverses.
// The reversal's Credit Note waits while the refused one is parked (the
// operator may still Resend it); once the operator marks it annulled, the
// reversal's Credit Note is issued and credits the old factura — the Sale's
// income is undone exactly once.
func TestReversalDuringAReissueCreditsTheOldSaleInvoiceWhenTheReissueCreditNoteDies(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	correctedID, reissueNoteID := reissueParkedByTheSRI(t, operatorSessionID, facturaID)

	atInvoicingClock(t, fixedClock.Add(time.Hour))
	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)

	docs := documentsOfSale(t, operatorSessionID, ref)
	reversalNoteID := theOtherCreditNote(t, docs, reissueNoteID).ID
	if len(docs) != 4 || statusOf(docs, reissueNoteID) != "needs_attention" || statusOf(docs, correctedID) != "withdrawn" || statusOf(docs, reversalNoteID) != "owed" {
		t.Fatalf("the Sale's documents = %+v; want the factura, the reissue's Credit Note parked, the corrected factura withdrawn, the reversal's Credit Note owed", docs)
	}
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("drain with the reissue's Credit Note parked = %+v; want nothing claimed: the reversal's Credit Note waits for its answer", result)
	}

	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	annulOK(t, operatorSessionID, reissueNoteID)
	sriStub.answerAsUsual()
	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain after the annulment = %+v; want the reversal's Credit Note authorized and delivered", result)
	}
	received := sriStub.allReceived()
	if len(received) != 3 || !strings.Contains(string(received[2].signedXML), "<motivo>"+reversalRoutes[0].motivo+"</motivo>") {
		t.Fatalf("the SRI received %d documents; want the reversal's nota de crédito third", len(received))
	}
	if note := getDrainedInvoice(t, operatorSessionID, reversalNoteID); note.Status != "authorized" || note.Number == nil || *note.Number != "001-001-000000002" {
		t.Fatalf("reversal credit note = %s %v; want authorized, the 04 sequence's second number", note.Status, note.Number)
	}
	if old := getDrainedInvoice(t, operatorSessionID, facturaID); old.CreditedByInvoiceID == nil || *old.CreditedByInvoiceID != reversalNoteID {
		t.Fatalf("old factura credited by %v; want the reversal's Credit Note, never the annulled one", old.CreditedByInvoiceID)
	}
}

// TestReversalAfterAReissueCreditsTheCorrectedSaleInvoiceOnly: both
// documents of the reissue are authorized when the buyer reverses. The
// reversal's Credit Note names the corrected factura — the current one —
// and the superseded factura, credited already by the reissue's Credit
// Note, is left exactly as it was.
func TestReversalAfterAReissueCreditsTheCorrectedSaleInvoiceOnly(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	reissueNoteID := creditNoteOf(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("setup drain = %+v; want both authorized", result)
	}
	env.email.Reset()

	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)

	docs := documentsOfSale(t, operatorSessionID, ref)
	reversalNoteID := theOtherCreditNote(t, docs, reissueNoteID).ID
	if len(docs) != 4 || statusOf(docs, reversalNoteID) != "owed" {
		t.Fatalf("the Sale's documents = %+v; want one new Credit Note owed", docs)
	}
	reversalNote := getReissuedInvoice(t, operatorSessionID, reversalNoteID)
	if reversalNote.CreditsInvoiceID == nil || *reversalNote.CreditsInvoiceID != corrected.ID || reversalNote.Recipient.TaxID != companyRUC {
		t.Fatalf("reversal credit note credits %v naming %s; want the corrected factura and its company Recipient", reversalNote.CreditsInvoiceID, reversalNote.Recipient.TaxID)
	}
	if old := getDrainedInvoice(t, operatorSessionID, facturaID); old.CreditedByInvoiceID == nil || *old.CreditedByInvoiceID != reissueNoteID {
		t.Fatalf("superseded factura credited by %v; want still the reissue's Credit Note alone", old.CreditedByInvoiceID)
	}

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain = %+v; want the reversal's Credit Note alone", result)
	}
	received := sriStub.allReceived()
	if len(received) != 4 || !strings.Contains(string(received[3].signedXML), "<numDocModificado>001-001-000000002</numDocModificado>") ||
		!strings.Contains(string(received[3].signedXML), "<identificacionComprador>"+companyRUC+"</identificacionComprador>") {
		t.Fatalf("the SRI received %d documents; want the reversal's nota de crédito naming the corrected factura's number and Recipient", len(received))
	}
	if sent := deliveriesSent(t, env); len(sent) != 1 || sent[0].Kind != "credit_note" || sent[0].Reason != "customer" {
		t.Fatalf("deliveries = %+v; want the reversal's Credit Note alone", sent)
	}
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing due", again)
	}
}

// TestSecondReissueChainsFiveDocumentsWithTheThirdSaleInvoiceCurrent: a
// reissue of the corrected factura works exactly as the first, and the
// chain reads both ways on every document: factura 1 superseded by 3,
// credited by note 2; factura 3 supersedes 1, superseded by 5, credited by
// note 4; factura 5 supersedes 3, current — superseded by nothing, credited
// by nothing, the only one reissuable.
func TestSecondReissueChainsFiveDocumentsWithTheThirdSaleInvoiceCurrent(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, firstID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	second := reissueOK(t, operatorSessionID, firstID, companyRecipient())
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("first reissue's drain = %+v; want both authorized", result)
	}
	third := reissueOK(t, operatorSessionID, second.ID, reissueBody{Recipient: reissueRecipientBody{
		TaxIDType: "cedula", TaxID: otherCedula, LegalName: "Beatriz Mora",
	}})
	result := drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("second reissue's drain = %+v; want its Credit Note and the third factura authorized and delivered", result)
	}

	docs := documentsOfSale(t, operatorSessionID, ref)
	if len(docs) != 5 {
		t.Fatalf("the Sale's documents = %+v; want five", docs)
	}
	for _, d := range docs {
		if d.Status != "authorized" {
			t.Fatalf("document %+v; want every one authorized", d)
		}
	}
	notes := creditNotesOfSale(t, operatorSessionID, ref)
	if len(notes) != 2 || statusOf(docs, firstID) == "" || statusOf(docs, second.ID) == "" || statusOf(docs, third.ID) == "" {
		t.Fatalf("the Sale's documents = %+v; want three facturas and two Credit Notes", docs)
	}
	// note2 credits the first factura, note4 the second, whichever the
	// list has first.
	note2, note4 := notes[0].ID, notes[1].ID
	if n := getReissuedInvoice(t, operatorSessionID, note2); n.CreditsInvoiceID != nil && *n.CreditsInvoiceID == second.ID {
		note2, note4 = note4, note2
	}

	first := getReissuedInvoice(t, operatorSessionID, firstID)
	if first.SupersedesInvoiceID != nil || first.SupersededByInvoiceID == nil || *first.SupersededByInvoiceID != second.ID ||
		first.CreditedByInvoiceID == nil || *first.CreditedByInvoiceID != note2 {
		t.Fatalf("first factura: supersedes %v superseded by %v credited by %v; want nothing, the second, note 2", first.SupersedesInvoiceID, first.SupersededByInvoiceID, first.CreditedByInvoiceID)
	}
	mid := getReissuedInvoice(t, operatorSessionID, second.ID)
	if mid.SupersedesInvoiceID == nil || *mid.SupersedesInvoiceID != firstID || mid.SupersededByInvoiceID == nil || *mid.SupersededByInvoiceID != third.ID ||
		mid.CreditedByInvoiceID == nil || *mid.CreditedByInvoiceID != note4 {
		t.Fatalf("second factura: supersedes %v superseded by %v credited by %v; want the first, the third, note 4", mid.SupersedesInvoiceID, mid.SupersededByInvoiceID, mid.CreditedByInvoiceID)
	}
	current := getReissuedInvoice(t, operatorSessionID, third.ID)
	if current.SupersedesInvoiceID == nil || *current.SupersedesInvoiceID != second.ID || current.SupersededByInvoiceID != nil || current.CreditedByInvoiceID != nil ||
		current.Number == nil || *current.Number != "001-001-000000003" || current.Recipient.TaxID != otherCedula {
		t.Fatalf("third factura: supersedes %v superseded by %v credited by %v number %v; want the second, nothing, nothing, the 01 sequence's third", current.SupersedesInvoiceID, current.SupersededByInvoiceID, current.CreditedByInvoiceID, current.Number)
	}
	for _, pair := range []struct{ note, credits string }{{note2, firstID}, {note4, second.ID}} {
		if note := getReissuedInvoice(t, operatorSessionID, pair.note); note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != pair.credits || note.CreditNoteReason == nil || *note.CreditNoteReason != "reissue" {
			t.Fatalf("credit note %s credits %v for %v; want %s, reissue", pair.note, note.CreditsInvoiceID, note.CreditNoteReason, pair.credits)
		}
	}
	received := sriStub.allReceived()
	if len(received) != 5 || !strings.Contains(string(received[3].signedXML), "<numDocModificado>001-001-000000002</numDocModificado>") {
		t.Fatalf("the SRI received %d documents; want five, the second nota de crédito naming the second factura", len(received))
	}

	// Only the current factura is reissuable.
	resp, body := reissueInvoice(t, operatorSessionID, firstID, companyRecipient())
	expectRefusal(t, "reissue of the first factura", resp, body, http.StatusConflict, "INVOICE_SUPERSEDED")
	resp, body = reissueInvoice(t, operatorSessionID, second.ID, companyRecipient())
	expectRefusal(t, "reissue of the second factura", resp, body, http.StatusConflict, "INVOICE_SUPERSEDED")
	fourth := reissueOK(t, operatorSessionID, third.ID, companyRecipient())
	if fourth.SupersedesInvoiceID == nil || *fourth.SupersedesInvoiceID != third.ID {
		t.Fatalf("third reissue supersedes %v; want the third factura", fourth.SupersedesInvoiceID)
	}
}

// TestSupersessionClearsTheRecipientWarningWhenTheCorrectedSaleInvoiceIsAuthorized:
// the warned factura is reissued. The warning stands — and the dashboard
// counts it — through the reissue and while the corrected factura waits:
// the old one is not truly superseded until the SRI has authorized its
// replacement, and a reissue that dies leaves it current. The moment the
// corrected factura is authorized the warning is cleared and the count
// drops; the corrected factura carries none of its own.
func TestCreditingClearsTheRecipientWarningWhenTheReissueCreditNoteIsAuthorized(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	authorizeWithAdvertencia("62", "IDENTIFICACION INCORRECTA")
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("setup drain = %+v; want the factura authorized", result)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("warning count = %d; want 1", n)
	}

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, facturaID); !d.RecipientWarning {
		t.Fatal("the reissue itself cleared the warning; the corrected factura is not authorized yet")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("warning count right after the reissue = %d; want still 1", n)
	}

	// The Credit Note authorizes and the corrected factura is held EN
	// PROCESAMIENTO: the old factura is credited — it declares nothing to
	// the SRI any more — so its warning goes now, not when the corrected
	// factura authorizes, which an annulled one never does.
	sriStub.answerAsUsual()
	hold := 0
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		hold++
		if hold == 1 {
			return http.StatusOK, authorizedSOAP(accessKey)
		}
		return http.StatusOK, inProcessingSOAP(accessKey)
	})
	if result := drainSaleInvoices(t); result.Claimed != 2 || result.Authorized != 1 || result.Pending != 1 {
		t.Fatalf("drain = %+v; want the Credit Note authorized and the corrected factura pending", result)
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, facturaID); d.RecipientWarning {
		t.Fatal("the Credit Note's authorization left the warning on a factura that is credited")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("warning count once the factura is credited = %d; want 0", n)
	}

	// The corrected factura authorizes: superseded, and the warning stays gone.
	sriStub.answerAsUsual()
	atInvoicingClock(t, fixedClock.Add(2*time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 {
		t.Fatalf("drain with the corrected factura authorized = %+v", result)
	}
	old := getRecipientWarningDetail(t, sriEnv, operatorSessionID, facturaID)
	if old.RecipientWarning || old.Status != "authorized" {
		t.Fatalf("superseded factura = %s warning %v; want authorized with the warning cleared", old.Status, old.RecipientWarning)
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, corrected.ID); d.RecipientWarning {
		t.Fatal("the corrected factura carries a warning the SRI never raised on it")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("warning count once superseded = %d; want 0", n)
	}
	if filtered := listInvoicesWith(t, sriEnv, operatorSessionID, "?recipient_warning=true"); filtered.Pagination.Total != 0 {
		t.Fatalf("filtered list total = %d; want 0", filtered.Pagination.Total)
	}
}

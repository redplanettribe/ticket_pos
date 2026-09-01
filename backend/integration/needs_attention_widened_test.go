package integration

import (
	"net/http"
	"testing"
	"time"
)

// The needs-attention queue widens (#581, parent #575, ADR 0068): it lists
// documents parked `needs_attention` OR Sale Invoices that are `abandoned`
// with no live successor on a Ticket Sale that still stands, and its count
// — the Operator Dashboard's daily badge — widens with it.
//
// WHY THE UNION EXISTS. Abandon (#578) and Issue again (#580) are two
// presses, deliberately, so a Sale can sit abandoned-and-unreplaced between
// them: its buyer holds no valid factura and nothing on any surface says
// so, because `abandoned` is not `needs_attention`. Rather than a second
// badge meaning "unfinished invoicing work" — which is how an operator
// learns to ignore one — the queue that already means that widens to say
// what its name promises.
//
// What these prove at the HTTP seam, and nowhere else, because the queue's
// contract is a claim about what the operator SEES and not about a WHERE
// clause (#581: proved through observable behaviour, not by asserting the
// query):
//
//   - the abandoned Sale appears in the queue and in the count, and leaves
//     both the instant Issue again owes a replacement — the self-clearing
//     that makes the union safe;
//   - it reappears if that replacement dies in its turn, because the rule
//     is "no LIVE successor" and a dead one stands for nothing (#579);
//   - an abandoned document whose Sale was reversed is not in it: nothing
//     is owed for income that no longer stands, so nothing needs an
//     operator;
//   - a document no Issue again can ever reach — a Credit Note — is not in
//     it, because a queue entry that can never be cleared is noise;
//   - THE TWO HALVES SHARE ONE CLOCK: a row's place is the instant it
//     entered the queue by whichever door, so the composed order interleaves
//     rather than putting one status ahead of the other.

// TestAbandonedSaleWaitsInTheQueueUntilIssuedAgain is the ticket's
// load-bearing test: the state production's 001-001-000000025 is in, seen
// from the Operator Dashboard, and the press that clears it.
func TestAbandonedSaleWaitsInTheQueueUntilIssuedAgain(t *testing.T) {
	_, operatorSessionID, abandonedID := abandonedSaleInvoice(t)

	// The document is terminally dead, off the Drainer's claim set and out
	// of `needs_attention` — and the Sale it was owed for still stands with
	// no factura. That is precisely the state the queue must now show.
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count after the abandonment = %d; want 1 — the Sale has no factura and nothing yet owes it one", n)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if queue.Pagination.Total != 1 || len(queue.Data) != 1 {
		t.Fatalf("queue after the abandonment = %+v; want the abandoned document alone", queue)
	}
	row := queue.Data[0]
	if row.ID != abandonedID || row.Status != "abandoned" || row.Kind != "sale" {
		t.Fatalf("row = %+v; want the abandoned Sale Invoice %s", row, abandonedID)
	}
	// It is the ordinary queue row, with the same facts as any other: the
	// number it consumed, its Sale, and what the authority last said. No new
	// envelope, no new copy — the row the operator already reads.
	if row.Number == nil || row.SaleConfirmationRef == nil {
		t.Fatalf("row = %+v; want its consumed number and its Sale's reference", row)
	}
	if len(row.Messages) != 1 || row.Messages[0].Identifier != "45" {
		t.Fatalf("messages = %+v; want the authority's 45 verbatim, as on any queued document", row.Messages)
	}

	// THE PRESS. The moment Issue again owes a replacement, the abandoned
	// document has a live successor and drops out of both. This is the
	// self-clearing the union rests on: no second act, no operator's
	// dismissal, nothing to remember.
	replacement := issueAgainOK(t, operatorSessionID, abandonedID, nil)
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after Issue again = %d; want 0 — the replacement is owed and the work is done", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); queue.Pagination.Total != 0 || len(queue.Data) != 0 {
		t.Fatalf("queue after Issue again = %+v; want empty", queue.Data)
	}

	// And the replacement itself never enters the queue by being owed: it is
	// on its way, not stuck. A Drainer round signs it and the queue is still
	// empty afterwards.
	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want the replacement authorized", result)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after the replacement authorized = %d; want 0", n)
	}
	if got := getReissuedInvoice(t, operatorSessionID, replacement.ID); got.Status != "authorized" {
		t.Fatalf("the replacement = %s; want authorized", got.Status)
	}
}

// TestAnAbandonedSaleReturnsToTheQueueWhenItsReplacementDies: the rule is
// "no LIVE successor" and it is #579's live, read from the one place that
// computes it (superseded_by_invoice_id). A replacement that is itself
// abandoned supersedes nothing, so its predecessor's Sale is unreplaced
// again and both dead documents are back in front of the operator — who
// still has one press that clears them both, from either end of the chain.
//
// The queue is a queue of DOCUMENTS, not of Sales, so one Sale contributing
// two rows here is the honest reading and not a miscount.
func TestAnAbandonedSaleReturnsToTheQueueWhenItsReplacementDies(t *testing.T) {
	_, operatorSessionID, firstID := abandonedSaleInvoice(t)
	second := issueAgainOK(t, operatorSessionID, firstID, nil)
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after Issue again = %d; want 0", n)
	}

	// The authority refuses the replacement's number too — the hazard ADR
	// 0068 names while #573's pacing fix is undeployed — so it is parked,
	// checked and abandoned in its turn.
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain of the replacement = %+v; want it parked on the same refusal", result)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count while the replacement is parked = %d; want 1 — the parked replacement, and the first document replaced by a live one", n)
	}
	if resp, body := checkInvoice(t, operatorSessionID, second.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("check the replacement: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := abandonInvoice(t, operatorSessionID, second.ID, map[string]any{"note": "refused by number again"})
	if view := abandonedView(t, resp, body); view.Status != "abandoned" {
		t.Fatalf("the replacement is %s; want abandoned", view.Status)
	}

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 2 {
		t.Fatalf("count with both documents dead = %d; want 2 — a dead successor supersedes nothing (#579)", n)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if len(queue.Data) != 2 {
		t.Fatalf("queue = %+v; want both abandoned documents", queue.Data)
	}
	seen := map[string]bool{}
	for _, row := range queue.Data {
		if row.Status != "abandoned" {
			t.Fatalf("row = %+v; want both rows abandoned", row)
		}
		seen[row.ID] = true
	}
	if !seen[firstID] || !seen[second.ID] {
		t.Fatalf("queue = %+v; want the original %s and its dead replacement %s", queue.Data, firstID, second.ID)
	}

	// One press from the end of the chain clears both: the third document
	// supersedes the second, and the first is no longer unreplaced either,
	// because the chain it starts ends in something live.
	issueAgainOK(t, operatorSessionID, second.ID, nil)
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count after the second Issue again = %d; want 1 — the first document is still unreplaced in its own right", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 1 || queue.Data[0].ID != firstID {
		t.Fatalf("queue = %+v; want the first document alone", queue.Data)
	}
}

// TestAnAbandonedDocumentOnAReversedSaleIsNotInTheQueue: income that no
// longer stands is never declared at all, so nothing is owed and nothing
// needs an operator. This is the same fact Issue again refuses on
// (INVOICE_SALE_REVERSED), read the same way — a queue that listed a Sale
// the platform will not let anyone act on would be an entry nobody could
// ever clear.
func TestAnAbandonedDocumentOnAReversedSaleIsNotInTheQueue(t *testing.T) {
	env, operatorSessionID, abandonedID := abandonedSaleInvoice(t)
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count before the reversal = %d; want 1", n)
	}

	reversalRoutes[0].reverse(t, env, operatorSessionID, lastConfirmation(t, env).Reference)

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after the Sale was reversed = %d; want 0 — a Sale that no longer stands is owed nothing", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 0 {
		t.Fatalf("queue after the reversal = %+v; want empty", queue.Data)
	}
	// The document itself is untouched: it is out of the queue, not out of
	// the record.
	if got := getAbandonedDetail(t, operatorSessionID, abandonedID); got.Status != "abandoned" || got.Number == nil {
		t.Fatalf("the abandoned document = %s %v; want it standing with its consumed number", got.Status, got.Number)
	}
	resp, body := issueAgain(t, operatorSessionID, abandonedID, nil)
	expectRefusal(t, "issue again on the reversed Sale", resp, body, http.StatusConflict, "INVOICE_SALE_REVERSED")
}

// TestAnAbandonedCreditNoteIsNotInTheQueue: the union's second half is
// SALE-KIND, and deliberately. A Credit Note is never issued again (#580
// refuses it by its own code) and neither is a manual Tax Invoice — it is
// typed again by hand — so a queue entry for either could never be cleared
// by any press the platform offers. A permanent row is not a queue; it is
// the noise that teaches an operator to stop looking.
func TestAnAbandonedCreditNoteIsNotInTheQueue(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	// The reissue's Credit Note — codDoc 04 in the ninth and tenth digits of
	// the clave — is the one the authority refuses by number.
	sriStub.setReception(func(accessKey string) (int, string) {
		if len(accessKey) > 10 && accessKey[8:10] == "04" {
			return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
		}
		return http.StatusOK, receivedSOAP(accessKey)
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the Credit Note parked", result)
	}
	noteID := deref(getReissuedInvoice(t, operatorSessionID, facturaID).CreditedByInvoiceID)
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count while the Credit Note is parked = %d; want 1", n)
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	if resp, body := checkInvoice(t, operatorSessionID, noteID); resp.StatusCode != http.StatusOK {
		t.Fatalf("check the credit note: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := abandonInvoice(t, operatorSessionID, noteID, map[string]any{"note": "the SRI refuses the credit note's number"})
	if view := abandonedView(t, resp, body); view.Status != "abandoned" {
		t.Fatalf("the credit note is %s; want abandoned", view.Status)
	}

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after the Credit Note was abandoned = %d; want 0 — no press could ever clear such a row", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 0 {
		t.Fatalf("queue = %+v; want empty", queue.Data)
	}
}

// TestTheQueuesTwoHalvesShareOneClock is the composition of the two
// orderings, and the reason the queue needed a second sort key at all:
// `attention_since` is NULL on an abandoned row — the abandonment clears
// it, in the same write that takes the document off the Drainer — and
// `abandoned_at` is the instant it entered the queue by the other door.
//
// The queue orders on COALESCE(attention_since, abandoned_at): ONE clock,
// "since when has this needed me", so the two halves INTERLEAVE. A document
// parked at noon and abandoned at three is behind one parked at one — its
// wait as a parked document ENDED when the operator acted on it, and what
// it is waiting for now (a replacement) has been outstanding since three.
// The alternative, sorting all of one status ahead of the other, would mean
// the badge's oldest entry was not the oldest work.
func TestTheQueuesTwoHalvesShareOneClock(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceIDs := houseSalesOwed(t, env, 2)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})

	// One document per round, so each is parked at a clock of its own.
	sriApp.InvoicingService.WithSaleInvoiceDrainBatch(1)
	t.Cleanup(func() { sriApp.InvoicingService.WithSaleInvoiceDrainBatch(0) })
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("first drain = %+v; want one document parked", result)
	}
	firstParked := getNeedsAttentionQueue(t, operatorSessionID).Data[0].ID
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("second drain = %+v; want the other document parked", result)
	}
	var secondParked string
	for _, id := range invoiceIDs {
		if id != firstParked {
			secondParked = id
		}
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 2 || queue.Data[0].ID != firstParked {
		t.Fatalf("queue = %+v; want both parked, the earlier one first", queue.Data)
	}

	// Two hours on, the operator abandons the one that has waited LONGEST.
	// It stays in the queue — the Sale still has no factura — but it entered
	// it again, now, so it goes to the back.
	atInvoicingClock(t, fixedClock.Add(3*time.Hour))
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	if resp, body := checkInvoice(t, operatorSessionID, firstParked); resp.StatusCode != http.StatusOK {
		t.Fatalf("check: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := abandonInvoice(t, operatorSessionID, firstParked, map[string]any{"note": "not registered at the portal"})
	if view := abandonedView(t, resp, body); view.Status != "abandoned" {
		t.Fatalf("abandon left the document %s", view.Status)
	}

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 2 {
		t.Fatalf("count = %d; want 2 — one parked, one abandoned and unreplaced", n)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if len(queue.Data) != 2 {
		t.Fatalf("queue = %+v; want two rows", queue.Data)
	}
	if queue.Data[0].ID != secondParked || queue.Data[1].ID != firstParked {
		t.Fatalf("queue order = [%s, %s]; want the still-parked document (%s) ahead of the one abandoned an hour later (%s)",
			queue.Data[0].ID, queue.Data[1].ID, secondParked, firstParked)
	}
	// The abandoned row says WHEN by the same clock the order used: its
	// attention_since is cleared, and abandoned_at is what the list shows.
	front, back := queue.Data[0], queue.Data[1]
	if front.AttentionSince == nil || *front.AttentionSince != fixedClock.Add(time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("the parked row's attention_since = %v; want the hour it was parked", front.AttentionSince)
	}
	if back.AttentionSince != nil {
		t.Fatalf("the abandoned row's attention_since = %v; want null — the abandonment cleared it", back.AttentionSince)
	}
	if back.AbandonedAt == nil || *back.AbandonedAt != fixedClock.Add(3*time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("the abandoned row's abandoned_at = %v; want the moment it was abandoned", back.AbandonedAt)
	}
}

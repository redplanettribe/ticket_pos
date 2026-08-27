package integration

import (
	"testing"
)

// The chain on the operator surfaces (#486, parent #478, ADR 0061): the
// invoicing list marks a superseded Sale Invoice with the id of the
// corrected one, and the operator's Sale lookup lists the Sale's documents
// in CHAIN ORDER — superseded factura, the Credit Note that credited it,
// the corrected factura, and onward for a longer chain — each with its
// role (superseded, credit_note, current), with the reissue's who, when
// and note beside every document a reissue concerns. Order is the chain's,
// never the clock's: a reissue writes its two documents in one transaction
// and the list would otherwise sort them by id. A reversal Credit Note
// keeps its place after the factura it credits.
//
// Everything is asserted through the operator API: the list and the Sale
// lookup.

// chainDocumentView is a Sale lookup document as these tests read it: the
// list row's facts, the role, the chain links and the reissue's trail.
type chainDocumentView struct {
	ID                    string  `json:"id"`
	Kind                  string  `json:"kind"`
	Status                string  `json:"status"`
	Number                *string `json:"number"`
	Role                  string  `json:"role"`
	SupersedesInvoiceID   *string `json:"supersedes_invoice_id"`
	SupersededByInvoiceID *string `json:"superseded_by_invoice_id"`
	CreditsInvoiceID      *string `json:"credits_invoice_id"`
	CreditNoteReason      *string `json:"credit_note_reason"`
	ReissuedBy            *string `json:"reissued_by"`
	ReissuedAt            *string `json:"reissued_at"`
	ReissueNote           *string `json:"reissue_note"`
}

func saleLookupDocuments(t *testing.T, env *testEnv, operatorSessionID, ref string) []chainDocumentView {
	t.Helper()
	var found struct {
		Documents []chainDocumentView `json:"documents"`
	}
	operatorGetOK(t, env, operatorSessionID, operatorSaleLookupPath(ref), &found)
	return found.Documents
}

// supersededByInList reads one row's superseded_by marker off the invoicing
// list, failing when the row is not listed.
func supersededByInList(t *testing.T, operatorSessionID, id string) *string {
	t.Helper()
	for _, row := range getSaleInvoiceList(t, operatorSessionID).Data {
		if row.ID == id {
			return row.SupersededByInvoiceID
		}
	}
	t.Fatalf("document %s is not in the invoicing list", id)
	return nil
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestInvoiceListMarksASupersededSaleInvoice: after a reissue the list's
// row for the old factura names the corrected one as what superseded it —
// and no other row carries the marker: not the corrected factura, not the
// Credit Note, not the factura before the reissue.
func TestInvoiceListMarksASupersededSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	if marker := supersededByInList(t, operatorSessionID, facturaID); marker != nil {
		t.Fatalf("an unreissued factura is marked superseded by %s", *marker)
	}

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID := creditNoteOf(t, operatorSessionID)

	if marker := supersededByInList(t, operatorSessionID, facturaID); marker == nil || *marker != corrected.ID {
		t.Fatalf("the reissued factura's marker = %s; want superseded by the corrected factura %s", deref(marker), corrected.ID)
	}
	if marker := supersededByInList(t, operatorSessionID, corrected.ID); marker != nil {
		t.Fatalf("the corrected factura is marked superseded by %s", *marker)
	}
	if marker := supersededByInList(t, operatorSessionID, noteID); marker != nil {
		t.Fatalf("the Credit Note is marked superseded by %s", *marker)
	}
}

// TestSaleLookupListsTheChainInOrderWithRoles: two reissues, the first
// drained and the second still owed. The lookup lists five documents in
// chain order — the first factura, its Credit Note, the second factura,
// its Credit Note, the third — the two old facturas superseded, the two
// notes credit notes, the last one current; each Credit Note names the
// factura it credits with reason reissue; the reissue's trail is read
// beside the three documents of each reissue, the first with its note,
// the second without.
func TestSaleLookupListsTheChainInOrderWithRoles(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, firstID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference

	// Before any reissue: one document, current, with no trail.
	docs := saleLookupDocuments(t, env, operatorSessionID, ref)
	if len(docs) != 1 || docs[0].ID != firstID || docs[0].Role != "current" || docs[0].ReissuedBy != nil || docs[0].SupersededByInvoiceID != nil {
		t.Fatalf("documents before any reissue = %+v; want the one factura, current, no trail", docs)
	}

	second := reissueOK(t, operatorSessionID, firstID, companyRecipient())
	firstNoteID := creditNoteOf(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("drain = %+v; want the Credit Note and the corrected factura authorized", result)
	}
	third := reissueOK(t, operatorSessionID, second.ID, reissueBody{Recipient: reissueRecipientBody{
		TaxIDType: "cedula", TaxID: otherCedula, LegalName: "Beatriz Mora",
	}})

	docs = saleLookupDocuments(t, env, operatorSessionID, ref)
	if len(docs) != 5 {
		t.Fatalf("documents = %d; want five in chain order", len(docs))
	}
	var secondNoteID string
	for _, d := range docs {
		if d.Kind == "credit_note" && d.ID != firstNoteID {
			secondNoteID = d.ID
		}
	}
	want := []struct {
		id, kind, status, role string
	}{
		{firstID, "sale", "authorized", "superseded"},
		{firstNoteID, "credit_note", "authorized", "credit_note"},
		{second.ID, "sale", "authorized", "superseded"},
		{secondNoteID, "credit_note", "owed", "credit_note"},
		{third.ID, "sale", "owed", "current"},
	}
	for i, w := range want {
		got := docs[i]
		if got.ID != w.id || got.Kind != w.kind || got.Status != w.status || got.Role != w.role {
			t.Fatalf("document %d = %s %s %s %s; want %s %s %s %s", i, got.ID, got.Kind, got.Status, got.Role, w.id, w.kind, w.status, w.role)
		}
	}

	// The links: each Credit Note credits the factura before it for a
	// reissue; each corrected factura supersedes the one two places back;
	// each superseded factura names its successor.
	if deref(docs[1].CreditsInvoiceID) != firstID || deref(docs[1].CreditNoteReason) != "reissue" ||
		deref(docs[3].CreditsInvoiceID) != second.ID || deref(docs[3].CreditNoteReason) != "reissue" {
		t.Fatalf("credit notes = %+v / %+v; want each crediting the factura before it for reissue", docs[1], docs[3])
	}
	if deref(docs[0].SupersededByInvoiceID) != second.ID || deref(docs[2].SupersedesInvoiceID) != firstID ||
		deref(docs[2].SupersededByInvoiceID) != third.ID || deref(docs[4].SupersedesInvoiceID) != second.ID || docs[4].SupersededByInvoiceID != nil {
		t.Fatalf("chain links = %+v; want each factura linked to its neighbours", docs)
	}

	// The trail: the first reissue's — the operator, with the note — beside
	// its three documents, the second factura included, since a document's
	// own trail wins over the reissue that later superseded it; the
	// second's, without a note, beside the second Credit Note and the third
	// factura.
	for _, idx := range []int{0, 1, 2} {
		if deref(docs[idx].ReissuedBy) != "operator@example.com" || docs[idx].ReissuedAt == nil || deref(docs[idx].ReissueNote) != "buyer wrote in: the factura goes to the company" {
			t.Fatalf("document %d trail = %s / %v / %s; want the first reissue's, with its note", idx, deref(docs[idx].ReissuedBy), docs[idx].ReissuedAt, deref(docs[idx].ReissueNote))
		}
	}
	for _, idx := range []int{3, 4} {
		if deref(docs[idx].ReissuedBy) != "operator@example.com" || docs[idx].ReissuedAt == nil || docs[idx].ReissueNote != nil {
			t.Fatalf("document %d trail = %s / %v / %v; want the second reissue's, no note", idx, deref(docs[idx].ReissuedBy), docs[idx].ReissuedAt, docs[idx].ReissueNote)
		}
	}
}

// TestSaleLookupKeepsAReversalCreditNoteAfterItsFactura: nothing changes
// for a Sale that was reversed and never reissued — the factura is current
// (there is no other) and the reversal's Credit Note follows it, naming its
// route, with no reissue trail on either.
func TestSaleLookupKeepsAReversalCreditNoteAfterItsFactura(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	if result := operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(500),
		PlatformFeeKept:     boolPtr(false),
	}); result.Status != "reversed" {
		t.Fatalf("reversal = %+v", result)
	}

	docs := saleLookupDocuments(t, env, operatorSessionID, ref)
	if len(docs) != 2 {
		t.Fatalf("documents = %+v; want the factura and its Credit Note", docs)
	}
	if docs[0].ID != facturaID || docs[0].Role != "current" || docs[0].ReissuedBy != nil ||
		docs[1].Kind != "credit_note" || docs[1].Role != "credit_note" || deref(docs[1].CreditsInvoiceID) != facturaID || deref(docs[1].CreditNoteReason) != "platform" || docs[1].ReissuedBy != nil {
		t.Fatalf("documents = %+v; want the current factura then its platform-route Credit Note, no trail", docs)
	}
}

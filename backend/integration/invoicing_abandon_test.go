package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Abandon (#578, parent #575, ADR 0068): the operator's record that the SRI
// never took this document and never will, because it refuses the NUMBER it
// carries — error 45, "ERROR SECUENCIAL REGISTRADO".
//
// What these prove at the HTTP seam, and nowhere else: the fresh-Check gate
// refuses the press and then admits it; the abandoned document keeps its
// number, its clave, its bytes and every attempt; Mark annulled has given
// its ground up to Abandon; the terminal state is terminal for all three
// actions; and — the load-bearing one — a late AUTORIZADO for an abandoned
// clave does not heal the row and is recorded in the ledger all the same.
// That last one proves an EXISTING guard: an outcome is applied only to the
// status the row was read in, so nothing new had to be built for it, and
// this is where that claim is checked rather than assumed.

// abandonedInvoiceView is what #578 adds to the invoice detail, beside the
// facts the act must leave untouched.
type abandonedInvoiceView struct {
	Status      string  `json:"status"`
	Number      *string `json:"number"`
	AbandonedBy *string `json:"abandoned_by"`
	AbandonedAt *string `json:"abandoned_at"`
	AbandonNote *string `json:"abandon_note"`
	Ecuador     *struct {
		AccessKey           string  `json:"access_key"`
		Secuencial          int64   `json:"secuencial"`
		AuthorizationNumber *string `json:"authorization_number"`
	} `json:"ecuador"`
	Attempts []struct {
		Operation string `json:"operation"`
		Outcome   string `json:"outcome"`
	} `json:"attempts"`
}

func abandonInvoice(t *testing.T, sessionID, id string, body any) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath+"/"+id+"/abandon", body, authHeader(sessionID))
}

func getAbandonedDetail(t *testing.T, sessionID, id string) abandonedInvoiceView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view abandonedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

func abandonedView(t *testing.T, resp *http.Response, env envelope) abandonedInvoiceView {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("abandon status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view abandonedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// refusedByNumberSaleInvoice drives a House Organization's Sale Invoice to
// the position production's 001-001-000000025 and 26 are in: submitted once,
// returned by recepción with error 45, parked needs_attention with the
// authority's own message on the row.
func refusedByNumberSaleInvoice(t *testing.T) (operatorSessionID, invoiceID string) {
	t.Helper()
	env := setupTest(t)
	operatorSessionID, invoiceID = houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	// Every Check status from here answers "nothing known under that clave",
	// which is what the SRI portal shows the operator and what makes the
	// case anomalous enough to abandon.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	return operatorSessionID, invoiceID
}

// TestAbandonWithoutAFreshCheckIsRefusedAndWithOneTheDocumentIsAbandoned is
// the whole arc: the gate, the act, and everything the act must not touch.
func TestAbandonWithoutAFreshCheckIsRefusedAndWithOneTheDocumentIsAbandoned(t *testing.T) {
	operatorSessionID, invoiceID := refusedByNumberSaleInvoice(t)
	before := getAbandonedDetail(t, operatorSessionID, invoiceID)
	if before.Number == nil || before.Ecuador == nil {
		t.Fatalf("detail = %+v; want a signed document with a number and a clave", before)
	}

	// The ledger holds one Submit, which the SRI refused. Nothing has asked
	// the authority what it holds, so there is no current answer for the
	// abandonment to rest on.
	resp, body := abandonInvoice(t, operatorSessionID, invoiceID, map[string]any{"note": "not registered at the portal"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_CHECK_NOT_FRESH" {
		t.Fatalf("abandon: status=%d error=%+v; want 409 INVOICE_CHECK_NOT_FRESH", resp.StatusCode, body.Error)
	}
	if got := getAbandonedDetail(t, operatorSessionID, invoiceID); got.Status != "needs_attention" {
		t.Fatalf("status = %q after a refused abandon; want the document untouched at needs_attention", got.Status)
	}

	// Mark annulled would record a portal annulment that never happened: the
	// number is not at the portal to have been annulled. It is refused, and
	// the refusal names the act that is true.
	annulResp, annulBody := annulInvoice(t, operatorSessionID, invoiceID)
	if annulResp.StatusCode != http.StatusConflict || annulBody.Error == nil || annulBody.Error.Code != "INVOICE_ABANDON_INSTEAD" {
		t.Fatalf("annul: status=%d error=%+v; want 409 INVOICE_ABANDON_INSTEAD", annulResp.StatusCode, annulBody.Error)
	}

	// One Check status, answered "nothing known", and the ledger now carries
	// the authority's own current answer immediately before the act.
	checkResp, checkBody := checkInvoice(t, operatorSessionID, invoiceID)
	if view := actionOK(t, "check", checkResp, checkBody); view.Status != "needs_attention" {
		t.Fatalf("status = %q after a check that decided nothing; want needs_attention", view.Status)
	}
	sent := sriStub.receptionCount()

	abandonResp, abandonBody := abandonInvoice(t, operatorSessionID, invoiceID, map[string]any{"note": "not registered at the portal, confirmed by phone"})
	view := abandonedView(t, abandonResp, abandonBody)

	if view.Status != "abandoned" {
		t.Fatalf("status = %q; want abandoned", view.Status)
	}
	if view.AbandonedBy == nil || view.AbandonedAt == nil {
		t.Fatalf("abandoned_by = %v abandoned_at = %v; want the act to have an author and an instant", view.AbandonedBy, view.AbandonedAt)
	}
	if view.AbandonNote == nil || *view.AbandonNote != "not registered at the portal, confirmed by phone" {
		t.Fatalf("abandon_note = %v; want the operator's note kept with the record", view.AbandonNote)
	}
	// Nothing was sent, and nothing about the issuance was lost: the number,
	// the clave and the secuencial stand, and the ledger still holds every
	// attempt — the Submit the SRI refused and the Check that preceded the
	// act — plus nothing new.
	if got := sriStub.receptionCount(); got != sent {
		t.Fatalf("recepción received %d documents; want %d — Abandon sends nothing", got, sent)
	}
	if view.Number == nil || *view.Number != *before.Number {
		t.Fatalf("number = %v; want the document's own %v kept forever", view.Number, before.Number)
	}
	if view.Ecuador == nil || view.Ecuador.AccessKey != before.Ecuador.AccessKey || view.Ecuador.Secuencial != before.Ecuador.Secuencial {
		t.Fatalf("ecuador = %+v; want the clave and secuencial of %+v", view.Ecuador, before.Ecuador)
	}
	if len(view.Attempts) != 2 ||
		view.Attempts[0].Operation != "submit" ||
		view.Attempts[1].Operation != "query" {
		t.Fatalf("attempts = %+v; want the Submit and the Check kept, in order, and nothing added", view.Attempts)
	}
	// The signed bytes are still on file and still downloadable: the
	// abandonment is an account of the document, not its deletion.
	xmlResp, xml := sriEnv.getRaw(t, signedXMLPath(invoiceID), authHeader(operatorSessionID))
	if xmlResp.StatusCode != http.StatusOK || len(xml) == 0 {
		t.Fatalf("signed XML download status=%d bytes=%d; want the document's bytes — an abandoned document keeps them", xmlResp.StatusCode, len(xml))
	}

	// A terminal state is terminal: all three actions are refused, by name.
	for _, tc := range []struct {
		name string
		do   func() (*http.Response, envelope)
	}{
		{"check", func() (*http.Response, envelope) { return checkInvoice(t, operatorSessionID, invoiceID) }},
		{"resend", func() (*http.Response, envelope) { return resendInvoice(t, operatorSessionID, invoiceID) }},
		{"abandon", func() (*http.Response, envelope) { return abandonInvoice(t, operatorSessionID, invoiceID, nil) }},
	} {
		resp, body := tc.do()
		if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_ABANDONED" {
			t.Fatalf("%s on an abandoned document: status=%d error=%+v; want 409 INVOICE_ABANDONED", tc.name, resp.StatusCode, body.Error)
		}
	}
}

// TestAbandonIsRefusedOnADocumentRefusedForItsContent: the gate is narrow.
// A document the SRI examined and disliked has a real remedy — fix it and
// resend under the same clave — and must not be given up on.
func TestAbandonIsRefusedOnADocumentRefusedForItsContent(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	checkResp, checkBody := checkInvoice(t, operatorSessionID, invoiceID)
	actionOK(t, "check", checkResp, checkBody)

	resp, body := abandonInvoice(t, operatorSessionID, invoiceID, nil)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_NOT_REFUSED_BY_NUMBER" {
		t.Fatalf("abandon: status=%d error=%+v; want 409 INVOICE_NOT_REFUSED_BY_NUMBER", resp.StatusCode, body.Error)
	}
	// And Mark annulled is still the operator's to press there: #578 narrows
	// it for the refusal by number and for nothing else.
	annulResp, annulBody := annulInvoice(t, operatorSessionID, invoiceID)
	if annulResp.StatusCode != http.StatusOK {
		t.Fatalf("annul: status=%d error=%+v; want 200 — Mark annulled is unchanged for every other refusal", annulResp.StatusCode, annulBody.Error)
	}
}

// TestAnAuthorizedDocumentIsNeverAbandonable: a legal artifact is never
// disowned this way, whatever the authority once said about its number.
func TestAnAuthorizedDocumentIsNeverAbandonable(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want the document authorized", result)
	}

	resp, body := abandonInvoice(t, operatorSessionID, invoiceID, nil)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_ALREADY_AUTHORIZED" {
		t.Fatalf("abandon: status=%d error=%+v; want 409 INVOICE_ALREADY_AUTHORIZED", resp.StatusCode, body.Error)
	}
}

// TestALateAuthorizationDoesNotHealAnAbandonedDocument is the hazard ADR
// 0068 names: the operator abandons a number and the SRI authorizes it
// after all. If the row healed, the Sale could end up with two authorized
// facturas and double-declared income — the exact harm ADR 0061 exists to
// prevent.
//
// It is closed by a guard that ALREADY EXISTED and was built for a different
// race (an operator's Mark annulled against a Drainer round): an outcome is
// applied only to the status the row was READ in. Here that guard is put in
// the way of an abandonment rather than an annulment, and the race is made
// real rather than simulated — the fake SRI abandons the document from
// inside its own autorización handler, so the answer the platform is about
// to read describes a row that has moved underneath it.
//
// What must hold: the row stays abandoned, with no authorization number, and
// the authority's answer is in the attempts ledger all the same, because an
// event that changed nothing must still be reconcilable.
func TestALateAuthorizationDoesNotHealAnAbandonedDocument(t *testing.T) {
	operatorSessionID, invoiceID := refusedByNumberSaleInvoice(t)

	// The Check the abandonment will rest on.
	checkResp, checkBody := checkInvoice(t, operatorSessionID, invoiceID)
	actionOK(t, "check", checkResp, checkBody)

	// The next call to autorización abandons the document before answering
	// AUTORIZADO: the platform asked about a needs_attention row and will be
	// answered about one that is abandoned by the time it writes.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		resp, body := abandonInvoice(t, operatorSessionID, invoiceID, map[string]any{"note": "abandoned while the authority was being asked"})
		abandonedView(t, resp, body)
		return http.StatusOK, authorizedSOAP(accessKey)
	})

	lateResp, lateBody := checkInvoice(t, operatorSessionID, invoiceID)
	view := abandonedView(t, lateResp, lateBody)

	if view.Status != "abandoned" {
		t.Fatalf("status = %q after a late AUTORIZADO; want abandoned — the answer applies only to the status the row was read in", view.Status)
	}
	if view.Ecuador == nil || view.Ecuador.AuthorizationNumber != nil {
		t.Fatalf("ecuador = %+v; want no authorization number on a document the authority never held for us", view.Ecuador)
	}
	// The answer changed nothing and is on the ledger regardless.
	authorized := false
	for _, a := range view.Attempts {
		if a.Operation == "query" && a.Outcome == "authorized" {
			authorized = true
		}
	}
	if !authorized {
		t.Fatalf("attempts = %+v; want the late AUTORIZADO recorded in the ledger", view.Attempts)
	}
}

// TestTheInvoiceListFiltersByAbandoned: every number the platform has given
// up on is findable later, which is the whole reason the list learned a
// status filter.
func TestTheInvoiceListFiltersByAbandoned(t *testing.T) {
	operatorSessionID, invoiceID := refusedByNumberSaleInvoice(t)
	checkResp, checkBody := checkInvoice(t, operatorSessionID, invoiceID)
	actionOK(t, "check", checkResp, checkBody)
	abandonResp, abandonBody := abandonInvoice(t, operatorSessionID, invoiceID, nil)
	abandonedView(t, abandonResp, abandonBody)

	resp, env := sriEnv.get(t, invoicesPath+"?status=abandoned", authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var page struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(env.Data, &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != invoiceID || page.Data[0].Status != "abandoned" {
		t.Fatalf("list = %+v; want the abandoned document and nothing else", page.Data)
	}

	badResp, badEnv := sriEnv.get(t, invoicesPath+"?status=nonsense", authHeader(operatorSessionID))
	if badResp.StatusCode != http.StatusBadRequest || badEnv.Error == nil || badEnv.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("list with a mistyped status: status=%d error=%+v; want 400 VALIDATION_FAILED", badResp.StatusCode, badEnv.Error)
	}
}

// TestAbandoningAReissuesCreditNoteLeavesItsCorrectedFacturaToTheDrainer is
// the answer to a claim made against #578: that Abandon shipped with the
// defect Mark annulled does not have, because AnnulInvoice withdraws the
// unsigned corrected factura of a dead reissue Credit Note in its own
// transaction (#484) and AbandonInvoice does not, leaving that successor
// `owed` forever and its Sale stuck on REISSUE_IN_FLIGHT.
//
// IT DOES NOT HOLD, and this is where that is checked rather than argued.
// The asymmetry in the two acts is real: Mark annulled withdraws the
// successor itself, so the operator may reissue AT ONCE, and Abandon does
// not. But "forever" is the part that is false. The Drainer's own net
// (#484, withdrawCorrectedFacturaOfADeadCreditNote) is what catches it:
// ClaimDueInvoice admits a corrected factura once NO Credit Note against
// the factura it supersedes is live, an abandoned Credit Note is not live,
// and the round that claims it withdraws it unsigned. So the successor is
// withdrawn on the very next round, the factura is current and uncredited
// again, and the Sale is reissuable — one round later than after a Mark
// annulled, and never stuck.
//
// It is asserted here, on #580's ticket, because #580's Issue again is what
// would have been blocked if the claim were true.
func TestAbandoningAReissuesCreditNoteLeavesItsCorrectedFacturaToTheDrainer(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	// The reissue's Credit Note — codDoc 04 in the ninth and tenth digits of
	// the clave — is the one the SRI refuses by number.
	sriStub.setReception(func(accessKey string) (int, string) {
		if len(accessKey) > 10 && accessKey[8:10] == "04" {
			return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
		}
		return http.StatusOK, receivedSOAP(accessKey)
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the Credit Note parked and the corrected factura still waiting on it", result)
	}
	noteID := deref(getReissuedInvoice(t, operatorSessionID, facturaID).CreditedByInvoiceID)
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

	// Immediately after the act the corrected factura is still owed, and the
	// Sale still reads as having a reissue in flight. This is the whole of
	// the difference from Mark annulled, and it lasts exactly one round.
	if got := getReissuedInvoice(t, operatorSessionID, corrected.ID); got.Status != "owed" {
		t.Fatalf("the corrected factura is %s straight after the abandonment; want still owed", got.Status)
	}
	refusedResp, refusedBody := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
	expectRefusal(t, "reissue while the abandoned Credit Note's successor is still owed", refusedResp, refusedBody, http.StatusConflict, "REISSUE_IN_FLIGHT")

	// One round. The Drainer claims the corrected factura — no live Credit
	// Note stands against the factura it supersedes any more — and withdraws
	// it unsigned, consuming no number and sending nothing.
	if result := drainSaleInvoices(t); result.Withdrawn != 1 || result.Authorized != 0 {
		t.Fatalf("drain = %+v; want the corrected factura withdrawn unsigned", result)
	}
	withdrawn := getReissuedInvoice(t, operatorSessionID, corrected.ID)
	if withdrawn.Status != "withdrawn" || withdrawn.Number != nil {
		t.Fatalf("the corrected factura = %s %v; want withdrawn with no number consumed", withdrawn.Status, withdrawn.Number)
	}

	// And the Sale is out of the hole: the factura is current, credited by
	// nothing live and superseded by nothing, so the operator may reissue it.
	factura := getReissuedInvoice(t, operatorSessionID, facturaID)
	if factura.Status != "authorized" || factura.CreditedByInvoiceID != nil || factura.SupersededByInvoiceID != nil {
		t.Fatalf("the factura = %s credited_by %v superseded_by %v; want it current again", factura.Status, factura.CreditedByInvoiceID, factura.SupersededByInvoiceID)
	}
	again := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	if again.Status != "owed" {
		t.Fatalf("the second reissue = %s; want a fresh owed corrected factura", again.Status)
	}
}

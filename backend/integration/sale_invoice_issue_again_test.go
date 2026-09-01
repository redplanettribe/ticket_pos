package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Issue again (#580, parent #575, ADR 0068): the second press, which owes a
// Ticket Sale a FRESH Sale Invoice when the one it had is terminally dead —
// abandoned because the SRI refuses its number, or annulled at the portal.
//
// What these prove at the HTTP seam, and nowhere else, because the pure
// guard table proves the refusals and this proves the arc:
//
//   - abandon → issue again → a Drainer round signs the replacement under
//     the NEXT secuencial, by the ordinary owed path and with no new Drainer
//     knowledge. Sequence freshness is asserted the way the invoice-refusal
//     tests assert it: by the number the next successful issue takes, and by
//     the sequence table, which no API exposes.
//   - the abandoned document is never delivered and the replacement is, so
//     the buyer ends with exactly one valid factura.
//   - the chain reads in both directions and survives more than one hop.
//     The second hop is also the FIRST behavioural exercise migration 120's
//     widened partial unique index has had: it writes a live successor into
//     a slot a dead row still occupies in the index that preceded it.
//   - Issue again works identically from an `annulled` document, which is
//     the absorbed #480 case.
//   - every refusal, by its own code.

func issueAgainPath(id string) string {
	return invoicesPath + "/" + id + "/issue-again"
}

func issueAgain(t *testing.T, sessionID, id string, body any) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	return sriEnv.post(t, issueAgainPath(id), body, headers)
}

func issueAgainOK(t *testing.T, sessionID, id string, note *string) reissuedInvoiceView {
	t.Helper()
	resp, env := issueAgain(t, sessionID, id, map[string]any{"note": note})
	if resp.StatusCode != http.StatusCreated || env.Error != nil {
		t.Fatalf("issue again status=%d error=%+v; want 201", resp.StatusCode, env.Error)
	}
	var view reissuedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode the replacement: %v", err)
	}
	return view
}

// abandonedSaleInvoice drives a House Organization's Sale Invoice all the
// way to `abandoned`: refused by number, checked, given up on. It is
// refusedByNumberSaleInvoice plus the two presses #578 built, returning the
// harness as well, since what the buyer received is read off it.
func abandonedSaleInvoice(t *testing.T) (env *testEnv, operatorSessionID, invoiceID string) {
	t.Helper()
	env = setupTest(t)
	operatorSessionID, invoiceID = houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the document parked needs_attention", result)
	}
	// The authority holds nothing under that clave — what the operator sees
	// at the portal, and what an Abandon must rest on.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	if resp, body := checkInvoice(t, operatorSessionID, invoiceID); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup check status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := abandonInvoice(t, operatorSessionID, invoiceID, map[string]any{"note": "not registered at the portal"})
	if view := abandonedView(t, resp, body); view.Status != "abandoned" {
		t.Fatalf("setup abandon left the document %s", view.Status)
	}
	return env, operatorSessionID, invoiceID
}

// lastSecuencial reads the sequence table directly: no API exposes it, and
// "the abandoned number was never handed out again" is a fact about it
// alone (sale_invoice_drainer_test.go reads it the same way).
func lastSecuencial(t *testing.T, env *testEnv) int64 {
	t.Helper()
	var last int64
	if err := env.db.QueryRow(`SELECT last_secuencial FROM invoicing_sequences_ec WHERE cod_doc = '01'`).Scan(&last); err != nil {
		t.Fatalf("read the factura sequence: %v", err)
	}
	return last
}

// TestIssueAgainOwesAReplacementTheDrainerSignsUnderTheNextSecuencial is
// the whole arc, and the ticket's load-bearing test: the state production's
// 001-001-000000025 is in, the two presses that get out of it, and every
// consequence the buyer and the sequence must see.
func TestIssueAgainOwesAReplacementTheDrainerSignsUnderTheNextSecuencial(t *testing.T) {
	env, operatorSessionID, abandonedID := abandonedSaleInvoice(t)
	abandoned := getReissuedInvoice(t, operatorSessionID, abandonedID)
	if abandoned.Status != "abandoned" || abandoned.Number == nil || *abandoned.Number != "001-001-000000001" {
		t.Fatalf("the abandoned document = %s %v; want abandoned under 001-001-000000001", abandoned.Status, abandoned.Number)
	}
	if got := lastSecuencial(t, env); got != 1 {
		t.Fatalf("last_secuencial = %d after the refusal and the abandonment; want 1", got)
	}
	// The buyer has received nothing: the abandoned document never
	// authorized, so it was never delivered and never will be.
	if sent := deliveriesSent(t, env); len(sent) != 0 {
		t.Fatalf("deliveries = %+v; want none — a document the SRI never took is never mailed", sent)
	}

	// The press. One document comes back: a fresh Sale Invoice, owed and
	// unsigned, carrying the dead document's lines, amounts and Recipient,
	// and naming what it replaces.
	replacement := issueAgainOK(t, operatorSessionID, abandonedID, strPtr("the SRI refuses 1; issued again under a fresh number"))
	if replacement.Status != "owed" || replacement.Kind != "sale" || replacement.Number != nil || replacement.EcuadorFull != nil {
		t.Fatalf("replacement = %s %s number %v ecuador %v; want an owed, unsigned Sale Invoice with no number and no clave",
			replacement.Kind, replacement.Status, replacement.Number, replacement.EcuadorFull)
	}
	if replacement.ID == abandonedID {
		t.Fatalf("the replacement is the abandoned document itself; nothing issued is ever rewritten")
	}
	if replacement.Recipient != abandoned.Recipient {
		t.Fatalf("recipient = %+v; want the abandoned document's %+v, carried over unchanged", replacement.Recipient, abandoned.Recipient)
	}
	if replacement.Totals != abandoned.Totals || len(replacement.Lines) != len(abandoned.Lines) {
		t.Fatalf("totals/lines = %+v %d; want the abandoned document's %+v %d", replacement.Totals, len(replacement.Lines), abandoned.Totals, len(abandoned.Lines))
	}
	// NO CREDIT NOTE is owed: there is nothing to cancel. This is the whole
	// difference from a reissue, and the reason Issue again reaches a Sale
	// that a reissue answers INVOICE_ALREADY_CREDITED on (#579).
	docs := documentsOfSale(t, operatorSessionID, lastConfirmation(t, env).Reference)
	if len(docs) != 2 || docs[0].ID != abandonedID || docs[1].ID != replacement.ID {
		t.Fatalf("the Sale's documents = %+v; want the abandoned factura and its replacement, and no Credit Note", docs)
	}
	// The trail is the reissue's, reused: who, when, the note.
	if replacement.SupersedesInvoiceID == nil || *replacement.SupersedesInvoiceID != abandonedID ||
		replacement.ReissuedBy == nil || *replacement.ReissuedBy != "operator@example.com" ||
		replacement.ReissuedAt == nil || replacement.ReissueNote == nil {
		t.Fatalf("the replacement's trail = supersedes %v by %v at %v note %v",
			replacement.SupersedesInvoiceID, replacement.ReissuedBy, replacement.ReissuedAt, replacement.ReissueNote)
	}
	// And the chain reads in BOTH directions at once: the abandoned
	// document's page names what replaced it.
	if got := getReissuedInvoice(t, operatorSessionID, abandonedID); got.SupersededByInvoiceID == nil || *got.SupersededByInvoiceID != replacement.ID {
		t.Fatalf("the abandoned document's superseded_by = %v; want the replacement %s", got.SupersededByInvoiceID, replacement.ID)
	}

	// A Drainer round, with the SRI answering as it usually does. Nothing
	// about the round is special: it claims, signs, submits, polls and
	// delivers exactly as it does for a checkout's own document.
	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 || result.Withdrawn != 0 || result.Failed != 0 {
		t.Fatalf("drain = %+v; want the replacement claimed, authorized and delivered by the ordinary path", result)
	}

	// THE NEXT SECUENCIAL, never the abandoned one. The sequence only moves
	// forward, and the authority sees a number it has never refused.
	signed := getReissuedInvoice(t, operatorSessionID, replacement.ID)
	if signed.Status != "authorized" || signed.Number == nil || *signed.Number != "001-001-000000002" {
		t.Fatalf("the replacement = %s %v; want authorized under 001-001-000000002 — the next free number", signed.Status, signed.Number)
	}
	if got := lastSecuencial(t, env); got != 2 {
		t.Fatalf("last_secuencial = %d; want 2 — one number for the abandoned document, one for its replacement", got)
	}
	still := getReissuedInvoice(t, operatorSessionID, abandonedID)
	if still.Status != "abandoned" || still.Number == nil || *still.Number != "001-001-000000001" {
		t.Fatalf("the abandoned document = %s %v; want it untouched under its own consumed number", still.Status, still.Number)
	}

	// THE BUYER ENDS WITH EXACTLY ONE VALID FACTURA. The abandoned document
	// was never mailed and never will be; the replacement was.
	sent := deliveriesSent(t, env)
	if len(sent) != 1 || sent[0].Kind != "sale" {
		t.Fatalf("deliveries = %+v; want exactly one, the replacement's", sent)
	}
	if signed.DeliveredAt == nil || still.DeliveredAt != nil {
		t.Fatalf("delivered: replacement %v abandoned %v; want the replacement mailed and the abandoned document never", signed.DeliveredAt, still.DeliveredAt)
	}
	// And a second round sends nothing more: the abandoned document is off
	// the queue forever.
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing left to do", result)
	}
	if got := deliveriesSent(t, env); len(got) != 1 {
		t.Fatalf("deliveries after a second round = %d; want still one", len(got))
	}
}

// TestTheChainSurvivesMoreThanOneHop: a replacement that is itself
// abandoned is replaced again, and the trail reads all the way back.
//
// This is also where migration 120's widened partial unique index is
// EXERCISED rather than asserted. Before #579 the index excluded only
// `withdrawn`, so the abandoned first replacement would still have occupied
// the slot on its predecessor and this second press could not have been
// written at all. It writes a new live successor into a slot a dead row
// still holds under the old index; that it commits is the proof.
func TestTheChainSurvivesMoreThanOneHop(t *testing.T) {
	env, operatorSessionID, firstID := abandonedSaleInvoice(t)

	// Hop one. The SRI refuses the replacement's number too — the hazard
	// ADR 0068 names, since #573's pacing fix is not deployed — so the
	// operator abandons it in its turn.
	second := issueAgainOK(t, operatorSessionID, firstID, nil)
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain of the first replacement = %+v; want it parked needs_attention on the same refusal", result)
	}
	if resp, body := checkInvoice(t, operatorSessionID, second.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("check the first replacement: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := abandonInvoice(t, operatorSessionID, second.ID, map[string]any{"note": "refused by number again"})
	if view := abandonedView(t, resp, body); view.Status != "abandoned" {
		t.Fatalf("the first replacement is %s; want abandoned", view.Status)
	}

	// With its successor dead, the first document is unreplaced again — and
	// is itself no way back in, because a document that has been issued
	// again is not the one to press.
	if got := getReissuedInvoice(t, operatorSessionID, firstID); got.SupersededByInvoiceID != nil {
		t.Fatalf("the first document's superseded_by = %v; a dead replacement supersedes nothing (#579)", got.SupersededByInvoiceID)
	}

	// Hop two, from the second document. The slot on IT is free; the slot on
	// the first is occupied by a row the widened index no longer counts.
	third := issueAgainOK(t, operatorSessionID, second.ID, strPtr("third time"))
	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain of the second replacement = %+v; want it authorized and delivered", result)
	}

	// The trail reads all the way back through two dead documents, and each
	// dead one still names the one before it: nothing issued is rewritten.
	final := getReissuedInvoice(t, operatorSessionID, third.ID)
	if final.Status != "authorized" || final.Number == nil || *final.Number != "001-001-000000003" {
		t.Fatalf("the second replacement = %s %v; want authorized under 001-001-000000003 — a third number, the first two consumed and abandoned", final.Status, final.Number)
	}
	if final.SupersedesInvoiceID == nil || *final.SupersedesInvoiceID != second.ID {
		t.Fatalf("the second replacement supersedes %v; want the first replacement %s", final.SupersedesInvoiceID, second.ID)
	}
	middle := getReissuedInvoice(t, operatorSessionID, second.ID)
	if middle.SupersedesInvoiceID == nil || *middle.SupersedesInvoiceID != firstID {
		t.Fatalf("the first replacement supersedes %v; want the original %s — the link is never dropped", middle.SupersedesInvoiceID, firstID)
	}
	if middle.SupersededByInvoiceID == nil || *middle.SupersededByInvoiceID != third.ID {
		t.Fatalf("the first replacement's superseded_by = %v; want the second replacement %s", middle.SupersededByInvoiceID, third.ID)
	}
	// Three documents, one delivery, one valid factura.
	if sent := deliveriesSent(t, env); len(sent) != 1 {
		t.Fatalf("deliveries = %+v; want exactly one across the whole chain", sent)
	}
	if got := lastSecuencial(t, env); got != 3 {
		t.Fatalf("last_secuencial = %d; want 3 — two abandoned numbers, never reallocated, and the one that authorized", got)
	}

	// AND THE FIRST DOCUMENT IS STILL NOT A WAY IN, though its own successor
	// died and it therefore reads unreplaced. The invariant is the Sale's,
	// not the document's: a third factura now stands for this Sale, and
	// owing a fourth here would leave the buyer holding two valid ones. The
	// refusal is the same code the direct case answers — from the operator's
	// side it is one fact, "this Sale has its factura".
	if resp, body := issueAgain(t, operatorSessionID, firstID, map[string]any{"note": nil}); resp.StatusCode != http.StatusConflict ||
		body.Error == nil || body.Error.Code != "INVOICE_ALREADY_REPLACED" {
		t.Fatalf("issue again on the first document after the chain settled: status=%d error=%+v; want 409 INVOICE_ALREADY_REPLACED", resp.StatusCode, body.Error)
	}
	// And nothing is left waiting: the queue asks the same question, so the
	// first document — abandoned, with a dead successor, on a Sale that is
	// invoiced — is no longer work anybody could do (#581).
	if got := getNeedsAttentionCount(t, operatorSessionID); got != 0 {
		t.Fatalf("needs-attention count = %d; want 0 — every document of this Sale is dead, and the Sale has its factura", got)
	}
}

// TestIssueAgainWorksFromAnAnnulledSaleInvoice is the absorbed #480 case: a
// Sale whose factura the operator disowned by hand at the SRI portal stops
// being a dead end. Nothing about the act differs — which is the point of
// gating it on "terminally dead" rather than on "abandoned".
func TestIssueAgainWorksFromAnAnnulledSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	// Refused for its CONTENT, which is a refusal Mark annulled still
	// covers: #578 narrowed that action for the refusal BY NUMBER alone.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the document parked", result)
	}
	if view := annulOK(t, operatorSessionID, invoiceID); view.Status != "annulled" {
		t.Fatalf("setup annul left the document %s", view.Status)
	}

	replacement := issueAgainOK(t, operatorSessionID, invoiceID, strPtr("annulled at the portal; the Sale still owes a factura"))
	if replacement.Status != "owed" || replacement.SupersedesInvoiceID == nil || *replacement.SupersedesInvoiceID != invoiceID {
		t.Fatalf("replacement = %s superseding %v; want an owed factura naming the annulled one", replacement.Status, replacement.SupersedesInvoiceID)
	}
	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain = %+v; want the replacement authorized and delivered", result)
	}
	signed := getReissuedInvoice(t, operatorSessionID, replacement.ID)
	if signed.Number == nil || *signed.Number != "001-001-000000002" {
		t.Fatalf("the replacement took %v; want the next free number", signed.Number)
	}
	if sent := deliveriesSent(t, env); len(sent) != 1 {
		t.Fatalf("deliveries = %+v; want exactly the replacement's — the annulled document is never mailed", sent)
	}
}

// TestIssueAgainIsRefusedWhereItCannotOwe: every refusal, by its own code,
// through the API. The pure guard table says which refusal WINS; this says
// that the codes reach the operator.
func TestIssueAgainIsRefusedWhereItCannotOwe(t *testing.T) {
	// A manual Tax Invoice is typed again by hand, and a fresh, authorized
	// Sale Invoice is corrected by a reissue, never replaced.
	t.Run("a manual tax invoice", func(t *testing.T) {
		env := setupTest(t)
		sessionID := operatorSession(t, env, "operator@example.com")
		issuerReady(t, sessionID)
		manual := issueOK(t, sessionID, validInvoiceBody())
		resp, body := issueAgain(t, sessionID, manual.ID, nil)
		expectRefusal(t, "issue again on a manual document", resp, body, http.StatusConflict, "INVOICE_MANUAL_NOT_ISSUABLE_AGAIN")
	})

	t.Run("a credit note", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
		note := getReissuedInvoice(t, operatorSessionID, facturaID)
		if note.CreditedByInvoiceID == nil {
			t.Fatalf("the reissue owed no Credit Note against %s", facturaID)
		}
		resp, body := issueAgain(t, operatorSessionID, *note.CreditedByInvoiceID, nil)
		expectRefusal(t, "issue again on a credit note", resp, body, http.StatusConflict, "CREDIT_NOTE_NOT_ISSUABLE_AGAIN")
		// And the corrected factura, which is merely owed, is not dead.
		resp, body = issueAgain(t, operatorSessionID, corrected.ID, nil)
		expectRefusal(t, "issue again on an owed document", resp, body, http.StatusConflict, "INVOICE_NOT_TERMINALLY_DEAD")
	})

	t.Run("an authorized sale invoice", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		resp, body := issueAgain(t, operatorSessionID, facturaID, nil)
		expectRefusal(t, "issue again on the Sale's own factura", resp, body, http.StatusConflict, "INVOICE_NOT_TERMINALLY_DEAD")
	})

	// A live replacement already stands: one Sale never ends up with two
	// competing owed facturas.
	t.Run("a live replacement already exists", func(t *testing.T) {
		_, operatorSessionID, abandonedID := abandonedSaleInvoice(t)
		issueAgainOK(t, operatorSessionID, abandonedID, nil)
		resp, body := issueAgain(t, operatorSessionID, abandonedID, nil)
		expectRefusal(t, "a second issue again", resp, body, http.StatusConflict, "INVOICE_ALREADY_REPLACED")
	})

	// A Sale that no longer stands is never reinvoiced: the reversal
	// withdrew what it could and the income is not declared again.
	t.Run("a sale that no longer stands", func(t *testing.T) {
		env, operatorSessionID, abandonedID := abandonedSaleInvoice(t)
		reversalRoutes[0].reverse(t, env, operatorSessionID, lastConfirmation(t, env).Reference)
		resp, body := issueAgain(t, operatorSessionID, abandonedID, nil)
		expectRefusal(t, "issue again on a reversed Sale", resp, body, http.StatusConflict, "INVOICE_SALE_REVERSED")
	})

	t.Run("no document has that id", func(t *testing.T) {
		env := setupTest(t)
		sessionID := operatorSession(t, env, "operator@example.com")
		resp, body := issueAgain(t, sessionID, "00000000-0000-4000-8000-000000000000", nil)
		expectRefusal(t, "issue again on a missing document", resp, body, http.StatusNotFound, "INVOICE_NOT_FOUND")
	})
}

// TestIssueAgainIsOperatorsOnly: the act writes a document the platform
// owes and declares, so it is behind the operator gate like every other
// invoicing route.
func TestIssueAgainIsOperatorsOnly(t *testing.T) {
	env, _, abandonedID := abandonedSaleInvoice(t)
	memberSession := verifyOTP(t, env, "member@example.com")

	resp, body := issueAgain(t, "", abandonedID, nil)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated: status=%d error=%+v; want 401 UNAUTHORIZED", resp.StatusCode, body.Error)
	}
	resp, body = issueAgain(t, memberSession, abandonedID, nil)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("as a Member: status=%d error=%+v; want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
}

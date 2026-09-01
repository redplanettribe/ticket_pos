package integration

import (
	"net/http"
	"testing"
)

// A terminal-dead successor stops blocking its Sale (#579, parent #575, ADR
// 0068).
//
// ADR 0061 gave the reissue chain one invariant — a factura has at most one
// LIVE successor, so the chain never forks — and spelled "live" as "not
// withdrawn", because when it was written withdrawn was the only way a
// successor could die (#484: its Credit Note was annulled before it was ever
// signed). It stopped being the only way. A corrected factura that reached
// the Tax Authority can die annulled, and since #578 abandoned; both are as
// dead as withdrawn, and neither freed the slot. The superseded factura
// stayed superseded behind a document that no longer exists in any sense the
// authority recognises — the dead end recorded on #480.
//
// What these prove, at the HTTP seam: after the successor dies, the factura
// is no longer superseded by it anywhere the platform says so — the detail,
// the invoicing list, the operator's Sale lookup and the reissue guard — and
// the refusal the operator meets is no longer INVOICE_SUPERSEDED, which was
// simply false. The link itself is untouched: the dead document still names
// the factura it superseded, so the chain reads backwards through documents
// that died.
//
// WHAT DOES NOT CHANGE HERE, DELIBERATELY. Freeing the slot does not make
// the Sale invoiceable again by itself: a reissue would owe a SECOND Credit
// Note against a factura an authorized one already credits, so the refusal
// becomes INVOICE_ALREADY_CREDITED — the Sale-with-no-current-factura gap
// that error was named for, and the one #580's Issue again closes by owing a
// fresh factura with no Credit Note at all. That refusal is asserted here by
// name, because the difference between "superseded by a ghost" and "already
// credited" is the whole of what this ticket moves.

// deadSuccessor drives a Sale to the shape #480 recorded: an authorized
// factura, a reissue whose Credit Note authorized, and a corrected factura
// that reached the SRI and died there — abandoned because the authority
// refused its number, or annulled by the operator at the portal. It returns
// the operator's session, the superseded factura, the dead corrected one and
// the Sale's Confirmation reference.
//
// The two deaths are driven the same way and differ only in the SRI's answer
// and the operator's act, so the test that follows can hold both to one
// standard: the platform must not care WHICH terminal death it was.
func deadSuccessor(t *testing.T, death string) (env *testEnv, operatorSessionID, facturaID, correctedID, ref string) {
	t.Helper()
	env = setupTest(t)
	operatorSessionID, facturaID, _ = houseSaleAuthorized(t, env)
	ref = lastConfirmation(t, env).Reference
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	correctedID = corrected.ID

	// The Credit Note is taken and authorized as usual; the corrected
	// factura — the only OTHER factura the SRI is about to see, the first
	// having authorized already — is the one that dies. The clave de acceso
	// carries the document type in its ninth and tenth digits: 01 a factura,
	// 04 a nota de crédito.
	isFactura := func(accessKey string) bool { return len(accessKey) > 10 && accessKey[8:10] == "01" }
	switch death {
	case "abandoned":
		sriStub.setReception(func(accessKey string) (int, string) {
			if isFactura(accessKey) {
				return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
			}
			return http.StatusOK, receivedSOAP(accessKey)
		})
	case "annulled":
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			if isFactura(accessKey) {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
			}
			return http.StatusOK, authorizedSOAP(accessKey)
		})
	default:
		t.Fatalf("unknown death %q", death)
	}
	if result := drainSaleInvoices(t); result.Authorized != 1 || result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the Credit Note authorized and the corrected factura parked", result)
	}

	switch death {
	case "abandoned":
		// The authority holds nothing under that clave, which is what the
		// operator sees at the portal and what Abandon must rest on (#578).
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, emptyAuthorizationSOAP(accessKey)
		})
		if resp, body := checkInvoice(t, operatorSessionID, correctedID); resp.StatusCode != http.StatusOK {
			t.Fatalf("setup check status=%d error=%+v", resp.StatusCode, body.Error)
		}
		resp, body := abandonInvoice(t, operatorSessionID, correctedID, map[string]any{"note": "not registered at the portal"})
		if view := abandonedView(t, resp, body); view.Status != "abandoned" {
			t.Fatalf("setup abandon left the corrected factura %s", view.Status)
		}
	case "annulled":
		if view := annulOK(t, operatorSessionID, correctedID); view.Status != "annulled" {
			t.Fatalf("setup annul left the corrected factura %s", view.Status)
		}
	}
	return env, operatorSessionID, facturaID, correctedID, ref
}

// TestATerminalDeadSuccessorStopsSupersedingItsFactura: the whole of the
// rule, told through every surface that reads it, for both terminal deaths.
func TestATerminalDeadSuccessorStopsSupersedingItsFactura(t *testing.T) {
	for _, death := range []string{"abandoned", "annulled"} {
		t.Run(death, func(t *testing.T) {
			env, operatorSessionID, facturaID, correctedID, ref := deadSuccessor(t, death)

			// The dead document keeps everything it had, including the link
			// BACK to the factura it superseded and the trail of its own
			// reissue: nothing issued is ever rewritten, and the chain must
			// still read backwards through it.
			dead := getReissuedInvoice(t, operatorSessionID, correctedID)
			if dead.Status != death {
				t.Fatalf("the corrected factura is %s; want %s", dead.Status, death)
			}
			if dead.SupersedesInvoiceID == nil || *dead.SupersedesInvoiceID != facturaID {
				t.Fatalf("the dead successor supersedes %v; want the factura %s — the link is never dropped", dead.SupersedesInvoiceID, facturaID)
			}
			if dead.ReissuedBy == nil || dead.ReissuedAt == nil {
				t.Fatalf("the dead successor's trail = %v %v; want who reissued and when, kept forever", dead.ReissuedBy, dead.ReissuedAt)
			}

			// The factura is superseded by nobody: the detail, the list and
			// the Sale lookup all say so, and the reissue trail read beside
			// a superseded factura goes with the successor that died.
			factura := getReissuedInvoice(t, operatorSessionID, facturaID)
			if factura.SupersededByInvoiceID != nil {
				t.Fatalf("superseded_by = %v on a factura whose successor is %s; a dead successor supersedes nothing", factura.SupersededByInvoiceID, death)
			}
			if factura.ReissuedBy != nil || factura.ReissuedAt != nil || factura.ReissueNote != nil {
				t.Fatalf("the factura still shows a reissue trail (%v, %v, %v) for a reissue that died", factura.ReissuedBy, factura.ReissuedAt, factura.ReissueNote)
			}
			if got := supersededByInList(t, operatorSessionID, facturaID); got != nil {
				t.Fatalf("the invoicing list marks the factura superseded by %s; want no marker", *got)
			}

			// The operator's Sale lookup reads the chain in order and calls
			// each document what it now is: the factura current again, the
			// dead successor neither current nor superseded.
			docs := saleLookupDocuments(t, env, operatorSessionID, ref)
			if len(docs) != 3 {
				t.Fatalf("the Sale's documents = %d; want the factura, its Credit Note and the dead successor", len(docs))
			}
			if docs[0].ID != facturaID || docs[0].Role != "current" || docs[0].SupersededByInvoiceID != nil {
				t.Fatalf("the factura = %+v; want it current again, superseded by nobody", docs[0])
			}
			if docs[2].ID != correctedID || docs[2].Role != "not_current" || deref(docs[2].SupersedesInvoiceID) != facturaID {
				t.Fatalf("the dead successor = %+v; want not_current, still naming the factura it superseded", docs[2])
			}

			// And the refusal the operator meets is the true one. Not
			// INVOICE_SUPERSEDED — the factura is not superseded by a
			// document the authority never authorized — but the Sale's real
			// remaining obstacle: an authorized Credit Note already stands
			// against it, which is #480's gap and #580's Issue again to
			// close. The dead successor itself is still no route in.
			resp, body := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
			expectRefusal(t, "reissue of a factura whose successor is "+death, resp, body, http.StatusConflict, "INVOICE_ALREADY_CREDITED")
			resp, body = reissueInvoice(t, operatorSessionID, correctedID, companyRecipient())
			expectRefusal(t, "reissue of the "+death+" successor", resp, body, http.StatusConflict, "INVOICE_NOT_AUTHORIZED")
		})
	}
}

// TestALiveSuccessorStillBlocksASecondReissue: the invariant the rule exists
// for is untouched. A successor that can still become something — authorized,
// or parked with a remedy — holds the slot, and one Sale never ends up with
// two competing corrected facturas.
func TestALiveSuccessorStillBlocksASecondReissue(t *testing.T) {
	t.Run("authorized", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
		if result := drainSaleInvoices(t); result.Authorized != 2 {
			t.Fatalf("setup drain = %+v; want the Credit Note and the corrected factura authorized", result)
		}
		factura := getReissuedInvoice(t, operatorSessionID, facturaID)
		if factura.SupersededByInvoiceID == nil || *factura.SupersededByInvoiceID != corrected.ID {
			t.Fatalf("superseded_by = %v; want the live corrected factura %s", factura.SupersededByInvoiceID, corrected.ID)
		}
		resp, body := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
		expectRefusal(t, "second reissue behind a live successor", resp, body, http.StatusConflict, "INVOICE_SUPERSEDED")
	})

	// Parked for its CONTENT — error 65, a refusal with a real remedy — is
	// not a death: the document can still be resent under the same clave and
	// authorized, so it holds the slot. This is the case the widened rule
	// must not swallow.
	t.Run("parked with a remedy", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			if len(accessKey) > 10 && accessKey[8:10] == "01" {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
			}
			return http.StatusOK, authorizedSOAP(accessKey)
		})
		if result := drainSaleInvoices(t); result.Authorized != 1 || result.NeedsAttention != 1 {
			t.Fatalf("setup drain = %+v; want the Credit Note authorized and the corrected factura parked", result)
		}
		factura := getReissuedInvoice(t, operatorSessionID, facturaID)
		if factura.SupersededByInvoiceID == nil || *factura.SupersededByInvoiceID != corrected.ID {
			t.Fatalf("superseded_by = %v; want the parked corrected factura %s — parked is not dead", factura.SupersededByInvoiceID, corrected.ID)
		}
		resp, body := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
		expectRefusal(t, "second reissue behind a parked successor", resp, body, http.StatusConflict, "REISSUE_IN_FLIGHT")
	})
}

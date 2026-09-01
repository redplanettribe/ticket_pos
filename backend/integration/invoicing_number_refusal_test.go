package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A refusal by number (#576, parent #575, ADR 0068): the SRI answers recepción
// with 45 "ERROR SECUENCIAL REGISTRADO" — it will not take the document
// because of the secuencial it carries, not because of anything in it.
//
// The document is parked exactly as any other refusal parks it: nothing about
// the status changes, because the number is consumed and the row keeps its
// clave, its bytes and its ledger whatever the authority said. What changes is
// that the API now tells the operator WHICH refusal this is, so the detail
// page can stop showing copy that suggests a fixable data problem. The answer
// is derived from the messages the row already stores, which is why the two
// production documents at 25 and 26 answer true with no migration behind them.

// numberRefusalView is what #576 adds to the invoice detail, beside the
// status and the authority's messages it is derived from.
type numberRefusalView struct {
	Status          string `json:"status"`
	RefusedByNumber bool   `json:"refused_by_number"`
	Messages        []struct {
		Identifier string `json:"identifier"`
		Type       string `json:"type"`
	} `json:"messages"`
}

func getNumberRefusalDetail(t *testing.T, sessionID, id string) numberRefusalView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view numberRefusalView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// TestRecepcion45ParksTheDocumentAsRefusedByNumber: the Drainer submits a
// House Organization's Sale Invoice, the SRI returns it with error 45, and the
// document is parked needs_attention with the authority's own message on it —
// and the detail says the refusal was by number.
func TestRecepcion45ParksTheDocumentAsRefusedByNumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})

	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	detail := getNumberRefusalDetail(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" {
		t.Fatalf("status = %q; want needs_attention — a refusal by number parks the document like any other", detail.Status)
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Identifier != "45" {
		t.Fatalf("messages = %+v; want the SRI's 45 kept verbatim", detail.Messages)
	}
	if !detail.RefusedByNumber {
		t.Fatal("refused_by_number = false on a document the SRI refused with 45")
	}
}

// TestAGenericRefusalIsNotARefusalByNumber: the same park, the same status,
// the same page — and the API must still answer the two apart, or the
// operator reads the wrong cause. Error 35 is a document the SRI examined and
// disliked; its number is fine.
func TestAGenericRefusalIsNotARefusalByNumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})

	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	detail := getNumberRefusalDetail(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" {
		t.Fatalf("status = %q; want needs_attention", detail.Status)
	}
	if detail.RefusedByNumber {
		t.Fatal("refused_by_number = true on a document the SRI refused with 35: a schema refusal is not a refusal by number")
	}
}

// TestAnAuthorizedDocumentIsNeverRefusedByNumber: the advertencia every
// pruebas authorization carries is not an error, and nothing the SRI said
// about an authorized document may read as a refusal.
func TestAnAuthorizedDocumentIsNeverRefusedByNumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)

	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want the document authorized", result)
	}
	detail := getNumberRefusalDetail(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.RefusedByNumber {
		t.Fatalf("detail = %s refused_by_number %v; want authorized and not refused by number", detail.Status, detail.RefusedByNumber)
	}
}

// RESEND IS REFUSED ON A DOCUMENT REFUSED BY NUMBER (#577, parent #575,
// ADR 0068). Resend's contract is the same clave and the same secuencial
// (S1 §5.10), which is correct for every other refusal and is the entirety
// of what error 45 objects to. The operator pressed it on both production
// documents two days after their first refusal and earned another rejection;
// the button provably cannot work, so the API refuses it and says why.
//
// What these prove at the HTTP seam: the refusal's own code, that Check
// status is untouched on the very same document, that an ordinary refusal
// resends exactly as it always did — and, the load-bearing one, that the
// refused Resend SENDS NOTHING. That is asserted against the fake SRI's
// reception count, because "refused" would be worth little if the document
// had already gone out.

// TestResendIsRefusedOnADocumentTheSRIRefusesByNumberAndSendsNothing.
func TestResendIsRefusedOnADocumentTheSRIRefusesByNumberAndSendsNothing(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	sent := sriStub.receptionCount()

	resp, body := resendInvoice(t, operatorSessionID, invoiceID)

	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_REFUSED_BY_NUMBER" {
		t.Fatalf("resend: status=%d error=%+v; want 409 INVOICE_REFUSED_BY_NUMBER", resp.StatusCode, body.Error)
	}
	if got := sriStub.receptionCount(); got != sent {
		t.Fatalf("recepción received %d documents; want %d — a refused resend sends nothing", got, sent)
	}

	// The refusal is a refusal, not a state change: the document is parked
	// exactly where the SRI's answer left it, and still says why.
	detail := getNumberRefusalDetail(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" || !detail.RefusedByNumber {
		t.Fatalf("detail = %s refused_by_number %v; want the document untouched by the refusal", detail.Status, detail.RefusedByNumber)
	}
}

// TestCheckStatusStaysAvailableOnADocumentTheSRIRefusesByNumber: the other
// half of the asymmetry. Check asks the authority and never sends, and its
// answer — here "nothing known under that clave" — is the evidence an
// operator needs before doing anything irreversible about the number.
func TestCheckStatusStaysAvailableOnADocumentTheSRIRefusesByNumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	sent := sriStub.receptionCount()
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})

	checkResp, checkBody := checkInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "check", checkResp, checkBody)

	if view.Status != "needs_attention" {
		t.Fatalf("status = %q; want needs_attention — an answer that decides nothing changes nothing", view.Status)
	}
	if got := sriStub.receptionCount(); got != sent {
		t.Fatalf("recepción received %d documents; want %d — Check status never sends", got, sent)
	}
}

// TestResendIsUnchangedForARefusalThatIsNotByNumber: the guard is narrow.
// A document the SRI refused for its content is rebuilt, re-signed and sent
// again under the same clave and secuencial, exactly as before #577.
func TestResendIsUnchangedForARefusalThatIsNotByNumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	first, ok := sriStub.lastReceived()
	if !ok {
		t.Fatal("the drain sent nothing to recepción")
	}
	sent := sriStub.receptionCount()
	sriStub.answerAsUsual()

	resendResp, resendBody := resendInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "resend", resendResp, resendBody)

	if view.Status != "authorized" {
		t.Fatalf("status = %q; want authorized", view.Status)
	}
	if got := sriStub.receptionCount(); got != sent+1 {
		t.Fatalf("recepción received %d documents; want %d — the resend must send", got, sent+1)
	}
	second, _ := sriStub.lastReceived()
	if second.accessKey != first.accessKey {
		t.Fatalf("resend carried clave %q; want the document's own %q", second.accessKey, first.accessKey)
	}
}

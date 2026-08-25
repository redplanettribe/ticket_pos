package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// Check status and Resend (#455): the two things a Platform Operator can do
// to a Tax Invoice the SRI has not authorized. Check status asks autorización
// again; Resend rebuilds and re-signs the document and submits it under the
// SAME clave de acceso and secuencial, which is what the Ficha requires
// (research §2.5). Asserted at the HTTP seam and on what the fake SRI
// received, never on the module's tables — with one exception below.

func checkInvoice(t *testing.T, sessionID, id string) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath+"/"+id+"/check", nil, authHeader(sessionID))
}

func resendInvoice(t *testing.T, sessionID, id string) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath+"/"+id+"/resend", nil, authHeader(sessionID))
}

func actionOK(t *testing.T, what string, resp *http.Response, env envelope) invoiceDetailView {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status=%d error=%+v", what, resp.StatusCode, env.Error)
	}
	if env.Error != nil {
		t.Fatalf("%s returned error %+v", what, env.Error)
	}
	var view invoiceDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// invoiceActionView is the part of the detail #455 adds: whether the page
// should say "the SRI has it — check status" rather than show an error.
type invoiceActionView struct {
	CheckStatusHint bool `json:"check_status_hint"`
}

func actionHint(t *testing.T, env envelope) bool {
	t.Helper()
	var view invoiceActionView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view.CheckStatusHint
}

// storedSignedXML reads the signed document off the invoice row. The API that
// serves it (the XML download) is #456's and does not exist on this branch,
// and the rule under test — the artifact on file is replaced only when the SRI
// took the resend — is invisible any other way.
func storedSignedXML(t *testing.T, id string) []byte {
	t.Helper()
	var xml []byte
	if err := sriEnv.db.QueryRow(`SELECT signed_xml FROM invoicing_invoices WHERE id = $1`, id).Scan(&xml); err != nil {
		t.Fatalf("read signed_xml: %v", err)
	}
	return xml
}

// issuePending issues an invoice the fake keeps EN PROCESAMIENTO, so the
// budget runs out and the invoice is pending.
func issuePending(t *testing.T, sessionID string) invoiceDetailView {
	t.Helper()
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, inProcessingSOAP(accessKey)
	})
	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "pending" {
		t.Fatalf("setup: status=%q, want pending", view.Status)
	}
	return view
}

// issueRejected issues an invoice the fake answers DEVUELTA with error 35.
func issueRejected(t *testing.T, sessionID string) invoiceDetailView {
	t.Helper()
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})
	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "rejected" {
		t.Fatalf("setup: status=%q, want rejected", view.Status)
	}
	return view
}

var signatureValueInXML = regexp.MustCompile(`(?s)<ds:SignatureValue[^>]*>(.*?)</ds:SignatureValue>`)

func signatureValue(t *testing.T, signed []byte) string {
	t.Helper()
	m := signatureValueInXML.FindSubmatch(signed)
	if m == nil {
		t.Fatalf("no ds:SignatureValue in the received document")
	}
	return string(m[1])
}

// TestCheckStatusPendingToAuthorized: the SRI was still working when Issue
// was pressed; Check status finds it AUTORIZADO and the invoice gains its
// number, date and the authorization XML — one more attempts row.
func TestCheckStatusPendingToAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	pending := issuePending(t, sessionID)
	before := len(pending.Attempts)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, authorizedSOAP(accessKey)
	})
	resp, body := checkInvoice(t, sessionID, pending.ID)
	view := actionOK(t, "check", resp, body)

	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized (messages %+v)", view.Status, view.Messages)
	}
	if view.Ecuador.AuthorizationNumber == nil || *view.Ecuador.AuthorizationNumber != pending.Ecuador.AccessKey {
		t.Fatalf("authorization number=%v, want the clave %q", view.Ecuador.AuthorizationNumber, pending.Ecuador.AccessKey)
	}
	if view.Ecuador.AuthorizationDate == nil {
		t.Fatal("authorized invoice has no authorization date")
	}
	if !view.HasAuthorizationXML {
		t.Fatal("authorized invoice kept no authorization XML")
	}
	if len(view.Attempts) != before+1 {
		t.Fatalf("attempts grew from %d to %d, want exactly one more", before, len(view.Attempts))
	}
	last := view.Attempts[len(view.Attempts)-1]
	if last.Operation != "query" || last.Outcome != "authorized" {
		t.Fatalf("new attempt = %+v, want query/authorized", last)
	}
	// Check status never resends: the fake received exactly the one document.
	if sriStub.receptionCount() != 1 {
		t.Fatalf("reception count=%d, want 1", sriStub.receptionCount())
	}
}

// TestCheckStatusNotAuthorizedStoresMessages: NO AUTORIZADO on a check moves
// the invoice to not_authorized with the SRI's messages verbatim.
func TestCheckStatusNotAuthorizedStoresMessages(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	pending := issuePending(t, sessionID)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "Error en la firma", "ERROR"))
	})
	resp, body := checkInvoice(t, sessionID, pending.ID)
	view := actionOK(t, "check", resp, body)

	if view.Status != "not_authorized" {
		t.Fatalf("status=%q, want not_authorized", view.Status)
	}
	if len(view.Messages) != 1 || view.Messages[0].Identifier != "39" || view.Messages[0].AdditionalInfo != "Error en la firma" {
		t.Fatalf("messages = %+v, want the SRI's 39 verbatim", view.Messages)
	}
	if view.HasAuthorizationXML {
		t.Fatal("a not_authorized invoice must hold no authorization XML")
	}
}

// TestCheckStatusStillInProcessingStaysPending: EN PROCESAMIENTO on a check
// leaves the invoice pending, with the hint that the SRI has it.
func TestCheckStatusStillInProcessingStaysPending(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	pending := issuePending(t, sessionID)

	resp, body := checkInvoice(t, sessionID, pending.ID)
	view := actionOK(t, "check", resp, body)
	if view.Status != "pending" {
		t.Fatalf("status=%q, want pending", view.Status)
	}
	if !actionHint(t, body) {
		t.Fatal("a pending invoice the SRI holds should carry the check-status hint")
	}
}

// TestCheckStatusAllowedFromRejectedAndNotAuthorized: both are non-authorized
// states the operator may ask about again.
func TestCheckStatusAllowedFromRejectedAndNotAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)

	// A DEVUELTA document is not at the SRI; autorización knows nothing. An
	// answer that decides nothing changes nothing: the invoice stays rejected,
	// and the ask is on the ledger.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	resp, body := checkInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "check from rejected", resp, body)
	if view.Status != "rejected" {
		t.Fatalf("status=%q after an empty answer, want rejected still", view.Status)
	}
	if len(view.Attempts) != len(rejected.Attempts)+1 {
		t.Fatalf("attempts grew from %d to %d, want exactly one more", len(rejected.Attempts), len(view.Attempts))
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
	})
	resp, body = checkInvoice(t, sessionID, rejected.ID)
	view = actionOK(t, "check to not_authorized", resp, body)
	if view.Status != "not_authorized" {
		t.Fatalf("status=%q, want not_authorized", view.Status)
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, authorizedSOAP(accessKey)
	})
	resp, body = checkInvoice(t, sessionID, rejected.ID)
	view = actionOK(t, "check from not_authorized", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", view.Status)
	}
}

// TestCheckStatusRefusedOnAuthorized: an authorized invoice is a legal
// artifact; nothing about it is asked again.
func TestCheckStatusRefusedOnAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	authorized := issueOK(t, sessionID, validInvoiceBody())
	if authorized.Status != "authorized" {
		t.Fatalf("setup: status=%q", authorized.Status)
	}

	resp, body := checkInvoice(t, sessionID, authorized.ID)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("check on authorized status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "INVOICE_ALREADY_AUTHORIZED" {
		t.Fatalf("error = %+v, want INVOICE_ALREADY_AUTHORIZED", body.Error)
	}
	if string(body.Data) != "null" {
		t.Fatalf("data = %s, want null", body.Data)
	}
	after := getInvoice(t, sessionID, authorized.ID)
	if len(after.Attempts) != len(authorized.Attempts) {
		t.Fatalf("a refused check wrote %d attempts", len(after.Attempts)-len(authorized.Attempts))
	}
}

// TestCheckStatusUnknownInvoice: 404 with the invoice code.
func TestCheckStatusUnknownInvoice(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	resp, body := checkInvoice(t, sessionID, "00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("status=%d error=%+v, want 404 INVOICE_NOT_FOUND", resp.StatusCode, body.Error)
	}
	resp, body = resendInvoice(t, sessionID, "not-a-uuid")
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("resend status=%d error=%+v, want 404 INVOICE_NOT_FOUND", resp.StatusCode, body.Error)
	}
}

// TestResendFromRejectedSendsSameClaveWithFreshSignature: the resend carries
// the same clave and secuencial as the first send, a different signature
// (the signer draws fresh ids), verifies, and — the fake having answered
// RECIBIDA — is now the document on file.
func TestResendFromRejectedSendsSameClaveWithFreshSignature(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)
	first, ok := sriStub.lastReceived()
	if !ok {
		t.Fatal("the fake received nothing on issue")
	}
	if !bytes.Equal(storedSignedXML(t, rejected.ID), first.signedXML) {
		t.Fatal("setup: the stored document is not the one the fake received")
	}

	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusOK, receivedSOAP(accessKey) })
	resp, body := resendInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "resend", resp, body)

	if view.Status != "authorized" {
		t.Fatalf("status=%q after resend, want authorized (messages %+v)", view.Status, view.Messages)
	}
	if sriStub.receptionCount() != 2 {
		t.Fatalf("reception count=%d, want 2", sriStub.receptionCount())
	}
	second, _ := sriStub.lastReceived()
	if second.accessKey != rejected.Ecuador.AccessKey || second.accessKey != first.accessKey {
		t.Fatalf("resend carried clave %q, want %q", second.accessKey, rejected.Ecuador.AccessKey)
	}
	if view.Ecuador.Secuencial != rejected.Ecuador.Secuencial || view.Number != rejected.Number {
		t.Fatalf("resend renumbered: %s (seq %d), was %s (seq %d)", view.Number, view.Ecuador.Secuencial, rejected.Number, rejected.Ecuador.Secuencial)
	}
	if signatureValue(t, second.signedXML) == signatureValue(t, first.signedXML) {
		t.Fatal("resend reused the first signature; want a fresh one")
	}
	if _, err := sri.Verify(second.signedXML); err != nil {
		t.Fatalf("resent document does not verify: %v", err)
	}
	validateReceivedAgainstXSD(t, second.signedXML)
	if !bytes.Equal(storedSignedXML(t, rejected.ID), second.signedXML) {
		t.Fatal("RECIBIDA resend did not replace the document on file")
	}
	// The ledger: the rejected submit from issue, then the resend's submit
	// and its query.
	if len(view.Attempts) < len(rejected.Attempts)+2 {
		t.Fatalf("attempts = %d, want at least %d", len(view.Attempts), len(rejected.Attempts)+2)
	}
	if a := view.Attempts[len(rejected.Attempts)]; a.Operation != "submit" || a.Outcome != "received" {
		t.Fatalf("resend's first attempt = %+v, want submit/received", a)
	}
}

// TestResendRejectedAgainKeepsStoredXML: DEVUELTA again — the SRI holds
// nothing, the artifact on file stays the first one, the ledger grows by the
// one submit.
func TestResendRejectedAgainKeepsStoredXML(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)
	first, _ := sriStub.lastReceived()

	resp, body := resendInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "resend", resp, body)

	if view.Status != "rejected" {
		t.Fatalf("status=%q, want rejected", view.Status)
	}
	if sriStub.receptionCount() != 2 {
		t.Fatalf("reception count=%d, want 2", sriStub.receptionCount())
	}
	second, _ := sriStub.lastReceived()
	if second.accessKey != first.accessKey {
		t.Fatalf("resend carried clave %q, want %q", second.accessKey, first.accessKey)
	}
	if !bytes.Equal(storedSignedXML(t, rejected.ID), first.signedXML) {
		t.Fatal("a DEVUELTA resend replaced the document on file")
	}
	if len(view.Attempts) != len(rejected.Attempts)+1 {
		t.Fatalf("attempts grew from %d to %d, want exactly one more", len(rejected.Attempts), len(view.Attempts))
	}
	if actionHint(t, body) {
		t.Fatal("a rejected invoice must not carry the check-status hint")
	}
}

// TestResendAfterIssuerCorrectionReflectsIt: the operator fixed the razón
// social; the resent document carries it, under the same clave, and the
// invoice's Issuer snapshot now reads the corrected value.
func TestResendAfterIssuerCorrectionReflectsIt(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)
	first, _ := sriStub.lastReceived()
	if !bytes.Contains(first.signedXML, []byte("Red Planet Tribe S.A.S.")) {
		t.Fatal("setup: first document does not carry the original razón social")
	}

	corrected := validEcuadorIssuerBody()
	corrected["razon_social"] = "RED PLANET TRIBE CORREGIDA S.A.S."
	putEcuadorIssuer(t, sriEnv, sessionID, corrected)

	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusOK, receivedSOAP(accessKey) })
	resp, body := resendInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "resend", resp, body)

	second, _ := sriStub.lastReceived()
	if second.accessKey != first.accessKey {
		t.Fatalf("resend carried clave %q, want %q", second.accessKey, first.accessKey)
	}
	if !bytes.Contains(second.signedXML, []byte("RED PLANET TRIBE CORREGIDA S.A.S.")) {
		t.Fatal("resent document does not carry the corrected razón social")
	}
	if bytes.Contains(second.signedXML, []byte("Red Planet Tribe S.A.S.")) {
		t.Fatal("resent document still carries the old razón social")
	}
	if view.Issuer.RazonSocial != "RED PLANET TRIBE CORREGIDA S.A.S." {
		t.Fatalf("invoice issuer snapshot = %q, want the correction", view.Issuer.RazonSocial)
	}
	if view.Issuer.RUC != rejected.Issuer.RUC {
		t.Fatalf("resend changed the snapshot's RUC to %q", view.Issuer.RUC)
	}
}

// TestResendRefusedOnAuthorized: 409, nothing sent, nothing on the ledger.
func TestResendRefusedOnAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	authorized := issueOK(t, sessionID, validInvoiceBody())

	resp, body := resendInvoice(t, sessionID, authorized.ID)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("resend on authorized status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "INVOICE_ALREADY_AUTHORIZED" {
		t.Fatalf("error = %+v, want INVOICE_ALREADY_AUTHORIZED", body.Error)
	}
	if sriStub.receptionCount() != 1 {
		t.Fatalf("reception count=%d, want 1", sriStub.receptionCount())
	}
	after := getInvoice(t, sessionID, authorized.ID)
	if len(after.Attempts) != len(authorized.Attempts) {
		t.Fatal("a refused resend wrote to the ledger")
	}
}

// TestResendErrors43And70LeavePendingWithHint: the SRI already has the
// document (43) or is still processing it (70). The invoice goes to pending
// with the hint to check status; the artifact on file is untouched, because
// what the SRI holds is the first send and not this one.
func TestResendErrors43And70LeavePendingWithHint(t *testing.T) {
	for _, code := range []string{"43", "70"} {
		t.Run("error "+code, func(t *testing.T) {
			env := setupTest(t)
			sessionID := operatorSession(t, env, "operator@example.com")
			issuerReady(t, sessionID)
			rejected := issueRejected(t, sessionID)
			first, _ := sriStub.lastReceived()

			sriStub.setReception(func(accessKey string) (int, string) {
				return http.StatusOK, returnedSOAP(accessKey, sriMessage(code, "CLAVE DE ACCESO "+code, "", "ERROR"))
			})
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, inProcessingSOAP(accessKey)
			})
			resp, body := resendInvoice(t, sessionID, rejected.ID)
			view := actionOK(t, "resend", resp, body)

			if view.Status != "pending" {
				t.Fatalf("status=%q, want pending", view.Status)
			}
			if !actionHint(t, body) {
				t.Fatal("want the check-status hint")
			}
			if len(view.Messages) == 0 || view.Messages[0].Identifier != code {
				t.Fatalf("messages = %+v, want the SRI's %s kept", view.Messages, code)
			}
			if !bytes.Equal(storedSignedXML(t, rejected.ID), first.signedXML) {
				t.Fatalf("error %s replaced the document on file", code)
			}
			if len(view.Attempts) <= len(rejected.Attempts) {
				t.Fatal("resend wrote nothing to the ledger")
			}
			if a := view.Attempts[len(rejected.Attempts)]; a.Operation != "submit" || a.Outcome != "received" {
				t.Fatalf("resend attempt = %+v, want submit/received", a)
			}
		})
	}
}

// TestResendFromPendingAfterTransportFailure: the first send never reached
// the SRI; the resend does, and the invoice is authorized.
func TestResendFromPendingAfterTransportFailure(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusInternalServerError, "" })
	pending := issueOK(t, sessionID, validInvoiceBody())
	if pending.Status != "pending" {
		t.Fatalf("setup: status=%q", pending.Status)
	}
	if actionHint(t, envelopeOf(t, sessionID, pending.ID)) {
		t.Fatal("a pending invoice the SRI never took must not carry the check-status hint")
	}

	sriStub.reset()
	resp, body := resendInvoice(t, sessionID, pending.ID)
	view := actionOK(t, "resend", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", view.Status)
	}
	second, _ := sriStub.lastReceived()
	if second.accessKey != pending.Ecuador.AccessKey {
		t.Fatalf("resend carried clave %q, want %q", second.accessKey, pending.Ecuador.AccessKey)
	}
}

// TestInvoiceActionsAreOperatorOnly: a signed-in Member off the allowlist is
// refused both actions.
func TestInvoiceActionsAreOperatorOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	authorized := issueOK(t, sessionID, validInvoiceBody())
	member := verifyOTP(t, env, "member@example.com")

	for _, path := range []string{"/check", "/resend"} {
		resp, body := sriEnv.post(t, invoicesPath+"/"+authorized.ID+path, nil, authHeader(member))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s as a Member: status=%d error=%+v, want 403 FORBIDDEN", path, resp.StatusCode, body.Error)
		}
	}
}

func envelopeOf(t *testing.T, sessionID, id string) envelope {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	return env
}

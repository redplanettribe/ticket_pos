package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
)

// The Sale Invoice Drainer (#474, parent #471, ADR 0060): the owed Sale
// Invoice a paid House checkout left behind is signed, submitted and polled
// to authorized with nobody waiting — by the internal drain endpoint a
// scheduler ticks, and by the kick right after the commit. Everything is
// asserted through the drain response, the operator invoicing detail, what
// the fake SRI received, and the sequence table where no surface says "no
// number was consumed".
//
// The checkout is made through the PayPhone app and the drain through the
// SRI app: three apps over one database, exactly as the earlier tickets
// drive them. The invoicing clock is the SRI app's, and the ladder tests
// move it.

const saleInvoiceDrainPath = "/api/v1/internal/sale-invoices/drain"

// saleInvoiceDrainResult is the drain's own tally: counts and states only.
type saleInvoiceDrainResult struct {
	Claimed        int            `json:"claimed"`
	Authorized     int            `json:"authorized"`
	Pending        int            `json:"pending"`
	NeedsAttention int            `json:"needs_attention"`
	Delivered      int            `json:"delivered"`
	Withdrawn      int            `json:"withdrawn"`
	Failed         int            `json:"failed"`
	Standing       map[string]int `json:"standing"`
}

func drainSaleInvoices(t *testing.T) saleInvoiceDrainResult {
	t.Helper()
	resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var result saleInvoiceDrainResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return result
}

// drainedInvoiceView is the detail as the Drainer's tests read it: the
// signed-side facts beside the Sale-side ones.
type drainedInvoiceView struct {
	saleInvoiceDetailView
	Environment         *string `json:"environment"`
	IssuedOn            *string `json:"issued_on"`
	IssuedBy            *string `json:"issued_by"`
	CreditedByInvoiceID *string `json:"credited_by_invoice_id"`
	CheckStatusHint     bool    `json:"check_status_hint"`
	Messages            []struct {
		Identifier string `json:"identifier"`
		Message    string `json:"message"`
		Type       string `json:"type"`
	} `json:"messages"`
	EcuadorFull *struct {
		AccessKey           string  `json:"access_key"`
		Secuencial          int64   `json:"secuencial"`
		Ambiente            string  `json:"ambiente"`
		AuthorizationNumber *string `json:"authorization_number"`
	} `json:"ecuador"`
	AttemptRows []struct {
		Operation string `json:"operation"`
		Outcome   string `json:"outcome"`
	} `json:"attempts"`
}

func getDrainedInvoice(t *testing.T, sessionID, id string) drainedInvoiceView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view drainedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// atInvoicingClock moves the SRI app's invoicing clock — the one the
// Drainer signs, schedules and measures the 24 h rule by — and puts it back
// after the test.
func atInvoicingClock(t *testing.T, at time.Time) {
	t.Helper()
	sriApp.InvoicingService.WithClock(func() time.Time { return at })
	t.Cleanup(func() { sriApp.InvoicingService.WithClock(func() time.Time { return fixedClock }) })
}

// houseSaleOwed designates the test Organization, publishes a House Event
// at the given price and makes one paid checkout of `quantity` tickets,
// returning the operator session and the owed Sale Invoice's id.
func houseSaleOwed(t *testing.T, env *testEnv, priceCents, quantity int) (operatorSessionID, invoiceID string) {
	t.Helper()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID = operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", priceCents, 10)
	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, quantity)))
	list := getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 1 || list.Data[0].Status != "owed" {
		t.Fatalf("setup: invoices = %+v; want one owed Sale Invoice", list.Data)
	}
	return operatorSessionID, list.Data[0].ID
}

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func nextAttemptAt(t *testing.T, view drainedInvoiceView) time.Time {
	t.Helper()
	if view.NextAttemptAt == nil {
		t.Fatalf("invoice %s has no next_attempt_at", view.ID)
	}
	at, err := time.Parse(time.RFC3339, *view.NextAttemptAt)
	if err != nil {
		t.Fatalf("next_attempt_at %q: %v", *view.NextAttemptAt, err)
	}
	return at
}

func receivedFactura(t *testing.T) *etree.Document {
	t.Helper()
	received, ok := sriStub.lastReceived()
	if !ok {
		t.Fatal("the SRI received nothing")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(received.signedXML); err != nil {
		t.Fatalf("parse received factura: %v", err)
	}
	return doc
}

func xmlText(t *testing.T, doc *etree.Document, path string) string {
	t.Helper()
	el := doc.FindElement(path)
	if el == nil {
		t.Fatalf("received factura lacks %s", path)
	}
	return el.Text()
}

// TestSaleInvoiceDrainerAuthorizesOnFirstDrain: the usual case. One drain
// signs the owed document from its stored snapshot — consuming secuencial
// 1 only now — submits it, polls it to authorized, and the operator is
// looking at an authorized Sale Invoice signed by the Drainer. The factura
// the fake SRI received validates against the XSD and states what the
// buyer paid: cédula code 05, "Ana Lopez", the Ticket Type and the Event on
// the line, the backed-out unit price, and totals equal to the Payment to
// the cent. The same round delivers it (#475): delivered_at is set.
func TestSaleInvoiceDrainerAuthorizesOnFirstDrain(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 2)
	issuerReady(t, operatorSessionID)
	if n := sequenceRows(t, env); n != 0 {
		t.Fatalf("sequence rows before the drain = %d; want none", n)
	}

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Pending != 0 || result.NeedsAttention != 0 || result.Failed != 0 {
		t.Fatalf("drain = %+v; want one document claimed and authorized", result)
	}
	if result.Standing["authorized"] != 1 || len(result.Standing) != 1 {
		t.Fatalf("standing = %v; want authorized: 1 and nothing else", result.Standing)
	}

	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.Kind != "sale" {
		t.Fatalf("detail = %s %s; want an authorized Sale Invoice", detail.Kind, detail.Status)
	}
	if detail.Number == nil || *detail.Number != "001-001-000000001" {
		t.Fatalf("number = %v; want 001-001-000000001, the first number consumed at signing", detail.Number)
	}
	if detail.IssuedBy == nil || *detail.IssuedBy != "sale-invoice-drainer" {
		t.Fatalf("issued_by = %v; want the Drainer", detail.IssuedBy)
	}
	if detail.Environment == nil || *detail.Environment != "test" {
		t.Fatalf("environment = %v; want test, copied from the Issuer at signing", detail.Environment)
	}
	if detail.IssuedOn == nil || *detail.IssuedOn != "2026-07-07" {
		t.Fatalf("issued_on = %v; want 2026-07-07, the signing date in Guayaquil", detail.IssuedOn)
	}
	if detail.Issuer == nil || detail.Issuer.RUC != "1790012345001" {
		t.Fatalf("issuer snapshot = %+v; want the Issuer as it stood at signing", detail.Issuer)
	}
	if detail.EcuadorFull == nil || detail.EcuadorFull.Secuencial != 1 || detail.EcuadorFull.AuthorizationNumber == nil {
		t.Fatalf("ecuador = %+v; want secuencial 1 and an authorization", detail.EcuadorFull)
	}
	if !detail.HasAuthorizationXML || detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
		t.Fatalf("authorization xml %v delivered_at %v next_attempt_at %v; want the SRI's XML on file, delivered, nothing due", detail.HasAuthorizationXML, detail.DeliveredAt, detail.NextAttemptAt)
	}
	if len(detail.AttemptRows) < 2 || detail.AttemptRows[0].Operation != "submit" || detail.AttemptRows[0].Outcome != "received" || detail.AttemptRows[len(detail.AttemptRows)-1].Outcome != "authorized" {
		t.Fatalf("attempts = %+v; want a received submit and an authorized query", detail.AttemptRows)
	}
	if n := sequenceRows(t, env); n != 1 {
		t.Fatalf("sequence rows after the drain = %d; want the one started at signing", n)
	}

	// What the SRI received.
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want one", n)
	}
	received, _ := sriStub.lastReceived()
	validateReceivedAgainstXSD(t, received.signedXML)
	if received.accessKey != detail.EcuadorFull.AccessKey {
		t.Fatalf("received clave %s; want the document's %s", received.accessKey, detail.EcuadorFull.AccessKey)
	}
	doc := receivedFactura(t)
	checks := map[string]string{
		"/factura/infoTributaria/ambiente":                              "1",
		"/factura/infoTributaria/secuencial":                            "000000001",
		"/factura/infoFactura/fechaEmision":                             "07/07/2026",
		"/factura/infoFactura/tipoIdentificacionComprador":              "05",
		"/factura/infoFactura/identificacionComprador":                  validCedula,
		"/factura/infoFactura/razonSocialComprador":                     "Ana Lopez",
		"/factura/infoFactura/totalSinImpuestos":                        "19.39",
		"/factura/infoFactura/totalDescuento":                           "0.00",
		"/factura/infoFactura/importeTotal":                             "22.30",
		"/factura/infoFactura/pagos/pago/formaPago":                     "19",
		"/factura/infoFactura/pagos/pago/total":                         "22.30",
		"/factura/detalles/detalle/cantidad":                            "2.00",
		"/factura/detalles/detalle/precioUnitario":                      "9.695",
		"/factura/detalles/detalle/precioTotalSinImpuesto":              "19.39",
		"/factura/detalles/detalle/impuestos/impuesto/codigoPorcentaje": "4",
		"/factura/detalles/detalle/impuestos/impuesto/valor":            "2.91",
	}
	for path, want := range checks {
		if got := xmlText(t, doc, path); got != want {
			t.Fatalf("%s = %q; want %q", path, got, want)
		}
	}
	if desc := xmlText(t, doc, "/factura/detalles/detalle/descripcion"); !strings.Contains(desc, "GA") || !strings.Contains(desc, "House Fest") {
		t.Fatalf("descripcion = %q; want the Ticket Type and the Event", desc)
	}
	if doc.FindElement("/factura/infoFactura/direccionComprador") != nil {
		t.Fatal("the factura carries a direccionComprador; a Sale Invoice has no address")
	}
	if got := xmlText(t, doc, "/factura/infoAdicional/campoAdicional[@nombre='email']"); got != "guest@example.com" {
		t.Fatalf("email campoAdicional = %q; want the Sale's email", got)
	}

	// Settled: nothing is due, and the legal artifact is neither checked nor
	// resent.
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing claimed", again)
	}
	if resp, body := checkInvoice(t, operatorSessionID, invoiceID); resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_ALREADY_AUTHORIZED" {
		t.Fatalf("check on an authorized Sale Invoice: status=%d error=%+v; want 409 INVOICE_ALREADY_AUTHORIZED", resp.StatusCode, body.Error)
	}
}

// TestSaleInvoiceDrainerRetriesAnUnreachableSRIOnTheLadder: the SRI answers
// 500 to recepción. The first drain signs the document (the number is
// consumed: the SRI may well have read it) and leaves it pending, due in a
// minute; a drain before then finds nothing; the round a minute later fails
// again and schedules five minutes out; the round after that finds the SRI
// recovered and authorizes — the SAME clave and number every time.
func TestSaleInvoiceDrainerRetriesAnUnreachableSRIOnTheLadder(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusInternalServerError, "" })

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("first drain = %+v; want the document claimed and left pending", result)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "pending" || detail.Number == nil || *detail.Number != "001-001-000000001" {
		t.Fatalf("after an unreachable SRI: status %s number %v; want pending with its number kept", detail.Status, detail.Number)
	}
	if len(detail.AttemptRows) != 1 || detail.AttemptRows[0].Operation != "submit" || detail.AttemptRows[0].Outcome != "error" {
		t.Fatalf("attempts = %+v; want one errored submit", detail.AttemptRows)
	}
	if at := nextAttemptAt(t, detail); !at.Equal(fixedClock.Add(time.Minute)) {
		t.Fatalf("next_attempt_at = %s; want one minute after signing (%s)", at, fixedClock.Add(time.Minute))
	}
	clave := detail.EcuadorFull.AccessKey

	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain before the ladder's first rung = %+v; want nothing claimed", again)
	}

	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if second := drainSaleInvoices(t); second.Claimed != 1 || second.Pending != 1 {
		t.Fatalf("drain a minute later = %+v; want the document claimed and still pending", second)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if at := nextAttemptAt(t, detail); !at.Equal(fixedClock.Add(6 * time.Minute)) {
		t.Fatalf("next_attempt_at after the second failure = %s; want five minutes on (%s)", at, fixedClock.Add(6*time.Minute))
	}
	if len(detail.AttemptRows) != 2 {
		t.Fatalf("attempts = %+v; want two errored submits", detail.AttemptRows)
	}

	sriStub.answerAsUsual()
	atInvoicingClock(t, fixedClock.Add(6*time.Minute))
	if third := drainSaleInvoices(t); third.Claimed != 1 || third.Authorized != 1 {
		t.Fatalf("drain with the SRI recovered = %+v; want authorized", third)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || *detail.Number != "001-001-000000001" || detail.EcuadorFull.AccessKey != clave || detail.NextAttemptAt != nil {
		t.Fatalf("after recovery: %s %s clave %s next %v; want authorized under the same number and clave, nothing due", detail.Status, *detail.Number, detail.EcuadorFull.AccessKey, detail.NextAttemptAt)
	}
	if n := sriStub.receptionCount(); n != 3 {
		t.Fatalf("the SRI received %d submissions; want three, each the same document", n)
	}
	if n := sequenceRows(t, env); n != 1 {
		t.Fatalf("sequence rows = %d", n)
	}
	var last int64
	if err := env.db.QueryRow(`SELECT last_secuencial FROM invoicing_sequences_ec`).Scan(&last); err != nil || last != 1 {
		t.Fatalf("last_secuencial = %d (%v); want 1: retries consume no numbers", last, err)
	}
}

// TestSaleInvoiceDrainerParksARefusalForTheOperator: NO AUTORIZADO parks the
// document needs_attention with the SRI's messages, stops the ladder, and
// is the operator's to remedy — Check status works on a Sale Invoice as on
// a manual one, and heals it when the SRI changes its mind.
func TestSaleInvoiceDrainerParksARefusalForTheOperator(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	if result.Standing["needs_attention"] != 1 {
		t.Fatalf("standing = %v", result.Standing)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" || detail.NextAttemptAt != nil || detail.CheckStatusHint {
		t.Fatalf("refused document: status %s next %v hint %v; want needs_attention, off the ladder, no hint", detail.Status, detail.NextAttemptAt, detail.CheckStatusHint)
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Identifier != "65" {
		t.Fatalf("messages = %+v; want the SRI's 65 verbatim", detail.Messages)
	}
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain after a refusal = %+v; want nothing claimed — a refusal is not retried", again)
	}

	// The operator's remedy.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	resp, body := checkInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "check", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("check status on the refused Sale Invoice = %s; want authorized", view.Status)
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the one submission — a check sends nothing", n)
	}
}

// TestSaleInvoiceDrainerResendWorksOnARejectedSaleInvoice: DEVUELTA parks
// the document too, and Resend sends the same document under the same clave
// exactly as it does for a manual Tax Invoice.
func TestSaleInvoiceDrainerResendWorksOnARejectedSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})

	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked needs_attention", result)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" || len(detail.Messages) != 1 || detail.Messages[0].Identifier != "35" {
		t.Fatalf("rejected document = %s %+v; want needs_attention with the SRI's 35", detail.Status, detail.Messages)
	}
	clave := detail.EcuadorFull.AccessKey

	sriStub.answerAsUsual()
	resp, body := resendInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "resend", resp, body)
	if view.Status != "authorized" || view.Ecuador.AccessKey != clave || view.Number != "001-001-000000001" {
		t.Fatalf("resend = %s %s %s; want authorized under the same clave and number", view.Status, view.Ecuador.AccessKey, view.Number)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the original and the resend", n)
	}
	received, _ := sriStub.lastReceived()
	if received.accessKey != clave {
		t.Fatalf("resend carried clave %s; want %s", received.accessKey, clave)
	}
}

// TestSaleInvoiceDrainerPollsSilencePastADayThenHeals: the SRI keeps the
// document EN PROCESAMIENTO. Every round polls and never resubmits; at 24
// hours the document is parked needs_attention with the check-status hint,
// still on the ladder; a late AUTORIZADO on the next hourly round heals it.
func TestSaleInvoiceDrainerPollsSilencePastADayThenHeals(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })

	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("first drain = %+v; want pending", result)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "pending" || !detail.CheckStatusHint {
		t.Fatalf("after RECIBIDA / EN PROCESAMIENTO: status %s hint %v; want pending with the hint", detail.Status, detail.CheckStatusHint)
	}
	if at := nextAttemptAt(t, detail); !at.Equal(fixedClock.Add(time.Minute)) {
		t.Fatalf("next_attempt_at = %s; want a minute on", at)
	}
	attemptsAfterFirst := len(detail.AttemptRows)

	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if second := drainSaleInvoices(t); second.Claimed != 1 || second.Pending != 1 {
		t.Fatalf("drain a minute later = %+v; want still pending", second)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if len(detail.AttemptRows) != attemptsAfterFirst+1 || detail.AttemptRows[len(detail.AttemptRows)-1].Operation != "query" {
		t.Fatalf("attempts = %+v; want exactly one more query — a held document is polled, never resubmitted", detail.AttemptRows)
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d submissions; want one", n)
	}

	atInvoicingClock(t, fixedClock.Add(24*time.Hour))
	if late := drainSaleInvoices(t); late.Claimed != 1 || late.NeedsAttention != 1 {
		t.Fatalf("drain at 24 h = %+v; want the document parked needs_attention", late)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "needs_attention" || !detail.CheckStatusHint {
		t.Fatalf("at 24 h: status %s hint %v; want needs_attention with the hint", detail.Status, detail.CheckStatusHint)
	}
	if at := nextAttemptAt(t, detail); !at.Equal(fixedClock.Add(25 * time.Hour)) {
		t.Fatalf("next_attempt_at at 24 h = %s; want hourly polling to go on (%s)", at, fixedClock.Add(25*time.Hour))
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d submissions; want still one", n)
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(25*time.Hour))
	if healed := drainSaleInvoices(t); healed.Claimed != 1 || healed.Authorized != 1 {
		t.Fatalf("drain with a late AUTORIZADO = %+v; want authorized", healed)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.NextAttemptAt != nil || !detail.HasAuthorizationXML {
		t.Fatalf("healed document: %s next %v xml %v; want authorized, nothing due, the SRI's XML on file", detail.Status, detail.NextAttemptAt, detail.HasAuthorizationXML)
	}
}

// TestSaleInvoiceDrainerParksAnUnsignableDocumentWithoutANumber: no Issuer,
// then no certificate, then an expired one — each parks the document
// needs_attention at once, says why in the platform's own voice, consumes
// no number and sends nothing, and is retried hourly so the operator's fix
// heals it. The Issuer's environment at SIGNING time is what the document
// gets: moved to production before the last round, it signs in production.
func TestSaleInvoiceDrainerParksAnUnsignableDocumentWithoutANumber(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)

	expectParked := func(round string, at time.Time, code string) {
		t.Helper()
		result := drainSaleInvoices(t)
		if result.Claimed != 1 || result.NeedsAttention != 1 {
			t.Fatalf("%s: drain = %+v; want parked needs_attention", round, result)
		}
		detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
		if detail.Status != "needs_attention" || detail.Number != nil || detail.EcuadorFull != nil || detail.Issuer != nil {
			t.Fatalf("%s: status %s number %v ecuador %v issuer %v; want needs_attention and unsigned", round, detail.Status, detail.Number, detail.EcuadorFull, detail.Issuer)
		}
		if len(detail.Messages) != 1 || detail.Messages[0].Identifier != code || detail.Messages[0].Type != "PLATFORM" {
			t.Fatalf("%s: messages = %+v; want one PLATFORM message %s", round, detail.Messages, code)
		}
		if len(detail.AttemptRows) != 0 {
			t.Fatalf("%s: attempts = %+v; want none — nothing was asked of the SRI", round, detail.AttemptRows)
		}
		if next := nextAttemptAt(t, detail); !next.Equal(at.Add(time.Hour)) {
			t.Fatalf("%s: next_attempt_at = %s; want an hour on (%s)", round, next, at.Add(time.Hour))
		}
		if n := sequenceRows(t, env); n != 0 {
			t.Fatalf("%s: sequence rows = %d; want no number consumed", round, n)
		}
		if n := sriStub.receptionCount(); n != 0 {
			t.Fatalf("%s: the SRI received %d documents; want none", round, n)
		}
	}

	expectParked("no Issuer", fixedClock, "ISSUER_NOT_FOUND")

	putEcuadorIssuer(t, sriEnv, operatorSessionID, validEcuadorIssuerBody())
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	expectParked("no certificate", fixedClock.Add(time.Hour), "CERTIFICATE_NOT_UPLOADED")

	uploadCertificate(t, sriEnv, operatorSessionID, throwawayP12ValidUntil(t, rsaKey(t), "1790012345001", "s3cret", fixedClock.Add(-24*time.Hour)), "s3cret")
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	expectParked("expired certificate", fixedClock.Add(2*time.Hour), "CERTIFICATE_EXPIRED")

	// Fixed, and moved to production: the next hourly round signs there.
	uploadCertificate(t, sriEnv, operatorSessionID, rsaP12(t, "1790012345001", "s3cret"), "s3cret")
	body := validEcuadorIssuerBody()
	body["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, operatorSessionID, body)
	atInvoicingClock(t, fixedClock.Add(3*time.Hour))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 {
		t.Fatalf("drain once fixed = %+v; want authorized", result)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.Number == nil || *detail.Number != "001-001-000000001" {
		t.Fatalf("healed document = %s %v", detail.Status, detail.Number)
	}
	if detail.Environment == nil || *detail.Environment != "production" || detail.EcuadorFull.Ambiente != "2" {
		t.Fatalf("environment = %v ambiente %s; want production, the Issuer's environment at signing", detail.Environment, detail.EcuadorFull.Ambiente)
	}
	for _, m := range detail.Messages {
		if m.Type == "PLATFORM" {
			t.Fatalf("messages after authorization = %+v; want the platform's parking message gone", detail.Messages)
		}
	}
	if got := xmlText(t, receivedFactura(t), "/factura/infoTributaria/ambiente"); got != "2" {
		t.Fatalf("received ambiente = %s; want 2", got)
	}
}

// TestSaleInvoiceDrainerNeverWorksOneDocumentTwice: two drains at the same
// moment, with the SRI slow enough that they overlap, claim the one owed
// document once between them: it is signed once, submitted once and
// authorized once.
func TestSaleInvoiceDrainerNeverWorksOneDocumentTwice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setSleep(150 * time.Millisecond)

	var wg sync.WaitGroup
	results := make([]saleInvoiceDrainResult, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = drainSaleInvoices(t)
		}(i)
	}
	wg.Wait()
	claimed := results[0].Claimed + results[1].Claimed
	authorized := results[0].Authorized + results[1].Authorized
	if claimed != 1 || authorized != 1 {
		t.Fatalf("two concurrent drains = %+v and %+v; want the document claimed and authorized exactly once between them", results[0], results[1])
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want one", n)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || *detail.Number != "001-001-000000001" {
		t.Fatalf("detail = %s %v", detail.Status, detail.Number)
	}
	var last int64
	if err := env.db.QueryRow(`SELECT last_secuencial FROM invoicing_sequences_ec`).Scan(&last); err != nil || last != 1 {
		t.Fatalf("last_secuencial = %d (%v); want 1", last, err)
	}
}

// TestSaleInvoiceKickIssuesRightAfterCheckout: with the kick on, a paid
// House checkout returns at once while the SRI is slow — or down — and the
// document is worked in the background: pending after the SRI's failure,
// authorized within seconds once the SRI answers. The checkout response
// never waits on the SRI and never fails because of it.
func TestSaleInvoiceKickIssuesRightAfterCheckout(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	issuerReady(t, operatorSessionID)
	payphoneApp.InvoicingService.WithSaleInvoiceKick(true)
	t.Cleanup(func() { payphoneApp.InvoicingService.WithSaleInvoiceKick(false) })

	waitFor := func(id string, want func(drainedInvoiceView) bool) drainedInvoiceView {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			detail := getDrainedInvoice(t, operatorSessionID, id)
			if want(detail) {
				return detail
			}
			if time.Now().After(deadline) {
				t.Fatalf("invoice %s never reached the wanted state: %s with %d attempts", id, detail.Status, len(detail.AttemptRows))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// The SRI is down and slow: the checkout must not notice.
	sriStub.setSleep(time.Second)
	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusInternalServerError, "" })
	started := time.Now()
	ref := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	if took := time.Since(started); took >= time.Second {
		t.Fatalf("the checkout took %s with the SRI sleeping a second; the kick must never delay the response", took)
	}
	if lastConfirmation(t, env).Reference != ref {
		t.Fatalf("the Sale Confirmation was not sent for %s", ref)
	}
	list := getSaleInvoiceList(t, operatorSessionID)
	down := waitFor(list.Data[0].ID, func(v drainedInvoiceView) bool { return len(v.AttemptRows) >= 1 })
	if down.Status != "pending" || down.Number == nil || down.NextAttemptAt == nil {
		t.Fatalf("after the kick against a down SRI: %s number %v next %v; want pending, numbered, on the ladder", down.Status, down.Number, down.NextAttemptAt)
	}

	// The SRI is back: the next checkout's factura follows in seconds.
	sriStub.answerAsUsual()
	paidCheckoutApproved(t, "house-fest", checkoutBody("second@example.com", "Bea", "Mora", cartLine(gaID, 1)))
	list = getSaleInvoiceList(t, operatorSessionID)
	var secondID string
	for _, row := range list.Data {
		if row.ID != down.ID {
			secondID = row.ID
		}
	}
	up := waitFor(secondID, func(v drainedInvoiceView) bool { return v.Status == "authorized" })
	if up.IssuedBy == nil || *up.IssuedBy != "sale-invoice-drainer" || up.Number == nil || *up.Number != "001-001-000000002" {
		t.Fatalf("kicked document = issued_by %v number %v; want the Drainer's second number", up.IssuedBy, up.Number)
	}
	// Nothing left for the scheduled drain but the one the SRI failed.
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("scheduled drain right after the kicks = %+v; want nothing due yet", result)
	}
}

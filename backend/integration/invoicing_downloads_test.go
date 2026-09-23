package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

// Handing over the documents (#456): the signed XML exactly as the SRI
// received it, the SRI's authorization XML exactly as it answered, and the
// data the RIDE is rendered from — all through the HTTP seam. A test never
// reads the module's tables: what the fake SRI saw and what the operator
// downloads are compared byte for byte.

func signedXMLPath(id string) string        { return invoicesPath + "/" + id + "/xml" }
func authorizationXMLPath(id string) string { return invoicesPath + "/" + id + "/authorization-xml" }
func ridePath(id string) string             { return invoicesPath + "/" + id + "/ride" }

// getRaw fetches a path without decoding an envelope: downloads are bytes,
// not JSON.
func (env *testEnv) getRaw(t *testing.T, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func decodeErrorEnvelope(t *testing.T, body []byte) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope from %q: %v", body, err)
	}
	return env
}

var autorizacionInSOAP = regexp.MustCompile(`(?s)<autorizacion>(.*?)</autorizacion>`)

// expectedAuthorizationXML is the legal artifact as the client keeps it: the
// SRI's <autorizacion> element verbatim, under an XML declaration.
func expectedAuthorizationXML(t *testing.T, accessKey string) []byte {
	t.Helper()
	m := autorizacionInSOAP.FindStringSubmatch(authorizedSOAP(accessKey))
	if m == nil {
		t.Fatal("the fake's AUTORIZADO answer carries no <autorizacion>")
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><autorizacion>` + m[1] + `</autorizacion>`)
}

func assertXMLDownload(t *testing.T, resp *http.Response, body []byte, filename string, want []byte) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml; charset=utf-8" {
		t.Fatalf("Content-Type=%q, want application/xml; charset=utf-8", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="`+filename+`"` {
		t.Fatalf("Content-Disposition=%q, want attachment; filename=%q", cd, filename)
	}
	// The document carries the buyer's name, Tax ID and purchase: no cache on
	// the way may keep a copy of it.
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", cc)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("downloaded bytes differ from the stored document:\n got %d bytes: %.200s\nwant %d bytes: %.200s", len(body), body, len(want), want)
	}
}

// TestDownloadSignedXMLIsTheDocumentTheSRIReceived: the signed XML download
// is, byte for byte, what the fake SRI received on recepción, served as XML
// under a filename built from the clave.
func TestDownloadSignedXMLIsTheDocumentTheSRIReceived(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", view.Status)
	}
	got, ok := sriStub.lastReceived()
	if !ok {
		t.Fatal("the fake SRI received nothing")
	}

	resp, body := sriEnv.getRaw(t, signedXMLPath(view.ID), authHeader(sessionID))
	assertXMLDownload(t, resp, body, view.Ecuador.AccessKey+".xml", got.signedXML)
	if !bytes.HasPrefix(body, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Fatalf("signed XML does not start with the declaration: %.80s", body)
	}
}

// TestDownloadSignedXMLAvailableInEveryStatus: a rejected and a pending
// invoice hand over their signed XML just the same — the document exists
// from the moment the number was consumed.
func TestDownloadSignedXMLAvailableInEveryStatus(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "", "ERROR"))
	})
	rejected := issueOK(t, sessionID, validInvoiceBody())
	if rejected.Status != "rejected" {
		t.Fatalf("status=%q, want rejected", rejected.Status)
	}
	rejectedDoc, _ := sriStub.lastReceived()

	sriStub.reset()
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, inProcessingSOAP(accessKey)
	})
	pending := issueOK(t, sessionID, validInvoiceBody())
	if pending.Status != "pending" {
		t.Fatalf("status=%q, want pending", pending.Status)
	}
	pendingDoc, _ := sriStub.lastReceived()

	resp, body := sriEnv.getRaw(t, signedXMLPath(rejected.ID), authHeader(sessionID))
	assertXMLDownload(t, resp, body, rejected.Ecuador.AccessKey+".xml", rejectedDoc.signedXML)

	resp, body = sriEnv.getRaw(t, signedXMLPath(pending.ID), authHeader(sessionID))
	assertXMLDownload(t, resp, body, pending.Ecuador.AccessKey+".xml", pendingDoc.signedXML)

	if bytes.Equal(rejectedDoc.signedXML, pendingDoc.signedXML) {
		t.Fatal("two invoices handed over the same document")
	}
}

// TestDownloadAuthorizationXMLIsTheSRIsAnswer: an authorized invoice hands
// over the SRI's <autorizacion> exactly as the fake returned it.
func TestDownloadAuthorizationXMLIsTheSRIsAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "authorized" || !view.HasAuthorizationXML {
		t.Fatalf("status=%q has_authorization_xml=%v, want authorized with the XML on file", view.Status, view.HasAuthorizationXML)
	}

	resp, body := sriEnv.getRaw(t, authorizationXMLPath(view.ID), authHeader(sessionID))
	assertXMLDownload(t, resp, body, view.Ecuador.AccessKey+"-autorizacion.xml", expectedAuthorizationXML(t, view.Ecuador.AccessKey))
	if !bytes.Contains(body, []byte("<numeroAutorizacion>"+view.Ecuador.AccessKey+"</numeroAutorizacion>")) {
		t.Fatalf("authorization XML does not carry the authorization number: %.300s", body)
	}
}

// TestDownloadAuthorizationXMLRefusedUnlessAuthorized: a rejected, a
// not-authorized and a pending invoice have no legal artifact to hand over,
// and say so with a not-found-style refusal rather than an empty file.
func TestDownloadAuthorizationXMLRefusedUnlessAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	cases := []struct {
		status string
		setup  func()
	}{
		{"rejected", func() {
			sriStub.setReception(func(accessKey string) (int, string) {
				return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "", "ERROR"))
			})
		}},
		{"not_authorized", func() {
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
			})
		}},
		{"pending", func() {
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, inProcessingSOAP(accessKey)
			})
		}},
	}
	for _, c := range cases {
		sriStub.reset()
		c.setup()
		view := issueOK(t, sessionID, validInvoiceBody())
		if view.Status != c.status {
			t.Fatalf("status=%q, want %s", view.Status, c.status)
		}
		resp, body := sriEnv.getRaw(t, authorizationXMLPath(view.ID), authHeader(sessionID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status=%d body=%.200s, want 404", c.status, resp.StatusCode, body)
		}
		errEnv := decodeErrorEnvelope(t, body)
		if errEnv.Error == nil || errEnv.Error.Code != "AUTHORIZATION_XML_NOT_FOUND" || string(errEnv.Data) != "null" {
			t.Fatalf("%s: envelope=%+v, want AUTHORIZATION_XML_NOT_FOUND with null data", c.status, errEnv)
		}
	}
}

// TestDownloadRIDEIsAPDFNamedAfterTheClave (#494, ADR 0062): an authorized
// invoice hands over its RIDE as a PDF under the clave's name, rendered on
// the request — and rendered the same way twice, since nothing is stored
// and the buyer's copy must be the operator's.
func TestDownloadRIDEIsAPDFNamedAfterTheClave(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", view.Status)
	}

	resp, body := sriEnv.getRaw(t, ridePath(view.ID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%.200s, want 200", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type=%q, want application/pdf", ct)
	}
	if cd, want := resp.Header.Get("Content-Disposition"), `attachment; filename="`+view.Ecuador.AccessKey+`.pdf"`; cd != want {
		t.Fatalf("Content-Disposition=%q, want %q", cd, want)
	}
	// The document carries the buyer's name, Tax ID and purchase: no cache on
	// the way may keep a copy of it.
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", cc)
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("body is not a PDF: %.40q", body)
	}

	_, again := sriEnv.getRaw(t, ridePath(view.ID), authHeader(sessionID))
	if !bytes.Equal(body, again) {
		t.Fatal("two downloads of the same RIDE differ")
	}
}

// TestDownloadRIDERefusedUnlessAuthorized: a rejected, a not-authorized and
// a pending invoice have no RIDE — one without the authorization number has
// no validity — and say so with the same not-found-style refusal the
// authorization XML gives, never a placeholder PDF.
func TestDownloadRIDERefusedUnlessAuthorized(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	cases := []struct {
		status string
		setup  func()
	}{
		{"rejected", func() {
			sriStub.setReception(func(accessKey string) (int, string) {
				return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "", "ERROR"))
			})
		}},
		{"not_authorized", func() {
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
			})
		}},
		{"pending", func() {
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, inProcessingSOAP(accessKey)
			})
		}},
	}
	for _, c := range cases {
		sriStub.reset()
		c.setup()
		view := issueOK(t, sessionID, validInvoiceBody())
		if view.Status != c.status {
			t.Fatalf("status=%q, want %s", view.Status, c.status)
		}
		resp, body := sriEnv.getRaw(t, ridePath(view.ID), authHeader(sessionID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status=%d body=%.200s, want 404", c.status, resp.StatusCode, body)
		}
		errEnv := decodeErrorEnvelope(t, body)
		if errEnv.Error == nil || errEnv.Error.Code != "RIDE_NOT_FOUND" || string(errEnv.Data) != "null" {
			t.Fatalf("%s: envelope=%+v, want RIDE_NOT_FOUND with null data", c.status, errEnv)
		}
	}
}

// TestInvoiceDownloadsAreOperatorOnly: all three downloads refuse a missing
// session with 401, a signed-in Org Admin with 403 — org roles grant nothing
// platform-wide — and an operator asking for an invoice that does not exist
// with 404, as JSON envelopes and never as a file.
func TestInvoiceDownloadsAreOperatorOnly(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, operatorSessionID)
	view := issueOK(t, operatorSessionID, validInvoiceBody())

	for _, path := range []string{signedXMLPath(view.ID), authorizationXMLPath(view.ID), ridePath(view.ID)} {
		resp, body := sriEnv.getRaw(t, path, nil)
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusUnauthorized || errEnv.Error == nil || errEnv.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("%s unauthenticated status=%d body=%s, want 401 UNAUTHORIZED", path, resp.StatusCode, body)
		}
		resp, body = sriEnv.getRaw(t, path, authHeader(adminSessionID))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusForbidden || errEnv.Error == nil || errEnv.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s as org_admin status=%d body=%s, want 403 FORBIDDEN", path, resp.StatusCode, body)
		}
	}

	for _, path := range []string{signedXMLPath(uuid.NewString()), authorizationXMLPath(uuid.NewString()), ridePath(uuid.NewString()), signedXMLPath("not-a-uuid"), ridePath("not-a-uuid")} {
		resp, body := sriEnv.getRaw(t, path, authHeader(operatorSessionID))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
			t.Fatalf("%s status=%d body=%s, want 404 INVOICE_NOT_FOUND", path, resp.StatusCode, body)
		}
	}
}

// rideView is what the RIDE page renders, read from the detail endpoint: the
// fields the printed document must carry. No new read endpoint exists for
// the RIDE — this test is the proof that the detail already carries them.
type rideView struct {
	Number      string `json:"number"`
	IssuedOn    string `json:"issued_on"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Currency    string `json:"currency"`
	Issuer      struct {
		RUC                      string  `json:"ruc"`
		RazonSocial              string  `json:"razon_social"`
		NombreComercial          string  `json:"nombre_comercial"`
		DireccionMatriz          string  `json:"direccion_matriz"`
		DireccionEstablecimiento string  `json:"direccion_establecimiento"`
		Establecimiento          string  `json:"establecimiento"`
		PuntoEmision             string  `json:"punto_emision"`
		ObligadoContabilidad     bool    `json:"obligado_contabilidad"`
		Regimen                  string  `json:"regimen"`
		AgenteRetencion          *string `json:"agente_retencion"`
	} `json:"issuer"`
	Recipient struct {
		TaxIDType string `json:"tax_id_type"`
		TaxID     string `json:"tax_id"`
		LegalName string `json:"legal_name"`
		Address   string `json:"address"`
		Email     string `json:"email"`
	} `json:"recipient"`
	Lines []struct {
		Description    string `json:"description"`
		Quantity       string `json:"quantity"`
		UnitPriceCents int64  `json:"unit_price_cents"`
		DiscountCents  int64  `json:"discount_cents"`
		IVARate        string `json:"iva_rate"`
		RatePercent    int    `json:"rate_percent"`
		BaseCents      int64  `json:"base_cents"`
		IVACents       int64  `json:"iva_cents"`
	} `json:"lines"`
	Totals struct {
		ByRate []struct {
			IVARate     string `json:"iva_rate"`
			RatePercent int    `json:"rate_percent"`
			BaseCents   int64  `json:"base_cents"`
			IVACents    int64  `json:"iva_cents"`
		} `json:"by_rate"`
		SubtotalCents int64 `json:"subtotal_cents"`
		DiscountCents int64 `json:"discount_cents"`
		IVACents      int64 `json:"iva_cents"`
		TotalCents    int64 `json:"total_cents"`
	} `json:"totals"`
	PaymentMethod      string `json:"payment_method"`
	PaymentMethodLabel string `json:"payment_method_label"`
	AdditionalFields   []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"additional_fields"`
	Ecuador struct {
		AccessKey           string  `json:"access_key"`
		Estab               string  `json:"estab"`
		PtoEmi              string  `json:"pto_emi"`
		Secuencial          int64   `json:"secuencial"`
		Ambiente            string  `json:"ambiente"`
		AuthorizationNumber *string `json:"authorization_number"`
		AuthorizationDate   *string `json:"authorization_date"`
	} `json:"ecuador"`
}

func fetchRIDE(t *testing.T, sessionID, id string) rideView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK || env.Error != nil {
		t.Fatalf("detail status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view rideView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return view
}

var fortyNineDigits = regexp.MustCompile(`^[0-9]{49}$`)

// TestRIDEDataOnAuthorizedInvoice: the detail an authorized invoice's RIDE is
// rendered from carries the Issuer's identity, the número, the emission date,
// the environment, the clave, the Recipient, the lines with discount and IVA,
// the per-rate subtotals, the totals, the forma de pago label, the additional
// fields, and the authorization number and date.
func TestRIDEDataOnAuthorizedInvoice(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	body := validInvoiceBody()
	body["lines"] = []map[string]any{
		{"description": "Platform Fee - July 2026", "quantity": "2", "unit_price_cents": 10000, "discount_cents": 500, "iva_rate": "15"},
		{"description": "Exempt service", "quantity": "1.5", "unit_price_cents": 1000, "iva_rate": "0"},
	}
	issued := issueOK(t, sessionID, body)
	if issued.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", issued.Status)
	}
	ride := fetchRIDE(t, sessionID, issued.ID)

	issuer := validEcuadorIssuerBody()
	if ride.Issuer.RUC != issuer["ruc"] || ride.Issuer.RazonSocial != issuer["razon_social"] || ride.Issuer.DireccionMatriz != issuer["direccion_matriz"] || ride.Issuer.DireccionEstablecimiento != issuer["direccion_establecimiento"] {
		t.Fatalf("issuer identity = %+v, want the Issuer as saved %+v", ride.Issuer, issuer)
	}
	if ride.Issuer.Establecimiento != "001" || ride.Issuer.PuntoEmision != "001" || ride.Number != "001-001-000000001" {
		t.Fatalf("number = %q from %s-%s", ride.Number, ride.Issuer.Establecimiento, ride.Issuer.PuntoEmision)
	}
	if ride.IssuedOn != fixedClock.Format("2006-01-02") {
		t.Fatalf("issued_on=%q", ride.IssuedOn)
	}
	if ride.Environment != "test" || ride.Ecuador.Ambiente != "1" {
		t.Fatalf("environment=%q ambiente=%q, want test/1", ride.Environment, ride.Ecuador.Ambiente)
	}
	if !fortyNineDigits.MatchString(ride.Ecuador.AccessKey) {
		t.Fatalf("clave=%q, want 49 digits for the barcode", ride.Ecuador.AccessKey)
	}
	if ride.Recipient.LegalName != "ORGANIZACION EJEMPLO S.A." || ride.Recipient.TaxID != "1790012345001" || ride.Recipient.TaxIDType != "ruc" || ride.Recipient.Address == "" || ride.Recipient.Email != "billing@example.com" {
		t.Fatalf("recipient = %+v", ride.Recipient)
	}
	if len(ride.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(ride.Lines))
	}
	l0, l1 := ride.Lines[0], ride.Lines[1]
	if l0.Quantity != "2.00" || l0.UnitPriceCents != 10000 || l0.DiscountCents != 500 || l0.RatePercent != 15 || l0.BaseCents != 19500 || l0.IVACents != 2925 {
		t.Fatalf("line 1 = %+v", l0)
	}
	if l1.Quantity != "1.50" || l1.UnitPriceCents != 1000 || l1.DiscountCents != 0 || l1.RatePercent != 0 || l1.BaseCents != 1500 || l1.IVACents != 0 {
		t.Fatalf("line 2 = %+v", l1)
	}
	if len(ride.Totals.ByRate) != 2 || ride.Totals.ByRate[0].RatePercent != 15 || ride.Totals.ByRate[0].BaseCents != 19500 || ride.Totals.ByRate[0].IVACents != 2925 || ride.Totals.ByRate[1].IVARate != "0" || ride.Totals.ByRate[1].BaseCents != 1500 {
		t.Fatalf("by_rate = %+v", ride.Totals.ByRate)
	}
	if ride.Totals.SubtotalCents != 21000 || ride.Totals.DiscountCents != 500 || ride.Totals.IVACents != 2925 || ride.Totals.TotalCents != 23925 {
		t.Fatalf("totals = %+v", ride.Totals)
	}
	if ride.PaymentMethod != "20" || ride.PaymentMethodLabel == "" {
		t.Fatalf("payment method = %q label=%q", ride.PaymentMethod, ride.PaymentMethodLabel)
	}
	if len(ride.AdditionalFields) != 1 || ride.AdditionalFields[0].Name != "Periodo" || ride.AdditionalFields[0].Value != "2026-07" {
		t.Fatalf("additional fields = %+v", ride.AdditionalFields)
	}
	if ride.Ecuador.AuthorizationNumber == nil || *ride.Ecuador.AuthorizationNumber != ride.Ecuador.AccessKey || ride.Ecuador.AuthorizationDate == nil || *ride.Ecuador.AuthorizationDate == "" {
		t.Fatalf("authorization = %v / %v, want the number and date", ride.Ecuador.AuthorizationNumber, ride.Ecuador.AuthorizationDate)
	}
}

// TestRIDEDataOnUnauthorizedInvoiceHasNoAuthorizationNumber: a pending,
// rejected or not-authorized invoice's detail carries the status and no
// authorization number or date, so the RIDE cannot print one by mistake.
func TestRIDEDataOnUnauthorizedInvoiceHasNoAuthorizationNumber(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
	})
	issued := issueOK(t, sessionID, validInvoiceBody())
	ride := fetchRIDE(t, sessionID, issued.ID)
	if ride.Status != "not_authorized" {
		t.Fatalf("status=%q, want not_authorized", ride.Status)
	}
	if ride.Ecuador.AuthorizationNumber != nil || ride.Ecuador.AuthorizationDate != nil {
		t.Fatalf("authorization = %v / %v on a not_authorized invoice, want none", ride.Ecuador.AuthorizationNumber, ride.Ecuador.AuthorizationDate)
	}
	// The clave still prints: it identifies the document even when it is not
	// yet — or never — a valid one.
	if !fortyNineDigits.MatchString(ride.Ecuador.AccessKey) {
		t.Fatalf("clave=%q", ride.Ecuador.AccessKey)
	}
}

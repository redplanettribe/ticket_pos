package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer receives the factura (#475, parent #471, ADR 0060): once the
// Drainer has an authorized document it mails the buyer one message in the
// Sale's Locale — the Event and the Sale Confirmation reference in the
// words, the signed XML and its RIDE attached (#496, ADR 0062), a link to
// the Sale in the Customer Area — and records delivered_at; a delivery the
// sender refused is retried on the ladder while the document stays
// authorized, and a document whose RIDE cannot be rendered leaves the queue
// undelivered instead. The Customer Area reads the
// Sale's documents through a customer route and downloads the XML through
// another, both gated on the Customer Session that owns the Sale or a
// Confirmation Link to it, and neither reachable by another Customer or by
// nobody.
//
// Everything is asserted through the drain response, the captured mail, the
// customer routes and the operator detail; the fake SRI's reception count
// says a delivery retry never resubmits anything.

func customerDocumentsPath(saleID string) string {
	return "/api/v1/customer/ticket-sales/" + saleID + "/tax-documents"
}

func customerDocumentXMLPath(saleID, documentID string) string {
	return customerDocumentsPath(saleID) + "/" + documentID + "/xml"
}

func customerDocumentRIDEPath(saleID, documentID string) string {
	return customerDocumentsPath(saleID) + "/" + documentID + "/ride"
}

// customerDocumentView is one document as the buyer reads it: the kind, a
// state in the platform's words and never the authority's, and where to
// download it — the signed XML and its RIDE (#497) — once there is
// something to download.
type customerDocumentView struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	DownloadURL *string `json:"download_url"`
	RideURL     *string `json:"ride_url"`
}

// assertCustomerDownloads holds a listed document's two download paths to
// the state it is in: both offered, at the buyer's own paths, once
// authorized; neither before.
func assertCustomerDownloads(t *testing.T, label string, doc customerDocumentView, saleID string, offered bool) {
	t.Helper()
	if offered != (doc.DownloadURL != nil) || offered != (doc.RideURL != nil) {
		t.Fatalf("%s: download_url=%v ride_url=%v; want both offered=%v", label, doc.DownloadURL, doc.RideURL, offered)
	}
	if !offered {
		return
	}
	if *doc.DownloadURL != customerDocumentXMLPath(saleID, doc.ID) {
		t.Fatalf("%s: download_url = %s; want %s", label, *doc.DownloadURL, customerDocumentXMLPath(saleID, doc.ID))
	}
	if *doc.RideURL != customerDocumentRIDEPath(saleID, doc.ID) {
		t.Fatalf("%s: ride_url = %s; want %s", label, *doc.RideURL, customerDocumentRIDEPath(saleID, doc.ID))
	}
}

// assertRIDEDownload is a buyer's RIDE download held against the operator's
// (#497, ADR 0062 §4): a PDF under the clave's filename, and the same bytes.
func assertRIDEDownload(t *testing.T, resp *http.Response, body []byte, filename string, want []byte) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%.200s, want 200", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type=%q, want application/pdf", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="`+filename+`"` {
		t.Fatalf("Content-Disposition=%q, want attachment; filename=%q", cd, filename)
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("body is not a PDF: %.40q", body)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("the buyer's RIDE (%d bytes) differs from the operator's (%d bytes)", len(body), len(want))
	}
}

func listCustomerDocuments(t *testing.T, sessionID, saleID string) ([]customerDocumentView, json.RawMessage) {
	t.Helper()
	resp, body := sriEnv.get(t, customerDocumentsPath(saleID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer documents status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var docs []customerDocumentView
	if err := json.Unmarshal(body.Data, &docs); err != nil {
		t.Fatalf("decode customer documents: %v", err)
	}
	return docs, body.Data
}

// ticketTypeIDBySlug reads the one Ticket Type of the test Organization's
// Event with the slug through the public catalog, for a second checkout on
// an Event a helper published.
func ticketTypeIDBySlug(t *testing.T, env *testEnv, eventSlug string) string {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event %q status=%d error=%+v", eventSlug, resp.StatusCode, body.Error)
	}
	var detail struct {
		TicketTypes []struct {
			ID string `json:"id"`
		} `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if len(detail.TicketTypes) != 1 {
		t.Fatalf("public event %q lists %d ticket types; want the one the helper published", eventSlug, len(detail.TicketTypes))
	}
	return detail.TicketTypes[0].ID
}

func deliveriesSent(t *testing.T, env *testEnv) []platform.TaxDocumentDelivery {
	t.Helper()
	return env.email.TaxDocumentDeliveriesSent()
}

// operatorRIDE downloads a document's RIDE through the operator route: the
// one PDF there is, rendered on demand (#494, ADR 0062), so a delivery's
// attachment can be held against it byte for byte.
func operatorRIDE(t *testing.T, sessionID, invoiceID string) []byte {
	t.Helper()
	resp, body := sriEnv.getRaw(t, ridePath(invoiceID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator RIDE status=%d body=%.200s; want 200", resp.StatusCode, body)
	}
	return body
}

// attachmentNames is a delivery's attachment filenames, for a failure
// message.
func attachmentNames(mail platform.TaxDocumentDelivery) []string {
	names := make([]string, 0, len(mail.Attachments))
	for _, a := range mail.Attachments {
		names = append(names, a.Filename)
	}
	return names
}

// assertDeliveryCarriesXMLAndRIDE checks the two attachments every Tax
// Document delivery carries (#496, ADR 0062): the signed XML first, exactly
// the bytes the SRI received, under <clave>.xml; then the RIDE under
// <clave>.pdf, a PDF, and the same bytes the operator downloads — buyer and
// operator hold one artifact.
func assertDeliveryCarriesXMLAndRIDE(t *testing.T, mail platform.TaxDocumentDelivery, accessKey string, signedXML, ride []byte) {
	t.Helper()
	if len(mail.Attachments) != 2 {
		t.Fatalf("%d attachments on the delivery; want the XML and the RIDE", len(mail.Attachments))
	}
	xml, pdf := mail.Attachments[0], mail.Attachments[1]
	if xml.Filename != accessKey+".xml" || xml.ContentType != "application/xml; charset=utf-8" {
		t.Fatalf("first attachment = %s %s; want <clave>.xml as XML", xml.Filename, xml.ContentType)
	}
	if !bytes.Equal(xml.Body, signedXML) {
		t.Fatalf("the attached XML is not the document the SRI received (%d vs %d bytes)", len(xml.Body), len(signedXML))
	}
	if pdf.Filename != accessKey+".pdf" || pdf.ContentType != "application/pdf" {
		t.Fatalf("second attachment = %s %s; want <clave>.pdf as PDF", pdf.Filename, pdf.ContentType)
	}
	if len(pdf.Body) == 0 || !bytes.HasPrefix(pdf.Body, []byte("%PDF-")) {
		t.Fatalf("the attached RIDE is not a PDF: %.40q", pdf.Body)
	}
	if !bytes.Equal(pdf.Body, ride) {
		t.Fatalf("the attached RIDE (%d bytes) differs from the operator's download (%d bytes); buyer and operator must hold the same document", len(pdf.Body), len(ride))
	}
}

// houseSaleAuthorized is houseSaleOwed drained to authorized (and, with the
// sender working, delivered), returning the operator session, the invoice
// id and the Sale's id.
func houseSaleAuthorized(t *testing.T, env *testEnv) (operatorSessionID, invoiceID, saleID string) {
	t.Helper()
	operatorSessionID, invoiceID = houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("setup drain = %+v; want authorized", result)
	}
	return operatorSessionID, invoiceID, saleIDByRef(t, env, lastConfirmation(t, env).Reference)
}

// TestSaleInvoiceDeliveryMailsTheBuyerOnceAuthorized: the same drain that
// authorizes the document mails it. One message, to the Sale's email,
// naming the Event and the Sale Confirmation reference, with the very bytes
// the SRI received attached under the clave's filename, the RIDE beside it
// (#496), and a link to the Sale's card in the Customer Area; delivered_at
// is recorded, nothing is due, and a second drain sends nothing more.
func TestSaleInvoiceDeliveryMailsTheBuyerOnceAuthorized(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 2)
	issuerReady(t, operatorSessionID)
	ref := lastConfirmation(t, env).Reference
	saleID := saleIDByRef(t, env, ref)

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain = %+v; want one document authorized and delivered", result)
	}

	sent := deliveriesSent(t, env)
	if len(sent) != 1 {
		t.Fatalf("%d delivery mails were sent; want exactly one", len(sent))
	}
	mail := sent[0]
	if mail.To != "guest@example.com" || mail.Kind != "sale" || mail.EventName != "House Fest" || mail.Reference != ref {
		t.Fatalf("delivery = to %s kind %s event %s ref %s; want the buyer, a sale document, House Fest, %s", mail.To, mail.Kind, mail.EventName, mail.Reference, ref)
	}
	if mail.Locale != platform.LocaleEN {
		t.Fatalf("delivery locale = %s; want en for a sale made in English", mail.Locale)
	}
	wantLink := "http://storefront.example/tickets#sale-" + saleID
	if mail.CustomerAreaURL != wantLink {
		t.Fatalf("customer area link = %q; want %q", mail.CustomerAreaURL, wantLink)
	}
	subject, text := mail.Subject(), mail.Text()
	if !strings.Contains(subject, "House Fest") || !strings.Contains(subject, "factura") {
		t.Fatalf("subject = %q; want the Event and the document named", subject)
	}
	for _, want := range []string{"House Fest", ref, wantLink} {
		if !strings.Contains(text, want) {
			t.Fatalf("delivery text lacks %q:\n%s", want, text)
		}
	}

	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	received, _ := sriStub.lastReceived()
	assertDeliveryCarriesXMLAndRIDE(t, mail, detail.EcuadorFull.AccessKey, received.signedXML, operatorRIDE(t, operatorSessionID, invoiceID))
	if text := mail.Text(); !strings.Contains(text, "and its RIDE") {
		t.Fatalf("delivery text does not name the RIDE:\n%s", text)
	}
	if detail.Status != "authorized" || detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
		t.Fatalf("after delivery: status %s delivered_at %v next %v; want authorized, delivered, nothing due", detail.Status, detail.DeliveredAt, detail.NextAttemptAt)
	}

	if again := drainSaleInvoices(t); again.Claimed != 0 || again.Delivered != 0 {
		t.Fatalf("second drain = %+v; want nothing claimed and nothing sent", again)
	}
	if n := len(deliveriesSent(t, env)); n != 1 {
		t.Fatalf("%d delivery mails after the second drain; want still one", n)
	}
}

// TestSaleInvoiceDeliveryIsWrittenInTheSaleLocale: a sale made in Spanish is
// delivered in Spanish — the Sale Locale, as the receipt before it.
func TestSaleInvoiceDeliveryIsWrittenInTheSaleLocale(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	body := checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1))
	body["locale"] = "es"
	ref := paidCheckoutApproved(t, "house-fest", body)
	issuerReady(t, operatorSessionID)

	if result := drainSaleInvoices(t); result.Delivered != 1 {
		t.Fatalf("drain = %+v; want delivered", result)
	}
	sent := deliveriesSent(t, env)
	if len(sent) != 1 || sent[0].Locale != platform.LocaleES {
		t.Fatalf("deliveries = %+v; want one in Spanish", sent)
	}
	if subject := sent[0].Subject(); !strings.Contains(subject, "Su factura") || !strings.Contains(subject, "House Fest") {
		t.Fatalf("subject = %q; want Spanish naming the Event", subject)
	}
	if text := sent[0].Text(); !strings.Contains(text, ref) || strings.Contains(text, "Your ") {
		t.Fatalf("text = %q; want Spanish carrying the reference", text)
	}
}

// TestSaleInvoiceDeliveryRetriesAfterASenderFailure: the sender is down when
// the document authorizes. The document is authorized all the same, nothing
// is delivered, and delivery alone is due again on the ladder; a drain
// before then does nothing; the round after the sender recovers mails it —
// once — without asking the SRI anything again.
func TestSaleInvoiceDeliveryRetriesAfterASenderFailure(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	env.email.FailWith(errors.New("resend returned 503"))
	t.Cleanup(func() { env.email.FailWith(nil) })

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 0 {
		t.Fatalf("drain with the sender down = %+v; want authorized and not delivered", result)
	}
	if n := len(deliveriesSent(t, env)); n != 0 {
		t.Fatalf("%d delivery mails were sent with the sender down; want none", n)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.DeliveredAt != nil {
		t.Fatalf("after a failed delivery: status %s delivered_at %v; want authorized and undelivered", detail.Status, detail.DeliveredAt)
	}
	if at := nextAttemptAt(t, detail); !at.Equal(fixedClock.Add(time.Minute)) {
		t.Fatalf("next_attempt_at = %s; want the ladder's first rung (%s)", at, fixedClock.Add(time.Minute))
	}
	attempts := len(detail.AttemptRows)

	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain before the rung = %+v; want nothing claimed", again)
	}

	env.email.FailWith(nil)
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	retry := drainSaleInvoices(t)
	if retry.Claimed != 1 || retry.Authorized != 1 || retry.Delivered != 1 {
		t.Fatalf("drain with the sender back = %+v; want the document claimed for delivery alone and delivered", retry)
	}
	if n := len(deliveriesSent(t, env)); n != 1 {
		t.Fatalf("%d delivery mails after the retry; want exactly one", n)
	}
	detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
		t.Fatalf("after the retry: status %s delivered_at %v next %v; want authorized, delivered, nothing due", detail.Status, detail.DeliveredAt, detail.NextAttemptAt)
	}
	if len(detail.AttemptRows) != attempts {
		t.Fatalf("attempts grew from %d to %d over a delivery retry; a delivery asks the SRI nothing", attempts, len(detail.AttemptRows))
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the one submission", n)
	}
}

// TestSaleInvoiceDeliveryParksADocumentWhoseRIDECannotRender (#496, ADR
// 0062 §2): the ladder is for the transport alone. An authorized Credit Note
// whose RIDE cannot be rendered is not mailed at all — not the XML by
// itself — and is not due again: it stays authorized with delivered_at and
// next_attempt_at both null, the same dead end as a document with no Sale,
// and a later drain leaves it there. The SRI is asked nothing more.
//
// The failure is forced from stored data, as a real one would be: the
// Credit Note's RIDE reads the modified document out of its signed XML, so
// its stored bytes are corrupted in place — through SQL, since no API
// alters a signed document, which is the point of it being signed — after
// the SRI authorized it and before the Drainer's delivery pass. The sender
// being down for that first pass is what separates the two: the same drain
// otherwise authorizes and delivers.
func TestSaleInvoiceDeliveryParksADocumentWhoseRIDECannotRender(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, _, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)
	noteID := creditNoteOf(t, operatorSessionID)

	// Authorized with the sender down: undelivered, on the ladder.
	env.email.FailWith(errors.New("resend returned 503"))
	t.Cleanup(func() { env.email.FailWith(nil) })
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 0 {
		t.Fatalf("drain with the sender down = %+v; want the Credit Note authorized and not delivered", result)
	}
	note := getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.DeliveredAt != nil || note.NextAttemptAt == nil {
		t.Fatalf("credit note before the retry = %s delivered %v next %v; want authorized, undelivered, due", note.Status, note.DeliveredAt, note.NextAttemptAt)
	}
	sent := len(deliveriesSent(t, env))
	receptions := sriStub.receptionCount()

	// The stored signed XML is no longer XML. Read and written directly:
	// nothing on the API rewrites a signed document.
	if _, err := env.db.Exec(`UPDATE invoicing_invoices SET signed_xml = $2 WHERE id = $1`, noteID, []byte("<notaCredito><infoNotaCredito>")); err != nil {
		t.Fatalf("corrupt the Credit Note's signed XML: %v", err)
	}

	env.email.FailWith(nil)
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Delivered != 0 || result.Failed != 0 {
		t.Fatalf("drain with the RIDE unrenderable = %+v; want the Credit Note claimed and not delivered", result)
	}
	if n := len(deliveriesSent(t, env)); n != sent {
		t.Fatalf("%d delivery mails after the failed render; want still %d — the XML is never mailed alone", n, sent)
	}
	note = getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.DeliveredAt != nil || note.NextAttemptAt != nil {
		t.Fatalf("credit note after the failed render = %s delivered %v next %v; want authorized, undelivered and off the queue", note.Status, note.DeliveredAt, note.NextAttemptAt)
	}

	// Off the queue means off it: an hour on, nothing claims it.
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain an hour later = %+v; want nothing claimed — a failed render is not on the ladder", again)
	}
	if n := len(deliveriesSent(t, env)); n != sent {
		t.Fatalf("%d delivery mails after the later drain; want still %d", n, sent)
	}
	if n := sriStub.receptionCount(); n != receptions {
		t.Fatalf("the SRI received %d documents; want still %d — a delivery asks it nothing", n, receptions)
	}
}

// TestCustomerSaleDocumentsFollowTheDocumentState: the buyer's own read of
// the Sale's documents says "on its way" for owed, pending and
// needs_attention alike — never the SRI's words — and offers the download
// only once authorized; a Sale that owes nothing lists nothing.
func TestCustomerSaleDocumentsFollowTheDocumentState(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	ref := lastConfirmation(t, env).Reference
	saleID := saleIDByRef(t, env, ref)
	buyer := buyerSession(t, env, "guest@example.com")

	expect := func(state, status string, download bool) {
		t.Helper()
		docs, raw := listCustomerDocuments(t, buyer, saleID)
		if len(docs) != 1 || docs[0].ID != invoiceID || docs[0].Kind != "sale" || docs[0].Status != status {
			t.Fatalf("%s: documents = %+v; want one sale document %s", state, docs, status)
		}
		assertCustomerDownloads(t, state, docs[0], saleID, download)
		if strings.Contains(string(raw), "FECHA EMISION") || strings.Contains(string(raw), "messages") {
			t.Fatalf("%s: the buyer's read carries the authority's words:\n%s", state, raw)
		}
	}

	expect("owed", "on_its_way", false)

	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("drain = %+v; want pending", result)
	}
	expect("pending", "on_its_way", false)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want needs_attention", result)
	}
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); len(detail.Messages) != 1 {
		t.Fatalf("operator detail messages = %+v; want the SRI's refusal on file for the operator", detail.Messages)
	}
	expect("needs_attention", "on_its_way", false)

	// The operator's Check status heals it; the Drainer then delivers it.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	if resp, body := checkInvoice(t, operatorSessionID, invoiceID); resp.StatusCode != http.StatusOK {
		t.Fatalf("check status=%d error=%+v", resp.StatusCode, body.Error)
	}
	expect("authorized", "authorized", true)
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Delivered != 1 {
		t.Fatalf("drain after the operator's check = %+v; want the healed document delivered", result)
	}
	if n := len(deliveriesSent(t, env)); n != 1 {
		t.Fatalf("%d deliveries; want one", n)
	}

	// A Sale that owes nothing: the Organization undesignated, the next sale
	// lists no document at all — an empty list, not null.
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	if resp, body := env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("undesignate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	plainRef := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(ticketTypeIDBySlug(t, env, "house-fest"), 1)))
	docs, raw := listCustomerDocuments(t, buyer, saleIDByRef(t, env, plainRef))
	if len(docs) != 0 || strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("documents of a sale that owes none = %s; want []", raw)
	}
}

// TestCustomerSaleDocumentDownloadIsGatedOnTheSale: the owning Customer
// Session and a Confirmation Link to the Sale download the XML the SRI
// received; the link session cannot see another Sale of the same buyer;
// another Customer sees an empty list and no download; nobody at all is
// refused at the door; and the operator routes are exactly as they were.
func TestCustomerSaleDocumentDownloadIsGatedOnTheSale(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID, saleID := houseSaleAuthorized(t, env)
	received, _ := sriStub.lastReceived()
	filename := getDrainedInvoice(t, operatorSessionID, invoiceID).EcuadorFull.AccessKey + ".xml"
	path := customerDocumentXMLPath(saleID, invoiceID)

	// The owner.
	owner := buyerSession(t, env, "guest@example.com")
	resp, body := sriEnv.getRaw(t, path, authHeader(owner))
	assertXMLDownload(t, resp, body, filename, received.signedXML)

	// A Confirmation Link to this Sale.
	// Redeemed through the app that signed it: the PayPhone app minted the
	// receipt, and each app derives its own link secret in this suite.
	_, link := redeemConfirmationLinkOK(t, payphoneEnv, lastConfirmationLinkToken(t, env), "")
	resp, body = sriEnv.getRaw(t, path, authHeader(link))
	assertXMLDownload(t, resp, body, filename, received.signedXML)

	// The same buyer's OTHER Sale, through that link: invisible.
	otherRef := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(ticketTypeIDBySlug(t, env, "house-fest"), 1)))
	otherSaleID := saleIDByRef(t, env, otherRef)
	otherInvoiceID := ""
	for _, row := range getSaleInvoiceList(t, operatorSessionID).Data {
		if row.ID != invoiceID {
			otherInvoiceID = row.ID
		}
	}
	if docs, _ := listCustomerDocuments(t, link, otherSaleID); len(docs) != 0 {
		t.Fatalf("a Confirmation Link session lists %+v on another Sale; want nothing", docs)
	}
	resp, body = sriEnv.getRaw(t, customerDocumentXMLPath(otherSaleID, otherInvoiceID), authHeader(link))
	if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("link session on another Sale's document: status=%d body=%s; want 404 INVOICE_NOT_FOUND", resp.StatusCode, body)
	}
	// And the owner's own owed document has nothing to download yet.
	resp, body = sriEnv.getRaw(t, customerDocumentXMLPath(otherSaleID, otherInvoiceID), authHeader(owner))
	if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("owner on an owed document: status=%d body=%s; want 404 INVOICE_NOT_FOUND", resp.StatusCode, body)
	}

	// Another Customer.
	other := customerSignIn(t, env, "other@example.com")
	if docs, _ := listCustomerDocuments(t, other, saleID); len(docs) != 0 {
		t.Fatalf("another Customer lists %+v; want nothing", docs)
	}
	resp, body = sriEnv.getRaw(t, path, authHeader(other))
	if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("another Customer's download: status=%d body=%s; want 404 INVOICE_NOT_FOUND", resp.StatusCode, body)
	}

	// Nobody.
	for _, p := range []string{path, customerDocumentsPath(saleID)} {
		resp, body = sriEnv.getRaw(t, p, nil)
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusUnauthorized || errEnv.Error == nil || errEnv.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("%s anonymous: status=%d body=%s; want 401 UNAUTHORIZED", p, resp.StatusCode, body)
		}
	}

	// A document that is not this Sale's, and a path that is not an id.
	for _, p := range []string{customerDocumentXMLPath(saleID, uuid.NewString()), customerDocumentXMLPath(saleID, "not-a-uuid"), customerDocumentXMLPath(uuid.NewString(), invoiceID)} {
		resp, body = sriEnv.getRaw(t, p, authHeader(owner))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
			t.Fatalf("%s: status=%d body=%s; want 404 INVOICE_NOT_FOUND", p, resp.StatusCode, body)
		}
	}

	// The operator route is untouched: the operator downloads, the buyer's
	// session does not open it.
	resp, body = sriEnv.getRaw(t, signedXMLPath(invoiceID), authHeader(operatorSessionID))
	assertXMLDownload(t, resp, body, filename, received.signedXML)
	resp, body = sriEnv.getRaw(t, signedXMLPath(invoiceID), authHeader(owner))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a Customer Session on the operator download: status=%d body=%s; want 401", resp.StatusCode, body)
	}
}

// TestCustomerSaleDocumentRIDEIsGatedOnTheSale (#497, ADR 0062): the RIDE
// stands behind the gate the XML does and refuses with the same one word.
// The owning Customer Session and a Confirmation Link to the Sale download
// a PDF under the clave's filename that is byte for byte the operator's;
// the link session cannot reach another Sale of the same buyer; an owed
// document has no RIDE yet; another Customer is told INVOICE_NOT_FOUND —
// never RIDE_NOT_FOUND, a word the XML download would not say; nobody at
// all is refused at the door; and an id that is not this Sale's, or not an
// id, is the same not-found.
func TestCustomerSaleDocumentRIDEIsGatedOnTheSale(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID, saleID := houseSaleAuthorized(t, env)
	filename := getDrainedInvoice(t, operatorSessionID, invoiceID).EcuadorFull.AccessKey + ".pdf"
	want := operatorRIDE(t, operatorSessionID, invoiceID)
	path := customerDocumentRIDEPath(saleID, invoiceID)

	// The owner.
	owner := buyerSession(t, env, "guest@example.com")
	resp, body := sriEnv.getRaw(t, path, authHeader(owner))
	assertRIDEDownload(t, resp, body, filename, want)

	// A Confirmation Link to this Sale, redeemed through the app that signed it.
	_, link := redeemConfirmationLinkOK(t, payphoneEnv, lastConfirmationLinkToken(t, env), "")
	resp, body = sriEnv.getRaw(t, path, authHeader(link))
	assertRIDEDownload(t, resp, body, filename, want)

	// The same buyer's OTHER Sale, through that link: invisible. And through
	// the owner's own session: owed, so nothing to render yet.
	otherRef := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(ticketTypeIDBySlug(t, env, "house-fest"), 1)))
	otherSaleID := saleIDByRef(t, env, otherRef)
	otherInvoiceID := ""
	for _, row := range getSaleInvoiceList(t, operatorSessionID).Data {
		if row.ID != invoiceID {
			otherInvoiceID = row.ID
		}
	}
	for label, session := range map[string]string{"link session on another Sale's document": link, "owner on an owed document": owner} {
		resp, body = sriEnv.getRaw(t, customerDocumentRIDEPath(otherSaleID, otherInvoiceID), authHeader(session))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
			t.Fatalf("%s: status=%d body=%s; want 404 INVOICE_NOT_FOUND", label, resp.StatusCode, body)
		}
	}
	if docs, _ := listCustomerDocuments(t, owner, otherSaleID); len(docs) != 1 || docs[0].RideURL != nil || docs[0].DownloadURL != nil {
		t.Fatalf("the owed document lists %+v; want it on its way with neither download", docs)
	}

	// Another Customer.
	other := customerSignIn(t, env, "other@example.com")
	resp, body = sriEnv.getRaw(t, path, authHeader(other))
	if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
		t.Fatalf("another Customer's RIDE download: status=%d body=%s; want 404 INVOICE_NOT_FOUND", resp.StatusCode, body)
	}

	// Nobody.
	resp, body = sriEnv.getRaw(t, path, nil)
	if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusUnauthorized || errEnv.Error == nil || errEnv.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("anonymous RIDE download: status=%d body=%s; want 401 UNAUTHORIZED", resp.StatusCode, body)
	}

	// A document that is not this Sale's, and a path that is not an id.
	for _, p := range []string{customerDocumentRIDEPath(saleID, uuid.NewString()), customerDocumentRIDEPath(saleID, "not-a-uuid"), customerDocumentRIDEPath(uuid.NewString(), invoiceID)} {
		resp, body = sriEnv.getRaw(t, p, authHeader(owner))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
			t.Fatalf("%s: status=%d body=%s; want 404 INVOICE_NOT_FOUND", p, resp.StatusCode, body)
		}
	}

	// The buyer's session does not open the operator's RIDE route.
	resp, body = sriEnv.getRaw(t, ridePath(invoiceID), authHeader(owner))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a Customer Session on the operator RIDE: status=%d body=%s; want 401", resp.StatusCode, body)
	}
}

// TestSaleInvoiceDeliveryHappensOnceWhenTwoDrainsOverlap: the document is
// pending and held by the SRI when a round authorizes it; the sender is
// slow, and a second drain starts while the first is still mailing. The
// document stays that round's — leased through its delivery — so the
// second drain claims nothing, the buyer gets ONE mail, and delivered_at
// is recorded once. Before this held, the authorization released the
// lease and the second drain mailed the buyer again.
func TestSaleInvoiceDeliveryHappensOnceWhenTwoDrainsOverlap(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("setup drain = %+v; want pending", result)
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	env.email.SlowTaxDocumentDeliveryBy(600 * time.Millisecond)
	t.Cleanup(func() { env.email.SlowTaxDocumentDeliveryBy(0) })
	atInvoicingClock(t, fixedClock.Add(time.Minute))

	var wg sync.WaitGroup
	var first, second saleInvoiceDrainResult
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = drainSaleInvoices(t)
	}()
	// Well inside the first round's send: it has polled, applied the
	// authorization and is waiting on the sender.
	time.Sleep(200 * time.Millisecond)
	second = drainSaleInvoices(t)
	wg.Wait()

	if first.Claimed != 1 || first.Authorized != 1 || first.Delivered != 1 {
		t.Fatalf("first drain = %+v; want the document claimed, authorized and delivered", first)
	}
	if second.Claimed != 0 || second.Delivered != 0 {
		t.Fatalf("overlapping drain = %+v; want nothing claimed — the document is the first round's until it is delivered", second)
	}
	if n := len(deliveriesSent(t, env)); n != 1 {
		t.Fatalf("%d delivery mails were sent by two overlapping drains; want exactly one", n)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
		t.Fatalf("after both drains: status %s delivered_at %v next %v; want authorized, delivered, nothing due", detail.Status, detail.DeliveredAt, detail.NextAttemptAt)
	}
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain afterwards = %+v; want nothing claimed", again)
	}
}

// TestSaleInvoiceDeliveryFollowsAnOperatorsHeal: a document the SRI refused
// is parked off the ladder — nothing is due — until the operator's Check
// status finds a late AUTORIZADO; a document the SRI returned is parked the
// same way until the operator's Resend gets it authorized. Either heal
// leaves delivery due — at once after a Check, which asks and holds
// nothing; at the ladder's first rung after a Resend, whose own RECIBIDA
// put it there — the drain then mails the buyer exactly once, and nothing
// is asked of the SRI again.
func TestSaleInvoiceDeliveryFollowsAnOperatorsHeal(t *testing.T) {
	heals := []struct {
		name string
		park func(t *testing.T)
		heal func(t *testing.T, operatorSessionID, invoiceID string)
		// due is when delivery is due after the heal.
		due time.Time
	}{
		{
			name: "check status",
			due:  fixedClock,
			park: func(t *testing.T) {
				sriStub.setAuthorization(func(accessKey string) (int, string) {
					return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
				})
			},
			heal: func(t *testing.T, operatorSessionID, invoiceID string) {
				t.Helper()
				sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
				resp, body := checkInvoice(t, operatorSessionID, invoiceID)
				if view := actionOK(t, "check", resp, body); view.Status != "authorized" {
					t.Fatalf("check = %s; want authorized", view.Status)
				}
			},
		},
		{
			name: "resend",
			due:  fixedClock.Add(time.Minute),
			park: func(t *testing.T) {
				sriStub.setReception(func(accessKey string) (int, string) {
					return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "", "ERROR"))
				})
			},
			heal: func(t *testing.T, operatorSessionID, invoiceID string) {
				t.Helper()
				sriStub.answerAsUsual()
				resp, body := resendInvoice(t, operatorSessionID, invoiceID)
				if view := actionOK(t, "resend", resp, body); view.Status != "authorized" {
					t.Fatalf("resend = %s; want authorized", view.Status)
				}
			},
		},
	}
	for _, h := range heals {
		t.Run(h.name, func(t *testing.T) {
			env := setupTest(t)
			operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
			issuerReady(t, operatorSessionID)
			h.park(t)
			if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
				t.Fatalf("setup drain = %+v; want the document parked", result)
			}
			if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.NextAttemptAt != nil {
				t.Fatalf("parked document has next_attempt_at %v; want nothing due", detail.NextAttemptAt)
			}

			h.heal(t, operatorSessionID, invoiceID)

			detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
			if detail.Status != "authorized" || detail.DeliveredAt != nil {
				t.Fatalf("healed document: status %s delivered_at %v; want authorized and not yet delivered", detail.Status, detail.DeliveredAt)
			}
			if at := nextAttemptAt(t, detail); !at.Equal(h.due) {
				t.Fatalf("next_attempt_at after the heal = %s; want delivery due at %s", at, h.due)
			}
			if n := len(deliveriesSent(t, env)); n != 0 {
				t.Fatalf("%d delivery mails before any drain; want none — the operator's action delivers nothing itself", n)
			}
			received := sriStub.receptionCount()

			atInvoicingClock(t, h.due)
			result := drainSaleInvoices(t)
			if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
				t.Fatalf("drain after the heal = %+v; want the document claimed for delivery alone and delivered", result)
			}
			if n := len(deliveriesSent(t, env)); n != 1 {
				t.Fatalf("%d delivery mails; want exactly one", n)
			}
			if n := sriStub.receptionCount(); n != received {
				t.Fatalf("the SRI received %d more documents over the delivery; want none", n-received)
			}
			detail = getDrainedInvoice(t, operatorSessionID, invoiceID)
			if detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
				t.Fatalf("after delivery: delivered_at %v next %v; want delivered and nothing due", detail.DeliveredAt, detail.NextAttemptAt)
			}
			if again := drainSaleInvoices(t); again.Claimed != 0 || len(deliveriesSent(t, env)) != 1 {
				t.Fatalf("second drain = %+v with %d mails; want nothing claimed and still one mail", again, len(deliveriesSent(t, env)))
			}
		})
	}
}

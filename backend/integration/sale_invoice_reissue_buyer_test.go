package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's side of a Sale Invoice Reissue (#485, parent #478, ADR 0061):
// they are mailed the Credit Note in words that say the earlier factura was
// cancelled for a correction and a corrected one follows — never that the
// sale was reversed — and then the corrected factura as any factura is
// delivered, each once, when each authorizes. The Customer Area lists the
// whole chain with each document's role: the corrected factura is current,
// the old one superseded and still downloadable, the Credit Note credits it.
// Their Tickets, their Sale Confirmation and their Reversal Window are
// exactly what they were.
//
// Everything is asserted through the reissue endpoint, the drain response,
// the captured mail, the customer routes and the operator detail.

// customerChainDocument is one document as the buyer reads it after #485:
// the four facts of #475 and the chain beside them.
type customerChainDocument struct {
	customerDocumentView
	Role                  string  `json:"role"`
	SupersedesInvoiceID   *string `json:"supersedes_invoice_id"`
	SupersededByInvoiceID *string `json:"superseded_by_invoice_id"`
	CreditsInvoiceID      *string `json:"credits_invoice_id"`
}

func listCustomerChain(t *testing.T, sessionID, saleID string) []customerChainDocument {
	t.Helper()
	_, raw := listCustomerDocuments(t, sessionID, saleID)
	var docs []customerChainDocument
	if err := json.Unmarshal(raw, &docs); err != nil {
		t.Fatalf("decode customer chain: %v", err)
	}
	return docs
}

// chainDocument finds one document of the buyer's list by id.
func chainDocument(t *testing.T, docs []customerChainDocument, id string) customerChainDocument {
	t.Helper()
	for _, d := range docs {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("document %s is not in the buyer's list %+v", id, docs)
	return customerChainDocument{}
}

func strEq(p *string, want string) bool {
	return p != nil && *p == want
}

// TestReissueMailsTheCreditNoteThenTheCorrectedSaleInvoiceEachOnce: the
// SRI holds each document EN PROCESAMIENTO on its first poll, so the two
// authorize on different drains. Nothing is mailed while the Credit Note is
// pending; the drain that authorizes it mails the reissue words — a
// correction, never a reversal — and submits the corrected factura, which
// is not mailed until its own authorization on a later drain, in the
// ordinary factura's words plus one line saying it replaces the earlier
// document. Two mails in all, one per document, and a further drain sends
// nothing.
func TestReissueMailsTheCreditNoteThenTheCorrectedSaleInvoiceEachOnce(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	env.email.Reset()
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID := creditNoteOf(t, operatorSessionID)

	// Drain 1: the Credit Note is submitted and held; nothing is mailed.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 || result.Delivered != 0 {
		t.Fatalf("first drain = %+v; want the Credit Note alone, pending, nothing delivered", result)
	}
	if n := len(deliveriesSent(t, env)); n != 0 {
		t.Fatalf("%d mails while the Credit Note is pending; want none", n)
	}
	noteKey := getReissuedInvoice(t, operatorSessionID, noteID).EcuadorFull.AccessKey

	// Drain 2: the Credit Note authorizes and is mailed; the corrected
	// factura follows to the SRI and is held there — not mailed.
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		if accessKey == noteKey {
			return http.StatusOK, authorizedSOAP(accessKey)
		}
		return http.StatusOK, inProcessingSOAP(accessKey)
	})
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 2 || result.Authorized != 1 || result.Pending != 1 || result.Delivered != 1 {
		t.Fatalf("second drain = %+v; want the Credit Note authorized and delivered, the corrected factura pending", result)
	}
	sent := deliveriesSent(t, env)
	if len(sent) != 1 {
		t.Fatalf("%d mails after the Credit Note authorized; want exactly one", len(sent))
	}
	noteMail := sent[0]
	if noteMail.Kind != "credit_note" || noteMail.Reason != "reissue" || noteMail.To != "guest@example.com" || noteMail.Reference != ref || len(noteMail.Attachments) != 2 || noteMail.Attachments[0].Filename != noteKey+".xml" || noteMail.Attachments[1].Filename != noteKey+".pdf" {
		t.Fatalf("credit note mail = kind %s reason %s to %s ref %s files %v; want the reissue Credit Note to the buyer with its own XML and RIDE", noteMail.Kind, noteMail.Reason, noteMail.To, noteMail.Reference, attachmentNames(noteMail))
	}
	if text := strings.ToLower(noteMail.Text()); !strings.Contains(text, "recipient details can be corrected") || !strings.Contains(text, "corrected tax invoice") || strings.Contains(text, "reversed") || strings.Contains(text, "reversal") {
		t.Fatalf("credit note mail says:\n%s\nwant a correction with a corrected factura to follow, never a reversal", noteMail.Text())
	}
	if after := getReissuedInvoice(t, operatorSessionID, corrected.ID); after.Status != "pending" || after.DeliveredAt != nil {
		t.Fatalf("corrected factura after the second drain = %s delivered %v; want pending and not delivered", after.Status, after.DeliveredAt)
	}

	// Drain 3: the corrected factura authorizes and is mailed as any factura.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(6*time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("third drain = %+v; want the corrected factura authorized and delivered", result)
	}
	sent = deliveriesSent(t, env)
	if len(sent) != 2 {
		t.Fatalf("%d mails after the corrected factura authorized; want exactly two", len(sent))
	}
	after := getReissuedInvoice(t, operatorSessionID, corrected.ID)
	facturaMail := sent[1]
	if facturaMail.Kind != "sale" || facturaMail.Reason != "" || facturaMail.To != "guest@example.com" || facturaMail.Reference != ref || len(facturaMail.Attachments) != 2 || facturaMail.Attachments[0].Filename != after.EcuadorFull.AccessKey+".xml" || facturaMail.Attachments[1].Filename != after.EcuadorFull.AccessKey+".pdf" {
		t.Fatalf("factura mail = kind %s reason %q to %s ref %s files %v; want the ordinary factura delivery to the buyer with the corrected XML and its RIDE", facturaMail.Kind, facturaMail.Reason, facturaMail.To, facturaMail.Reference, attachmentNames(facturaMail))
	}
	if subject := facturaMail.Subject(); !strings.Contains(subject, "factura") || !strings.Contains(subject, "House Fest") || strings.Contains(strings.ToLower(subject), "credit") {
		t.Fatalf("factura mail subject = %q; want the ordinary factura subject", subject)
	}
	text := facturaMail.Text()
	if !strings.Contains(text, "Attached is the tax invoice (factura) for your purchase for House Fest") || !strings.Contains(text, ref) {
		t.Fatalf("factura mail is not the ordinary factura delivery:\n%s", text)
	}
	if !strings.Contains(text, "replaces the earlier one") || strings.Contains(strings.ToLower(text), "reversed") {
		t.Fatalf("factura mail does not say it replaces the earlier document, or says the sale was reversed:\n%s", text)
	}
	if noteMail.Text() == text {
		t.Fatalf("the two mails read the same")
	}
	// The line is the reissue's alone: a first factura never carries it,
	// in either language.
	first := platform.TaxDocumentDelivery{Kind: "sale", CustomerName: "Ana Lopez", EventName: "House Fest", Reference: ref, Locale: platform.LocaleEN}
	if strings.Contains(first.Text(), "replaces") {
		t.Fatalf("an ordinary factura mail says it replaces something:\n%s", first.Text())
	}
	facturaMail.Locale = platform.LocaleES
	if es := facturaMail.Text(); !strings.Contains(es, "Adjuntamos la factura de su compra de House Fest") || !strings.Contains(es, "sustituye a la anterior") || strings.Contains(strings.ToLower(es), "revertid") {
		t.Fatalf("the Spanish factura mail:\n%s", es)
	}

	// Each delivered once, and settled.
	if note := getReissuedInvoice(t, operatorSessionID, noteID); note.DeliveredAt == nil || note.NextAttemptAt != nil {
		t.Fatalf("credit note = delivered %v next %v; want delivered and off the queue", note.DeliveredAt, note.NextAttemptAt)
	}
	if after.DeliveredAt == nil || after.NextAttemptAt != nil {
		t.Fatalf("corrected factura = delivered %v next %v; want delivered and off the queue", after.DeliveredAt, after.NextAttemptAt)
	}
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if again := drainSaleInvoices(t); again.Claimed != 0 || again.Delivered != 0 {
		t.Fatalf("drain afterwards = %+v; want nothing claimed and nothing mailed", again)
	}
	if n := len(deliveriesSent(t, env)); n != 2 {
		t.Fatalf("%d mails after the last drain; want still two", n)
	}
	if n := sriStub.receptionCount(); n != 3 {
		t.Fatalf("the SRI received %d documents; want the factura, the nota de crédito and the corrected factura", n)
	}
}

// TestCustomerSaleDocumentsNameEachDocumentsRoleInTheChain: before any
// reissue a factura is `current` with no links; the moment a reissue is
// owed the old factura is `superseded`, still authorized and downloadable,
// the Credit Note credits it and the corrected factura supersedes it, both
// on their way; once drained everything is authorized and the superseded
// XML still downloads. A second reissue extends the chain to five with the
// newest factura the only current one. Nothing the buyer was never shown —
// no message, no number, no state of the SRI's — travels with the roles.
func TestCustomerSaleDocumentsNameEachDocumentsRoleInTheChain(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	buyer := buyerSession(t, env, "guest@example.com")

	docs := listCustomerChain(t, buyer, saleID)
	if len(docs) != 1 || docs[0].ID != facturaID || docs[0].Role != "current" || docs[0].SupersedesInvoiceID != nil || docs[0].SupersededByInvoiceID != nil || docs[0].CreditsInvoiceID != nil {
		t.Fatalf("before any reissue: documents = %+v; want the factura current with no links", docs)
	}

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID := creditNoteOf(t, operatorSessionID)

	docs = listCustomerChain(t, buyer, saleID)
	if len(docs) != 3 {
		t.Fatalf("after the reissue: %d documents; want three", len(docs))
	}
	old := chainDocument(t, docs, facturaID)
	if old.Kind != "sale" || old.Role != "superseded" || old.Status != "authorized" || old.DownloadURL == nil || !strEq(old.SupersededByInvoiceID, corrected.ID) || old.SupersedesInvoiceID != nil || old.CreditsInvoiceID != nil {
		t.Fatalf("superseded factura = %+v; want role superseded, still authorized and downloadable, superseded by the corrected one", old)
	}
	note := chainDocument(t, docs, noteID)
	if note.Kind != "credit_note" || note.Role != "credit_note" || note.Status != "on_its_way" || note.DownloadURL != nil || !strEq(note.CreditsInvoiceID, facturaID) || note.SupersedesInvoiceID != nil || note.SupersededByInvoiceID != nil {
		t.Fatalf("credit note = %+v; want role credit_note, on its way, crediting the old factura", note)
	}
	cur := chainDocument(t, docs, corrected.ID)
	if cur.Kind != "sale" || cur.Role != "current" || cur.Status != "on_its_way" || cur.DownloadURL != nil || !strEq(cur.SupersedesInvoiceID, facturaID) || cur.SupersededByInvoiceID != nil || cur.CreditsInvoiceID != nil {
		t.Fatalf("corrected factura = %+v; want role current, on its way, superseding the old factura", cur)
	}

	// Drained: all three authorized, the superseded XML still downloads.
	if result := drainSaleInvoices(t); result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("drain = %+v; want the Credit Note and the corrected factura authorized and delivered", result)
	}
	docs = listCustomerChain(t, buyer, saleID)
	for _, d := range docs {
		if d.Status != "authorized" || d.DownloadURL == nil {
			t.Fatalf("after the drain: %+v; want every document authorized and downloadable", d)
		}
	}
	if chainDocument(t, docs, facturaID).Role != "superseded" || chainDocument(t, docs, corrected.ID).Role != "current" || chainDocument(t, docs, noteID).Role != "credit_note" {
		t.Fatalf("after the drain the roles moved: %+v", docs)
	}
	oldFilename := getDrainedInvoice(t, operatorSessionID, facturaID).EcuadorFull.AccessKey + ".xml"
	resp, body := sriEnv.getRaw(t, customerDocumentXMLPath(saleID, facturaID), authHeader(buyer))
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), oldFilename) || !strings.Contains(string(body), "<factura") {
		t.Fatalf("the superseded factura's download: status=%d disposition=%q; want the signed XML under its clave", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
	_, raw := listCustomerDocuments(t, buyer, saleID)
	for _, never := range []string{"messages", "number", "access_key", "reissue_note", "reissued_by", "recipient"} {
		if strings.Contains(string(raw), "\""+never+"\"") {
			t.Fatalf("the buyer's read carries %q:\n%s", never, raw)
		}
	}

	// A second reissue: five documents, one current.
	second := reissueOK(t, operatorSessionID, corrected.ID, reissueBody{Recipient: reissueRecipientBody{
		TaxIDType: "cedula", TaxID: otherCedula, LegalName: "Beatriz Mora",
	}})
	docs = listCustomerChain(t, buyer, saleID)
	if len(docs) != 5 {
		t.Fatalf("after the second reissue: %d documents; want five", len(docs))
	}
	roles := map[string]int{}
	for _, d := range docs {
		roles[d.Role]++
	}
	if roles["current"] != 1 || roles["superseded"] != 2 || roles["credit_note"] != 2 {
		t.Fatalf("roles = %v; want one current, two superseded, two credit notes", roles)
	}
	if chainDocument(t, docs, second.ID).Role != "current" || !strEq(chainDocument(t, docs, second.ID).SupersedesInvoiceID, corrected.ID) {
		t.Fatalf("the second corrected factura is not the current one superseding the first: %+v", docs)
	}
	mid := chainDocument(t, docs, corrected.ID)
	if mid.Role != "superseded" || !strEq(mid.SupersedesInvoiceID, facturaID) || !strEq(mid.SupersededByInvoiceID, second.ID) || mid.DownloadURL == nil {
		t.Fatalf("the first corrected factura = %+v; want superseded, linked both ways, still downloadable", mid)
	}
}

// TestReissueLeavesTheBuyersSaleTicketsAndReversalWindowUntouched: the
// Sale as the Customer Area shows it — its Sale Confirmation reference,
// its lines, its status, whether and until when it may be undone — and the
// Tickets the buyer holds are the same bytes before the reissue, after it
// is owed, and after both documents are issued and mailed.
func TestReissueLeavesTheBuyersSaleTicketsAndReversalWindowUntouched(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	buyer := buyerSession(t, env, "guest@example.com")
	// The buyer's Tickets route serves only with the ticket-questions flag
	// open (ADR 0045); the harness closes it again after this test.
	enableTicketQuestions(t)

	snapshot := func() (string, string) {
		t.Helper()
		sale := saleByRef(t, readCustomerArea(t, env, buyer, ""), ref)
		saleJSON, err := json.Marshal(sale)
		if err != nil {
			t.Fatalf("marshal sale: %v", err)
		}
		ticketsJSON, err := json.Marshal(listBuyerTickets(t, env, buyer, saleID))
		if err != nil {
			t.Fatalf("marshal tickets: %v", err)
		}
		return string(saleJSON), string(ticketsJSON)
	}

	before := saleByRef(t, readCustomerArea(t, env, buyer, ""), ref)
	if before.ID != saleID || before.ConfirmationRef != ref || before.Status != "active" || !before.Reversible || before.ReversibleUntil == nil || before.ReversalPending {
		t.Fatalf("before the reissue the sale = %+v; want active, reversible until a deadline, nothing pending", before)
	}
	if tickets := listBuyerTickets(t, env, buyer, saleID); len(tickets) != 1 {
		t.Fatalf("before the reissue the buyer holds %d tickets; want one", len(tickets))
	}
	saleBefore, ticketsBefore := snapshot()

	reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	if sale, tickets := snapshot(); sale != saleBefore || tickets != ticketsBefore {
		t.Fatalf("owing the reissue changed the buyer's sale or tickets:\nsale before %s\nsale after  %s\ntickets before %s\ntickets after  %s", saleBefore, sale, ticketsBefore, tickets)
	}

	if result := drainSaleInvoices(t); result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("drain = %+v; want both documents authorized and delivered", result)
	}
	if sale, tickets := snapshot(); sale != saleBefore || tickets != ticketsBefore {
		t.Fatalf("issuing the reissue changed the buyer's sale or tickets:\nsale before %s\nsale after  %s\ntickets before %s\ntickets after  %s", saleBefore, sale, ticketsBefore, tickets)
	}
	after := saleByRef(t, readCustomerArea(t, env, buyer, ""), ref)
	if after.ConfirmationRef != ref || !after.Reversible || after.ReversibleUntil == nil || *after.ReversibleUntil != *before.ReversibleUntil {
		t.Fatalf("after the reissue the sale = %+v; want the same reference and the same Reversal Window", after)
	}
}

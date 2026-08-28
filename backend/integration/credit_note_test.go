package integration

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A reversed House sale is credited with a Credit Note (#476, parent #471,
// ADR 0060). The reversal — the buyer's own undo through the PayPhone stub,
// or an Operator Reversal — settles the Sale's paperwork in its own
// transaction by the Sale Invoice's state: never sent → withdrawn and the
// SRI hears nothing; authorized → a Credit Note owed for the whole amount,
// worked by the Drainer under codDoc 04, mailed, and shown beside the
// factura; still undecided → the Credit Note waits for the factura's
// answer and is issued or withdrawn with it. The reversal itself returns
// exactly as before in every case.
//
// Everything is asserted through the reversal responses, the operator
// invoicing list and detail, the drain response, what the fake SRI
// received, the captured mail, and the buyer's own document routes; the
// sequence table is read directly where no surface says "no number was
// consumed".

// reversalRoute is one of the two online reversal routes, as the tests
// drive it and as a Credit Note names it.
type reversalRoute struct {
	name   string
	reason string
	motivo string
	// reverse performs the reversal of the Sale with the reference, as the
	// route's actor, through the PayPhone app.
	reverse func(t *testing.T, env *testEnv, operatorSessionID, ref string)
}

var reversalRoutes = []reversalRoute{
	{
		name:   "customer",
		reason: "customer",
		motivo: "Anulación de la venta por el comprador",
		reverse: func(t *testing.T, env *testEnv, _ string, ref string) {
			t.Helper()
			buyer := buyerSession(t, env, "guest@example.com")
			sale := saleByRef(t, readCustomerArea(t, payphoneEnv, buyer, ""), ref)
			if !sale.Reversible {
				t.Fatalf("the House sale %s is not offered as reversible", ref)
			}
			if result := reverseSaleOK(t, payphoneEnv, buyer, sale.ID); result.ConfirmationRef != ref {
				t.Fatalf("undo = %+v; want %s reversed", result, ref)
			}
		},
	},
	{
		name:   "operator",
		reason: "platform",
		motivo: "Anulación de la venta por el operador de la plataforma",
		reverse: func(t *testing.T, _ *testEnv, operatorSessionID string, ref string) {
			t.Helper()
			// A partial refund with the fee kept: the memo must never reach
			// the Credit Note, which is for the whole amount (#471 story 30).
			result := operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
				RefundedAmountCents: intPtr(500),
				PlatformFeeKept:     boolPtr(true),
				Note:                strPtr("refunded by hand"),
			})
			if result.Status != "reversed" || result.ConfirmationRef != ref {
				t.Fatalf("operator reversal = %+v; want %s reversed", result, ref)
			}
		},
	},
}

// creditNoteOf finds the one Credit Note in the invoicing list, and fails
// when there is none or more than one.
func creditNoteOf(t *testing.T, operatorSessionID string) (id string) {
	t.Helper()
	list := getSaleInvoiceList(t, operatorSessionID)
	for _, row := range list.Data {
		if row.Kind == "credit_note" {
			if id != "" {
				t.Fatalf("two Credit Notes in the list: %+v", list.Data)
			}
			id = row.ID
		}
	}
	if id == "" {
		t.Fatalf("no Credit Note in the list: %+v", list.Data)
	}
	return id
}

func receivedNotaCredito(t *testing.T) *etree.Document {
	t.Helper()
	doc := receivedFactura(t)
	if doc.FindElement("/notaCredito") == nil {
		t.Fatalf("the SRI's last document is not a nota de crédito:\n%s", func() string {
			r, _ := sriStub.lastReceived()
			return string(r.signedXML)
		}())
	}
	return doc
}

// downloadCustomerDocument fetches one document's XML through the buyer's
// route.
func downloadCustomerDocument(t *testing.T, sessionID, saleID, documentID string) []byte {
	t.Helper()
	resp, raw := sriEnv.getRaw(t, customerDocumentXMLPath(saleID, documentID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d body=%s", resp.StatusCode, raw)
	}
	return raw
}

// TestReversalWithdrawsASaleInvoiceNeverSent: whichever route reverses the
// sale, a Sale Invoice that was never sent — owed, or parked unsignable
// with no number — is withdrawn in the reversal's transaction: nothing is
// due, no Credit Note exists, a drain finds nothing, the SRI receives
// nothing, no number is ever consumed, and the buyer's Sale lists no
// document. The reversal answers exactly as before.
func TestReversalWithdrawsASaleInvoiceNeverSent(t *testing.T) {
	for _, route := range reversalRoutes {
		for _, parked := range []bool{false, true} {
			name := route.name + "/owed"
			if parked {
				name = route.name + "/parked unsignable"
			}
			t.Run(name, func(t *testing.T) {
				env := setupTest(t)
				operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
				ref := lastConfirmation(t, env).Reference
				saleID := saleIDByRef(t, env, ref)
				if parked {
					// No Issuer: the first drain parks it needs_attention, unsigned.
					if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
						t.Fatalf("setup drain = %+v; want parked", result)
					}
				}
				env.email.Reset()

				route.reverse(t, env, operatorSessionID, ref)

				detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
				if detail.Status != "withdrawn" || detail.NextAttemptAt != nil || detail.Number != nil || detail.EcuadorFull != nil {
					t.Fatalf("after the reversal: status %s next %v number %v ecuador %v; want withdrawn, nothing due, unsigned", detail.Status, detail.NextAttemptAt, detail.Number, detail.EcuadorFull)
				}
				if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 1 {
					t.Fatalf("invoices after the reversal = %+v; want the withdrawn Sale Invoice alone, no Credit Note", list.Data)
				}
				if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
					t.Fatalf("needs_attention count = %d; want 0, a withdrawn document needs nobody", n)
				}
				// The operator's actions say what it is, not that it is owed.
				if resp, body := checkInvoice(t, operatorSessionID, invoiceID); resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_WITHDRAWN" {
					t.Fatalf("check on a withdrawn document: status=%d error=%+v; want 409 INVOICE_WITHDRAWN", resp.StatusCode, body.Error)
				}
				if resp, body := resendInvoice(t, operatorSessionID, invoiceID); resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_WITHDRAWN" {
					t.Fatalf("resend of a withdrawn document: status=%d error=%+v; want 409 INVOICE_WITHDRAWN", resp.StatusCode, body.Error)
				}

				// A working Issuer changes nothing: withdrawn is never worked.
				issuerReady(t, operatorSessionID)
				atInvoicingClock(t, fixedClock.Add(2*time.Hour))
				if result := drainSaleInvoices(t); result.Claimed != 0 {
					t.Fatalf("drain after the reversal = %+v; want nothing claimed", result)
				}
				if n := sriStub.receptionCount(); n != 0 {
					t.Fatalf("the SRI received %d documents; want none", n)
				}
				if n := sequenceRows(t, env); n != 0 {
					t.Fatalf("sequence rows = %d; want none, no number was consumed", n)
				}
				if n := len(deliveriesSent(t, env)); n != 0 {
					t.Fatalf("%d delivery mails; want none", n)
				}
				buyer := buyerSession(t, env, "guest@example.com")
				if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 0 {
					t.Fatalf("the buyer's documents = %+v; want none, a withdrawn document is never shown", docs)
				}
			})
		}
	}
}

// TestReversalCreditsAnAuthorizedSaleInvoice: whichever route reverses the
// sale, an authorized factura is credited. The reversal owes a Credit Note
// for the whole amount — the operator's partial refund and kept fee
// notwithstanding — naming the factura, the route and the same Recipient,
// due at once; the drain signs it under codDoc 04 with its own secuencial
// 1, the SRI receives a nota de crédito that validates against the
// official schema and references the factura's number and date, the
// buyer is mailed it once, and both documents stand authorized on the
// buyer's Sale, downloadable, linked to each other in the operator's
// detail.
func TestReversalCreditsAnAuthorizedSaleInvoice(t *testing.T) {
	for _, route := range reversalRoutes {
		t.Run(route.name, func(t *testing.T) {
			env := setupTest(t)
			operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
			ref := lastConfirmation(t, env).Reference
			factura := getDrainedInvoice(t, operatorSessionID, facturaID)
			if factura.CreditedByInvoiceID != nil {
				t.Fatalf("an uncredited factura names a Credit Note: %v", *factura.CreditedByInvoiceID)
			}

			route.reverse(t, env, operatorSessionID, ref)

			// Owed by the reversal, before any drain.
			noteID := creditNoteOf(t, operatorSessionID)
			note := getDrainedInvoice(t, operatorSessionID, noteID)
			if note.Status != "owed" || note.Number != nil || note.NextAttemptAt == nil {
				t.Fatalf("credit note right after the reversal = %s number %v next %v; want owed, unsigned, due", note.Status, note.Number, note.NextAttemptAt)
			}
			if note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID || note.CreditNoteReason == nil || *note.CreditNoteReason != route.reason {
				t.Fatalf("credit note credits %v for %v; want %s for %s", note.CreditsInvoiceID, note.CreditNoteReason, facturaID, route.reason)
			}
			if note.SaleConfirmationRef == nil || *note.SaleConfirmationRef != ref || note.IVARate == nil || *note.IVARate != "15" {
				t.Fatalf("credit note sale ref %v rate %v; want %s at 15", note.SaleConfirmationRef, note.IVARate, ref)
			}
			if note.Totals.TotalCents != factura.Totals.TotalCents || note.Totals.TotalCents != 1115 || note.Totals.SubtotalCents != factura.Totals.SubtotalCents || note.Totals.IVACents != factura.Totals.IVACents {
				t.Fatalf("credit note totals = %+v; want the factura's %+v — the whole amount, whatever the operator refunded", note.Totals, factura.Totals)
			}
			if len(note.Lines) != len(factura.Lines) || note.Lines[0].Description != factura.Lines[0].Description || note.Lines[0].UnitPriceCents != factura.Lines[0].UnitPriceCents {
				t.Fatalf("credit note lines = %+v; want the factura's %+v", note.Lines, factura.Lines)
			}
			factura = getDrainedInvoice(t, operatorSessionID, facturaID)
			if factura.CreditedByInvoiceID == nil || *factura.CreditedByInvoiceID != noteID {
				t.Fatalf("factura credited_by = %v; want the Credit Note %s", factura.CreditedByInvoiceID, noteID)
			}
			buyer := buyerSession(t, env, "guest@example.com")
			if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 2 || docs[1].Kind != "credit_note" || docs[1].Status != "on_its_way" || docs[1].DownloadURL != nil {
				t.Fatalf("the buyer's documents right after the reversal = %+v; want the factura and a Credit Note on its way", docs)
			}

			// Drained: signed, submitted, authorized, delivered in one round.
			result := drainSaleInvoices(t)
			if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 || result.Withdrawn != 0 {
				t.Fatalf("drain = %+v; want the Credit Note claimed, authorized and delivered", result)
			}
			note = getDrainedInvoice(t, operatorSessionID, noteID)
			if note.Status != "authorized" || note.Kind != "credit_note" || note.Number == nil || *note.Number != "001-001-000000001" {
				t.Fatalf("credit note after the drain = %s %s %v; want an authorized Credit Note numbered 1 in its own sequence", note.Kind, note.Status, note.Number)
			}
			if note.IssuedBy == nil || *note.IssuedBy != "sale-invoice-drainer" || note.DeliveredAt == nil || note.NextAttemptAt != nil || !note.HasAuthorizationXML {
				t.Fatalf("credit note = issued_by %v delivered %v next %v xml %v", note.IssuedBy, note.DeliveredAt, note.NextAttemptAt, note.HasAuthorizationXML)
			}
			if n := sequenceRows(t, env); n != 2 {
				t.Fatalf("sequence rows = %d; want two: the factura's 01 and the nota de crédito's 04", n)
			}
			// Read directly: no surface says which sequence a number came
			// out of, and "the 04 sequence stands at 1" is the fact.
			var codDoc string
			var last int64
			if err := env.db.QueryRow(`SELECT cod_doc, last_secuencial FROM invoicing_sequences_ec WHERE cod_doc = '04'`).Scan(&codDoc, &last); err != nil || last != 1 {
				t.Fatalf("04 sequence = %s %d (%v); want its first number consumed", codDoc, last, err)
			}

			// What the SRI received.
			if n := sriStub.receptionCount(); n != 2 {
				t.Fatalf("the SRI received %d documents; want the factura and the nota de crédito", n)
			}
			received, _ := sriStub.lastReceived()
			validateReceivedAgainstXSD(t, received.signedXML)
			if received.accessKey != note.EcuadorFull.AccessKey {
				t.Fatalf("received clave %s; want the Credit Note's %s", received.accessKey, note.EcuadorFull.AccessKey)
			}
			doc := receivedNotaCredito(t)
			checks := map[string]string{
				"/notaCredito/infoTributaria/codDoc":                         "04",
				"/notaCredito/infoTributaria/secuencial":                     "000000001",
				"/notaCredito/infoTributaria/ambiente":                       "1",
				"/notaCredito/infoNotaCredito/fechaEmision":                  "07/07/2026",
				"/notaCredito/infoNotaCredito/tipoIdentificacionComprador":   "05",
				"/notaCredito/infoNotaCredito/identificacionComprador":       validCedula,
				"/notaCredito/infoNotaCredito/razonSocialComprador":          "Ana Lopez",
				"/notaCredito/infoNotaCredito/codDocModificado":              "01",
				"/notaCredito/infoNotaCredito/numDocModificado":              "001-001-000000001",
				"/notaCredito/infoNotaCredito/fechaEmisionDocSustento":       "07/07/2026",
				"/notaCredito/infoNotaCredito/totalSinImpuestos":             "9.70",
				"/notaCredito/infoNotaCredito/valorModificacion":             "11.15",
				"/notaCredito/infoNotaCredito/motivo":                        route.motivo,
				"/notaCredito/detalles/detalle/cantidad":                     "1.00",
				"/notaCredito/detalles/detalle/precioTotalSinImpuesto":       "9.70",
				"/notaCredito/detalles/detalle/impuestos/impuesto/valor":     "1.45",
				"/notaCredito/infoAdicional/campoAdicional[@nombre='email']": "guest@example.com",
			}
			for path, want := range checks {
				if got := xmlText(t, doc, path); got != want {
					t.Fatalf("%s = %q; want %q", path, got, want)
				}
			}
			if *factura.Number != "001-001-000000001" {
				t.Fatalf("factura number = %s; the nota de crédito's numDocModificado must be it", *factura.Number)
			}

			// The buyer's mail: once, a credit note, the received bytes and the
			// nota de crédito's own RIDE attached (#496).
			sent := deliveriesSent(t, env)
			if len(sent) != 2 || sent[1].Kind != "credit_note" || sent[1].To != "guest@example.com" || sent[1].Reference != ref {
				t.Fatalf("deliveries = %d, last %+v; want the factura's and then the Credit Note's to the buyer", len(sent), sent[len(sent)-1])
			}
			if !strings.Contains(strings.ToLower(sent[1].Subject()), "nota de crédito") && !strings.Contains(strings.ToLower(sent[1].Subject()), "credit note") {
				t.Fatalf("credit note mail subject = %q; want the document named", sent[1].Subject())
			}
			assertDeliveryCarriesXMLAndRIDE(t, sent[1], note.EcuadorFull.AccessKey, received.signedXML, operatorRIDE(t, operatorSessionID, noteID))

			// The buyer's Sale: both documents, both downloadable.
			docs, _ := listCustomerDocuments(t, buyer, saleID)
			if len(docs) != 2 || docs[0].Kind != "sale" || docs[1].Kind != "credit_note" || docs[1].Status != "authorized" || docs[1].DownloadURL == nil {
				t.Fatalf("the buyer's documents = %+v; want the factura and the authorized Credit Note beside it", docs)
			}
			if got := downloadCustomerDocument(t, buyer, saleID, noteID); !bytes.Equal(got, received.signedXML) {
				t.Fatalf("the buyer's download of the Credit Note is not the document the SRI received (%d vs %d bytes)", len(got), len(received.signedXML))
			}

			// Settled: nothing more is due, nothing more is sent.
			if again := drainSaleInvoices(t); again.Claimed != 0 {
				t.Fatalf("second drain = %+v; want nothing claimed", again)
			}
			if n := len(deliveriesSent(t, env)); n != 2 {
				t.Fatalf("%d deliveries after the second drain; want still two", n)
			}
		})
	}
}

// TestCreditNoteWaitsForAPendingSaleInvoiceThenCreditsIt: the factura is
// still EN PROCESAMIENTO when the buyer undoes the sale. The Credit Note
// is owed at once but not worked: the next round polls the factura and
// leaves the Credit Note untouched, and the SRI receives nothing new. Once
// a late AUTORIZADO settles the factura, the same drain that authorizes it
// signs and submits the Credit Note referencing it, and the buyer gets
// both mails.
func TestCreditNoteWaitsForAPendingSaleInvoiceThenCreditsIt(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID := houseSaleOwed(t, env, 1000, 1)
	ref := lastConfirmation(t, env).Reference
	saleID := saleIDByRef(t, env, ref)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("setup drain = %+v; want the factura pending", result)
	}

	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)

	noteID := creditNoteOf(t, operatorSessionID)
	note := getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "owed" || note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID {
		t.Fatalf("credit note = %s crediting %v; want owed, crediting the pending factura", note.Status, note.CreditsInvoiceID)
	}
	buyer := buyerSession(t, env, "guest@example.com")
	if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 2 || docs[0].Status != "on_its_way" || docs[1].Status != "on_its_way" {
		t.Fatalf("the buyer's documents = %+v; want both on their way", docs)
	}

	// The factura is polled; the Credit Note waits.
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("drain with the factura still pending = %+v; want the factura alone claimed, still pending", result)
	}
	note = getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "owed" || note.Number != nil || len(note.AttemptRows) != 0 {
		t.Fatalf("waiting credit note = %s number %v attempts %d; want owed, unsigned, untouched", note.Status, note.Number, len(note.AttemptRows))
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the factura alone", n)
	}

	// The factura authorizes: the Credit Note follows in the same drain.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(6*time.Minute))
	result := drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("drain with a late AUTORIZADO = %+v; want the factura and then the Credit Note authorized and delivered", result)
	}
	note = getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.Number == nil || *note.Number != "001-001-000000001" || note.DeliveredAt == nil {
		t.Fatalf("credit note = %s %v delivered %v; want authorized, numbered, delivered", note.Status, note.Number, note.DeliveredAt)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the factura and the nota de crédito", n)
	}
	doc := receivedNotaCredito(t)
	if got := xmlText(t, doc, "/notaCredito/infoNotaCredito/numDocModificado"); got != "001-001-000000001" {
		t.Fatalf("numDocModificado = %s; want the factura's number", got)
	}
	if sent := deliveriesSent(t, env); len(sent) != 2 || sent[0].Kind != "sale" || sent[1].Kind != "credit_note" {
		t.Fatalf("deliveries = %+v; want the factura's and the Credit Note's", sent)
	}
	if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 2 || docs[0].Status != "authorized" || docs[1].Status != "authorized" {
		t.Fatalf("the buyer's documents = %+v; want both authorized", docs)
	}
}

// TestCreditNoteIsWithdrawnWhenItsSaleInvoiceDies: the SRI refused the
// factura, which is parked needs_attention and off the ladder, when the
// operator records a reversal. The Credit Note is owed and waits — a
// drain claims nothing, since the factura is neither authorized nor
// dead — until the operator marks the factura annulled at the portal;
// the next drain then withdraws the Credit Note unsigned, with a platform
// message saying why, consuming no number and sending nothing. The
// buyer's Sale lists neither.
func TestCreditNoteIsWithdrawnWhenItsSaleInvoiceDies(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID := houseSaleOwed(t, env, 1000, 1)
	ref := lastConfirmation(t, env).Reference
	saleID := saleIDByRef(t, env, ref)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("setup drain = %+v; want the factura parked", result)
	}

	reversalRoutes[1].reverse(t, env, operatorSessionID, ref)

	noteID := creditNoteOf(t, operatorSessionID)
	if note := getDrainedInvoice(t, operatorSessionID, noteID); note.Status != "owed" || note.CreditNoteReason == nil || *note.CreditNoteReason != "platform" {
		t.Fatalf("credit note = %s for %v; want owed for the platform route", note.Status, note.CreditNoteReason)
	}
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("drain with the factura parked = %+v; want nothing claimed — a refused factura is neither authorized nor dead", result)
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the factura alone", n)
	}

	// The operator annuls the factura by hand; the Credit Note goes with it.
	if view := annulOK(t, operatorSessionID, facturaID); view.Status != "annulled" {
		t.Fatalf("annul = %s", view.Status)
	}
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Withdrawn != 1 || result.Authorized != 0 || result.NeedsAttention != 0 {
		t.Fatalf("drain after the annulment = %+v; want the Credit Note claimed and withdrawn", result)
	}
	note := getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "withdrawn" || note.Number != nil || note.EcuadorFull != nil || note.NextAttemptAt != nil || len(note.AttemptRows) != 0 {
		t.Fatalf("credit note = %s number %v ecuador %v next %v attempts %d; want withdrawn, unsigned, off the queue, nothing asked of the SRI", note.Status, note.Number, note.EcuadorFull, note.NextAttemptAt, len(note.AttemptRows))
	}
	if len(note.Messages) != 1 || note.Messages[0].Type != "PLATFORM" || note.Messages[0].Identifier != "CREDIT_NOTE_FACTURA_NOT_AUTHORIZED" {
		t.Fatalf("credit note messages = %+v; want one PLATFORM message saying why", note.Messages)
	}
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want still the factura alone", n)
	}
	// Read directly: no surface says "no nota de crédito number was ever
	// consumed"; the absence of a 04 sequence row is that fact.
	var notes int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM invoicing_sequences_ec WHERE cod_doc = '04'`).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("04 sequence rows = %d (%v); want none, no number consumed", notes, err)
	}
	if n := len(deliveriesSent(t, env)); n != 0 {
		t.Fatalf("%d deliveries; want none", n)
	}
	buyer := buyerSession(t, env, "guest@example.com")
	if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 0 {
		t.Fatalf("the buyer's documents = %+v; want none: an annulled factura and a withdrawn Credit Note are never shown", docs)
	}
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain after the withdrawal = %+v; want nothing claimed", again)
	}
}

// TestNonHouseReversalOwesNothing: a paid sale of an Organization that is
// not a House Organization owes no document, and its reversal — by either
// route — settles nothing: no row, nothing due, nothing received.
func TestNonHouseReversalOwesNothing(t *testing.T) {
	for _, route := range reversalRoutes {
		t.Run(route.name, func(t *testing.T) {
			env := setupTest(t)
			adminSessionID := orgAdminSession(t, env)
			operatorSessionID := operatorSession(t, env, "operator@example.com")
			_, gaID := publishCheckoutEvent(t, env, adminSessionID, "Paid Fest", "paid-fest", 1000, 10)
			ref := paidCheckoutApproved(t, "paid-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
			issuerReady(t, operatorSessionID)

			route.reverse(t, env, operatorSessionID, ref)

			if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
				t.Fatalf("invoices after a non-House reversal = %+v; want none", list.Data)
			}
			if result := drainSaleInvoices(t); result.Claimed != 0 {
				t.Fatalf("drain = %+v; want nothing claimed", result)
			}
			if n := sriStub.receptionCount(); n != 0 {
				t.Fatalf("the SRI received %d documents; want none", n)
			}
			status, _, _ := saleProvenance(t, env, ref)
			if status != "reversed" {
				t.Fatalf("sale status = %s; want reversed", status)
			}
		})
	}
}

// TestReversalWinsAgainstADrainerParkingAnUnsignableDocument: the Drainer
// has claimed the owed document and is about to park it unsignable (no
// Issuer) when the buyer's reversal withdraws it. The park finds the row
// no longer owed and leaves it exactly as the reversal left it: withdrawn,
// off the queue — never resurrected needs_attention, which the next round
// with a working Issuer would have signed into a factura for a sale that
// no longer stands.
//
// The Drainer is held between its claim and its park by a table lock on
// the Issuer it reads next: there is no API and no fake that can hold a
// round at that instant, and the claim is visible through the operator
// detail (next_attempt_at moves to the lease) so the reversal is made only
// once the round provably holds the document.
func TestReversalWinsAgainstADrainerParkingAnUnsignableDocument(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	ref := lastConfirmation(t, env).Reference

	hold, err := env.db.Begin()
	if err != nil {
		t.Fatalf("begin hold: %v", err)
	}
	t.Cleanup(func() { _ = hold.Rollback() })
	if _, err := hold.Exec(`LOCK TABLE invoicing_issuers IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("hold the Issuer: %v", err)
	}

	var wg sync.WaitGroup
	var result saleInvoiceDrainResult
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = drainSaleInvoices(t)
	}()
	lease := fixedClock.Add(5 * time.Minute)
	deadline := time.Now().Add(10 * time.Second)
	for {
		detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
		if detail.NextAttemptAt != nil && nextAttemptAt(t, detail).Equal(lease) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Drainer never claimed the document: next_attempt_at %v", detail.NextAttemptAt)
		}
		time.Sleep(20 * time.Millisecond)
	}

	reversalRoutes[0].reverse(t, env, operatorSessionID, ref)
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "withdrawn" {
		t.Fatalf("after the reversal, under the Drainer's claim: %s; want withdrawn", detail.Status)
	}

	_ = hold.Rollback()
	wg.Wait()

	if result.Claimed != 1 || result.NeedsAttention != 0 || result.Withdrawn != 1 || result.Failed != 0 {
		t.Fatalf("drain that lost the row to the reversal = %+v; want it claimed and reported withdrawn, not parked", result)
	}
	detail := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if detail.Status != "withdrawn" || detail.NextAttemptAt != nil || detail.Number != nil {
		t.Fatalf("after the round: status %s next %v number %v; want withdrawn, nothing due, unsigned", detail.Status, detail.NextAttemptAt, detail.Number)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("needs_attention count = %d; want 0", n)
	}

	// A working Issuer an hour later signs nothing: the sale is gone.
	issuerReady(t, operatorSessionID)
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain with a working Issuer = %+v; want nothing claimed", again)
	}
	if n := sriStub.receptionCount(); n != 0 {
		t.Fatalf("the SRI received %d documents; want none", n)
	}
	if n := sequenceRows(t, env); n != 0 {
		t.Fatalf("sequence rows = %d; want none", n)
	}
}

// TestReissueCreditNoteStatesACorrectionNotAReversal (#481, parent #478,
// ADR 0061): a Credit Note's reason is a reason, not a reversal route. A
// `reissue` Credit Note has no Sale Reversal behind it — the Sale stands,
// paid — and every reader keys on the reason: the SRI receives the fixed
// motivo "Corrección de los datos del receptor", the operator's detail
// names `reissue`, and the buyer's mail says the earlier factura was
// cancelled for a correction and a corrected one follows, never that the
// sale was reversed. The five reversal routes' Credit Notes are unchanged
// (TestReversalCreditsAnAuthorizedSaleInvoice).
func TestReissueCreditNoteStatesACorrectionNotAReversal(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	env.email.Reset()

	// Seeded directly: the reissue endpoint that owes this document is a
	// later ticket of #478, and no API can yet owe a Credit Note without a
	// reversal. The row is what that act will write — the factura copied
	// whole, of kind credit_note, crediting the factura, reason `reissue`,
	// due at once — as houseSaleOwed's checkout writes the factura's.
	var noteID string
	if err := env.db.QueryRow(`
		INSERT INTO invoicing_invoices
			(kind, country, status, ticket_sale_id, credits_invoice_id, credit_note_reason, iva_rate,
			 recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
			 currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
			 next_attempt_at, last_messages)
		SELECT 'credit_note', country, 'owed', ticket_sale_id, id, 'reissue', iva_rate,
		       recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
		       currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
		       $2, '[]'::jsonb
		FROM invoicing_invoices WHERE id = $1
		RETURNING id`, facturaID, fixedClock).Scan(&noteID); err != nil {
		t.Fatalf("seed the reissue Credit Note: %v", err)
	}
	if _, err := env.db.Exec(`
		INSERT INTO invoicing_invoice_lines
			(invoice_id, position, description, quantity_millionths, unit_price_cents, discount_cents, iva_rate, base_cents, iva_cents)
		SELECT $2, position, description, quantity_millionths, unit_price_cents, discount_cents, iva_rate, base_cents, iva_cents
		FROM invoicing_invoice_lines WHERE invoice_id = $1`, facturaID, noteID); err != nil {
		t.Fatalf("seed the reissue Credit Note's lines: %v", err)
	}

	note := getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "owed" || note.Kind != "credit_note" || note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID || note.CreditNoteReason == nil || *note.CreditNoteReason != "reissue" {
		t.Fatalf("seeded credit note = %s %s crediting %v for %v; want an owed Credit Note crediting the factura for reissue", note.Kind, note.Status, note.CreditsInvoiceID, note.CreditNoteReason)
	}
	if status, _, _ := saleProvenance(t, env, ref); status == "reversed" {
		t.Fatalf("the Sale is reversed; a reissue Credit Note has no reversal behind it")
	}

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("drain = %+v; want the reissue Credit Note claimed, authorized and delivered", result)
	}
	note = getDrainedInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.Number == nil || *note.Number != "001-001-000000001" || note.DeliveredAt == nil || note.CreditNoteReason == nil || *note.CreditNoteReason != "reissue" {
		t.Fatalf("credit note after the drain = %s %v delivered %v reason %v; want authorized, numbered, delivered, still reissue", note.Status, note.Number, note.DeliveredAt, note.CreditNoteReason)
	}

	// What the SRI received: a nota de crédito whose motivo is the fixed
	// correction text, never a reversal's.
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the factura and the nota de crédito", n)
	}
	received, _ := sriStub.lastReceived()
	validateReceivedAgainstXSD(t, received.signedXML)
	doc := receivedNotaCredito(t)
	for path, want := range map[string]string{
		"/notaCredito/infoTributaria/codDoc":             "04",
		"/notaCredito/infoNotaCredito/motivo":            "Corrección de los datos del receptor",
		"/notaCredito/infoNotaCredito/numDocModificado":  "001-001-000000001",
		"/notaCredito/infoNotaCredito/valorModificacion": "11.15",
	} {
		if got := xmlText(t, doc, path); got != want {
			t.Fatalf("%s = %q; want %q", path, got, want)
		}
	}

	// The buyer's mail: a credit note, in words about a correction.
	sent := deliveriesSent(t, env)
	if len(sent) != 1 || sent[0].Kind != "credit_note" || sent[0].To != "guest@example.com" || sent[0].Reference != ref {
		t.Fatalf("deliveries = %+v; want the Credit Note's alone, to the buyer", sent)
	}
	mail := sent[0]
	if mail.Reason != "reissue" {
		t.Fatalf("delivery reason = %q; want reissue", mail.Reason)
	}
	for _, c := range []struct {
		locale platform.Locale
		wants  []string
		nevers []string
	}{
		{platform.LocaleES, []string{"nota de crédito", "corregir los datos del receptor", "factura corregida"}, []string{"anulada", "reversi", "revertid"}},
		{platform.LocaleEN, []string{"credit note", "recipient details can be corrected", "corrected tax invoice"}, []string{"reversed", "reversal", "cancelled purchase"}},
	} {
		mail.Locale = c.locale
		text := strings.ToLower(mail.Text())
		for _, want := range c.wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s reissue mail = %q; want it to say %q", c.locale, mail.Text(), want)
			}
		}
		for _, never := range c.nevers {
			if strings.Contains(text, never) {
				t.Fatalf("%s reissue mail = %q; must never say %q — the sale was not reversed", c.locale, mail.Text(), never)
			}
		}
	}
	if len(mail.Attachments) != 2 || !bytes.Equal(mail.Attachments[0].Body, received.signedXML) {
		t.Fatalf("the attached XML is not the nota de crédito the SRI received")
	}

	// The buyer's Sale carries both, and still stands.
	buyer := buyerSession(t, env, "guest@example.com")
	if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 2 || docs[1].Kind != "credit_note" || docs[1].Status != "authorized" {
		t.Fatalf("the buyer's documents = %+v; want the factura and the authorized Credit Note", docs)
	}
	if status, _, _ := saleProvenance(t, env, ref); status == "reversed" {
		t.Fatalf("the Sale was reversed by a paperwork correction")
	}
}

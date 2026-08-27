package platform

import (
	"strings"
	"testing"
)

// The Tax Document delivery (#475, ADR 0060) is the second mail about a
// paid House sale, and these tests assert on the RENDERED words as the
// receipt's do: a test that only checked a Locale was carried would pass
// just as happily against a message that was never translated.

func spanishDelivery() TaxDocumentDelivery {
	return TaxDocumentDelivery{
		To:              "ana@example.com",
		Kind:            TaxDocumentKindSaleInvoice,
		CustomerName:    "Ana Lopez",
		EventName:       "Noche de Jazz",
		Reference:       "ABC123",
		CustomerAreaURL: "https://example.test/tickets#sale-1",
		Attachments: []EmailAttachment{
			{Filename: "0707202601.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura/>")},
			{Filename: "0707202601.pdf", ContentType: "application/pdf", Body: []byte("%PDF-1.3")},
		},
		Locale: LocaleES,
	}
}

func TestTaxDocumentDeliveryIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	d := spanishDelivery()
	d.Locale = ""

	if got := d.Subject(); got != "Your tax invoice (factura) for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := d.Text()
	for _, want := range []string{
		"Hi Ana Lopez,",
		"Attached is the tax invoice (factura) for your purchase for Noche de Jazz, authorized by the SRI.",
		"Reference: ABC123",
		"Attached are the XML — the document itself, exactly as the SRI authorized it — and its RIDE, the same document in printable form (PDF).",
		"after signing in:\nhttps://example.test/tickets#sale-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

func TestTaxDocumentDeliveryIsWrittenInSpanishForASpanishSale(t *testing.T) {
	d := spanishDelivery()

	if got := d.Subject(); got != "Su factura de Noche de Jazz" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := d.Text()
	for _, want := range []string{
		"Hola Ana Lopez:",
		"Adjuntamos la factura de su compra de Noche de Jazz, autorizada por el SRI.",
		"Referencia: ABC123",
		"Adjuntamos el XML — el documento en sí, tal como lo autorizó el SRI — y su RIDE, el mismo documento en formato imprimible (PDF).",
		"después de iniciar sesión:\nhttps://example.test/tickets#sale-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "Hi ") || strings.Contains(text, "Attached is") {
		t.Fatalf("text = %q, carries English", text)
	}
}

// TestTaxDocumentDeliveryNamesACreditNoteByItsOwnWord: the one message type
// serves both kinds, and the kind is what the reader is told they hold.
func TestTaxDocumentDeliveryNamesACreditNoteByItsOwnWord(t *testing.T) {
	d := spanishDelivery()
	d.Kind = TaxDocumentKindCreditNote

	if got := d.Subject(); got != "Su nota de crédito de Noche de Jazz" {
		t.Fatalf("subject = %q, want the credit note named", got)
	}
	if text := d.Text(); !strings.Contains(text, "Adjuntamos la nota de crédito de su compra anulada de Noche de Jazz") {
		t.Fatalf("text = %q, want the credit note named", text)
	}
	d.Locale = LocaleEN
	if got := d.Subject(); got != "Your credit note (nota de crédito) for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English credit note subject", got)
	}
}

// TestTaxDocumentDeliveryForAReissueSaysACorrectionFollows (#481, ADR
// 0061): a Credit Note owed by a Sale Invoice Reissue has no reversal
// behind it, and its words say so in either language — the earlier factura
// is cancelled to correct the recipient's details and a corrected one
// follows — never that the purchase was reversed. The subject is the
// Credit Note's as ever: what the reader holds is still a nota de crédito.
func TestTaxDocumentDeliveryForAReissueSaysACorrectionFollows(t *testing.T) {
	d := spanishDelivery()
	d.Kind = TaxDocumentKindCreditNote
	d.Reason = TaxDocumentReasonReissue

	if got := d.Subject(); got != "Su nota de crédito de Noche de Jazz" {
		t.Fatalf("subject = %q, want the credit note named", got)
	}
	text := d.Text()
	for _, want := range []string{
		"Hola Ana Lopez:",
		"Adjuntamos la nota de crédito, autorizada por el SRI, que deja sin efecto la factura anterior de su compra de Noche de Jazz para corregir los datos del receptor.",
		"Su compra y sus entradas no cambian: recibirá la factura corregida por correo.",
		"Referencia: ABC123",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "anulada") {
		t.Fatalf("text = %q, says the purchase was reversed", text)
	}

	d.Locale = LocaleEN
	if got := d.Subject(); got != "Your credit note (nota de crédito) for Noche de Jazz" {
		t.Fatalf("subject = %q, want the English credit note subject", got)
	}
	text = d.Text()
	for _, want := range []string{
		"Hi Ana Lopez,",
		"Attached is the credit note (nota de crédito) that cancels the earlier tax invoice (factura) for your purchase for Noche de Jazz, authorized by the SRI, so that its recipient details can be corrected.",
		"Your purchase and your tickets are unchanged: a corrected tax invoice (factura) will follow by email.",
		"Reference: ABC123",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "reversed") {
		t.Fatalf("text = %q, says the purchase was reversed", text)
	}
}

// TestTaxDocumentDeliveryForAReversalStillSaysReversed: every reversal
// route's Credit Note reads exactly as it did before a reason could be a
// reissue, and a Sale Invoice ignores Reason altogether.
func TestTaxDocumentDeliveryForAReversalStillSaysReversed(t *testing.T) {
	for _, reason := range []string{"customer", "platform", "import_undo", "staff_reversal", "correction", ""} {
		d := spanishDelivery()
		d.Kind = TaxDocumentKindCreditNote
		d.Reason = reason
		if text := d.Text(); !strings.Contains(text, "Adjuntamos la nota de crédito de su compra anulada de Noche de Jazz") || strings.Contains(text, "corregir") {
			t.Fatalf("reason %q: text = %q, want the reversal wording", reason, text)
		}
	}
	d := spanishDelivery()
	d.Reason = TaxDocumentReasonReissue
	if text := d.Text(); !strings.Contains(text, "Adjuntamos la factura de su compra de Noche de Jazz") {
		t.Fatalf("a Sale Invoice with a stray reason: text = %q, want the factura wording", text)
	}
}

// TestTaxDocumentDeliveryWithoutAnOriginOffersNoLink: a deployment that knows
// no Storefront origin still delivers the document; it simply has nowhere
// to point.
func TestTaxDocumentDeliveryWithoutAnOriginOffersNoLink(t *testing.T) {
	d := spanishDelivery()
	d.CustomerAreaURL = ""
	if text := d.Text(); strings.Contains(text, "iniciar sesión") || strings.Contains(text, "https://") {
		t.Fatalf("text = %q, want no link line", text)
	}
}

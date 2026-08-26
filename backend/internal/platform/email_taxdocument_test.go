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
		Attachment:      EmailAttachment{Filename: "0707202601.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura/>")},
		Locale:          LocaleES,
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
		"The attached XML is the document itself",
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
		"El XML adjunto es el documento en sí",
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

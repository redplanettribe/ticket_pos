package ride

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// WHAT THESE ASSERT (#494, #495, ADR 0062). The producer refuses everything
// that is not an authorized, signed document with an authorization on file;
// two renders of the same document are the same bytes; the factura layout
// carries every field the Ficha requires, and the Credit Note layout its
// own — headed as such, naming the factura it modifies, the motivo and the
// valor de modificación, with no forma de pago — read back out of the
// PDF's own content streams rather than trusted from the code that wrote
// them. The figures asserted are the STORED ones — a line's BaseCents, the
// document's totals, the valorModificacion in the signed XML — because the
// RIDE must never recompute a legal figure.

const clave = "2708202601179001234500110010010000000121234567813"

func authorizedManual() (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
	agente := "NAC-DNCRASC20-00000001"
	inv := &invoicing.Invoice{
		ID:          "inv-1",
		Kind:        invoicing.DocumentKindManual,
		Country:     invoicing.CountryEcuador,
		Environment: invoicing.EnvironmentTest,
		Status:      invoicing.InvoiceStatusAuthorized,
		IssuedOn:    time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		IssuedAt:    time.Date(2026, 8, 27, 15, 4, 5, 0, time.UTC),
		IssuedBy:    "operator@example.com",
		Recipient: invoicing.Recipient{
			TaxIDType: "ruc",
			TaxID:     "1790012345001",
			LegalName: "ORGANIZACION EJEMPLO S.A.",
			Address:   "Av. República del Salvador, Quito",
			Email:     "billing@example.com",
		},
		Issuer: &invoicing.IssuerSnapshot{
			RUC:                      "1791234567001",
			RazonSocial:              "RED PLANET TRIBE S.A.S.",
			NombreComercial:          "Ticket POS",
			DireccionMatriz:          "Calle Matriz 123, Quito",
			DireccionEstablecimiento: "Calle Sucursal 456, Quito",
			Establecimiento:          "001",
			PuntoEmision:             "001",
			ObligadoContabilidad:     true,
			Regimen:                  invoicing.RegimenRIMPEContribuyente,
			AgenteRetencion:          &agente,
		},
		Currency:      "USD",
		SubtotalCents: 21000,
		DiscountCents: 500,
		IVACents:      2925,
		TotalCents:    23925,
		PaymentMethod: "20",
		SignedXML:     []byte("<factura/>"),
		Lines: []invoicing.InvoiceLine{
			{Position: 1, Description: "Platform Fee - July 2026", QuantityMillionths: 2_000_000, UnitPriceCents: 10000, DiscountCents: 500, IVARate: invoicing.IVARate15, BaseCents: 19500, IVACents: 2925},
			{Position: 2, Description: "Exempt service", QuantityMillionths: 1_500_000, UnitPriceCents: 1000, IVARate: invoicing.IVARateZero, BaseCents: 1500},
		},
		AdditionalFields: []invoicing.AdditionalField{{Position: 1, Name: "Periodo", Value: "2026-07"}},
	}
	ec := &invoicing.EcuadorInvoiceDetails{
		CodDoc:     sri.DocumentTypeFactura,
		Estab:      "001",
		PtoEmi:     "001",
		Secuencial: 12,
		AccessKey:  clave,
		Authorization: &invoicing.Authorization{
			Number: clave,
			Date:   time.Date(2026, 8, 27, 20, 30, 45, 0, time.UTC), // 15:30:45 in Guayaquil
		},
	}
	return inv, ec
}

func authorizedSale() (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
	inv, ec := authorizedManual()
	inv.Kind = invoicing.DocumentKindSale
	inv.Environment = invoicing.EnvironmentProduction
	inv.Issuer.Regimen = invoicing.RegimenGeneral
	inv.Issuer.AgenteRetencion = nil
	inv.Issuer.ObligadoContabilidad = false
	inv.TicketSaleID = "sale-1"
	inv.IVARate = invoicing.SaleInvoiceIVARate
	inv.PaymentMethod = "19"
	inv.AdditionalFields = nil
	inv.Recipient = invoicing.Recipient{TaxIDType: "cedula", TaxID: "1712345675", LegalName: "Ana Lopez", Email: "ana@example.com"}
	inv.Lines = []invoicing.InvoiceLine{{Position: 1, Description: "GA — House Fest", QuantityMillionths: 1_000_000, UnitPriceCents: 1115, IVARate: invoicing.IVARate15, BaseCents: 970, IVACents: 145}}
	inv.SubtotalCents, inv.DiscountCents, inv.IVACents, inv.TotalCents = 970, 0, 145, 1115
	return inv, ec
}

// authorizedCreditNote is the Credit Note that credits the Sale Invoice of
// authorizedSale: the same buyer and the same line, its own secuencial and
// clave (codDoc 04, allocated as the platform allocates one so the XML
// builder accepts it), and a SignedXML body that is exactly what
// sri.BuildNotaCredito produces for it — the renderer reads the modified
// document, the motivo and the valor de modificación out of that body, so
// the fixture must be the real shape and not a stub. The body is unsigned:
// the renderer never looks at the signature, and the parse is the same
// either way.
func authorizedCreditNote(t *testing.T) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
	t.Helper()
	inv, ec := authorizedSale()
	inv.ID = "inv-cn-1"
	inv.Kind = invoicing.DocumentKindCreditNote
	inv.CreditsInvoiceID = "inv-1"
	inv.CreditNoteReason = "customer"
	inv.IssuedOn = time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	inv.IssuedAt = time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC) // 09:00 in Guayaquil
	inv.PaymentMethod = ""
	issuer := sri.Issuer{
		RUC: inv.Issuer.RUC, RazonSocial: inv.Issuer.RazonSocial, NombreComercial: inv.Issuer.NombreComercial,
		DirMatriz: inv.Issuer.DireccionMatriz, DirEstablecimiento: inv.Issuer.DireccionEstablecimiento,
		Establishment: "001", EmissionPoint: "001", Regime: sri.RegimeGeneral,
	}
	key, err := sri.NewAccessKey(sri.AccessKeyInput{
		IssuedOn: inv.IssuedAt, DocumentType: sri.DocumentTypeNotaCredito, RUC: issuer.RUC, Environment: sri.EnvironmentProduction,
		Establishment: issuer.Establishment, EmissionPoint: issuer.EmissionPoint, Sequential: "000000003", NumericCode: "12345678",
	})
	if err != nil {
		t.Fatalf("allocate the nota de crédito's clave: %v", err)
	}
	ec.CodDoc = sri.DocumentTypeNotaCredito
	ec.Secuencial = 3
	ec.AccessKey = key
	ec.Authorization = &invoicing.Authorization{Number: key, Date: time.Date(2026, 8, 28, 19, 5, 0, 0, time.UTC)} // 14:05 in Guayaquil

	built, err := sri.BuildNotaCredito(sri.NotaCredito{
		Environment: sri.EnvironmentProduction,
		Issuer:      issuer,
		AccessKey:   key,
		Sequential:  "000000003",
		IssuedOn:    inv.IssuedAt,
		Recipient:   sri.Recipient{IDType: sri.RecipientIDCedula, ID: inv.Recipient.TaxID, LegalName: inv.Recipient.LegalName, Email: inv.Recipient.Email},
		Lines:       []sri.Line{{Description: "GA — House Fest", Quantity: sri.QuantityFromInt(1), UnitPriceCents: 1115, IVA: sri.IVACode15, IVAInclusive: true}},
		Modifies: sri.ModifiedDocument{
			DocumentType: sri.DocumentTypeFactura,
			Number:       "001-001-000000012",
			IssuedOn:     time.Date(2026, 8, 27, 15, 4, 5, 0, time.UTC),
		},
		Motivo: invoicing.CreditNoteMotivo("customer"),
	})
	if err != nil {
		t.Fatalf("build nota de crédito fixture: %v", err)
	}
	inv.SignedXML = built.XML
	if built.Totals.TotalCents != inv.TotalCents {
		t.Fatalf("fixture drift: the XML credits %d cents, the row stores %d", built.Totals.TotalCents, inv.TotalCents)
	}
	return inv, ec
}

func domainCode(err error) string {
	var domain apperror.DomainError
	if errors.As(err, &domain) {
		return domain.Code()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

var streamPattern = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

// pdfStreams inflates every content stream in the PDF and joins them: the
// text an fpdf page draws sits inside them as `(…) Tj` operators, cp1252
// encoded, so an ASCII substring of a label or a value is found verbatim.
func pdfStreams(t *testing.T, pdf []byte) string {
	t.Helper()
	var out strings.Builder
	for _, m := range streamPattern.FindAllSubmatch(pdf, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			// An image stream or an uncompressed one: keep the raw bytes.
			out.Write(m[1])
			continue
		}
		inflated, err := io.ReadAll(r)
		if err != nil && len(inflated) == 0 {
			t.Fatalf("inflate stream: %v", err)
		}
		out.Write(inflated)
	}
	return out.String()
}

func assertAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("RIDE text does not contain %q", w)
		}
	}
}

func TestRenderRefusesEveryDocumentThatIsNotAuthorized(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails)
	}{
		{"pending", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Status = invoicing.InvoiceStatusPending
			return i, e
		}},
		{"not_authorized", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Status = invoicing.InvoiceStatusNotAuthorized
			return i, e
		}},
		{"rejected", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Status = invoicing.InvoiceStatusRejected
			return i, e
		}},
		{"owed", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Status = invoicing.InvoiceStatusOwed
			i.SignedXML = nil
			i.Issuer = nil
			return i, nil
		}},
		{"annulled", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Status = invoicing.InvoiceStatusAnnulled
			return i, e
		}},
		{"no Ecuador row", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			return i, nil
		}},
		{"no authorization on file", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			e.Authorization = nil
			return i, e
		}},
		{"unsigned", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.SignedXML = nil
			return i, e
		}},
		{"no issuer snapshot", func(i *invoicing.Invoice, e *invoicing.EcuadorInvoiceDetails) (*invoicing.Invoice, *invoicing.EcuadorInvoiceDetails) {
			i.Issuer = nil
			return i, e
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv, ec := c.mut(authorizedManual())
			doc, err := Render(inv, ec)
			if doc != nil || domainCode(err) != "RIDE_NOT_FOUND" {
				t.Fatalf("Render = %v, %v; want nil, RIDE_NOT_FOUND", doc, err)
			}
		})
	}
}

func TestRenderIsByteIdentical(t *testing.T) {
	inv, ec := authorizedManual()
	first, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if !bytes.Equal(first.Body, second.Body) {
		t.Fatal("two renders of the same document differ")
	}
	if !bytes.HasPrefix(first.Body, []byte("%PDF-")) {
		t.Fatalf("body does not start with a PDF header: %.20q", first.Body)
	}
	if first.Filename != clave+".pdf" || first.ContentType != invoicing.ContentTypePDF {
		t.Fatalf("document = %q %q, want %s.pdf application/pdf", first.Filename, first.ContentType, clave)
	}
}

// TestFacturaRIDECarriesEveryField: the manual Tax Invoice's RIDE — two IVA
// rates, a discount, an additional field, a RIMPE Issuer that is an agente
// de retención — carries every field of the Ficha's factura layout.
func TestFacturaRIDECarriesEveryField(t *testing.T) {
	inv, ec := authorizedManual()
	doc, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := pdfStreams(t, doc.Body)

	// Emisor.
	assertAll(t, text,
		"RED PLANET TRIBE S.A.S.", "Ticket POS", "1791234567001",
		"Calle Matriz 123, Quito", "Calle Sucursal 456, Quito",
		"OBLIGADO A LLEVAR CONTABILIDAD: SI",
		"Contribuyente R\xe9gimen RIMPE",
		"Agente de Retenci\xf3n Resoluci\xf3n No. NAC-DNCRASC20-00000001",
	)
	// Header: title, number, dates, ambiente, emisión, clave, authorization.
	assertAll(t, text,
		"FACTURA", "001-001-000000012", "27/08/2026",
		"PRUEBAS", "NORMAL", clave, "27/08/2026 15:30:45",
	)
	// Recipient.
	assertAll(t, text, "ORGANIZACION EJEMPLO S.A.", "1790012345001", "Av. Rep\xfablica del Salvador, Quito")
	// Lines: description, cantidad, precio unitario, descuento, tarifa, precio total.
	assertAll(t, text,
		"Platform Fee - July 2026", "2.00", "100.00", "5.00", "15%", "195.00",
		"Exempt service", "1.50", "10.00", "0%",
	)
	// Totals, from the stored figures grouped by rate.
	assertAll(t, text,
		"SUBTOTAL 15%", "195.00",
		"SUBTOTAL 0%", "15.00",
		"SUBTOTAL SIN IMPUESTOS", "210.00",
		"DESCUENTO", "5.00",
		"IVA 15%", "29.25",
		"VALOR TOTAL", "239.25",
	)
	// Forma de pago and información adicional.
	assertAll(t, text, "OTROS CON UTILIZACION DEL SISTEMA FINANCIERO", "billing@example.com", "Periodo", "2026-07")
}

// TestSaleInvoiceRIDE: a Sale Invoice — one rate, no discount, no
// additional fields, a general-régimen Issuer not obliged to keep books and
// no agente de retención — renders with the same layout, in producción,
// paid by card.
func TestSaleInvoiceRIDE(t *testing.T) {
	inv, ec := authorizedSale()
	doc, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := pdfStreams(t, doc.Body)
	assertAll(t, text,
		"FACTURA", "PRODUCCI\xd3N", "OBLIGADO A LLEVAR CONTABILIDAD: NO",
		"Ana Lopez", "1712345675", "ana@example.com",
		"GA \x97 House Fest", "1.00", "11.15", "9.70",
		"SUBTOTAL 15%", "SUBTOTAL SIN IMPUESTOS", "IVA 15%", "1.45", "VALOR TOTAL",
		"TARJETA DE CR\xc9DITO",
	)
	// The totals skeleton is the SRI's fixed one — every subtotal row
	// prints, 0.00 where no line carries the rate — so "SUBTOTAL 0%" is
	// present here too; what a general-régimen Issuer must not earn is a
	// RIMPE legend or an agente de retención line.
	for _, absent := range []string{"RIMPE", "Agente de Retenci"} {
		if strings.Contains(text, absent) {
			t.Errorf("Sale Invoice RIDE contains %q, which the Issuer does not warrant", absent)
		}
	}
}

// TestInclusiveLinePrintsTheXMLsUnitPrice (#489 human check): a Sale
// Invoice's line stores the price as paid, IVA included, and its XML prints
// precioUnitario as base ÷ cantidad so the row reconciles (sri.FormatUnitPrice,
// up to six decimals). The RIDE prints that same figure, not the stored
// inclusive price — three tickets at 11.15 paid are 3 × 9.696667 = 29.09 sin
// impuestos, and "11.15" appears nowhere on the page. A Manual Tax Invoice's
// stored unit price IS its precioUnitario and prints as stored.
func TestInclusiveLinePrintsTheXMLsUnitPrice(t *testing.T) {
	inv, ec := authorizedSale()
	base, iva, err := sri.BackOutIVA(3*1115, sri.IVACode15)
	if err != nil {
		t.Fatal(err)
	}
	inv.Lines = []invoicing.InvoiceLine{{Position: 1, Description: "GA — House Fest", QuantityMillionths: 3_000_000, UnitPriceCents: 1115, IVARate: invoicing.IVARate15, BaseCents: base, IVACents: iva}}
	inv.SubtotalCents, inv.IVACents, inv.TotalCents = base, iva, 3*1115
	doc, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := pdfStreams(t, doc.Body)
	assertAll(t, text, "3.00", sri.FormatUnitPrice(base, sri.Quantity(3_000_000)), "29.09", "33.45")
	if strings.Contains(text, "11.15") {
		t.Errorf("Sale Invoice RIDE prints the IVA-inclusive price 11.15 as a unit price; the XML's precioUnitario is %s", sri.FormatUnitPrice(base, sri.Quantity(3_000_000)))
	}

	manual, mec := authorizedManual()
	doc, err = Render(manual, mec)
	if err != nil {
		t.Fatalf("render manual: %v", err)
	}
	assertAll(t, pdfStreams(t, doc.Body), "100.00", "10.00")
}

// TestCreditNoteRIDEIsItsOwnDocument (#495, ADR 0062 §3): an authorized
// Credit Note's RIDE is headed "NOTA DE CRÉDITO", names the factura it
// modifies by type, number and fecha de emisión, states the motivo and the
// valor de modificación as the signed XML carries them, keeps the shared
// sections — emisor, authorization, Recipient, lines, totals — and has no
// forma de pago. It re-renders byte-identically, since the buyer's copy
// (#496) and the operator's are meant to be the same file.
func TestCreditNoteRIDEIsItsOwnDocument(t *testing.T) {
	inv, ec := authorizedCreditNote(t)
	doc, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if doc.Filename != ec.AccessKey+".pdf" || doc.ContentType != invoicing.ContentTypePDF {
		t.Fatalf("document = %q %q, want %s.pdf application/pdf", doc.Filename, doc.ContentType, ec.AccessKey)
	}
	if ec.AccessKey == clave {
		t.Fatal("fixture drift: the Credit Note shares the factura's clave")
	}
	text := pdfStreams(t, doc.Body)

	// Header: the title, the note's own number and clave, its authorization.
	assertAll(t, text, "NOTA DE CR\xc9DITO", "001-001-000000003", ec.AccessKey, "28/08/2026 14:05:00", "PRODUCCI\xd3N")
	if strings.Contains(text, "(FACTURA)") {
		t.Error("the Credit Note's RIDE is headed FACTURA")
	}
	// The documento modificado: type, number, fecha de emisión, motivo,
	// valor — the last two verbatim from the XML, the valor the stored total.
	// (A PDF string escapes its parentheses, hence the backslashes.)
	assertAll(t, text,
		"Comprobante que se modifica: FACTURA No. 001-001-000000012",
		`Fecha emisi`+"\xf3n"+` \(doc. sustento\): 27/08/2026`,
		"Raz\xf3n de modificaci\xf3n: Anulaci\xf3n de la venta por el comprador",
		"Valor de modificaci\xf3n: 11.15",
	)
	// The shared sections, as the factura draws them.
	assertAll(t, text,
		"RED PLANET TRIBE S.A.S.", "1791234567001",
		"Ana Lopez", "1712345675", "Fecha Emisi\xf3n: 28/08/2026",
		"GA \x97 House Fest", "1.00", "11.15", "9.70",
		"SUBTOTAL 15%", "SUBTOTAL SIN IMPUESTOS", "IVA 15%", "1.45", "VALOR TOTAL",
		"ana@example.com",
	)
	if strings.Contains(text, "FORMA DE PAGO") {
		t.Error("the Credit Note's RIDE has a forma de pago")
	}

	again, err := Render(inv, ec)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if !bytes.Equal(doc.Body, again.Body) {
		t.Fatal("two renders of the same Credit Note differ")
	}
}

// TestCreditNoteWithUnreadableXMLIsARenderError: the modified document is
// read from the stored signed XML, so a Credit Note whose bytes do not
// parse as a nota de crédito cannot be rendered — and that is an error to
// log, not RIDE_NOT_FOUND: the document is authorized and its RIDE is owed.
func TestCreditNoteWithUnreadableXMLIsARenderError(t *testing.T) {
	for name, body := range map[string][]byte{
		"not xml":              []byte("<notaCredito"),
		"a factura's xml":      []byte(`<factura id="comprobante" version="1.1.0"><infoTributaria/></factura>`),
		"no modified document": []byte(`<notaCredito id="comprobante"><infoNotaCredito><motivo>x</motivo></infoNotaCredito></notaCredito>`),
	} {
		t.Run(name, func(t *testing.T) {
			inv, ec := authorizedCreditNote(t)
			inv.SignedXML = body
			doc, err := Render(inv, ec)
			if doc != nil || err == nil {
				t.Fatalf("Render = %v, %v; want nil and an error", doc, err)
			}
			if domainCode(err) == "RIDE_NOT_FOUND" {
				t.Fatalf("Render refused with RIDE_NOT_FOUND; want a render error")
			}
		})
	}
}

// TestBarcodeIsBuiltFromTheClave: no decoder ships with the encoder, so the
// proof that the barcode decodes to the clave is that it was encoded from
// it and nothing else, and that its PNG is the same bytes every time.
func TestBarcodeIsBuiltFromTheClave(t *testing.T) {
	bc, err := claveBarcode(clave)
	if err != nil {
		t.Fatalf("claveBarcode: %v", err)
	}
	if bc.Content() != clave {
		t.Fatalf("barcode content = %q, want the clave", bc.Content())
	}
	if bc.Metadata().CodeKind != "Code 128" {
		t.Fatalf("barcode kind = %q, want Code 128", bc.Metadata().CodeKind)
	}
	first, err := barcodePNG(bc)
	if err != nil {
		t.Fatalf("barcodePNG: %v", err)
	}
	second, _ := barcodePNG(bc)
	if !bytes.Equal(first, second) {
		t.Fatal("two encodings of the same barcode differ")
	}
}

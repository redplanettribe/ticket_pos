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

// WHAT THESE ASSERT (#494, ADR 0062). The producer refuses everything that
// is not an authorized, signed document with an authorization on file; two
// renders of the same document are the same bytes; and the factura layout
// carries every field the Ficha requires, read back out of the PDF's own
// content streams rather than trusted from the code that wrote them. The
// figures asserted are the STORED ones — a line's BaseCents, the document's
// totals — because the RIDE must never recompute a legal figure.

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

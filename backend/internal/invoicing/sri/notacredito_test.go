package sri

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
)

// The nota de crédito builder (#476, ADR 0060): the document that undoes a
// Sale Invoice, built from the factura's own lines under the factura's own
// arithmetic, naming the factura it modifies and why, and valid against
// the vendored official schema.

var testCreditedOn = time.Date(2026, 8, 20, 9, 0, 0, 0, Guayaquil)

func testNotaCredito(t *testing.T, lines []Line) NotaCredito {
	t.Helper()
	n := NotaCredito{
		Environment: EnvironmentTest,
		Issuer:      testIssuer(),
		Sequential:  "000000007",
		IssuedOn:    testIssuedOn,
		Recipient:   testRecipient(),
		Lines:       lines,
		Modifies:    ModifiedDocument{DocumentType: DocumentTypeFactura, Number: "001-002-000000123", IssuedOn: testCreditedOn},
		Motivo:      "Anulación de la venta por el comprador",
	}
	key, err := NewAccessKey(AccessKeyInput{
		IssuedOn: n.IssuedOn, DocumentType: DocumentTypeNotaCredito, RUC: n.Issuer.RUC, Environment: n.Environment,
		Establishment: n.Issuer.Establishment, EmissionPoint: n.Issuer.EmissionPoint, Sequential: n.Sequential,
		NumericCode: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	n.AccessKey = key
	return n
}

// notaCreditoVariants are the documents pinned by golden files and, when
// xmllint is installed, validated against the vendored official XSD.
func notaCreditoVariants(t *testing.T) map[string]NotaCredito {
	t.Helper()
	v := map[string]NotaCredito{}
	// The Sale Invoice's shape: IVA-inclusive lines, no address, an email.
	sale := testNotaCredito(t, inclusiveLines())
	sale.Recipient = Recipient{IDType: RecipientIDCedula, ID: "1710034065", LegalName: "Ana Lopez", Email: "guest@example.com"}
	v["sale_lines"] = sale
	v["mixed_rates"] = testNotaCredito(t, mixedLines())
	rimpe := testNotaCredito(t, oneLine())
	rimpe.Issuer.Regime = RegimeRIMPE
	rimpe.Issuer.AgenteRetencion = "1"
	v["rimpe"] = rimpe
	none := testNotaCredito(t, oneLine())
	none.Recipient.Email = ""
	v["no_additional_fields"] = none
	return v
}

func TestBuildNotaCreditoGolden(t *testing.T) {
	for name, n := range notaCreditoVariants(t) {
		built, err := BuildNotaCredito(n)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertGolden(t, filepath.Join("testdata", "golden", "nota_credito_"+name+".xml"), built.XML)
	}
}

// TestBuildNotaCreditoValidatesAgainstXSD runs libxml2's xmllint against the
// vendored NotaCredito_V1.1.0.xsd, unsigned and signed.
func TestBuildNotaCreditoValidatesAgainstXSD(t *testing.T) {
	for name, n := range notaCreditoVariants(t) {
		built, err := BuildNotaCredito(n)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		validateNotaCreditoWithXSD(t, name, built.XML)
	}
	built, err := BuildNotaCredito(testNotaCredito(t, inclusiveLines()))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(built.XML, loadTestCertificate(t), SignOptions{SigningTime: pinnedSigningTime})
	if err != nil {
		t.Fatal(err)
	}
	validateNotaCreditoWithXSD(t, "signed", signed)
	if _, err := Verify(signed); err != nil {
		t.Fatalf("the signed nota de crédito does not verify: %v", err)
	}
}

func validateNotaCreditoWithXSD(t *testing.T, name string, xml []byte) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not installed; XSD validation skipped (golden files still pin the structure)")
	}
	path := filepath.Join(t.TempDir(), name+".xml")
	if err := os.WriteFile(path, xml, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(xmllint, "--noout", "--nonet", "--schema", filepath.Join("testdata", "NotaCredito_V1.1.0.xsd"), path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s does not validate against NotaCredito_V1.1.0.xsd:\n%s\n%s", name, out, xml)
	}
}

// TestBuildNotaCreditoStatesTheFacturaAndItsTotals: the middle block names
// the factura by type, number and date, credits exactly the factura's
// importeTotal, carries the reason, and has no pagos; the per-rate totals
// have no tarifa; the lines are rendered as the factura rendered them.
func TestBuildNotaCreditoStatesTheFacturaAndItsTotals(t *testing.T) {
	lines := inclusiveLines()
	factura, err := BuildFactura(testFactura(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	built, err := BuildNotaCredito(testNotaCredito(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	if built.Totals.TotalCents != factura.Totals.TotalCents || built.Totals.SubtotalCents != factura.Totals.SubtotalCents || built.Totals.IVACents != factura.Totals.IVACents {
		t.Fatalf("totals differ from the factura's: %+v vs %+v", built.Totals, factura.Totals)
	}
	if built.Totals.TotalCents != 5230 {
		t.Fatalf("total = %d; want 5230, the cents paid", built.Totals.TotalCents)
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(built.XML); err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"/notaCredito/infoTributaria/codDoc":                                         "04",
		"/notaCredito/infoTributaria/secuencial":                                     "000000007",
		"/notaCredito/infoNotaCredito/fechaEmision":                                  "25/08/2026",
		"/notaCredito/infoNotaCredito/codDocModificado":                              "01",
		"/notaCredito/infoNotaCredito/numDocModificado":                              "001-002-000000123",
		"/notaCredito/infoNotaCredito/fechaEmisionDocSustento":                       "20/08/2026",
		"/notaCredito/infoNotaCredito/totalSinImpuestos":                             "45.48",
		"/notaCredito/infoNotaCredito/valorModificacion":                             "52.30",
		"/notaCredito/infoNotaCredito/moneda":                                        "DOLAR",
		"/notaCredito/infoNotaCredito/totalConImpuestos/totalImpuesto/codigo":        "2",
		"/notaCredito/infoNotaCredito/totalConImpuestos/totalImpuesto/baseImponible": "45.48",
		"/notaCredito/infoNotaCredito/totalConImpuestos/totalImpuesto/valor":         "6.82",
		"/notaCredito/infoNotaCredito/motivo":                                        "Anulación de la venta por el comprador",
		"/notaCredito/detalles/detalle[1]/precioUnitario":                            "9.695",
		"/notaCredito/detalles/detalle[1]/precioTotalSinImpuesto":                    "19.39",
		"/notaCredito/detalles/detalle[2]/precioUnitario":                            "8.696667",
		"/notaCredito/detalles/detalle[2]/impuestos/impuesto/tarifa":                 "15.00",
		"/notaCredito/infoAdicional/campoAdicional[@nombre='email']":                 "facturas@example.com",
	}
	for path, want := range checks {
		el := doc.FindElement(path)
		if el == nil {
			t.Fatalf("document lacks %s", path)
		}
		if el.Text() != want {
			t.Fatalf("%s = %q; want %q", path, el.Text(), want)
		}
	}
	for _, absent := range []string{
		"/notaCredito/infoNotaCredito/pagos",
		"/notaCredito/infoNotaCredito/importeTotal",
		"/notaCredito/infoNotaCredito/totalConImpuestos/totalImpuesto/tarifa",
	} {
		if doc.FindElement(absent) != nil {
			t.Fatalf("document carries %s, which the schema has no place for", absent)
		}
	}
	// The clave says nota de crédito, and its check digit is the SRI's.
	clave := doc.FindElement("/notaCredito/infoTributaria/claveAcceso").Text()
	parsed, err := ParseAccessKey(clave)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.DocumentType != DocumentTypeNotaCredito {
		t.Fatalf("clave document type = %s; want 04", parsed.DocumentType)
	}
	if !strings.HasPrefix(string(built.XML), xmlDeclaration+"<notaCredito id=\"comprobante\" version=\"1.1.0\">") {
		t.Fatalf("document does not open with the prefix-free root:\n%s", built.XML[:120])
	}
}

func TestBuildNotaCreditoRefuses(t *testing.T) {
	cases := map[string]struct {
		mutate func(n *NotaCredito)
		want   error
	}{
		"clave under the factura's type": {func(n *NotaCredito) {
			f := testFactura(t, oneLine())
			n.AccessKey = f.AccessKey
			n.Sequential = f.Sequential
		}, ErrInvalidFactura},
		"no motivo":                    {func(n *NotaCredito) { n.Motivo = "" }, ErrInvalidNotaCredito},
		"motivo too long":              {func(n *NotaCredito) { n.Motivo = strings.Repeat("m", MaxMotivoLength+1) }, ErrInvalidNotaCredito},
		"motivo with a newline":        {func(n *NotaCredito) { n.Motivo = "a\nb" }, ErrInvalidNotaCredito},
		"modified number not a number": {func(n *NotaCredito) { n.Modifies.Number = "1-1-1" }, ErrInvalidNotaCredito},
		"modified type not two digits": {func(n *NotaCredito) { n.Modifies.DocumentType = "1" }, ErrInvalidNotaCredito},
		"modified date zero":           {func(n *NotaCredito) { n.Modifies.IssuedOn = time.Time{} }, ErrInvalidNotaCredito},
		"no lines":                     {func(n *NotaCredito) { n.Lines = nil }, ErrInvalidFactura},
		"bad recipient type":           {func(n *NotaCredito) { n.Recipient.IDType = "07" }, ErrInvalidFactura},
	}
	for name, c := range cases {
		n := testNotaCredito(t, oneLine())
		c.mutate(&n)
		if _, err := BuildNotaCredito(n); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v; want %v", name, err, c.want)
		}
	}
}

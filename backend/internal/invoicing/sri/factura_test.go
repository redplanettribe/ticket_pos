package sri

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
)

var update = flag.Bool("update", false, "rewrite golden files")

func testIssuer() Issuer {
	return Issuer{
		RUC:                  "1790012345001",
		RazonSocial:          "TICKET POS S.A.S.",
		NombreComercial:      "Ticket POS",
		DirMatriz:            "Av. Amazonas N24-03 y Colón, Quito",
		DirEstablecimiento:   "Av. Amazonas N24-03 y Colón, Quito",
		Establishment:        "001",
		EmissionPoint:        "002",
		ObligadoContabilidad: true,
		Regime:               RegimeGeneral,
	}
}

func testRecipient() Recipient {
	return Recipient{
		IDType:    RecipientIDRUC,
		ID:        "0992345678001",
		LegalName: "PRODUCCIONES & EVENTOS CIA. LTDA.",
		Address:   "Malecón 100, Guayaquil",
		Email:     "facturas@example.com",
	}
}

var testIssuedOn = time.Date(2026, 8, 25, 10, 30, 0, 0, Guayaquil)

func testFactura(t *testing.T, lines []Line) Factura {
	t.Helper()
	f := Factura{
		Environment: EnvironmentTest,
		Issuer:      testIssuer(),
		Sequential:  "000000123",
		IssuedOn:    testIssuedOn,
		Recipient:   testRecipient(),
		Lines:       lines,
	}
	key, err := NewAccessKey(AccessKeyInput{
		IssuedOn: f.IssuedOn, DocumentType: DocumentTypeFactura, RUC: f.Issuer.RUC, Environment: f.Environment,
		Establishment: f.Issuer.Establishment, EmissionPoint: f.Issuer.EmissionPoint, Sequential: f.Sequential,
		NumericCode: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.AccessKey = key
	return f
}

func oneLine() []Line {
	return []Line{{Code: "FEE", Description: "Platform Fee — August 2026", Quantity: QuantityFromInt(2), UnitPriceCents: 1000, IVA: IVACode15}}
}

func mixedLines() []Line {
	return []Line{
		{Description: "Platform Fee", Quantity: QuantityFromInt(1), UnitPriceCents: 10000, IVA: IVACode15},
		{Description: "Cultural show ticket (RUAC 0%)", Quantity: QuantityFromInt(3), UnitPriceCents: 550, IVA: IVACodeZero},
		{Description: "Exempt service", Quantity: QuantityFromInt(1), UnitPriceCents: 700, IVA: IVACodeExento},
		{Description: "Not subject", Quantity: QuantityFromInt(2), UnitPriceCents: 125, IVA: IVACodeNoObjeto},
	}
}

func discountLine() []Line {
	return []Line{{Description: "Platform Fee with discount", Quantity: QuantityFromInt(1), UnitPriceCents: 5000, DiscountCents: 500, IVA: IVACode15}}
}

func TestComputeTotalsOneLine(t *testing.T) {
	tot, err := ComputeTotals(oneLine())
	if err != nil {
		t.Fatal(err)
	}
	if tot.SubtotalCents != 2000 || tot.IVACents != 300 || tot.TotalCents != 2300 || tot.DiscountCents != 0 {
		t.Fatalf("totals = %+v", tot)
	}
	if len(tot.ByRate) != 1 || tot.ByRate[0].IVA != IVACode15 || tot.ByRate[0].BaseCents != 2000 || tot.ByRate[0].IVACents != 300 {
		t.Fatalf("by rate = %+v", tot.ByRate)
	}
}

func TestComputeTotalsMixedRates(t *testing.T) {
	tot, err := ComputeTotals(mixedLines())
	if err != nil {
		t.Fatal(err)
	}
	// 100.00 + 16.50 + 7.00 + 2.50 = 126.00; IVA only on the 15% line = 15.00.
	if tot.SubtotalCents != 12600 || tot.IVACents != 1500 || tot.TotalCents != 14100 {
		t.Fatalf("totals = %+v", tot)
	}
	want := []TaxTotal{
		{IVA: IVACodeZero, RatePercent: 0, BaseCents: 1650, IVACents: 0},
		{IVA: IVACode15, RatePercent: 15, BaseCents: 10000, IVACents: 1500},
		{IVA: IVACodeNoObjeto, RatePercent: 0, BaseCents: 250, IVACents: 0},
		{IVA: IVACodeExento, RatePercent: 0, BaseCents: 700, IVACents: 0},
	}
	if len(tot.ByRate) != len(want) {
		t.Fatalf("by rate = %+v", tot.ByRate)
	}
	for i := range want {
		if tot.ByRate[i] != want[i] {
			t.Errorf("by rate[%d] = %+v, want %+v", i, tot.ByRate[i], want[i])
		}
	}
}

func TestComputeTotalsDiscount(t *testing.T) {
	tot, err := ComputeTotals(discountLine())
	if err != nil {
		t.Fatal(err)
	}
	// 50.00 − 5.00 = 45.00 base; 15% = 6.75; total 51.75.
	if tot.SubtotalCents != 4500 || tot.DiscountCents != 500 || tot.IVACents != 675 || tot.TotalCents != 5175 {
		t.Fatalf("totals = %+v", tot)
	}
	if tot.Lines[0].GrossCents != 5000 || tot.Lines[0].BaseCents != 4500 {
		t.Fatalf("line = %+v", tot.Lines[0])
	}
}

func TestComputeTotalsRoundsHalfUp(t *testing.T) {
	q, _ := ParseQuantity("0.333333")
	tot, err := ComputeTotals([]Line{
		{Description: "a", Quantity: QuantityFromInt(3), UnitPriceCents: 10, IVA: IVACode15}, // 0.30 → IVA 0.045 → 0.05
		{Description: "b", Quantity: q, UnitPriceCents: 1000, IVA: IVACode15},                // 3.33333 → 3.33 → IVA 0.4995 → 0.50
	})
	if err != nil {
		t.Fatal(err)
	}
	if tot.Lines[0].BaseCents != 30 || tot.Lines[0].IVACents != 5 {
		t.Fatalf("line a = %+v", tot.Lines[0])
	}
	if tot.Lines[1].BaseCents != 333 || tot.Lines[1].IVACents != 50 {
		t.Fatalf("line b = %+v", tot.Lines[1])
	}
	if tot.TotalCents != 30+5+333+50 {
		t.Fatalf("total = %d", tot.TotalCents)
	}
}

func TestComputeTotalsRefuses(t *testing.T) {
	bad := [][]Line{
		nil,
		{{Description: "x", Quantity: 0, UnitPriceCents: 100, IVA: IVACode15}},
		{{Description: "x", Quantity: QuantityFromInt(1), UnitPriceCents: -1, IVA: IVACode15}},
		{{Description: "x", Quantity: QuantityFromInt(1), UnitPriceCents: 100, DiscountCents: 101, IVA: IVACode15}},
		{{Description: "x", Quantity: QuantityFromInt(1), UnitPriceCents: 100, DiscountCents: -1, IVA: IVACode15}},
		{{Description: "x", Quantity: QuantityFromInt(1), UnitPriceCents: 100, IVA: "2"}},
	}
	for i, lines := range bad {
		if _, err := ComputeTotals(lines); !errors.Is(err, ErrInvalidFactura) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}

func TestQuantityParseAndFormat(t *testing.T) {
	cases := map[string]string{
		"1": "1.00", "2.5": "2.50", "0.333333": "0.333333", "10.100": "10.10", "0.000001": "0.000001", "1234567.5": "1234567.50",
	}
	for in, want := range cases {
		q, err := ParseQuantity(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if q.String() != want {
			t.Errorf("ParseQuantity(%q).String() = %q, want %q", in, q.String(), want)
		}
	}
	for _, in := range []string{"", "-1", "1.1234567", "abc", "1,5"} {
		if _, err := ParseQuantity(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if QuantityFromInt(2).String() != "2.00" {
		t.Fatal("QuantityFromInt")
	}
}

func TestFormatCents(t *testing.T) {
	for c, want := range map[int64]string{0: "0.00", 5: "0.05", 1234: "12.34", 100: "1.00", -250: "-2.50"} {
		if got := FormatCents(c); got != want {
			t.Errorf("FormatCents(%d) = %q, want %q", c, got, want)
		}
	}
}

func TestBuildFacturaStructure(t *testing.T) {
	built, err := BuildFactura(testFactura(t, mixedLines()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(built.XML, []byte(`<?xml version="1.0" encoding="UTF-8"?><factura id="comprobante" version="1.1.0">`)) {
		t.Fatalf("prefix: %.120s", built.XML)
	}
	if bytes.Contains(built.XML, []byte("xmlns")) {
		t.Fatal("root must be prefix-free and declare no namespace")
	}
	if !bytes.Contains(built.XML, []byte("PRODUCCIONES &amp; EVENTOS CIA. LTDA.")) {
		t.Fatal("ampersand must be escaped as &amp;")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(built.XML); err != nil {
		t.Fatal(err)
	}
	get := func(path string) string {
		el := doc.FindElement(path)
		if el == nil {
			t.Fatalf("missing %s", path)
		}
		return el.Text()
	}
	if err := ValidateAccessKey(get("/factura/infoTributaria/claveAcceso")); err != nil {
		t.Fatal(err)
	}
	if get("/factura/infoTributaria/ambiente") != "1" || get("/factura/infoTributaria/tipoEmision") != "1" ||
		get("/factura/infoTributaria/codDoc") != "01" || get("/factura/infoTributaria/secuencial") != "000000123" {
		t.Fatal("infoTributaria")
	}
	if get("/factura/infoFactura/fechaEmision") != "25/08/2026" {
		t.Fatalf("fechaEmision = %q", get("/factura/infoFactura/fechaEmision"))
	}
	if get("/factura/infoFactura/obligadoContabilidad") != "SI" {
		t.Fatal("obligadoContabilidad")
	}
	if get("/factura/infoFactura/totalSinImpuestos") != "126.00" || get("/factura/infoFactura/importeTotal") != "141.00" {
		t.Fatal("totals")
	}
	if get("/factura/infoFactura/pagos/pago/formaPago") != "20" || get("/factura/infoFactura/pagos/pago/total") != "141.00" {
		t.Fatal("pagos must default to forma de pago 20 for the full total")
	}
	if n := len(doc.FindElements("/factura/infoFactura/totalConImpuestos/totalImpuesto")); n != 4 {
		t.Fatalf("totalImpuesto count = %d, want 4", n)
	}
	tarifas := doc.FindElements("/factura/detalles/detalle/impuestos/impuesto/tarifa")
	if len(tarifas) != 4 || tarifas[0].Text() != "15.00" || tarifas[1].Text() != "0.00" {
		t.Fatalf("tarifas = %v", tarifas)
	}
	if get("/factura/detalles/detalle[2]/cantidad") != "3.00" || get("/factura/detalles/detalle[2]/precioUnitario") != "5.50" {
		t.Fatal("line 2 rendering")
	}
	fields := doc.FindElements("/factura/infoAdicional/campoAdicional")
	if len(fields) != 1 || fields[0].SelectAttrValue("nombre", "") != "email" || fields[0].Text() != "facturas@example.com" {
		t.Fatalf("infoAdicional = %v", fields)
	}
	if doc.FindElement("/factura/infoTributaria/contribuyenteRimpe") != nil {
		t.Fatal("general régimen must not carry contribuyenteRimpe")
	}
}

func TestBuildFacturaRimpeAndOptionalTags(t *testing.T) {
	f := testFactura(t, oneLine())
	f.Issuer.Regime = RegimeRIMPE
	f.Issuer.AgenteRetencion = "1"
	f.Issuer.ObligadoContabilidad = false
	f.Issuer.NombreComercial = ""
	f.Issuer.DirEstablecimiento = ""
	f.Recipient.Address = ""
	f.Recipient.Email = ""
	f.PaymentMethod = "19"
	built, err := BuildFactura(f)
	if err != nil {
		t.Fatal(err)
	}
	s := string(built.XML)
	for _, want := range []string{
		"<agenteRetencion>1</agenteRetencion><contribuyenteRimpe>CONTRIBUYENTE RÉGIMEN RIMPE</contribuyenteRimpe></infoTributaria>",
		"<obligadoContabilidad>NO</obligadoContabilidad>",
		"<formaPago>19</formaPago>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	for _, absent := range []string{"nombreComercial", "dirEstablecimiento", "direccionComprador", "infoAdicional"} {
		if strings.Contains(s, absent) {
			t.Errorf("unexpected %s", absent)
		}
	}
	f.Issuer.Regime = RegimeRIMPENegocioPopular
	built, err = BuildFactura(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(built.XML), "<contribuyenteRimpe>CONTRIBUYENTE NEGOCIO POPULAR - RÉGIMEN RIMPE</contribuyenteRimpe>") {
		t.Fatal("negocio popular tag")
	}
}

func fifteenFields() []AdditionalField {
	var fields []AdditionalField
	for i := 0; i < 15; i++ {
		fields = append(fields, AdditionalField{Name: "Campo " + string(rune('A'+i)), Value: strings.Repeat("v", 300)})
	}
	return fields
}

func TestBuildFacturaAdditionalFieldsLimit(t *testing.T) {
	f := testFactura(t, oneLine())
	f.Recipient.Email = ""
	f.AdditionalFields = fifteenFields()
	if _, err := BuildFactura(f); err != nil {
		t.Fatalf("15 fields without email: %v", err)
	}
	f.Recipient.Email = "x@example.com"
	if _, err := BuildFactura(f); !errors.Is(err, ErrInvalidFactura) {
		t.Fatalf("email plus 15 fields must exceed the limit: %v", err)
	}
	f.AdditionalFields = f.AdditionalFields[:14]
	if _, err := BuildFactura(f); err != nil {
		t.Fatalf("email plus 14 fields: %v", err)
	}
	f.AdditionalFields = []AdditionalField{{Name: "n", Value: strings.Repeat("v", 301)}}
	if _, err := BuildFactura(f); !errors.Is(err, ErrInvalidFactura) {
		t.Fatalf("301-char value accepted: %v", err)
	}
}

func TestBuildFacturaRefuses(t *testing.T) {
	cases := map[string]func(*Factura){
		"mismatched clave (sequential)": func(f *Factura) { f.Sequential = "000000124" },
		"mismatched clave (RUC)":        func(f *Factura) { f.Issuer.RUC = "1790012346001" },
		"mismatched clave (date)":       func(f *Factura) { f.IssuedOn = f.IssuedOn.AddDate(0, 0, 1) },
		"mismatched clave (env)":        func(f *Factura) { f.Environment = EnvironmentProduction },
		"bad clave":                     func(f *Factura) { f.AccessKey = f.AccessKey[:48] + "x" },
		"RUC not ending 001":            func(f *Factura) { f.Issuer.RUC = "1790012345002"; f.AccessKey = "" },
		"newline in razón social":       func(f *Factura) { f.Issuer.RazonSocial = "A\nB" },
		"empty dirMatriz":               func(f *Factura) { f.Issuer.DirMatriz = "" },
		"consumidor final":              func(f *Factura) { f.Recipient.IDType = "07" },
		"empty recipient name":          func(f *Factura) { f.Recipient.LegalName = "" },
		"long recipient id":             func(f *Factura) { f.Recipient.ID = strings.Repeat("1", 21) },
		"bad forma de pago":             func(f *Factura) { f.PaymentMethod = "22" },
		"bad régimen":                   func(f *Factura) { f.Issuer.Regime = "rimpe?" },
		"no lines":                      func(f *Factura) { f.Lines = nil },
		"empty description":             func(f *Factura) { f.Lines[0].Description = "" },
		"long line code":                func(f *Factura) { f.Lines[0].Code = strings.Repeat("c", 26) },
		"empty field name":              func(f *Factura) { f.AdditionalFields = []AdditionalField{{Name: "", Value: "v"}} },
	}
	for name, mutate := range cases {
		f := testFactura(t, oneLine())
		mutate(&f)
		if _, err := BuildFactura(f); !errors.Is(err, ErrInvalidFactura) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// facturaVariants are the documents pinned by golden files and, when xmllint
// is installed, validated against the vendored official XSD.
func facturaVariants(t *testing.T) map[string]Factura {
	t.Helper()
	v := map[string]Factura{}
	v["one_line"] = testFactura(t, oneLine())
	v["mixed_rates"] = testFactura(t, mixedLines())
	v["discount"] = testFactura(t, discountLine())
	rimpe := testFactura(t, oneLine())
	rimpe.Issuer.Regime = RegimeRIMPE
	rimpe.Issuer.AgenteRetencion = "1"
	v["rimpe"] = rimpe
	none := testFactura(t, oneLine())
	none.Recipient.Email = ""
	v["no_additional_fields"] = none
	fifteen := testFactura(t, oneLine())
	fifteen.Recipient.Email = ""
	fifteen.AdditionalFields = fifteenFields()
	v["fifteen_additional_fields"] = fifteen
	return v
}

func TestBuildFacturaGolden(t *testing.T) {
	for name, f := range facturaVariants(t) {
		built, err := BuildFactura(f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertGolden(t, filepath.Join("testdata", "golden", "factura_"+name+".xml"), built.XML)
	}
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s differs from golden\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// TestBuildFacturaValidatesAgainstXSD runs libxml2's xmllint against the
// vendored factura_V1.1.0.xsd. No pure-Go XSD validator exists without cgo,
// so the test skips where xmllint is absent; the golden files above pin the
// structure regardless.
func TestBuildFacturaValidatesAgainstXSD(t *testing.T) {
	for name, f := range facturaVariants(t) {
		built, err := BuildFactura(f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		validateWithXSD(t, name, built.XML)
	}
}

func validateWithXSD(t *testing.T, name string, xml []byte) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not installed; XSD validation skipped (golden files still pin the structure)")
	}
	path := filepath.Join(t.TempDir(), name+".xml")
	if err := os.WriteFile(path, xml, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(xmllint, "--noout", "--nonet", "--schema", filepath.Join("testdata", "factura_V1.1.0.xsd"), path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s does not validate against factura_V1.1.0.xsd:\n%s\n%s", name, out, xml)
	}
}

package sri

import (
	"errors"
	"testing"

	"github.com/beevik/etree"
)

// An IVA-inclusive line (#474, ADR 0060): the unit price is what the buyer
// paid, the base is backed out of quantity × unit price so that base + IVA
// is the cents paid, and the document prints the backed-out unit price to
// six decimals. The totals equal the payment to the cent.

func inclusiveLines() []Line {
	return []Line{
		// 2 × 11.15 paid = 22.30 → base 19.39 + IVA 2.91.
		{Description: "GA — House Fest", Quantity: QuantityFromInt(2), UnitPriceCents: 1115, IVA: IVACode15, IVAInclusive: true},
		// 3 × 10.00 paid = 30.00 → base 26.09 + IVA 3.91.
		{Description: "VIP — House Fest", Quantity: QuantityFromInt(3), UnitPriceCents: 1000, IVA: IVACode15, IVAInclusive: true},
	}
}

func TestComputeTotalsInclusiveLinesSumToWhatWasPaid(t *testing.T) {
	tot, err := ComputeTotals(inclusiveLines())
	if err != nil {
		t.Fatal(err)
	}
	if len(tot.Lines) != 2 {
		t.Fatalf("lines = %d", len(tot.Lines))
	}
	if l := tot.Lines[0]; l.GrossCents != 2230 || l.BaseCents != 1939 || l.IVACents != 291 {
		t.Fatalf("line 1 = %+v; want 2230 paid = 1939 + 291", l)
	}
	if l := tot.Lines[1]; l.GrossCents != 3000 || l.BaseCents != 2609 || l.IVACents != 391 {
		t.Fatalf("line 2 = %+v; want 3000 paid = 2609 + 391", l)
	}
	if tot.SubtotalCents != 4548 || tot.IVACents != 682 || tot.TotalCents != 5230 || tot.DiscountCents != 0 {
		t.Fatalf("totals = %+v; want 4548 + 682 = 5230, the cents paid", tot)
	}
}

func TestComputeTotalsInclusiveLineRefusesADiscount(t *testing.T) {
	lines := inclusiveLines()
	lines[0].DiscountCents = 100
	if _, err := ComputeTotals(lines); !errors.Is(err, ErrInvalidFactura) {
		t.Fatalf("err = %v; want ErrInvalidFactura", err)
	}
}

func TestFormatUnitPrice(t *testing.T) {
	cases := []struct {
		base int64
		q    Quantity
		want string
	}{
		{1939, QuantityFromInt(2), "9.695"},
		{2609, QuantityFromInt(3), "8.696667"},
		{870, QuantityFromInt(1), "8.70"},
		{1000, QuantityFromInt(1), "10.00"},
		{1, QuantityFromInt(3), "0.003333"},
	}
	for _, c := range cases {
		if got := FormatUnitPrice(c.base, c.q); got != c.want {
			t.Fatalf("FormatUnitPrice(%d, %d) = %q; want %q", c.base, c.q, got, c.want)
		}
	}
}

func TestBuildFacturaInclusiveLinePrintsTheBackedOutUnitPrice(t *testing.T) {
	built, err := BuildFactura(testFactura(t, inclusiveLines()))
	if err != nil {
		t.Fatal(err)
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
	if got := get("/factura/detalles/detalle[1]/precioUnitario"); got != "9.695" {
		t.Fatalf("line 1 precioUnitario = %q; want 9.695 (19.39 ÷ 2)", got)
	}
	if got := get("/factura/detalles/detalle[1]/precioTotalSinImpuesto"); got != "19.39" {
		t.Fatalf("line 1 precioTotalSinImpuesto = %q; want 19.39", got)
	}
	if got := get("/factura/detalles/detalle[1]/impuestos/impuesto/valor"); got != "2.91" {
		t.Fatalf("line 1 IVA = %q; want 2.91", got)
	}
	if got := get("/factura/detalles/detalle[2]/precioUnitario"); got != "8.696667" {
		t.Fatalf("line 2 precioUnitario = %q; want 8.696667 (26.09 ÷ 3)", got)
	}
	if got := get("/factura/infoFactura/totalSinImpuestos"); got != "45.48" {
		t.Fatalf("totalSinImpuestos = %q", got)
	}
	if got := get("/factura/infoFactura/importeTotal"); got != "52.30" {
		t.Fatalf("importeTotal = %q; want 52.30, the cents paid", got)
	}
	if got := get("/factura/infoFactura/pagos/pago/total"); got != "52.30" {
		t.Fatalf("pago total = %q; want 52.30", got)
	}
	validateWithXSD(t, "inclusive", built.XML)
}

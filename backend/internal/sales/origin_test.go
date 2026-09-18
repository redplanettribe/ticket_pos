package sales_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The origin table, written out as the four routes a Ticket Sale can have
// arrived by (#370, ADR 0052). The HTTP tests in backend/integration prove the
// Sales list states these; this proves the predicate underneath them, which is
// worth its own test for one reason: recognising a Manually Recorded Sale is a
// three-way NEGATIVE, and the case that keeps it honest — a batchless imported
// sale that is a Sale Correction's replacement, not a typed one — is a single
// pointer away from being mislabelled.
func TestDeriveSaleOrigin(t *testing.T) {
	batch := "batch-1"
	replaced := "sale-1"

	cases := []struct {
		name           string
		channel        string
		importBatchID  *string
		replacesSaleID *string
		want           string
	}{
		{"an uploaded Sale Import batch's sale", "import", &batch, nil, sales.SaleOriginSaleImport},
		{"a hand-typed sale: imported, no batch, replacing nothing", "import", nil, nil, sales.SaleOriginManuallyRecorded},
		{"a Sale Correction's replacement is batchless too, and is NOT hand-typed", "import", nil, &replaced, sales.SaleOriginCorrectionReplacement},
		{"an Online Sale sold on its own channel", "online", nil, nil, sales.SaleOriginChannelSale},
		{"an In-Person Sale, when that channel starts writing", "in_person", nil, nil, sales.SaleOriginChannelSale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sales.DeriveSaleOrigin(tc.channel, tc.importBatchID, tc.replacesSaleID); got != tc.want {
				t.Errorf("DeriveSaleOrigin(%q, %v, %v) = %q, want %q",
					tc.channel, tc.importBatchID, tc.replacesSaleID, got, tc.want)
			}
		})
	}
}

// A corrected sale is the OTHER half of a Sale Correction: it was replaced, so
// it carries replaced_by_sale_id rather than replaces_sale_id, and it never
// stopped being the sale its own route brought in. The derivation is told only
// the linkage that points BACKWARD for exactly that reason.
func TestDeriveSaleOriginIgnoresWhatLaterHappenedToTheSale(t *testing.T) {
	batch := "batch-1"
	if got := sales.DeriveSaleOrigin("import", &batch, nil); got != sales.SaleOriginSaleImport {
		t.Errorf("a corrected sale from a batch = %q, want %q", got, sales.SaleOriginSaleImport)
	}
	if got := sales.DeriveSaleOrigin("import", nil, nil); got != sales.SaleOriginManuallyRecorded {
		t.Errorf("a corrected hand-typed sale = %q, want %q", got, sales.SaleOriginManuallyRecorded)
	}
}

// AN UPGRADE'S PAID SALE IS AN ONLINE SALE AND STAYS ONE (#651, ADR 0074).
//
// It carries replaces_sale_id — the free Sale it stands in for — and that column
// is the very thing this derivation reads to recognise a Sale Correction's
// replacement. The two are told apart by the CHANNEL and nothing else: the
// function short-circuits before it ever looks at the linkage, because an origin
// is how a sale REACHED the platform and an Upgrade's paid Sale reached it the
// way every Online Sale does.
//
// ASSERTED RATHER THAN ASSUMED, because that short-circuit is the only thing
// standing between ADR 0074 and an Upgrade reading `correction_replacement` on
// the Sales list, the Sales Export and the Customer Dossier at once — the three
// callers share this one derivation. The staff app mirrors the origin TOKENS
// without re-deriving anything, so nothing on that side would catch it either.
func TestDeriveSaleOriginOfAnUpgrade(t *testing.T) {
	freeSale := "free-sale-1"
	if got := sales.DeriveSaleOrigin("online", nil, &freeSale); got != sales.SaleOriginChannelSale {
		t.Errorf("an upgraded Online Sale = %q, want %q", got, sales.SaleOriginChannelSale)
	}
	// And the surrendered free Sale itself, which carries the linkage the other
	// way round and is not told it at all: it sold online, and being replaced
	// never changed where it came from.
	if got := sales.DeriveSaleOrigin("online", nil, nil); got != sales.SaleOriginChannelSale {
		t.Errorf("a surrendered free Online Sale = %q, want %q", got, sales.SaleOriginChannelSale)
	}
}

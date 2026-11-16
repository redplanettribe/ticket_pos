package service

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The follow-up chooser of an annulled factura (#747): which cards, and
// which are admitted, by the factura's kind and its Sale's facts. The
// presses themselves are the integration harness's.
func TestAnnulmentFollowUpsByKindAndSale(t *testing.T) {
	annulled := func(kind invoicing.DocumentKind, superseded string, facts repository.FollowUpFacts) *repository.InvoiceRow {
		inv := annulledFactura()
		inv.Kind = kind
		inv.SupersededByInvoiceID = superseded
		return &repository.InvoiceRow{Invoice: inv, FollowUps: &facts}
	}
	stands := repository.FollowUpFacts{SaleStatus: "active"}
	type card struct {
		kind     string
		admitted bool
		refusal  string
	}
	all := func(issueAgain, reissue string) []card {
		return []card{
			{FollowUpIssueAgain, issueAgain == "", issueAgain},
			{FollowUpReissue, reissue == "", reissue},
			{FollowUpReverseSale, true, ""},
		}
	}
	cases := []struct {
		name    string
		row     *repository.InvoiceRow
		enabled bool
		want    []card
	}{
		{"a standing sale with no factura", annulled(invoicing.DocumentKindSale, "", stands), true, all("", "")},
		{"at mismatch, reversing is still admitted", annulled(invoicing.DocumentKindSale, "", repository.FollowUpFacts{SaleStatus: "active", AnnulmentAtMismatch: true}), true,
			all("INVOICE_ANNULMENT_MISMATCH", "INVOICE_ANNULMENT_MISMATCH")},
		{"a reversed sale", annulled(invoicing.DocumentKindSale, "", repository.FollowUpFacts{SaleStatus: "reversed", AnnulmentAtMismatch: true}), true,
			[]card{{FollowUpNothingFollows, true, ""}}},
		{"a manual tax invoice", annulled(invoicing.DocumentKindManual, "", repository.FollowUpFacts{AnnulmentAtMismatch: true}), true,
			[]card{{FollowUpTypeAgain, true, ""}}},
		{"already replaced", annulled(invoicing.DocumentKindSale, "replacement", stands), true, []card{}},
		{"the sale already has a factura", annulled(invoicing.DocumentKindSale, "", repository.FollowUpFacts{SaleStatus: "active", SaleHasLiveFactura: true}), true, []card{}},
		{"sale invoicing closed", annulled(invoicing.DocumentKindSale, "", stands), false, all("SALE_INVOICING_UNAVAILABLE", "SALE_INVOICING_UNAVAILABLE")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := annulmentFollowUps(tc.row, tc.enabled)
			if got == nil || len(got) != len(tc.want) {
				t.Fatalf("follow-ups = %+v; want %+v", got, tc.want)
			}
			for i, w := range tc.want {
				g := got[i]
				refusal := ""
				if g.Refusal != nil {
					refusal = g.Refusal.Code
				}
				if g.Kind != w.kind || g.Admitted != w.admitted || refusal != w.refusal {
					t.Fatalf("card %d = %+v (refusal %q); want %+v", i, g, refusal, w)
				}
			}
		})
	}

	// Not annulled: no chooser at all.
	authorized := &repository.InvoiceRow{Invoice: authorizedFactura()}
	if got := annulmentFollowUps(authorized, true); got != nil {
		t.Fatalf("an authorized factura's follow-ups = %+v; want none", got)
	}
}

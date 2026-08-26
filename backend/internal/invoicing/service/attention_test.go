package service

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The Sale lookup's pure halves (#486, ADR 0061): the chain's order, which
// the clock cannot give since a reissue writes two documents at once, and
// each document's role, derived from the row as shown. That the rows come
// back at all is the integration harness's to prove.

func saleRow(id string, kind invoicing.DocumentKind, status invoicing.InvoiceStatus) repository.InvoiceRow {
	return repository.InvoiceRow{Invoice: invoicing.Invoice{ID: id, Kind: kind, Status: status, TicketSaleID: "sale"}}
}

func creditNoteRow(id, credits string) repository.InvoiceRow {
	row := saleRow(id, invoicing.DocumentKindCreditNote, invoicing.InvoiceStatusOwed)
	row.Invoice.CreditsInvoiceID = credits
	row.Invoice.CreditNoteReason = invoicing.CreditNoteReasonReissue
	return row
}

func successorRow(id, supersedes string, status invoicing.InvoiceStatus) repository.InvoiceRow {
	row := saleRow(id, invoicing.DocumentKindSale, status)
	row.Invoice.SupersedesInvoiceID = supersedes
	return row
}

func ids(rows []repository.InvoiceRow) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].Invoice.ID)
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChainOrderFollowsTheChainNotTheClock(t *testing.T) {
	// Oldest first as the repository gives them — but the second reissue's
	// two documents tied on created_at and their ids put the factura
	// before its Credit Note; the walk puts them right.
	rows := []repository.InvoiceRow{
		saleRow("f1", invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized),
		creditNoteRow("n1", "f1"),
		successorRow("f2", "f1", invoicing.InvoiceStatusAuthorized),
		successorRow("f3", "f2", invoicing.InvoiceStatusOwed),
		creditNoteRow("n2", "f2"),
	}
	want := []string{"f1", "n1", "f2", "n2", "f3"}
	if got := ids(chainOrder(rows)); !equalIDs(got, want) {
		t.Fatalf("chain order = %v; want %v", got, want)
	}
}

func TestChainOrderPutsAWithdrawnSuccessorBeforeTheLiveOne(t *testing.T) {
	// A reissue whose Credit Note died withdrew its corrected factura; the
	// old one was reissued again. Both Credit Notes credit the old factura
	// and follow it in creation order; the dead successor is listed before
	// the one that stuck, never dropped.
	rows := []repository.InvoiceRow{
		saleRow("f1", invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized),
		creditNoteRow("n1", "f1"),
		successorRow("f2", "f1", invoicing.InvoiceStatusWithdrawn),
		creditNoteRow("n2", "f1"),
		successorRow("f3", "f1", invoicing.InvoiceStatusOwed),
	}
	want := []string{"f1", "n1", "n2", "f2", "f3"}
	if got := ids(chainOrder(rows)); !equalIDs(got, want) {
		t.Fatalf("chain order = %v; want %v", got, want)
	}
}

func TestChainOrderKeepsAReversalCreditNoteAfterItsFactura(t *testing.T) {
	rows := []repository.InvoiceRow{
		saleRow("f1", invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized),
		creditNoteRow("n1", "f1"),
	}
	rows[1].Invoice.CreditNoteReason = "customer"
	if got := ids(chainOrder(rows)); !equalIDs(got, []string{"f1", "n1"}) {
		t.Fatalf("chain order = %v; want the factura then its Credit Note", got)
	}
	if got := ids(chainOrder(nil)); len(got) != 0 {
		t.Fatalf("chain order of nothing = %v; want empty", got)
	}
}

func TestChainOrderNeverDropsADocumentWhoseLinkPointsAway(t *testing.T) {
	// The schema forbids it; were it to happen, the document is listed at
	// the end rather than vanishing from the operator's page.
	rows := []repository.InvoiceRow{
		creditNoteRow("n0", "elsewhere"),
		saleRow("f1", invoicing.DocumentKindSale, invoicing.InvoiceStatusAuthorized),
	}
	if got := ids(chainOrder(rows)); !equalIDs(got, []string{"f1", "n0"}) {
		t.Fatalf("chain order = %v; want the factura then the stray", got)
	}
}

func TestDocumentRoleIsDerivedFromTheRowAsShown(t *testing.T) {
	successor := "f2"
	cases := []struct {
		name string
		item InvoiceListItem
		want DocumentRole
	}{
		{"an authorized factura with no successor is current", InvoiceListItem{Kind: "sale", Status: "authorized"}, DocumentRoleCurrent},
		{"an owed factura with no successor is current", InvoiceListItem{Kind: "sale", Status: "owed"}, DocumentRoleCurrent},
		{"a factura with a live successor is superseded", InvoiceListItem{Kind: "sale", Status: "authorized", SupersededByInvoiceID: &successor}, DocumentRoleSuperseded},
		{"a Credit Note is a credit note", InvoiceListItem{Kind: "credit_note", Status: "authorized"}, DocumentRoleCreditNote},
		{"a withdrawn factura is not current", InvoiceListItem{Kind: "sale", Status: "withdrawn"}, DocumentRoleNotCurrent},
		{"an annulled factura is not current", InvoiceListItem{Kind: "sale", Status: "annulled"}, DocumentRoleNotCurrent},
		{"a hidden marker means not superseded", InvoiceListItem{Kind: "sale", Status: "authorized", SupersededByInvoiceID: nil}, DocumentRoleCurrent},
	}
	for _, c := range cases {
		if got := documentRole(&c.item); got != c.want {
			t.Errorf("%s: role = %s; want %s", c.name, got, c.want)
		}
	}
}

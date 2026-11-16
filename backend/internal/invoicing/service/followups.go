package service

import (
	"errors"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// The follow-ups after an annulment (#747, spec #742, ADR 0078): what the
// annulled face of a factura's panel offers, one card per follow-up, each
// stating what it owes and what the buyer hears.
//
// THE GATES DECIDE, NOT THE PAGE. Issue again's and Reissue's cards are
// admitted by the very refusals the presses answer with (issueAgainRefusal,
// reissueRefusal), over the facts the detail read for the purpose
// (repository.FollowUpFacts), so the chooser can never offer a press the
// API is certain to refuse, nor hide one it would take. When pressed, the
// gates decide again on the same facts read under the Sale's lock.
//
// WHICH CARDS, by the factura:
//
//   - a manual Tax Invoice: "type it again by hand", alone. The platform
//     never re-owes a document an operator typed.
//   - a Sale Invoice of a reversed Sale: "nothing follows", alone. Income
//     that no longer stands is never declared, and the reversal already
//     owes no Credit Note against an annulled factura.
//   - a Sale Invoice of a standing Sale no factura stands for: Issue again,
//     Reissue and Reverse the Sale. At mismatch the first two are listed
//     refused, with INVOICE_ANNULMENT_MISMATCH, and Reverse the Sale stays
//     admitted: a reversal is never delayed by its paperwork, and owes no
//     Credit Note against an annulled factura, mismatch or not.
//   - a Sale Invoice whose Sale a factura already stands for (it was issued
//     again or reissued): none. The chain card names the replacement.

// Follow-up kinds, one card each.
const (
	FollowUpIssueAgain     = "issue_again"
	FollowUpReissue        = "reissue"
	FollowUpReverseSale    = "reverse_sale"
	FollowUpNothingFollows = "nothing_follows"
	FollowUpTypeAgain      = "type_again"
)

// AnnulmentFollowUpView is one follow-up an annulled factura's chooser
// draws: its kind, whether it is admitted now, and when it is not the
// refusal its press would answer with.
type AnnulmentFollowUpView struct {
	Kind     string       `json:"kind"`
	Admitted bool         `json:"admitted"`
	Refusal  *RefusalView `json:"refusal"`
}

// annulmentFollowUps is the chooser of an annulled factura, nil on one that
// is not annulled (the detail sends it as null) and empty where nothing is
// left to choose. Issue again and Reissue are behind SALE_INVOICING_ENABLED
// as their presses are: closed, both are listed refused with
// SALE_INVOICING_UNAVAILABLE.
func annulmentFollowUps(row *repository.InvoiceRow, saleInvoicingEnabled bool) []AnnulmentFollowUpView {
	inv := &row.Invoice
	if inv.Status != invoicing.InvoiceStatusAnnulled || row.FollowUps == nil {
		return nil
	}
	facts := row.FollowUps
	switch {
	case inv.Kind == invoicing.DocumentKindManual:
		return []AnnulmentFollowUpView{{Kind: FollowUpTypeAgain, Admitted: true}}
	case facts.SaleStatus == saleStatusReversed:
		return []AnnulmentFollowUpView{{Kind: FollowUpNothingFollows, Admitted: true}}
	case inv.SupersededByInvoiceID != "", facts.SaleHasLiveFactura:
		return []AnnulmentFollowUpView{}
	}
	if !saleInvoicingEnabled {
		closed := invoicing.ErrSaleInvoicingUnavailable()
		return []AnnulmentFollowUpView{
			followUpOf(FollowUpIssueAgain, closed),
			followUpOf(FollowUpReissue, closed),
			{Kind: FollowUpReverseSale, Admitted: true},
		}
	}
	issueAgain := issueAgainRefusal(inv, repository.IssueAgainSaleFacts{
		SaleStatus:          facts.SaleStatus,
		SaleHasLiveFactura:  facts.SaleHasLiveFactura,
		AnnulmentAtMismatch: facts.AnnulmentAtMismatch,
	})
	reissue := reissueRefusal(inv, repository.ReissueSaleFacts{
		SaleStatus:          facts.SaleStatus,
		SaleHasLiveFactura:  facts.SaleHasLiveFactura,
		AnnulmentAtMismatch: facts.AnnulmentAtMismatch,
	})
	return []AnnulmentFollowUpView{
		followUpOf(FollowUpIssueAgain, issueAgain),
		followUpOf(FollowUpReissue, reissue),
		{Kind: FollowUpReverseSale, Admitted: true},
	}
}

func followUpOf(kind string, refusal error) AnnulmentFollowUpView {
	out := AnnulmentFollowUpView{Kind: kind, Admitted: refusal == nil}
	var de apperror.DomainError
	if errors.As(refusal, &de) {
		out.Refusal = &RefusalView{Code: de.Code(), Message: de.Message()}
	}
	return out
}

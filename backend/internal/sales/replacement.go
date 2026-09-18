package sales

// WHY ONE TICKET SALE STANDS IN THE PLACE OF ANOTHER (#651, parent #645,
// ADR 0074) — the whole value set of `ticket_sales.replacement_reason`
// (migration 123).
//
// The pair of columns is one relation with two reasons to exist, and the reason
// is stored beside it rather than inferred from it. A Sale Correction is a
// mistake an Organization is putting right (ADR 0050); an Upgrade is a buyer
// changing their mind about a Sale that was recorded perfectly. Reading
// `corrected` off an Upgrade would tell an Organization its staff erred on a
// Sale no human touched, which is the sentence ADR 0074 forbids.
//
// THEY LIVE HERE, IN THE DOMAIN PACKAGE, AND NOT BESIDE THE WRITER, because the
// READERS are the reason the column exists at all. sales/repository writes the
// word; the Customer Dossier (catalog/service), the Sales list and the Sales
// Export all have to tell the two acts apart, and a constant only the writer can
// see would be copied as a string literal into each of them — which is how three
// surfaces come to disagree about one fact. This package is the one both sides
// already import, the same place DeriveSaleOrigin and the reversal routes sit.
//
// A THIRD REASON MUST ADD A WORD HERE AND A CASE AT EVERY READER. The column's
// CHECK refuses a link without a reason, so a new writer cannot inherit
// `correction` by saying nothing — but a new WORD would fall through the
// readers' switches, and each of them deliberately falls back to the correction
// sentence because that is the older, narrower claim. Grep this file's
// constants before adding one.
const (
	// ReplacementReasonCorrection: a Sale Correction (#351, ADR 0050). Staff
	// reversed an imported sale somebody had recorded wrongly and typed a
	// replacement in the same act. Every pair written before migration 123 is
	// one of these, and the migration backfilled them all.
	ReplacementReasonCorrection = "correction"
	// ReplacementReasonUpgrade: an Upgrade (#650, ADR 0074). The buyer's free
	// Ticket Sale was reversed inside the transaction that committed the paid
	// one they elected instead. Nobody erred and no member of staff acted.
	ReplacementReasonUpgrade = "upgrade"
)

// IsUpgradeReplacement answers whether ONE NAMED LINK on a Ticket Sale was
// written by an Upgrade: the caller passes the link it is about to draw —
// `replaced_by_sale_id` or `replaces_sale_id` — beside the reason.
//
// THE LINK IS A PARAMETER BECAUSE THE REASON ALONE CANNOT ANSWER. It is written
// on BOTH halves of the pair (#650), so "this Sale's reason is `upgrade`" is
// true of the surrendered free Sale AND of the paid Sale that replaced it. A
// surface asking the reason on its own gets a yes for the paid Sale too — and
// that Sale is an ordinary Online Sale that may later be reversed in its
// Reversal Window or by an Operator, at which point it would report that it was
// upgraded out of, naming a lever nobody pulled. Asking "was I replaced, and was
// it an Upgrade" is one question and is answered here once.
//
// THE OTHER HALF OF THE ASYMMETRY: `correction` is the OLDER and NARROWER claim
// and the one a surface may keep making by default. A row with a link and no
// reason cannot exist (migration 123's CHECK), and a reason this build has never
// heard of is not evidence of an Upgrade. So only the word `upgrade`, on a link
// that is actually there, moves a surface off the sentence it printed before
// this feature existed — which is what keeps a genuine Sale Correction reading
// `corrected` everywhere it ever did.
func IsUpgradeReplacement(replacementLinkSaleID, replacementReason *string) bool {
	return replacementLinkSaleID != nil &&
		replacementReason != nil &&
		*replacementReason == ReplacementReasonUpgrade
}

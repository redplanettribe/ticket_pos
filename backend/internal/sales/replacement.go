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

// IsUpgradeReplacement answers whether a Sale's replacement link was written by
// an Upgrade, from the column as it is read — nil and all.
//
// EVERY READER ASKS THE QUESTION THIS WAY ROUND, and that is the point of the
// helper rather than a switch at each call site. `correction` is the OLDER and
// NARROWER claim and the one a surface may keep making by default: a row with a
// link and no reason cannot exist (migration 123's CHECK), and a reason this
// build has never heard of is not evidence of an Upgrade. So only the word
// `upgrade` moves a surface off the sentence it printed before this feature
// existed, which is what keeps a genuine Sale Correction reading `corrected`
// everywhere it ever did.
func IsUpgradeReplacement(replacementReason *string) bool {
	return replacementReason != nil && *replacementReason == ReplacementReasonUpgrade
}

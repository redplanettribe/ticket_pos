# An imported Ticket Sale is corrected by replacement, never edited

Organizers make mistakes in the Sale Import template — a wrong email, quantity, Ticket Type or Tax ID — and until now the only remedy was undoing the whole latest batch and re-uploading. The obvious fix is an edit form on the sale. We chose instead a **Sale Correction**: a single-sale staff Sale Reversal on the `import` channel plus a replacement sale recorded in the same transaction, linked both ways (`replaced_by_sale_id` / `replaces_sale_id`), with `reversed_by = 'staff'` and no free-text note because the diff between the two records is the reason.

## Why not edit in place

A Ticket Sale is the one immutable fact in the system: every figure (Tickets Sold, Takings, capacity, Purchase Limit), every Ticket, Holder and Answer, the Sale Confirmation reference, the export and Sales Trends all read off it and none of them were built to cope with it changing underneath. Mutable sales would also be the first place a Ticket's identity outlives its Sale, which ADRs 0046–0048 refused. Reversal plus replacement reuses every existing consequence of a Sale Reversal for free — figures drop, Holders are told (#327, unconditional), the old reference stays visible — and the replacement is validated exactly like an import row.

## Consequences

- The single-sale reversal also stands alone as a plain reversal of one imported sale, from **any** batch, not only the latest: reversing one sale only frees capacity, so the latest-only guard that protects batch undo does not apply. A batch stays undoable for whatever active sales it has left; a replacement belongs to no batch, so a later batch undo never sweeps a correction away.
- Nothing carries over: the replacement's Tickets start `unassigned` and Answers are gone. Copying would only work when quantity and type are unchanged — the case least likely to need correcting — and would be a Ticket surviving its Sale.
- The buyer is mailed nothing by default. A Sale Import already sends every buyer a Sale Confirmation at commit; a correction offers an off-by-default "send the buyer a new Sale Confirmation" checkbox, the mirror of batch undo's notify toggle, because imported buyers dealt with a sales rep and a reversed-then-reissued pair of mails from a platform they never used would confuse more than inform. The cost accepted: until that box is ticked, the buyer holds a dead Confirmation Link and cannot assign or answer on the corrected sale.
- No time limit, matching batch undo. A correction that cannot record its replacement (Ticket Type gone, capacity gone, Purchase Limit) fails whole and leaves the old sale intact.
- The replacement is additionally refused on capacity at validation time, before the transaction opens, where the import leaves that check to batch commit.
- Gated by `canManageEventSales`, like the import itself; Event Staff see the "Corrected → TP-XXXX" state on the Sales list but no action. Online Sales are untouched: money is involved and they remain the Customer's or the Platform Operator's to reverse (ADR 0018, 0019).

-- A terminal-dead successor stops blocking its Sale (#579, parent #575,
-- ADR 0068).
--
-- 102 gave the reissue chain its invariant — a factura has at most one LIVE
-- successor, so the chain never forks — and spelled "live" as "not
-- withdrawn", because withdrawn was the only way a successor could die when
-- it was written (#484: the Credit Note it follows was annulled before the
-- successor was ever signed). It has not been the only way for some time.
-- A corrected factura that reached the Tax Authority can die annulled — the
-- operator disowned it by hand at the portal (#477) — and, since #578 and
-- ADR 0068, abandoned: the authority never took its number and never will.
--
-- Both are as dead as withdrawn, and neither freed the slot. A superseded
-- factura therefore stayed superseded forever behind a document that no
-- longer exists in any sense the authority recognises, and its Sale became
-- unreachable: reissuing the old factura answered INVOICE_SUPERSEDED and
-- reissuing the dead successor answered INVOICE_NOT_AUTHORIZED. That dead
-- end is the one recorded on #480, and this index is half of why it
-- existed; the other half is the successor read in the repository, widened
-- in the same commit so the write side and the read side spell one rule.
--
-- So: every TERMINAL-DEAD status is not live. `withdrawn` — never sent, and
-- never will be. `annulled` — held by the authority, then disowned at its
-- portal. `abandoned` — sent, never held, never a legal document. What
-- stays in the index is every status a successor can still become something
-- from: owed, pending, needs_attention, rejected, not_authorized and
-- authorized. A successor still in flight, or refused with a remedy, holds
-- the slot exactly as before — one Sale never ends up with two competing
-- owed facturas.
--
-- Replacing the index rather than adding a second one: there is one rule
-- and it belongs in one place. The name is kept so that "what enforces one
-- live successor" has the same answer before and after. No row is rewritten
-- and no link is dropped — a dead successor keeps its supersedes link, its
-- number and its trail forever, which is how the chain is still readable
-- backwards through documents that died.
DROP INDEX invoicing_invoices_one_live_successor;

CREATE UNIQUE INDEX invoicing_invoices_one_live_successor
    ON invoicing_invoices (supersedes_invoice_id)
    WHERE supersedes_invoice_id IS NOT NULL
      AND status NOT IN ('withdrawn', 'annulled', 'abandoned');

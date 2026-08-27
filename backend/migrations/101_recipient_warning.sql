-- The Recipient Warning (#482, parent #478, ADR 0061).
--
-- The SRI authorizes a factura whose Recipient's identification does not
-- exist (advertencia 59) or is incorrect (62) and merely WARNS. The document
-- stands — authorized, delivered, creditable — but the platform has declared
-- income to the wrong taxpayer, and until now nothing said so: the
-- advertencia sat in the attempts ledger beside the 60 every test-environment
-- authorization carries. This column is that fact, read off the messages of
-- the authorization outcome and kept beside the status, which is untouched:
-- a Recipient Warning is not `needs_attention` — the Drainer settled the
-- document — and never parks, blocks or clears anything.
--
-- A SALE INVOICE'S FACT ALONE: a manual Tax Invoice's Recipient was typed
-- by the operator, who issues another by hand; a Credit Note names the same
-- Recipient as the factura it credits. The CHECK holds it to kind `sale`.
--
-- SET ONCE, CLEARED ONLY BY SUPERSESSION. Delivery does not clear it, time
-- does not, and no operator action does; the one thing that will is the
-- Sale Invoice Reissue that makes the document superseded (#478's reissue
-- tickets), which writes FALSE here in the same transaction that links the
-- corrected factura to it.
--
-- RE-APPLIABLE ON PURPOSE. The integration harness applies migrations once
-- at start and cannot re-run the runner; the backfill test re-executes this
-- file against a database that already has the column, so every statement
-- here is written to be idempotent (IF NOT EXISTS, DROP-then-ADD).
ALTER TABLE invoicing_invoices
    ADD COLUMN IF NOT EXISTS recipient_warning BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE invoicing_invoices
    DROP CONSTRAINT IF EXISTS invoicing_invoices_recipient_warning_on_sale,
    ADD CONSTRAINT invoicing_invoices_recipient_warning_on_sale
        CHECK (NOT recipient_warning OR kind = 'sale');

-- The dashboard's count and the list's filter: the warned documents alone.
CREATE INDEX IF NOT EXISTS invoicing_invoices_recipient_warning
    ON invoicing_invoices (COALESCE(issued_at, created_at) DESC)
    WHERE recipient_warning;

-- BACKFILL from the ledger: a Sale Invoice authorized before this column
-- existed is marked when any of its authorized answers carried a 59 or 62
-- as an advertencia. The messages are stored verbatim as
-- {identifier, message, additional_info, type}; the match is on the
-- identifier and the type, exactly as the service decides it from then on.
UPDATE invoicing_invoices i
SET recipient_warning = TRUE
WHERE i.kind = 'sale'
  AND i.status = 'authorized'
  AND NOT i.recipient_warning
  AND EXISTS (
        SELECT 1
        FROM invoicing_attempts a
        CROSS JOIN LATERAL jsonb_array_elements(a.messages) AS m
        WHERE a.invoice_id = i.id
          AND a.outcome = 'authorized'
          AND m->>'type' = 'ADVERTENCIA'
          AND m->>'identifier' IN ('59', '62')
  );

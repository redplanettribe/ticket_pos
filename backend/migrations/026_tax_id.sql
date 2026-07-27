-- Tax ID on Customers and Ticket Sales (#96, ADR 0016).
--
-- Ecuadorian tax declarations (SRI) identify a buyer by a *tipo de
-- identificación* and its number. The glossary calls that pair a Tax ID: a Tax
-- ID Type (`cedula` | `ruc` | `passport`) plus the number. It lands in two
-- places, for two different reasons:
--
--   * on `customers`, as the person's one *current assertion* — what they say
--     about themselves today, editable and clearable by them;
--   * on `ticket_sales`, as an immutable *snapshot* of what a given sale was
--     actually transacted under, which may differ (a Customer may buy under
--     their company RUC once and their cédula the next time).
--
-- This is the same current-value/snapshot split the customer name already uses
-- (migration 011, ADR 0005), for the same reason: a profile edit must never
-- rewrite the identification a past sale was declared under.
--
-- Every column here is nullable FOREVER, and that is a decision, not an
-- oversight (ADR 0016). A Tax ID is required to record a sale on the native
-- Sales Channels (`online`, `in_person`) and optional on `import`, where the
-- sale happened off-platform and the ID may never have been collected. Every
-- sale recorded before this migration has none, and history is never backfilled
-- with fabricated numbers. A NOT NULL — or even a channel-conditional CHECK —
-- would therefore be falsified by rows the platform is required to keep. The
-- requirement lives in the sale-recording service layer instead, in one shared
-- path every present and future channel goes through.
--
-- There is deliberately NO uniqueness constraint anywhere below. A Tax ID is an
-- attribute a person asserts, not an identity: email remains the sole Customer
-- identity, the same cédula may legitimately appear on two Customers (one
-- person, two email addresses), and the same number appears on every sale that
-- person ever made.

-- The Customer's current assertion.
ALTER TABLE customers ADD COLUMN tax_id_type TEXT
    CHECK (tax_id_type IN ('cedula', 'ruc', 'passport'));
ALTER TABLE customers ADD COLUMN tax_id_number TEXT;

-- A Tax ID is a pair, so half of one is meaningless: a number without a type
-- cannot be validated or labelled, and a type without a number identifies
-- nobody. "Both set or both null" is the one shape rule the database can state
-- without asserting anything about *when* a Tax ID is required.
ALTER TABLE customers ADD CONSTRAINT customers_tax_id_pair_ck CHECK (
    (tax_id_type IS NULL) = (tax_id_number IS NULL)
);

-- The Ticket Sale's immutable snapshot, named with the customer_ prefix the
-- other buyer snapshots on this table already use (customer_email,
-- customer_first_name, customer_last_name). Written once at sale time; nothing
-- updates it afterwards.
ALTER TABLE ticket_sales ADD COLUMN customer_tax_id_type TEXT
    CHECK (customer_tax_id_type IN ('cedula', 'ruc', 'passport'));
ALTER TABLE ticket_sales ADD COLUMN customer_tax_id_number TEXT;

ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_customer_tax_id_pair_ck CHECK (
    (customer_tax_id_type IS NULL) = (customer_tax_id_number IS NULL)
);

-- Supports the Sales list's Tax ID search, which is event-scoped only — an
-- organizer looks up a buyer within the Event they sold, never across the
-- platform — so event_id leads, matching every other Sales list index
-- (migrations 013 and 014).
--
-- This index serves the common case: an organizer pastes or types a full number
-- read off an ID card. The list's unified search is a substring ILIKE (a leading
-- wildcard, so the door-staff "last four digits" lookup still scans), and that
-- scan stays acceptable for the same reason the existing email/name/confirmation
-- branches do: a single Event's sales are bounded (imports cap ~10,000 rows,
-- ADR-0006), with a pg_trgm trigram index as the documented upgrade path.
--
-- Partial, because the overwhelming majority of rows the day this ships have no
-- Tax ID at all and nulls are never searched for: indexing them would cost
-- write amplification for lookups nobody performs.
CREATE INDEX ticket_sales_event_customer_tax_id_idx
    ON ticket_sales (event_id, customer_tax_id_number)
    WHERE customer_tax_id_number IS NOT NULL;

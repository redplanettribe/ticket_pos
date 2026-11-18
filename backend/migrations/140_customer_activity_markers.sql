-- A Customer acting as themselves marks their day, and the daily close
-- freezes Active Customers (#817, spec #808, ADR 0080).
--
-- THIS IS THE FIRST TIME THE PLATFORM RECORDS WHAT A PERSON DID ON A DAY, and
-- it ships dark (ADR 0045): nothing writes a row here until
-- CUSTOMER_ACTIVITY_RECORDING_ENABLED is set, which waits on a Privacy Policy
-- edition describing the recording. Running this migration starts no
-- collection.
--
-- One row per Customer per platform day, America/Guayaquil, and nothing else
-- in it: no page, no time of day, no IP, no count of requests. ADR 0022 and
-- ADR 0057 refused a per-visitor record for Storefront traffic and still
-- stand: a row exists only for a person already proven to be that Customer,
-- acting through a full Customer Session or accepting a Ticket Assignment.
-- A Confirmation Link session writes none, since the link may have been
-- forwarded, and neither does a sale staff recorded on the Customer's behalf.
--
--   day          the platform day the Customer acted on.
--   customer_id  the Customer.
--
-- Writing a marker that exists is a no-op. The daily close reads these rows,
-- freezes the counts into platform_activity_totals and deletes the rows
-- older than thirteen months. No read of Platform Trends, and no route of
-- any kind, returns one: which Customers were active on a day is not a
-- feature (ADR 0080).
--
-- ON DELETE CASCADE, where every other reference to a Customer that carries
-- evidence is RESTRICT: erasure is manual (ADR 0010) and has no code path to
-- hook, so the database itself takes a Customer's markers with the Customer.
-- The frozen totals are numbers and outlive both.
--
-- The key leads on day because the close and the purge go by day. No index
-- leads on customer_id: the only read by Customer is the cascade of an
-- erasure, which is rare and runs over at most thirteen months of rows.
CREATE TABLE customer_activity_markers (
    day         DATE NOT NULL,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    PRIMARY KEY (day, customer_id)
);

-- ACTIVE CUSTOMERS, FROZEN BESIDE ACTIVE ORGANIZATIONS on the same row per
-- closed platform day.
--
--   active_customers                   the Customers active that day.
--   active_customers_7d                the different Customers active across
--                                      the 7 days ending that day.
--   active_customers_30d               the same across the 30 days ending
--                                      that day.
--   active_customers_new               those of the day's active Customers
--                                      who became Verified Customers that
--                                      day.
--   active_customers_returning         every other one of them: new plus
--                                      returning is the day's count.
--   active_customers_returning_bought  those of the returning who own an
--                                      Online Sale sold that day, reversed
--                                      since or not. A part of returning,
--                                      not a third kind beside it.
--
-- ALL SIX ARE NULL ON A DAY THAT HAS NO ACTIVE CUSTOMERS FIGURE, which is
-- different from zero: every day frozen before this migration, every day
-- closed while recording was switched off, and every day closed before the
-- first one a Customer marker was counted in. The first day they are not
-- NULL is the day recording began, and nothing else writes that day down:
-- unlike organization_activity_recording, these rows say it themselves.
--
-- No frozen row is rewritten to make room: adding a NULLable column changes
-- no value any row holds.
ALTER TABLE platform_activity_totals
    ADD COLUMN active_customers                  INTEGER CHECK (active_customers >= 0),
    ADD COLUMN active_customers_7d               INTEGER CHECK (active_customers_7d >= active_customers),
    ADD COLUMN active_customers_30d              INTEGER CHECK (active_customers_30d >= active_customers_7d),
    ADD COLUMN active_customers_new              INTEGER CHECK (active_customers_new >= 0),
    ADD COLUMN active_customers_returning        INTEGER CHECK (active_customers_returning >= 0),
    ADD COLUMN active_customers_returning_bought INTEGER CHECK (active_customers_returning_bought >= 0),
    ADD CONSTRAINT platform_activity_totals_customers_whole CHECK (
        num_nulls(active_customers, active_customers_7d, active_customers_30d,
                  active_customers_new, active_customers_returning, active_customers_returning_bought) IN (0, 6)
    ),
    ADD CONSTRAINT platform_activity_totals_customers_split CHECK (
        active_customers_new + active_customers_returning = active_customers
        AND active_customers_returning_bought <= active_customers_returning
    );

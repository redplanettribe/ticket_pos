-- The House Organization designation (#472, parent #471, ADR 0060).
--
-- A House Organization is one the platform's own legal entity runs: its
-- Events are the platform's own, so a ticket sold there is the platform's
-- sale for tax purposes, and the platform's Issuer invoices the buyer for it.
-- Designated, and undesignated, by a Platform Operator from the Operator
-- Dashboard; an Org Admin cannot make one (CONTEXT.md, House Organization).
--
-- TWO COLUMNS AND NO FLAG. The designation IS the trail: an Organization is a
-- House Organization exactly when somebody designated it, so "who" and "when"
-- are the fact and a separate boolean would be a second copy of it that could
-- disagree. The CHECK holds the pair together — both set or both null — so
-- clearing the designation empties both and nothing can be designated by
-- nobody. The projection derives is_house_organization from the pair.
--
-- ON THE ORGANIZATION, not on a side table, because it is a property of the
-- Organization with no history to keep: undesignation affects future sales
-- only and withdraws nothing already owed (ADR 0060), so there is nothing a
-- ledger of past designations would be read for. A designation that is taken
-- back and given again names the later act.
--
-- The designating operator is stored by EMAIL, the way Payouts store
-- recorded_by and Tax Invoices store issued_by: the operator allowlist is a
-- set of addresses with no id of its own (ADR 0015), and the address is what
-- the trail is read as.
--
-- Nothing here gates on currency. That the Issuer invoices in USD alone is a
-- rule the identity service applies when the designation is made, so the one
-- place it lives can change with the Issuer; a CHECK frozen into the schema
-- would refuse the migration that one day widens it.
ALTER TABLE organizations
    ADD COLUMN house_designated_by TEXT,
    ADD COLUMN house_designated_at TIMESTAMPTZ,
    ADD CONSTRAINT organizations_house_designation_whole
        CHECK ((house_designated_by IS NULL) = (house_designated_at IS NULL));

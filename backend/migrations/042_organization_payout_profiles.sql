-- The Payout Profile: where an Organization is paid (ADR 0025). Until now an
-- Organization had a name, a slug, a currency and a logo, and nowhere to say
-- which bank account its settlement should reach — so every Payout began with a
-- WhatsApp thread asking which bank, savings or current, and whose cédula goes
-- on the factura. This table is that conversation, stored once.
--
-- It is current rather than historical: it says where to pay today. What a given
-- Payout Request was told to pay is a snapshot on that request, added when the
-- request table lands, precisely so a later edit here cannot rewrite the account
-- an operator was aimed at six months ago.
--
-- It lives in the `sales` module and not in `identity`, even though it hangs off
-- an Organization. Ownership follows meaning rather than the foreign key: a
-- Payout Profile is meaningless except for payouts, and `identity` — which owns
-- organizations, members and sessions — must not learn what a bank account or a
-- factura is. `payouts.organization_id` already points at `organizations` from
-- inside `sales` (migration 023), and `organizations` gains no columns here.
--
-- The account number is the first financial instrument this database holds. It
-- is stored in a plain column and not encrypted, deliberately: an Ecuadorian
-- account number is handed to anyone who owes you money and cannot be used to
-- pull funds, so application-level encryption would be decrypted on essentially
-- every operator read, with the key inside the same trust boundary as the
-- database credentials. The rules that do defend it — masked in list contexts,
-- never logged, never echoed in a validation error — are enforced above.
CREATE TABLE organization_payout_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- One profile per Organization, structurally. The UNIQUE constraint is what
    -- makes "the Organization's bank account" a phrase with one referent; the
    -- editor upserts onto it rather than choosing between two rows. CASCADE for
    -- the reason every child of an Organization cascades: where to pay an
    -- Organization that no longer exists is not a fact worth keeping.
    organization_id UUID NOT NULL UNIQUE REFERENCES organizations (id) ON DELETE CASCADE,
    -- The bank, as free text. Ecuador has dozens of cooperativas and a curated
    -- list's only real effect is locking out the one its author had not heard
    -- of; a human reads this field and types it into their own banking app,
    -- where "Banco Pichincha" serves exactly as well as an enum value would.
    bank_name TEXT NOT NULL CHECK (bank_name <> '' AND char_length(bank_name) <= 120),
    -- Savings or current, in Spanish, because that is the word on the receiving
    -- bank's own form. Omitting it would mean every transfer starting with a
    -- message asking which one — the precise friction this table exists to
    -- delete.
    account_type TEXT NOT NULL CHECK (account_type IN ('ahorros', 'corriente')),
    -- Digits only, and TEXT rather than a numeric type: leading zeros are real
    -- and load-bearing, and an account number that loses one reaches the wrong
    -- account. The regex is the last line of the same rule the API enforces
    -- after stripping the spaces and dashes an organizer copies off a statement.
    account_number TEXT NOT NULL CHECK (account_number ~ '^[0-9]{1,34}$'),
    -- The name on the account, which the sending bank checks against the number.
    account_holder_name TEXT NOT NULL CHECK (account_holder_name <> '' AND char_length(account_holder_name) <= 120),
    -- The Organization's own Tax ID, for the factura. `cedula` or `ruc` and
    -- never `passport`, though the buyer-facing columns added in migration 026
    -- take all three: those answer how to identify a buyer on a sales
    -- declaration, where a foreign tourist's passport is ordinary, while this
    -- one asks who is being invoiced and wired to — and a passport holder has no
    -- Ecuadorian bank account to receive it. Same validator with its check
    -- digits and province prefixes, narrower vocabulary.
    tax_id_type TEXT NOT NULL CHECK (tax_id_type IN ('cedula', 'ruc')),
    tax_id_number TEXT NOT NULL CHECK (tax_id_number <> '' AND char_length(tax_id_number) <= 20),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- When the details last changed, which is the one thing an operator about to
    -- transfer wants to know beyond the details themselves: a profile edited
    -- this morning is worth a second glance.
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

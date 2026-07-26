-- Fee snapshots on payment lines and Ticket Sale Lines (ADR 0014). Checkout
-- freezes the per-unit economics here at begin-checkout — the price the
-- Organization set, the Platform Fee and Fee IVA withheld from it, and the
-- rates that produced both — and confirm copies them onto the sale's lines. A
-- rate change or a Fee Handling flip afterwards therefore moves nothing already
-- recorded.
--
-- unit_price_cents keeps its meaning on both tables: what the Customer paid per
-- unit. Under 'pass_on' that is base + fee + Fee IVA; under 'absorb' it is the
-- base price. Net Proceeds per unit is unit_price_cents − fee_cents −
-- fee_iva_cents in either mode, so the split reads the same way whatever the
-- Event's Fee Handling was.
--
-- Zero is the honest default: only Online Sales carry a fee, so in-person and
-- imported lines (and every line recorded before this migration) read as no
-- withholding at all.

ALTER TABLE payment_lines
    ADD COLUMN base_price_cents INTEGER NOT NULL DEFAULT 0 CHECK (base_price_cents >= 0),
    ADD COLUMN fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
    ADD COLUMN fee_iva_cents INTEGER NOT NULL DEFAULT 0 CHECK (fee_iva_cents >= 0),
    ADD COLUMN fee_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (fee_basis_points >= 0),
    ADD COLUMN fee_iva_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (fee_iva_basis_points >= 0);

ALTER TABLE ticket_sale_lines
    ADD COLUMN base_price_cents INTEGER NOT NULL DEFAULT 0 CHECK (base_price_cents >= 0),
    ADD COLUMN fee_cents INTEGER NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
    ADD COLUMN fee_iva_cents INTEGER NOT NULL DEFAULT 0 CHECK (fee_iva_cents >= 0),
    ADD COLUMN fee_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (fee_basis_points >= 0),
    ADD COLUMN fee_iva_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (fee_iva_basis_points >= 0);

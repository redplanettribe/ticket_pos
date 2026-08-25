-- Tax invoicing: the Tax Invoice (#454, parent #450, ADR 0059).
--
-- A Tax Invoice is a legal artifact with a consumed sequence number: nothing
-- here is ever deleted, and there is no down path for an issued document. The
-- core row holds what every country's invoice has — a Recipient snapshot, an
-- Issuer snapshot, money in cents, the signed document and the authority's
-- answer — and the Ecuador detail row holds what only the SRI numbers a
-- document by. A second country is a second detail table, never a column here.

CREATE TABLE invoicing_invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issuer_id UUID NOT NULL REFERENCES invoicing_issuers (id),
    -- Copied from the Issuer at issue time so the list can say which country
    -- and which environment without a join, and so a flip of the Issuer's
    -- environment afterwards changes nothing about what was issued.
    country TEXT NOT NULL CHECK (country ~ '^[a-z]{2}$'),
    environment TEXT NOT NULL CHECK (environment IN ('test', 'production')),

    -- pending: the authority has not answered, or answered "still working",
    -- or could not be reached. authorized: the legal artifact exists.
    -- not_authorized: examined and refused. rejected: not taken at all.
    status TEXT NOT NULL CHECK (status IN ('pending', 'authorized', 'not_authorized', 'rejected')),

    -- The emission date as the authority sees it (today in the Issuer's
    -- country when Issue was pressed), and the instant it was pressed.
    issued_on DATE NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    -- The Platform Operator who pressed Issue, by email: the platform is the
    -- Issuer of record and this is who acted for it.
    issued_by TEXT NOT NULL CHECK (issued_by <> ''),

    -- The Recipient exactly as entered. Snapshot columns, not a reference:
    -- a Recipient is not a Customer and not necessarily an Organization.
    recipient_tax_id_type TEXT NOT NULL CHECK (recipient_tax_id_type IN ('cedula', 'ruc', 'passport')),
    recipient_tax_id TEXT NOT NULL CHECK (recipient_tax_id <> ''),
    recipient_legal_name TEXT NOT NULL CHECK (recipient_legal_name <> ''),
    recipient_address TEXT NOT NULL DEFAULT '',
    recipient_email TEXT NOT NULL DEFAULT '',

    -- The Issuer's details as they stood, as JSON: editing the Issuer
    -- afterwards changes nothing on the invoice, and a resend (#455) rebuilds
    -- the document from this and never from the live row.
    issuer_snapshot JSONB NOT NULL,

    currency TEXT NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
    subtotal_cents BIGINT NOT NULL CHECK (subtotal_cents >= 0),
    discount_cents BIGINT NOT NULL CHECK (discount_cents >= 0),
    iva_cents BIGINT NOT NULL CHECK (iva_cents >= 0),
    total_cents BIGINT NOT NULL CHECK (total_cents >= 0),
    -- The authority's payment method code; in Ecuador the forma de pago.
    payment_method TEXT NOT NULL CHECK (payment_method <> ''),

    -- The signed document exactly as sent — what the Recipient is handed
    -- (#456) — and the authority's own authorization document once it is
    -- authorized: the legal proof, kept for the years the SRI requires.
    signed_xml BYTEA NOT NULL,
    authorization_xml BYTEA,
    -- The authority's messages from its last answer, verbatim, as a JSON
    -- array of {identifier, message, additional_info, type}. The full history
    -- is on invoicing_attempts.
    last_messages JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX invoicing_invoices_newest_first ON invoicing_invoices (issued_at DESC, created_at DESC, id DESC);

CREATE TABLE invoicing_invoice_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id UUID NOT NULL REFERENCES invoicing_invoices (id),
    position INT NOT NULL CHECK (position >= 1),
    description TEXT NOT NULL CHECK (description <> ''),
    -- Quantity in millionths: the SRI allows six decimals.
    quantity_millionths BIGINT NOT NULL CHECK (quantity_millionths > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0),
    discount_cents BIGINT NOT NULL CHECK (discount_cents >= 0),
    -- The platform's word for the rate: 15, 0, exento, no_objeto. The
    -- adapter translates to the authority's code.
    iva_rate TEXT NOT NULL CHECK (iva_rate IN ('15', '0', 'exento', 'no_objeto')),
    -- The arithmetic as stored on the document, so a detail page and a RIDE
    -- never recompute a legal artifact.
    base_cents BIGINT NOT NULL CHECK (base_cents >= 0),
    iva_cents BIGINT NOT NULL CHECK (iva_cents >= 0),

    UNIQUE (invoice_id, position)
);

-- The operator's extra name/value pairs; the Recipient's email is on the
-- invoice row and is written to the document automatically, which is why
-- the operator may add one fewer than the SRI's fifteen.
CREATE TABLE invoicing_additional_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id UUID NOT NULL REFERENCES invoicing_invoices (id),
    position INT NOT NULL CHECK (position >= 1),
    name TEXT NOT NULL CHECK (name <> ''),
    value TEXT NOT NULL CHECK (value <> ''),

    UNIQUE (invoice_id, position)
);

-- What only the SRI numbers a factura by. The clave de acceso is the
-- authority's reference for the document and the offline scheme's
-- authorization number; the six-column uniqueness is the SRI's own rule
-- (error 45: secuencial registrado) stated where nothing can get past it.
CREATE TABLE invoicing_invoices_ec (
    invoice_id UUID PRIMARY KEY REFERENCES invoicing_invoices (id),
    issuer_id UUID NOT NULL REFERENCES invoicing_issuers (id),
    environment TEXT NOT NULL CHECK (environment IN ('test', 'production')),
    cod_doc TEXT NOT NULL CHECK (cod_doc ~ '^[0-9]{2}$'),
    estab TEXT NOT NULL CHECK (estab ~ '^[0-9]{3}$'),
    pto_emi TEXT NOT NULL CHECK (pto_emi ~ '^[0-9]{3}$'),
    secuencial BIGINT NOT NULL CHECK (secuencial >= 1 AND secuencial <= 999999999),
    access_key TEXT NOT NULL UNIQUE CHECK (access_key ~ '^[0-9]{49}$'),
    -- The authorization the SRI granted: its number (the clave, in the
    -- offline scheme) and the fechaAutorizacion. NULL until authorized.
    authorization_number TEXT,
    authorization_date TIMESTAMPTZ,

    UNIQUE (issuer_id, environment, cod_doc, estab, pto_emi, secuencial)
);

-- Every request made to the authority for an invoice, append-only: when,
-- which operation, what came back, how long it took. What the operator reads
-- instead of server logs.
CREATE TABLE invoicing_attempts (
    id BIGSERIAL PRIMARY KEY,
    invoice_id UUID NOT NULL REFERENCES invoicing_invoices (id),
    -- submit: hand the document over (SRI recepción). query: ask what became
    -- of it (SRI autorización).
    operation TEXT NOT NULL CHECK (operation IN ('submit', 'query')),
    -- received / authorized / not_authorized / rejected as the core reads the
    -- authority's answer, or error when no answer could be read.
    outcome TEXT NOT NULL CHECK (outcome IN ('received', 'authorized', 'not_authorized', 'rejected', 'error')),
    messages JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- The transport or protocol error when outcome is error; '' otherwise.
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    duration_ms INT NOT NULL CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX invoicing_attempts_by_invoice ON invoicing_attempts (invoice_id, id);

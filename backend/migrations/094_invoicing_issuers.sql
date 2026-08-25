-- Tax invoicing, first cut: the Issuer (#451, parent #450, ADR 0059).
--
-- The platform is the sole Issuer — one registration with one country's Tax
-- Authority, held by Platform Operators, never by an Organization. This
-- migration lands the Issuer and nothing else: no certificate columns (#452),
-- no Tax Invoices (#454), nothing to freeze against (#453). The tables it does
-- add are shaped for what follows so that none of them has to be reshaped.
--
-- THIN CORE, COUNTRY ADAPTER. `invoicing_issuers` is the cross-country row:
-- which country, which of the authority's environments it points at, and the
-- custody and metadata columns later migrations add. Everything the SRI cares
-- about that Colombia's DIAN or Brazil's SEFAZ would not is on a per-country
-- detail row, `invoicing_issuers_ec`, keyed one-to-one on the core row. A
-- second country is a second detail table and never a column here.
CREATE TABLE invoicing_issuers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- ISO 3166-1 alpha-2, lower-case, as it appears in the route path
    -- (/operator/invoicing/issuers/ec). UNIQUE is the "at most one Issuer per
    -- country" rule from CONTEXT.md, stated where nothing can get past it.
    country TEXT NOT NULL UNIQUE CHECK (country ~ '^[a-z]{2}$'),

    -- Which of the authority's environments the Issuer points at. The words
    -- are the platform's, not the SRI's digits (`test` is ambiente 1 pruebas,
    -- `production` is ambiente 2 producción): the adapter translates, so the
    -- core never learns a code that means nothing in another country. The
    -- operator may flip it freely; sequences are keyed per environment, and
    -- every Tax Invoice will record the one it was issued under.
    environment TEXT NOT NULL CHECK (environment IN ('test', 'production')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The Ecuador Issuer's details: what the SRI expects in every factura's
-- infoTributaria and infoFactura. Column names are the SRI's Spanish, because
-- these are the SRI's fields and an operator reads them off the RUC
-- certificate in that language; translating them would put a second name on
-- every one of them.
CREATE TABLE invoicing_issuers_ec (
    issuer_id UUID PRIMARY KEY REFERENCES invoicing_issuers (id) ON DELETE CASCADE,

    -- Validated as the platform validates a `ruc` Tax ID; the CHECK is the
    -- shape only, the province and check-digit rules live in Go.
    ruc TEXT NOT NULL CHECK (ruc ~ '^[0-9]{13}$'),
    razon_social TEXT NOT NULL CHECK (razon_social <> ''),
    -- Optional on the SRI's schema too; an empty string is "none".
    nombre_comercial TEXT NOT NULL DEFAULT '',
    direccion_matriz TEXT NOT NULL CHECK (direccion_matriz <> ''),
    direccion_establecimiento TEXT NOT NULL CHECK (direccion_establecimiento <> ''),
    -- The establecimiento and punto de emisión the next factura is numbered
    -- under: three digits each, exactly as they appear in the número de
    -- comprobante (001-001-000000001). Both freeze once a sequence row exists
    -- under them (#453).
    establecimiento TEXT NOT NULL CHECK (establecimiento ~ '^[0-9]{3}$'),
    punto_emision TEXT NOT NULL CHECK (punto_emision ~ '^[0-9]{3}$'),
    obligado_contabilidad BOOLEAN NOT NULL,
    -- The three régimenes the factura schema distinguishes: general prints
    -- nothing, the two RIMPE values print the SRI's contribuyenteRimpe legend.
    regimen TEXT NOT NULL CHECK (regimen IN ('general', 'rimpe_contribuyente', 'rimpe_negocio_popular')),
    -- The resolución number if the Issuer is an agente de retención; NULL is
    -- "not one".
    agente_retencion TEXT CHECK (agente_retencion IS NULL OR agente_retencion <> ''),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The secuencial the next factura takes, per (Issuer, environment, document
-- type, establecimiento, punto de emisión) — the five things the SRI numbers
-- documents by. A row is created on first use and bumped with a single
-- `UPDATE … RETURNING`, which is what makes two operators pressing Issue at
-- the same moment get two distinct numbers (#454). It lands now, empty,
-- because its existence is what freezes establecimiento and punto de emisión
-- (#453): "a sequence has started under them" is "a row exists here".
CREATE TABLE invoicing_sequences_ec (
    issuer_id UUID NOT NULL REFERENCES invoicing_issuers (id) ON DELETE CASCADE,
    environment TEXT NOT NULL CHECK (environment IN ('test', 'production')),
    -- The SRI's código de documento: '01' factura. Only facturas are issued
    -- today; the column is here so a nota de crédito ('04') numbers separately
    -- the day one exists.
    cod_doc TEXT NOT NULL CHECK (cod_doc ~ '^[0-9]{2}$'),
    estab TEXT NOT NULL CHECK (estab ~ '^[0-9]{3}$'),
    pto_emi TEXT NOT NULL CHECK (pto_emi ~ '^[0-9]{3}$'),
    -- The last number handed out; 0 means none yet. Nine digits is the SRI's
    -- width for a secuencial.
    last_secuencial BIGINT NOT NULL DEFAULT 0 CHECK (last_secuencial >= 0 AND last_secuencial <= 999999999),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (issuer_id, environment, cod_doc, estab, pto_emi)
);

-- Tax invoicing: the Issuer's signing certificate (#453, parent #450, ADR 0059).
--
-- The platform's .p12 and its password live on the Issuer row, each AES-256-GCM
-- encrypted under INVOICING_CERTIFICATE_KEY (Secret Manager -> env), and only
-- the certificate's metadata is in clear: what the Issuer page shows, and what
-- the platform needs to know which certificate signs and when it expires
-- without opening it. A re-upload replaces every column here outright.
--
-- These sit on the cross-country core row and not on invoicing_issuers_ec,
-- because every Tax Authority that takes a signed document wants a key in
-- custody; only the RUC hint is Ecuador-flavoured, and it is metadata found
-- inside the certificate rather than a rule the core enforces.
ALTER TABLE invoicing_issuers
    -- nonce || ciphertext || tag, as invoicing.Custody seals it. Never read
    -- back by any API; opened into memory only at signing (#454).
    ADD COLUMN certificate_p12 BYTEA,
    ADD COLUMN certificate_password BYTEA,

    -- Metadata in clear. Subject is the RFC 2253 distinguished name.
    ADD COLUMN certificate_subject TEXT,
    -- The 13-digit RUC found in the subject or an extension, or '' when the
    -- certificate carries none the platform could discover. The read compares
    -- it with the Issuer's RUC and reports a mismatch as a WARNING: some CAs
    -- put the RUC somewhere else, and the SRI's own check is the final word.
    ADD COLUMN certificate_ruc TEXT,
    ADD COLUMN certificate_not_before TIMESTAMPTZ,
    ADD COLUMN certificate_not_after TIMESTAMPTZ,
    -- Lowercase hex SHA-256 of the DER certificate: what the operator compares
    -- across a re-upload.
    ADD COLUMN certificate_fingerprint_sha256 TEXT,
    ADD COLUMN certificate_uploaded_at TIMESTAMPTZ,

    -- A certificate is all of these or none of them: no row can hold sealed
    -- bytes without a password to open them, or metadata with nothing behind it.
    ADD CONSTRAINT invoicing_issuers_certificate_complete CHECK (
        num_nulls(
            certificate_p12, certificate_password, certificate_subject, certificate_ruc,
            certificate_not_before, certificate_not_after,
            certificate_fingerprint_sha256, certificate_uploaded_at
        ) IN (0, 8)
    ),
    ADD CONSTRAINT invoicing_issuers_certificate_ruc_shape CHECK (
        certificate_ruc IS NULL OR certificate_ruc = '' OR certificate_ruc ~ '^[0-9]{13}$'
    ),
    ADD CONSTRAINT invoicing_issuers_certificate_fingerprint_shape CHECK (
        certificate_fingerprint_sha256 IS NULL OR certificate_fingerprint_sha256 ~ '^[0-9a-f]{64}$'
    );

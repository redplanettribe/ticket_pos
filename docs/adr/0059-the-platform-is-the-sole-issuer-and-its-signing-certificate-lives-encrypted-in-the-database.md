# The platform is the sole Issuer, and its signing certificate lives encrypted in the database

Ecuador's SRI requires every taxpayer to issue electronic facturas, signed XAdES-BES with a `.p12`
certificate held in the name of the emisor's RUC (`docs/research-sri-facturacion-electronica.md`). The
platform sells one taxable service — the Platform Fee, with its Fee IVA — and the tickets are the
Organization's sale, taxed outside the system (ADR 0014, `CONTEXT.md`). We decided that **the platform
is the only Issuer**: one RUC, one certificate, one set of SRI authorizations per environment, and a
Platform Operator issuing Tax Invoices by hand from a form. An Organization never uploads a certificate,
never certifies against the SRI through us, and never invoices a ticket buyer here.

We also decided **where that certificate lives: in Postgres, `.p12` bytes and password each encrypted
with AES-256-GCM under a single key delivered from Secret Manager as an environment variable**, the way
the Confirmation Link key already is. Only certificate metadata (subject, RUC, validity window,
fingerprint) is stored in clear. The key material is decrypted into memory at signing time and nowhere
else; a re-upload replaces the old certificate outright.

## Considered options

- **Each Organization as emisor.** The business model's natural reading — but it makes the platform a
  third-party billing provider (Ficha Anexo 26: our RUC in every factura), demands N certificates in
  custody, N pruebas-then-producción certifications walked through by Organizations, and reverses the
  glossary's stance that ticket taxation stays outside the system. Nothing in the first, manual, fee
  invoicing needs it. It can be added later as more Issuers; it cannot be undone once Organizations rely
  on it.
- **Secret Manager per certificate.** Needs a Secret Manager client, create/version IAM, and a code path
  that differs between the laptop and Cloud Run — for at most one secret per country. The object storage
  buckets are public (covers, logos, avatars), so they are not an option for a private key at all.
- **Ask the operator for the password on every signing.** Safer custody; but the manual form is the
  first step towards issuing on every Sale, and an automated path cannot ask.

## Consequences

- A leaked database backup exposes ciphertext only; a leaked `INVOICING_CERTIFICATE_KEY` plus a backup
  exposes the platform's signing key. Rotating the key means re-encrypting one row per country.
- The country seam is an Issuer per country and a Tax Authority adapter behind it; a second country is
  a second adapter and a second Issuer row, never a second custody scheme.
- Every Tax Invoice is a legal artifact with a consumed sequence number: nothing deletes one, and the
  signed XML and the authority's authorization XML are kept in the row for the seven years the SRI
  requires.

# Research: automatic electronic invoicing to the SRI (Ecuador) for every Sale

Date: 2026-08-23. Author: research agent, for the Ticket POS repository.

Scope: everything needed to design a module that issues an electronic
**comprobante** (factura, and nota de crédito on reversal) to Ecuador's tax
authority, the **SRI** (Servicio de Rentas Internas), for each Ticket Sale, with
queuing and retry because the SRI is periodically unavailable.

Sourcing rule for this document: every technical or legal claim is tied to a
primary source (sri.gob.ec pages and PDFs, the SRI *Ficha Técnica de
Comprobantes Electrónicos — Esquema Off-line*, the official XSD bundle, the
live WSDLs, SRI resolutions, the Reglamento de Comprobantes de Venta), or to a
named open-source repository when the claim is about that repository. Anything
that could not be confirmed at such a source is marked **UNVERIFIED**.
Secondary sources (law-firm and vendor blogs) are named only where they were the
route to a primary document, and never as the authority for a claim.

Terminology: Spanish official names are kept (factura, clave de acceso, RIDE,
nota de crédito, consumidor final, ambiente de pruebas/producción). Repo
vocabulary (Sale, Ticket Sale, Organization, Customer, Tax ID, Sale Reversal,
Sales Channel) follows `CONTEXT.md`.

---

## 0. Executive summary

- **The regime is the "esquema off-line"**: the issuer builds the XML, computes
  a 49-digit *clave de acceso* that is also the authorization number, signs it
  **XAdES-BES / RSA-SHA1 (enveloped)**, POSTs it to the SOAP endpoint
  `RecepcionComprobantesOffline.validarComprobante`, then polls
  `AutorizacionComprobantesOffline.autorizacionComprobante` until the SRI says
  `AUTORIZADO` or `NO AUTORIZADO`. (Ficha Técnica v2.34 §5–7; sources S1, S2.)
- **Transmission must be immediate since 2026-01-01.** Resolution
  NAC-DGERCGC25-00000014 deleted the "up to four business days" grace from
  Art. 7 of NAC-DGERCGC18-00000233; the remaining text says the comprobante is
  transmitted "en el momento mismo de realizarse la generación". The SRI itself
  still promises to answer within 24 hours of `RECIBIDA`, and tells issuers to
  wait and poll. So the queue is not optional, but its job is *"send now, and
  keep trying until authorized"*, not *"batch tonight"*. (S9, S8, S1 §7.5.)
- **A ticket sale to a person is a factura**, usually with `tipoIdentificacionComprador=05/06/04`
  (cédula/pasaporte/RUC). "Consumidor final" (`07`, id `9999999999999`) is
  allowed **only up to USD 50** per factura, and a consumidor-final factura can
  **never** be annulled or credited afterwards (Res. 25-14 Art. 3). Since the
  platform already collects a Tax ID at checkout (ADR 0016), the module should
  always identify the buyer and treat consumidor final as the exception for
  the `import` channel where the Tax ID is nullable. (S1 §9.10, S6 Q34, S9 Art. 3.)
- **IVA is 15% (código 4) since 2024-04-01**; artistic/cultural shows can be
  0% (código 0) only if the organizer is in the RUAC and capacity ≤ 2,000.
  ICE is not levied on event tickets (no such item on the SRI ICE page or in
  Tabla 18 of the Ficha). (S12, S13, S1 Tabla 17/18.)
- **Reversals are notas de crédito** (codDoc `04`), which must identify the
  buyer (never consumidor final) and reference the factura. Online
  "anulación" via SRI en línea exists only until day 7 of the following month
  and is a manual portal action, so for an automated system a nota de crédito
  is the primary instrument. (S1 Tabla 6 note, S9, S10.)
- **The certificate is the emisor's**: a `.p12` from an ARCOTEL-accredited
  entity, in the name of the RUC that appears in `<ruc>`. Whether the platform
  is the emisor (one RUC, one certificate) or each Organization is (N RUCs, N
  certificates stored server-side, N ambiente-de-producción authorizations) is
  the single biggest open decision (section 6). And since Ficha 2.34 (July
  2026) a comprobante issued through a third-party billing system must carry
  the **provider's RUC** in `infoAdicional` ("RUC Proveedor"). (S1 Anexo 26.)
- **Libraries**: nothing in Go or TypeScript is "mature" in the sense of
  widely-used-and-maintained; the SRI's own validators are Java (MITyC). Best
  candidates: Go `nelsonmarro/go_ec_sri_invoice_signer` (MIT, tiny) on top of
  `russellhaering/goxmldsig` (Apache-2.0) and `SSLMate/go-pkcs12` (BSD-3);
  TypeScript `bryancalisto/ec-sri-invoice-signer` (MIT) and
  `miguelangarano/open-factura` (MIT). Pitfalls: inclusive C14N, namespace
  handling, SHA-1, and legacy RC2-40 `.p12` encryption (section 5).

---

## 1. Legal and registration prerequisites

### 1.1 Who must issue electronic comprobantes

- Since 2022-11-30, **every** sujeto pasivo del Impuesto a la Renta obligated to
  invoice, and every natural/legal person not an income-tax subject but
  obligated to invoice, must issue comprobantes electrónicos (Res.
  NAC-DGERCGC22-00000024, Arts. 1–3 and Disposición Transitoria Tercera). The
  only carve-out is *negocios populares* under RIMPE, who may keep using
  pre-printed notas de venta. (S7; S6 Q30: "todos los sujetos pasivos están
  obligados a emitir comprobantes electrónicos, a excepción de los
  contribuyentes considerados negocios populares".)
- The SRI page of obligated taxpayers lists the phased calendar ending with the
  2022-11-30 group. (S14.)
- **Consequence for Ticket POS**: an Organization with a RUC selling tickets is
  already obligated. If it is not issuing facturas for its ticket sales today,
  it is out of compliance regardless of what the platform does. Fines for
  *not transmitting* electronic comprobantes range from 1 RBU (negocio
  popular) to 30 RBU (gran contribuyente) per infraction, and "no entregar"
  from 1 to 20 RBU (Res. NAC-DGERCGC24-00000022 Art. 2, S15).

### 1.2 RUC and the two SRI en línea authorizations

- The emisor needs an **active RUC** with the economic activity registered, and
  at least one **establecimiento** (3 digits, e.g. `001`) and **punto de
  emisión** (3 digits, e.g. `001`). Together they form the 6-digit `serie`
  (`estab` + `ptoEmi`) that goes into the clave de acceso and the XML
  (`<estab>`, `<ptoEmi>`). (S1 Tabla 1.) Establecimiento codes come from the
  RUC registration; punto de emisión codes are the taxpayer's own. **UNVERIFIED
  at a primary source**: whether puntos de emisión must be pre-registered in SRI
  en línea for the electronic scheme (the validation order in S1 only checks
  "Establecimiento activo", not the punto de emisión).
- **Ambiente de pruebas** authorization: requested online in SRI en línea →
  Facturación electrónica → Pruebas → Autorización → Solicitud de
  autorizaciones; free; needs only the RUC and the online-services password.
  Documents issued there "no tienen validez tributaria". (S16, S6 Q4–Q7.)
- **Ambiente de producción** authorization: same path under "Producción", to
  be requested "previo debe obtener la certificación primero en pruebas". (S6
  Q8, S17.) The Ficha adds that the emisor "deberá acceder una vez que ha
  realizado las pruebas y esté seguro de que su aplicación funciona
  correctamente" (S1 §7.2.2). **UNVERIFIED**: whether the SRI checks any minimum
  number of test documents before granting producción, or whether the request
  is granted automatically.
- Validation the SRI runs on receipt (S1, table after error codes): XML schema
  → RUC active, establecimiento active, authorization to issue electronic
  comprobantes active, authorization for that document type → uniqueness of
  clave de acceso and secuencial → signature validity, trust chain, OCSP →
  emission date, receptor identification, supporting documents → arithmetic.

### 1.3 Firma electrónica (certificado .p12)

- Format: **PKCS#12 (`.p12`)**, X.509, RSA 2048-bit, signing algorithm
  **RSA-SHA1** (S1 §6.8). Only certificates from ARCOTEL-accredited entities
  are accepted; the SRI page lists the entities and links: ANFAC, ARGOSDATA,
  Banco Central del Ecuador (eci.bce.ec), Consejo de la Judicatura, Datilmedia,
  Eclipsoft, Security Data, Uanataca, Lazzate/eNEXT, Firma Segura EC, Newbest.
  (S2.)
- For a company, the certificate is issued to the **representante legal** (or a
  "miembro de empresa") and tied to the company RUC. Certificates expire
  (validity is set by each entity; typically 1–5 years) and the SRI FAQ notes
  "La firma electrónica tiene un tiempo de vigencia que es definido por cada
  entidad" (S6 Q10). **UNVERIFIED**: prices and the exact online issuance flow
  of the BCE (its site redirects to a portal that could not be fetched).
- The RUC inside the certificate must match `<ruc>` in the XML; the SRI's
  troubleshooting table says to open the signed file and check "el tag que
  contiene el dato del RUC" (S1 §14 table).

### 1.4 Clave de contingencia

- **There is no contingency emission in the offline scheme.** Ficha v2.34 Tabla
  2 lists exactly one tipo de emisión, `1` (normal), with the footnote "Para el
  método de autorización offline, solo existe el tipo de emisión normal", and
  the change log records "Se elimina las claves de uso complementario
  (contingencia)". (S1 §5.3.) The 2017 SRI comparison document says the same for
  the RIDE ("Tipo de emisión: Normal"). (S3.)
- What replaces it is Art. 8 of NAC-DGERCGC18-00000233 ("casos excepcionales"):
  under force majeure the emisor may issue **pre-printed** comprobantes up to
  1% of last year's volume (5% during the 2024 electricity emergency). (S18
  quoting Art. 8; S8.) For a platform this is not automatable; the practical
  reading is: the platform keeps trying, and the legal exposure of a late
  transmission is the emisor's.

### 1.5 Factura vs nota de venta; consumidor final

- Facturas are issued "con ocasión de la transferencia de bienes, de la
  prestación de servicios" (Reglamento Art. 11); **notas de venta** are reserved
  for the simplified regime (Art. 12), i.e. RIMPE negocios populares today. An
  Organization on the platform therefore issues **facturas**. (S5.)
- Consumidor final: the factura carries `tipoIdentificacionComprador = 07` and
  `identificacionComprador = 9999999999999` (S1 Tabla 6 note). The ceiling: "Si
  el valor de la factura es mayor a 50 USD se deberá especificar
  obligatoriamente los datos del adquirente" (S1 §9.10) and "por montos de hasta
  USD. 50.00" (S6 Q34). The Reglamento text on hand (2015 consolidation, S5
  Art. 19.1) still reads US$ 200; the reduction to US$ 50 was made by a later
  Decreto Ejecutivo (reported as D.E. 586 — **UNVERIFIED** at a primary source,
  but the USD 50 figure itself is stated by two SRI documents, S1 and S6).
- **A consumidor-final factura cannot be annulled or credited**: "Las facturas
  electrónicas emitidas con la leyenda 'consumidor final' no se podrán anular
  una vez emitidas y transmitidas al Servicio de Rentas Internas. En estos
  casos, no procede la emisión de notas de crédito." (Res. 25-14 Art. 3, S9.)
  The SRI may also annul such facturas *de oficio* during audits (Res. 25-17
  adds Disposición General Quinta, S10). For a platform that supports Sale
  Reversals this is decisive: **identify the buyer whenever a Tax ID exists**.
- Identification codes (S1 Tabla 6): `04` RUC, `05` cédula, `06` pasaporte,
  `07` consumidor final, `08` identificación del exterior. Mapping from the
  repo's `platform.TaxIDType` (`ruc`, `cedula`, `passport`) is direct; the SRI
  warns (advertencia 62) when a cédula fails its check digit, which
  `platform.ValidateTaxID` already enforces.
- RIMPE emisores must add `<contribuyenteRimpe>CONTRIBUYENTE RÉGIMEN RIMPE</contribuyenteRimpe>`
  (or the NEGOCIO POPULAR variant) inside `<infoTributaria>` after
  `<agenteRetencion>` (S1 Anexo 22). Whether an Organization is RIMPE is a fact
  the platform does not hold today.

### 1.6 IVA and ICE on event tickets

- **General IVA is 15%**, "en vigencia desde el 1 de abril de 2024, per Decreto
  Ejecutivo No. 198", with 0%, 5% (construction materials) and 8% (tourism,
  time-boxed) also existing (S12). Ficha Tabla 17 codes: `0`→0%, `2`→12%,
  `3`→14%, `4`→15%, `5`→5%, `6`→No objeto, `7`→Exento, `8`→IVA diferenciado,
  `10`→13% (S1 §9.13). Tabla 16: impuesto IVA = código `2`, ICE = `3`,
  IRBPNR = `5`.
- **0% for artistic and cultural shows** exists (Decretos Ejecutivos 829 and
  830; LRTI Arts. 52/55 per S13): the event must be organised by a promoter
  registered in the **RUAC** (or held in a registered cultural space) and have
  a **maximum capacity of 2,000**; otherwise the standard 15% applies. The
  SRI catalogue of qualifying activities lists "Servicios de organización,
  producción y presentación de espectáculos artísticos y culturales" (CIIU
  R9000.01.03 / R9000.02.03) (S19). This means the **IVA rate is a per-Event
  (or per-Organization) attribute** the module must be told; it cannot be
  inferred.
- **ICE**: the SRI ICE page lists goods (cigarettes, alcohol, beer, plastic
  bags…) and Ficha Tabla 18 lists ICE codes for goods and telephony services;
  neither mentions espectáculos or entradas (S20, S1 Tabla 18). Treat ICE as
  not applicable to tickets. **UNVERIFIED**: the full current text of LRTI
  Art. 82 was not fetched.
- The repo's existing "Fee IVA" (15% on the Platform Fee, `CONTEXT.md`) is the
  *platform's* tax on its own service. That is a separate factura the platform
  issues to the Organization, and it is out of scope here unless the platform
  is also the emisor of ticket facturas (section 6).

### 1.7 Delivery to the buyer and record keeping

- The emisor must deliver **XML + RIDE** to the email address the buyer gave;
  if no email, print and hand over the RIDE (S6 Q21). The RIDE is required
  when the buyer is not identified, asks for it, or email delivery fails (S6
  Q15). A RIDE without the clave de acceso/número de autorización "no tiene
  validez" (S6 Q20). The RIDE is not archived; the **XML must be kept seven
  years** by emisor and receptor (S6 Q18–19; Reglamento Art. 50 for annulled
  documents, S5).
- Since Res. 25-14 (Disposición Reformatoria Primera §1) emisores must also
  inform receptors of "cualquier modificación que se realice al estado del
  comprobante electrónico" — i.e. the buyer must be told when a factura is
  annulled or credited. (S9.)
- Delivery requires the buyer's prior consent to receive electronic documents
  (NAC-DGERCGC18-00000233 Art. 5, as quoted in S18 — **secondary
  transcription**; the original PDF was not fetched).

---

## 2. Technical flow of the esquema off-line

### 2.1 XML versions and schemas

- Official XSD bundle for facturas (S4) contains `factura_V1.0.0`,
  `factura_V1.1.0`, `factura_V2.0.0`, `factura_V2.1.0` (.xsd and sample .xml).
  **Versions 2.0.0/2.1.0 exist only for "rubros de terceros"** ("Incluyen los
  campos requeridos exclusivamente para rubros de terceros, caso contrario se
  deberá utilizar los formatos de factura establecidos en el anexo 1 y anexo 3",
  S1 Anexo 7). A ticket factura should therefore be **`<factura id="comprobante" version="1.1.0">`**
  (Anexo 3). Version 1.1.0 allows 6 decimals in `cantidad` and
  `precioUnitario`; 1.0.0 allows 2 (S1 §9.17 and §14 table). Error 36 ("Versión
  esquema descontinuada") is returned for retired versions.
- Notas de crédito: `<notaCredito id="comprobante" version="1.1.0">` (S1 Anexo
  3), codDoc `04`, with `<codDocModificado>01</codDocModificado>`,
  `<numDocModificado>001-001-000000123</numDocModificado>`,
  `<fechaEmisionDocSustento>` and `<motivo>`.
- Other structural rules from S1: values `123456.98` with a dot, max 2
  decimals except quantity/unit price; `&` must be written `&amp;` or the
  document is rejected (S1 §15 Glosario); up to 15 `<campoAdicional>` of 300
  chars each (S1 §9.11); `<obligadoContabilidad>SI|NO</obligadoContabilidad>`
  in `infoTributaria`; `<pagos><pago><formaPago>01</formaPago>...` (Tabla of
  formas de pago in S1 Anexo 21: `01` sin utilización del sistema financiero,
  `16` tarjeta de débito, `19` tarjeta de crédito, `20` otros con utilización
  del sistema financiero, etc. — read the table before mapping the repo's
  Payment Method).
- **New in v2.34 (2026-07-27): Anexo 26** — comprobantes issued through a
  third-party electronic-billing provider must carry
  `<campoAdicional nombre="RUC Proveedor">{RUC of the provider}</campoAdicional>`
  in `<infoAdicional>`, mandatory 60 calendar days after publication of Res.
  NAC-DGERCGC26-00000027 in the Registro Oficial (S1 Anexo 26). **If the
  Organizations are the emisores, Ticket POS is that provider and must put its
  own RUC in every factura.** Also new in v2.33: a `<placa>` tag for transport
  operators (not applicable).

### 2.2 The 49-digit clave de acceso (S1 §5.2, Tabla 1)

| # | Field | Format | Length |
|---|-------|--------|--------|
| 1 | Fecha de emisión | `ddmmaaaa` | 8 |
| 2 | Tipo de comprobante | Tabla 3 (`01` factura, `04` nota de crédito, `05` nota de débito, `06` guía, `07` retención, `03` liquidación) | 2 |
| 3 | RUC del emisor | 13 digits | 13 |
| 4 | Tipo de ambiente | `1` pruebas, `2` producción | 1 |
| 5 | Serie | `estab`+`ptoEmi`, e.g. `001001` | 6 |
| 6 | Secuencial | zero-padded | 9 |
| 7 | Código numérico | any 8 digits, emisor's choice ("potestad absoluta del contribuyente emisor") | 8 |
| 8 | Tipo de emisión | `1` | 1 |
| 9 | Dígito verificador | módulo 11 | 1 |

Check digit (S1 §5.2 and worked example): take the 48 digits, multiply from the
**rightmost** digit leftwards by the weights 2,3,4,5,6,7,2,3,4,… , sum, compute
`11 - (sum mod 11)`; if the result is 11 the digit is `0`, if 10 the digit is
`1`, else the result itself. The Ficha's example on `41261533` (weights
3,2,7,6,5,4,3,2 left-to-right) gives sum 104, 104 mod 11 = 5, 11 − 5 = 6.
"Si en el número secuencial no completa los 9 dígitos, la clave de acceso
estará mal conformada y será motivo de rechazo". The clave de acceso **is the
número de autorización** in the offline scheme (S1 §5.9, S3), so the XML's
`<claveAcceso>` and the RIDE's "Número de autorización" are the same value.

Because the date is part of the key, `fechaEmision` in the XML and the first 8
digits must agree, and error 58 ("Error en la estructura de clave acceso:
componentes diferentes a los del comprobante") catches any mismatch —
including RUC, ambiente, serie, secuencial, tipo emisión.

### 2.3 Signature: XAdES-BES, enveloped, RSA-SHA1

- Standard XAdES-BES (ETSI TS 101 903 v1.3.2), **enveloped**, UTF-8; the
  signature is "un nodo más" appended to the document; `ds:KeyInfo` must carry
  the signing certificate in base64 and be itself signed; three things are
  covered: the whole comprobante, `SignedProperties`, and `KeyInfo` (S1
  §6.1–6.5).
- Algorithms: "Algoritmo de firmado: RSA-SHA1", key 2048 bits, PKCS#12 file
  (S1 §6.8). The Ficha's worked signature (Anexo 4) uses
  `http://www.w3.org/2000/09/xmldsig#rsa-sha1` and `...#sha1` digests, and
  `<ds:RSAKeyValue>` in KeyInfo. **There is no mention of SHA-256 anywhere in
  Ficha v2.34** (grep of the full text). Whether the SRI validator *also accepts*
  RSA-SHA256 is **UNVERIFIED**; the Go signer library exposes SHA-256 as an
  option but calls SHA-1 "the current SRI standard" (S24). Build for SHA-1,
  keep the algorithm switchable.
- The SRI validated signatures with the Java **MITyCLibXADES** stack (S1 §6.6).
  Practical consequences reported by every implementation (S24, S25):
  inclusive canonicalization `REC-xml-c14n-20010315` for the references,
  `xmlns` declared locally on the `ds:Signature`/`etsi:` nodes, no
  DOCTYPE/prefixed root, and the `Reference URI="#comprobante"` pointing at the
  root's `id="comprobante"` attribute. Error 39 ("Firma inválida") is what all
  of these produce.
- Validation order matters (S1): a bad signature is only reported *after*
  schema, emisor, and uniqueness checks pass, so a document can be `RECIBIDA`
  and then `NO AUTORIZADO` with 39.

### 2.4 Web services (from the live WSDLs and S1 §7.2)

| Env | Recepción | Autorización |
|-----|-----------|--------------|
| Pruebas | `https://celcer.sri.gob.ec/comprobantes-electronicos-ws/RecepcionComprobantesOffline?wsdl` | `https://celcer.sri.gob.ec/comprobantes-electronicos-ws/AutorizacionComprobantesOffline?wsdl` |
| Producción | `https://cel.sri.gob.ec/comprobantes-electronicos-ws/RecepcionComprobantesOffline?wsdl` | `https://cel.sri.gob.ec/comprobantes-electronicos-ws/AutorizacionComprobantesOffline?wsdl` |

- `RecepcionComprobantesOfflineService`, namespace
  `http://ec.gob.sri.ws.recepcion`, document-literal SOAP 1.1. Operation
  **`validarComprobante(xml: base64Binary)`** → `RespuestaSolicitud { estado,
  comprobantes[ { claveAcceso, mensajes[ { identificador, mensaje,
  informacionAdicional, tipo } ] } ] }`. `estado` is `RECIBIDA` or `DEVUELTA`.
  (S21, S1 §7.2.3.)
- `AutorizacionComprobantesOfflineService`, namespace
  `http://ec.gob.sri.ws.autorizacion`. Operations
  **`autorizacionComprobante(claveAccesoComprobante: string)`** →
  `RespuestaAutorizacionComprobante { claveAccesoConsultada, numeroComprobantes,
  autorizaciones[ autorizacion { estado, numeroAutorizacion, fechaAutorizacion,
  ambiente, comprobante, mensajes[...] } ] }`, and `autorizacionComprobanteLote`
  for batches. `estado` is `AUTORIZADO`, `NO AUTORIZADO` or `EN PROCESO`/`EN
  PROCESAMIENTO` (the Ficha's Tabla 6 uses the words "En procesamiento (PPR)",
  "Autorizado (AUT)", "No autorizado (NAT)"). `comprobante` carries the signed
  XML back, wrapped in CDATA. (S22, S1 §5.12.)
- Since v2.31 there are also **consultation** services
  (`ConsultaComprobante?wsdl` → `consultarEstadoAutorizacionComprobante(claveAcceso)`,
  and `ConsultaFactura?wsdl`) on the same hosts, namespace
  `http://ec.gob.sri.ws.consultas` (S1 §8). They are read-only and useful for a
  reconciliation job.
- Size limits: 320 KB per individual comprobante; a batch is ≤ 500 KB / ~50
  comprobantes (S1 §7.5). The SRI's SSL certificates "podrían cambiar sin
  previo aviso" — do not pin them (S1 §7.1.4).
- Endpoints answer plain HTTPS POST with `Content-Type: text/xml` and a SOAP
  1.1 envelope; the veronica-soap README notes the WSDLs should be embedded so
  the app can boot while the SRI is down (S26). No SOAP library is required in
  Go: two hand-written envelopes and an `encoding/xml` decoder suffice.

### 2.5 The prescribed asynchronous protocol (S1 §7.4–7.5, §5.10–5.12)

1. Send the signed XML to *recepción*. `RECIBIDA` means it passed schema and
   the first checks; `DEVUELTA` carries error messages and the document was
   **not stored** ("Los comprobantes devueltos no se guardarán en la base de
   datos del SRI").
2. "Se debe esperar un determinado tiempo (se recomienda que este tiempo sea
   parametrizable)" and then call *autorización* with the clave de acceso.
3. Result `AUTORIZADO` → store the returned `autorizacion` XML (it is the
   legal artifact: number, date, ambiente, comprobante). `NO AUTORIZADO` →
   read `mensajes`, fix, **resend with the same clave de acceso and
   secuencial** ("el emisor deberá utilizar la misma clave de acceso y
   secuencial", §5.10; note 1 after the error table). `EN PROCESAMIENTO` →
   poll again.
4. The SRI's maximum processing time is 24 hours from `RECIBIDA` (§7.5); the
   old 2017 comparison document said the same (S3).
5. If the document was `NO AUTORIZADO` several times, the autorización
   service returns only the last state (§5.11). A `NO AUTORIZADO` for reasons
   external to the file (RUC clausurado, establecimiento cerrado, RUC inactive)
   cannot be fixed by re-sending (note 1).

### 2.6 Error codes that drive the state machine (S1 error table)

| Code | Description | Stage | What it means for the queue |
|------|-------------|-------|------------------------------|
| 35 | Documento inválido — XML fails schema | Recepción | Bug; needs-attention, never auto-retry |
| 36 | Versión esquema descontinuada | Recepción | Bug; needs-attention |
| 37 | RUC sin autorización de emisión | Autorización | Emisor config; needs-attention |
| 39 | Firma inválida | Autorización | Certificate/canonicalization; needs-attention |
| 40 | Error en el certificado | Autorización | Certificate; needs-attention |
| 43 | Clave acceso registrada — already in SRI DB | Recepción | **Already sent**: go straight to polling autorización with that clave (idempotency signal) |
| 45 | Secuencial registrado | Recepción | A *different* clave with the same secuencial exists at the SRI: sequence allocation bug; needs-attention |
| 46 | RUC no existe | Autorización | Emisor config |
| 47/48 | Tipo de comprobante / XSD no existe | Recepción | Bug |
| 49 | Argumentos nulos | Recepción | Bug |
| 50 | Error interno general | Recepción | **SRI-side**: retry with backoff |
| 52 | Error en diferencias — arithmetic | Autorización | Bug in totals; needs-attention |
| 56/57/63 | Establecimiento cerrado / autorización suspendida / RUC clausurado | Autorización | Emisor-side, not fixable by resend |
| 58 | Clave de acceso components differ from XML | Autorización | Bug |
| 65 | Fecha de emisión extemporánea | Recepción | Sent too late for the tipo de emisión; **the regulatory deadline bites here** |
| 67 | Fecha inválida | Recepción | Bug |
| 70 | Clave de acceso en procesamiento | Recepción | **Do not resend and do not mint a new clave**; poll autorización for up to 24 h (note 2) |
| 80 | Clave de acceso malformed in the autorización query | Autorización | Bug |

Advertencias (non-blocking): 59 identificación no existe, 60 ambiente de
pruebas, 62 identificación incorrecta (cédula check digit), 68 documento
sustento no electrónico.

### 2.7 Secuencial numbering rules

- 9 digits, zero-padded, per (RUC, estab, ptoEmi, tipo de comprobante). The
  emisor must guarantee comprobantes "sean emitidos en orden cronológico y
  secuencial, controlando que no exista duplicidad tanto en la secuencia como
  en las claves de acceso; así como también evitar el reenvío innecesario"
  (S1 §9.18). Error 45 is the SRI catching a duplicate; its troubleshooting
  note adds that such cases "debieron ser detectados y corregidos en el
  ambiente de pruebas".
- A rejected document keeps its secuencial and clave (§5.10). So a sequence
  number is consumed at *creation*, not at authorization, and a document that
  can never be authorized still occupies its number (it should be annulled
  rather than reused — see 2.9).
- Pruebas and producción are different ambientes in the clave, but the
  secuencial namespace is per estab/ptoEmi; keep separate counters per
  ambiente to avoid confusion during certification.

### 2.8 Transmission deadline (the timeline of Art. 7, NAC-DGERCGC18-00000233)

- Original (2018): "en el momento mismo de realizarse la generación del
  comprobante electrónico, o hasta dentro de un máximo de setenta y dos horas"
  (quoted in the considerandos of Res. 24-22, S15).
- 2024-11 (electricity emergency, Res. NAC-DGERCGC24-00000035): extended to
  "cuatro días hábiles" (SRI Boletín NAC-COM-24-061, S8).
- 2025-06-27, Res. NAC-DGERCGC25-00000014, Disposición Reformatoria Primera
  §2: "En el primer inciso del artículo 7, elimínase la siguiente frase: 'o
  hasta dentro de un máximo de cuatro días hábiles de haberse generado el
  mismo,'" — effective **2026-01-01** per Res. 25-17 (S9, S10). What remains
  is transmission *at the moment of generation*.
- What "at the moment" tolerates when the SRI is down is **not defined** in
  any fetched primary text; error 65 ("Fecha de emisión extemporánea") is the
  mechanical enforcement and its tolerance window is **UNVERIFIED**. Design
  consequence: attempt transmission synchronously with the Sale commit's
  follow-up (seconds), never on a nightly batch; keep `fechaEmision` = the
  date the factura is generated, in `America/Guayaquil`.

### 2.9 Anulación and notas de crédito (Res. NAC-DGERCGC25-00000014 as amended by 25-00000017)

- Art. 1: annulment applies to comprobantes "emitidos con errores o aquellos
  cuya operación, motivo de su emisión, no se haya producido". Art. 2: a
  factura is annulled **either online or by nota de crédito**; retenciones and
  complementary docs only online. (S9.)
- Art. 3 (as amended): online annulment is possible **until day 7 of the
  following month** (was day 10); after that, only a nota de crédito. Art. 4:
  notas de crédito/débito and retenciones need the receptor's acceptance
  within 5 business days; facturas do **not** need receptor confirmation for
  online annulment (S6 Q28), though the FAQ says to obtain consent because a
  declared factura must not be annulled. (S9, S10, S6.)
- Art. 5 (as amended by 25-17): notas de crédito "deberán emitirse únicamente
  en los casos señalados en el Reglamento" (annul operations, accept returns,
  grant discounts — Reglamento Art. 15, S5); the 12-month limit that 25-14 had
  introduced was **removed** by 25-17 (S10).
- Annulment is irreversible ("No es posible reversar la anulación… se debe
  emitir un nuevo comprobante", S6 Q27). Mass annulment (>1,000 per month) is
  a trámite (Disposición General Cuarta, S9).
- The online annulment is a portal action in SRI en línea (or a trámite with a
  spreadsheet, S11); **there is no web service for annulment in the Ficha**.
  For an automated platform the reversal instrument is therefore the
  **nota de crédito**, which is issued through the same recepción/autorización
  services, must name the buyer (Tabla 6 note: "se debe obligatoriamente
  identificar al receptor"), and is impossible for a consumidor-final factura.

---

## 3. Availability, retries, and the queue design

### 3.1 What is documented about SRI availability

- The SRI describes its authorization system as "alta disponibilidad" (S1
  §7.3) and promises answers within 24 h; it recommends the client wait a
  configurable time before polling (§7.4) and forbids re-sending while a clave
  is "en procesamiento" (error 70, note 2).
- Scheduled maintenance windows are announced ad hoc; e.g. all digital
  services including "el sistema de Comprobantes Electrónicos" were down from
  22:00 Fri 2026-03-06 to 06:00 Sat 2026-03-07 (S23, secondary report of an SRI
  communiqué). The 2024 electricity emergency produced a formal extension of
  the transmission window (S8). No SRI status page or SLA was found.
- **Rate limits, request timeouts, and concurrency limits are not documented
  anywhere fetched — UNVERIFIED.** Open-source clients default to conservative
  pacing (e.g. `SRI_REQUEST_DELAY_MS=150` in S27; embedded WSDL and
  configurable timeouts in S26) and treat `SocketTimeoutException` as the
  common failure. Error 50 ("Error interno general… error inesperado en el
  servidor") is the documented transient server failure.

### 3.2 How this repo already runs background work (what to reuse)

- **No in-process workers** (ADR 0007): scheduled work is a Cloud Scheduler
  job hitting an internal endpoint with an OIDC token; Terraform per job
  (`terraform/modules/ticket-pos/*.tf`), `retry_count = 0` because the queue
  row owns its own backoff, `attempt_deadline` bounds the run.
- **Queue rows with `next_attempt_at` + `attempt_count` + `status`**, claimed
  with `UPDATE … WHERE id = (SELECT … FOR UPDATE SKIP LOCKED LIMIT 1)` and
  a lease (`follow_digests`, `sale_reversals`; see
  `backend/internal/digest/repository/repository.go:ClaimDueDigest` and
  `backend/internal/sales/repository/reversal.go:ClaimDueReversalRequest`).
  The attempt count is incremented by the claim, not by the failure.
- **Reconciler pattern** (ADR 0024): a provider call with an ambiguous outcome
  is recorded *before* the call, and the tick re-asks a question with a
  definite answer. The SRI's error 43 ("clave de acceso registrada") and the
  autorización query are exactly that kind of definite answer.
- **Pacing** (`reminderpacing.go`): a fixed gap between provider requests, a
  60 s budget per run, and a batch of 50 with the remainder reported as
  backlog.
- **Feature flags** on `platform.Config` (`TICKET_QUESTIONS_ENABLED`,
  `TICKET_ASSIGNMENT_ENABLED`) with jobs that "ship paused".
- **Mail** through `platform.EmailSender` (Resend, ADR 0009); attachments are
  not used today — the XML + RIDE delivery needs the sender to grow an
  attachment parameter (Resend supports attachments; **UNVERIFIED** here since
  Resend docs were not fetched).

### 3.3 Proposed design

**Module**: `backend/internal/invoicing/` (domain, repository, service,
handler), owning tables `invoice_documents`, `invoice_sequences`,
`invoice_attempts`, plus emisor configuration (`invoice_emisores`) and
certificate storage.

**Tables (sketch)**

```
invoice_emisores
  id, organization_id (nullable if platform-wide), ruc, razon_social,
  nombre_comercial, dir_matriz, dir_establecimiento, estab, pto_emi,
  obligado_contabilidad bool, regimen ('general'|'rimpe'|'rimpe_np'),
  ambiente ('1'|'2'), certificate_ref (Secret Manager name), cert_not_after,
  enabled bool

invoice_sequences
  emisor_id, ambiente, cod_doc ('01'|'04'), estab, pto_emi, next_secuencial
  PRIMARY KEY (emisor_id, ambiente, cod_doc, estab, pto_emi)

invoice_documents
  id, emisor_id, ticket_sale_id, cod_doc, secuencial, clave_acceso UNIQUE,
  fecha_emision date, ambiente, status, iva_code, totals (numeric snapshot),
  buyer identification snapshot (tipo, numero, razon social, email),
  xml_signed bytea/GCS ref, xml_autorizacion bytea/GCS ref,
  numero_autorizacion, fecha_autorizacion,
  attempt_count, next_attempt_at, last_error_code, last_error_message,
  delivered_at, created_at, updated_at,
  credit_note_of uuid NULL (self-reference for notas de crédito)
  UNIQUE (emisor_id, ambiente, cod_doc, estab, pto_emi, secuencial)
  partial index on (next_attempt_at) WHERE status IN ('signed','sent','received')

invoice_attempts   -- append-only ledger, one row per SRI request
  document_id, at, operation ('recepcion'|'autorizacion'), http_status,
  estado, mensajes jsonb, duration_ms
```

**State machine per document**

```
draft ──sign──▶ signed ──validarComprobante──▶ sent
                                   │ RECIBIDA                 │ DEVUELTA
                                   ▼                          ▼
                               received                 rejected (error codes)
                                   │ autorizacionComprobante   │ 43 → received
                    ┌──────────────┼────────────────┐          │ 50/transport → signed (retry)
                    ▼              ▼                ▼          ▼
               authorized    en_procesamiento   not_authorized  needs_attention
                    │         (poll again)         │ (resend same clave after fix,
                    ▼                              │  or needs_attention)
               delivered (XML+RIDE mailed)         └──▶ needs_attention
```

Rules:

1. **Draft creation is transactional with the Sale commit.** Allocate the
   secuencial inside the same transaction that commits the Ticket Sale
   (`UPDATE invoice_sequences SET next_secuencial = next_secuencial + 1 …
   RETURNING`), compute the clave, snapshot buyer and totals, write `draft`.
   A serial allocation via a single-row `UPDATE … RETURNING` serialises
   concurrent checkouts on that emisor's counter without advisory locks; the
   UNIQUE constraint on the tuple is the backstop. Gaps are legally
   undesirable (§9.18) but a rolled-back checkout rolls back the counter too,
   so gaps only arise from documents that die *after* commit — those are kept
   and annulled, never reused.
2. **Sign and send immediately after commit**, in the request path but outside
   the transaction (goroutine with its own short deadline is *not* allowed on
   Cloud Run once the response is sent; instead do it synchronously with a
   ~5 s budget and let the sweep catch everything that missed). The
   regulatory clock (§2.8) is why the first attempt is not left to the sweep.
3. **Sweep** (`POST /api/v1/internal/invoices/drain`, Cloud Scheduler every
   1–5 minutes, paused by Terraform default like the other jobs): claim due
   rows with `FOR UPDATE SKIP LOCKED`, lease by moving `next_attempt_at`,
   process by state, pace requests with a fixed gap, stop on a 60 s budget,
   report counts only.
4. **Idempotency by clave de acceso.** Every recepción call is retry-safe:
   error 43 means "already there", error 70 means "in flight": both transition
   to `received` and schedule a poll. Never mint a second clave for the same
   `ticket_sale_id` + `cod_doc` (enforce with a UNIQUE on
   `(ticket_sale_id, cod_doc) WHERE credit_note_of IS NULL`).
5. **Backoff.** Transport errors, HTTP 5xx, SOAP faults and error 50 →
   exponential backoff from 30 s, capped at 15 min, unlimited attempts but a
   **needs_attention alert at 2 h** (the 24 h SRI window and the "immediate"
   rule make silence dangerous). `EN PROCESAMIENTO` → poll at 30 s, 1 min, 2
   min, then every 10 min up to 24 h, then needs_attention.
6. **Dead-letter = `needs_attention`**, never deletion: the document keeps
   its clave and secuencial; an operator screen shows the SRI `mensajes`, and
   the only automated exits are "re-sign and resend" (after the emisor's data
   was fixed) or "annul" (which is a manual portal action recorded here as
   `annulled` with who/when). Log lines, no PII (follow the sweeps' "counts
   only" rule).
7. **Storage.** Keep the exact signed XML bytes that were sent (re-signing
   changes the signature and therefore the artifact) and the full
   `autorizacion` XML returned; both for seven years (S6 Q19). GCS bucket
   objects keyed by clave de acceso, with the row holding the reference and a
   SHA-256 of each. The RIDE PDF can be regenerated from the XML and does not
   need to be stored, but storing what was mailed is cheap evidence.
8. **Delivery.** After `authorized`, mail XML + RIDE to the Sale's buyer
   address in the Sale Locale (ADR 0033), through the transactional sender;
   record `delivered_at`; retry delivery independently of authorization.
   This is a *second* mail per Sale unless folded into the Sale Confirmation —
   which is impossible because the Confirmation is sent before the SRI
   answers. Decide whether the receipt mail says "your factura will follow".
9. **Reversals → nota de crédito.** A Sale Reversal (any route in ADR 0018/
   0019/0024/0050) on a Sale whose factura is `authorized`/`delivered` enqueues
   a `cod_doc=04` document referencing the factura (`credit_note_of`), with the
   same buyer identification, the full amount, `motivo` = the reversal route,
   flowing through the same state machine. A reversal of a Sale whose factura
   is not yet authorized should **wait** for the factura's terminal state: if
   the factura ends `needs_attention` the operator annuls it instead. A
   consumidor-final factura cannot be credited (§1.5) — this is the strongest
   argument for never issuing one on the online channel.
10. **Reconciliation.** A daily job calls `consultarEstadoAutorizacionComprobante`
    for every document in `received`/`en_procesamiento` older than 1 h and for
    a sample of `authorized` ones, and flags disagreements.
11. **Ambiente.** Each emisor carries its ambiente; certification happens by
    pointing the same code at `celcer` with ambiente `1`; the advertencia 60 in
    every test authorization is the tell. Never mix environments in one
    sequence table row (the primary key above includes ambiente).

**Fitting into the existing modules**: the Sale commit lives in
`backend/internal/sales/service/checkout.go`, `manual_sale.go` and the import
path; reversal in `sale_reverse.go`/`reversal.go`/`reconciler.go`. The invoicing
service should subscribe through narrow interfaces the sales module declares
(as `AssignmentReminderSource` does), never the reverse, so a deployment
without an emisor configured invoices nothing and fails nothing.

---

## 4. Libraries and pitfalls

### 4.1 Go

| Library | What | License | Status (as fetched 2026-08-23) |
|---------|------|---------|--------------------------------|
| `github.com/nelsonmarro/go_ec_sri_invoice_signer` (S24) | XAdES-BES signer for SRI documents; enforces inclusive C14N via goxmldsig; SHA-1 default, SHA-256 option; no CGO; ships an `sri-tester` CLI against celcer | MIT | 21 commits, 3 stars — young and tiny; read it, consider vendoring rather than depending |
| `github.com/russellhaering/goxmldsig` (S28) | XML-DSig core: inclusive + exclusive C14N, RSA-SHA1, enveloped | Apache-2.0 | Maintained (SAML ecosystem); **no XAdES** — the `etsi:QualifyingProperties`/`SignedProperties` block must be hand-built |
| `software.sslmate.com/src/go-pkcs12` (S29) | Decode `.p12` (RC2-40/3DES legacy PBE, SHA-1 MAC) | BSD-3-Clause | Maintained; the README warns the format uses weak primitives |
| SOAP client | none needed: `net/http` + `encoding/xml` with two hand-written envelopes (namespaces `http://ec.gob.sri.ws.recepcion` and `http://ec.gob.sri.ws.autorizacion`) | — | — |

### 4.2 Node / TypeScript

| Library | What | License | Status |
|---------|------|---------|--------|
| `bryancalisto/ec-sri-invoice-signer` (S25) | Pure TS signer for facturas, NC, ND, guías, retenciones; xml-crypto + node-forge; RSA-SHA1; tested with Uanataca, Security Data, Lazzate, BCE `.p12` | MIT | 158 commits, 46 stars; requires no DOCTYPE, no xmlns prefixes, UTF-8 |
| `miguelangarano/open-factura` (S30) | Build JSON→XML, sign with `.p12`, send to recepción and autorización | MIT | 24 commits, 86 stars; README light on versions |
| `facturacion-electronica-ec` (npm) (S31) | Build/sign/send/authorize all six document types, XSD validation; "API unstable until v1.0"; 6/6 authorized against celcer | **UNVERIFIED** (npm page returned 403) | **UNVERIFIED** |
| `AngeloBarzolaVillamar/open-api-facturacion-sri` (S27) | NestJS REST API: multi-tenant, AES-256-encrypted `.p12` storage, BullMQ queue, `SRI_MAX_RETRIES`, `SRI_REQUEST_DELAY_MS` | MIT | 32 commits, 134 stars; useful as a design reference, not a dependency |
| `PeculiarVentures/xadesjs` (S32) | Generic XAdES-BES on WebCrypto | MIT | **Archived 2025-08-12**, merged into `xmldsigjs` |

The Go backend is where the Sale commit lives, so the signer should be Go; the
Node libraries are relevant only for a RIDE renderer if it is built in the
frontends' stack.

### 4.3 Pitfalls (each confirmed by at least one library README or the Ficha)

- **Canonicalization**: the SRI's MITyC validator expects inclusive C14N
  (`REC-xml-c14n-20010315`); Go's and many JS stacks default to exclusive
  C14N and produce error 39 (S24).
- **Namespaces**: declaring `xmlns:ds`/`xmlns:etsi` on the root instead of on
  the signature nodes changes the canonical bytes; the Go signer's README
  calls this "namespace contamination" (S24). Keep the factura root
  prefix-free with `id="comprobante"`.
- **Enveloped transform + direct hashing** of the whole document (S24), and
  the `Reference` set must cover the document, `SignedProperties` and
  `KeyInfo` (S1 §6.4–6.5).
- **SHA-1** is what the Ficha specifies (S1 §6.8); do not "modernise" to
  SHA-256 without a test in celcer.
- **Certificate chain / OCSP**: the SRI checks "Validez firma y cadena de
  confianza" and "OCSP" (S1 validation order). The `.p12` must include the
  issuing chain, or the signer must add the intermediates to `KeyInfo`;
  revoked or expired certificates fail at authorization (error 39/40), not
  at reception.
- **Legacy `.p12` encryption**: certificates from Ecuadorian CAs are commonly
  packaged with `pbeWithSHA1And40BitRC2-CBC`, which OpenSSL 3 only opens with
  the legacy provider (`openssl pkcs12 -legacy`, S33). Node's built-in crypto
  inherits that; `node-forge` and `go-pkcs12` parse RC2-40 natively. The
  ec-sri-invoice-signer README says it was tested per provider and asks for
  issues on others (S25) — budget for a per-CA test.
- **Several keys in one `.p12`**: UNVERIFIED but commonly reported; pick the
  key whose certificate has the RUC in its subject/extensions.
- **Ampersand and encoding**: `&` must be `&amp;` (S1 §15) and UTF-8 with
  the XML declaration first; a BOM breaks the enveloped digest.
- **Decimals**: 2 decimals in totals, up to 6 in quantity/unit price on
  v1.1.0; arithmetic must reconcile or error 52 (S1 §9.17, error table).
- **Clock**: `fechaEmision` is `dd/mm/yyyy` in the XML and `ddmmaaaa` in the
  clave; compute both from the same `America/Guayaquil` instant.

---

## 5. Open questions and decisions for the user

1. **Who is the emisor?** (a) The **platform** issues every ticket factura
   under its own RUC (one certificate, one production authorization, one
   sequence per punto de emisión; but then the platform is selling the
   tickets for tax purposes, contradicting `CONTEXT.md` "The tickets
   themselves are the Organization's sale, and their taxation stays outside
   the system" and ADR 0014's fee model). (b) **Each Organization** is the
   emisor: it uploads its `.p12` and password, enables pruebas then
   producción in its own SRI en línea, registers a punto de emisión for the
   platform, and tells the platform its IVA regime; the platform is a
   third-party billing provider and must put **its RUC in every factura**
   (Ficha Anexo 26, S1). (b) matches the business model; it also means
   certificate custody (Secret Manager per Organization, KMS-encrypted
   password, expiry monitoring, revocation on churn) and a per-Organization
   onboarding/certification flow. This must be an ADR.
2. **IVA rate per Event.** 15% default, 0% only when the Organization proves
   RUAC registration and the Event capacity ≤ 2,000 (S13). Is that an
   Organization attribute, an Event attribute, or an operator-only toggle?
   Who carries liability for a wrong claim?
3. **Consumidor final policy.** With ADR 0016 the online channel always has a
   Tax ID, the `import` channel may not. Should the platform refuse to invoice
   imports without a Tax ID above USD 50 (mandatory by law), issue consumidor
   final below it (irreversible, S9), or skip invoicing imports entirely
   (the external platform may already have invoiced)?
4. **Which Sales get a factura?** Online only; or also Manually Recorded
   Sales (ADR 0052) and Sale Imports (ADR 0050), where the money never touched
   the platform and the external platform may already have invoiced? A
   zero-total checkout (ADR 0017) still transfers a service — does it get a
   USD 0 factura? **UNVERIFIED** whether a 0-value factura is accepted.
5. **What is the line item?** One line per Ticket Sale Line (Ticket Type ×
   quantity × price after Promotion), with the Platform Fee handled how?
   Under "Fee Handling" that raises the buyer price, the buyer pays Org price +
   fee; the Organization's factura must state the amount the buyer paid to
   the Organization. Whether the fee portion is the Organization's revenue
   (then it invoices it and the platform invoices the Organization) or the
   platform's (then the buyer needs *two* facturas) is a tax question for
   counsel.
6. **Timing vs the Sale Confirmation.** The factura cannot be in the Sale
   Confirmation mail because authorization takes seconds to hours. Second mail
   with XML + RIDE, or a link in the Customer Area that becomes live? The
   obligation is to send XML + RIDE to the buyer's email (S6 Q21).
7. **The reversal window.** Customer reversals (ADR 0018/0024) and operator
   reversals (ADR 0019) become notas de crédito. Should a reversal be refused
   when the factura is in `needs_attention` (cannot be credited yet)? Should
   the Sale Correction route (ADR 0050: reverse + replace) produce a nota de
   crédito plus a new factura, or is an in-portal anulación (manual, until day
   7 of next month) acceptable for same-day corrections?
8. **Sequence ownership when an Organization also invoices elsewhere.** If
   the Organization uses its own POS for door sales with the same estab/ptoEmi,
   the platform's counter collides (error 45). Require a dedicated punto de
   emisión for the platform (recommended) and record it in the emisor row.
9. **Retention of PII.** The signed XML embeds the buyer's name, Tax ID and
   email for seven years; this interacts with the deletion-request stance
   (counsel escalation) and ADR 0045's "one place personal data lives".
10. **Certificate expiry and rotation.** Who is warned when a `.p12` is 30
    days from `notAfter`, and what does the queue do with drafts when the
    certificate is expired (park as `needs_attention` without consuming
    attempts)?
11. **Ambiente de pruebas budget.** Certification requires each Organization
    (or the platform) to push documents through celcer first; the SRI does
    not publish how many, so the onboarding UI needs a "run a test factura"
    button and a visible advertencia-60 result.
12. **Go signer maturity.** The best Go option has 3 stars. Either vendor it
    with a conformance test against celcer in CI (needs a real test `.p12`
    and network — cannot run in the current sandbox), or write the ~300-line
    XAdES envelope in-house on goxmldsig.

---

## 6. Sources

Primary (SRI / law / official schemas / live services):

- S1. SRI, *Ficha Técnica de Comprobantes Electrónicos — Esquema Off-line*, Versión 2.34 (27/07/2026): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/f8d9bb36-5632-4f96-b463-b9265b55338c/FICHA%20TE%cc%81CNICA%20COMPROBANTES%20ELECTRO%cc%81NICOS%20ESQUEMA%20OFFLINE%20Versio%cc%81n%202.34.pdf (text extracted with pdftotext; sections cited by number: §5 clave de acceso/estados, §6 firma, §7 web services, §8 consultas, §9 catálogos incl. Tablas 6/16/17/18/21, error table and validation order, Anexo 3 XML 1.1.0, Anexo 7 versions 2.x, Anexo 22 RIMPE, Anexo 26 RUC Proveedor, §14 troubleshooting, §15 glosario)
- S2. SRI, *Facturación Electrónica* portal page (downloads, certificate entities, pruebas/producción): https://www.sri.gob.ec/en/facturacion-electronica
- S3. SRI, *Comparativo Esquemas Online y Offline de Comprobantes Electrónicos* (Sept 2017): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/e1e79480-d01e-4255-9c12-65c18a309104/Comparativo+esquemas+Online+y+Offline.pdf
- S4. SRI, *XML y XSD Factura* (versions 1.0.0, 1.1.0, 2.0.0, 2.1.0): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/05546998-6f29-4870-be3b-62650f312a6c/XML%20y%20XSD%20Factura.zip — nota de crédito XSD: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/dfc944cd-5f18-4433-a626-3cc64cfc4549/XML%20y%20XSD%20Nota%20de%20Cr%c3%a9dito.zip
- S5. SRI (Dirección Nacional Jurídica), *Reglamento de Comprobantes de Venta, Retención y Documentos Complementarios* (consolidation to D.E. 580, R.O. 448, 28-II-2015): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/c3a2c922-5960-4c08-9a73-bde19fadce42/REGLAMENTO+DE+COMPROBANTES+DE+VENTA,+RETENCI%D3N+Y+DOCUMENTOS+COMPLEMENTARIOS.pdf
- S6. SRI, *Preguntas frecuentes facturación electrónica*: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/cef82829-f261-4374-a45c-96adb2c3ace8/Preguntas+frecuentes+facturaci%F3n+electr%F3nica.pdf
- S7. SRI, Resolución NAC-DGERCGC22-00000024 (obligatoriedad universal, 2022): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=c508d69a-4ea4-4940-8777-fbe89fef2fac&nombre=NAC-DGERCGC22-00000024.pdf
- S8. SRI, Boletín NAC-COM-24-061 (2024-11-08), plazo de transmisión ampliado a cuatro días hábiles (Res. NAC-DGERCGC24-00000035): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/a1e3b4ac-948c-46f7-8f64-8967be8a76cc/BOLETIN%20061_SRI%20AMPL%C3%8DA%20PLAZO%20PARA%20LA%20TRANSMISI%C3%93N%20DE%20COMPROBANTES%20ELECTR%C3%93NICOS%20DEBIDO%20A%20EMERGENCIA%20EL%C3%89CTRICA.pdf
- S9. SRI, Resolución NAC-DGERCGC25-00000014 (anulación; deletes the four-business-day phrase from Art. 7 of 18-233): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=137046a6-787c-47fb-a2d7-176595d292dc&nombre=NAC-DGERCGC25-00000014.pdf
- S10. SRI, Resolución NAC-DGERCGC25-00000017 (reforms 25-14: day 7, credit-note window removed, 2026-01-01 effective date for immediate transmission): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=e98fc8a6-299e-4ea9-8de7-2f6c70dbb4f5&nombre=NAC-DGERCGC25-00000017.pdf
- S11. gob.ec, trámite *Anulación de comprobantes electrónicos*: https://www.gob.ec/sri/tramites/anulacion-comprobantes-electronicos
- S12. SRI, *Impuesto al Valor Agregado (IVA)* page (15% since 2024-04-01, D.E. 198): https://www.sri.gob.ec/en/impuesto-al-valor-agregado-iva
- S13. SRI, *Servicios artísticos y culturales* (0% conditions: RUAC, aforo 2,000): https://www.sri.gob.ec/en/servicios-artisticos-y-culturales
- S14. SRI, *Contribuyentes obligados a emitir comprobantes electrónicos*: https://www.sri.gob.ec/en/contribuyentes-obligados-a-emitir-comprobantes-electronicos
- S15. SRI, Resolución NAC-DGERCGC24-00000022 (fines for not delivering / not transmitting; quotes Art. 7 of 18-233): https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=02272baf-02db-4b03-bf07-67b85a46629b&nombre=NAC-DGERCGC24-00000022.pdf
- S16. gob.ec, *Autorización en ambientes de pruebas de comprobantes electrónicos*: https://www.gob.ec/sri/tramites/autorizacion-ambientes-pruebas-comprobantes-electronicos
- S17. gob.ec, *Autorización en ambientes de producción de comprobantes electrónicos*: https://www.gob.ec/sri/tramites/autorizacion-ambientes-produccion-comprobantes-electronicos
- S19. SRI, *Catálogo de actividades artísticas y culturales que gravan tarifa 0% del IVA*: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/9bee2aa4-0616-41af-94e6-f28a48b7fba9/Cat%C3%A1logo%20de%20actividades%20art%C3%ADsticas%20y%20culturales.pdf
- S20. SRI, *Impuesto a los Consumos Especiales* page: https://www.sri.gob.ec/en/impuesto-consumos-especiales
- S21. Live WSDL, RecepcionComprobantesOffline (pruebas): https://celcer.sri.gob.ec/comprobantes-electronicos-ws/RecepcionComprobantesOffline?wsdl
- S22. Live WSDL, AutorizacionComprobantesOffline (pruebas): https://celcer.sri.gob.ec/comprobantes-electronicos-ws/AutorizacionComprobantesOffline?wsdl
- SRI en línea public validity check (portal): https://srienlinea.sri.gob.ec/comprobantes-electronicos-internet/publico/validezComprobantes.jsf

Open-source implementations (cited only for claims about themselves):

- S24. nelsonmarro/go_ec_sri_invoice_signer: https://github.com/nelsonmarro/go_ec_sri_invoice_signer
- S25. bryancalisto/ec-sri-invoice-signer: https://github.com/bryancalisto/ec-sri-invoice-signer
- S26. veronica-platform/veronica-soap (Java SOAP client; embedded WSDLs, timeouts): https://github.com/veronica-platform/veronica-soap
- S27. AngeloBarzolaVillamar/open-api-facturacion-sri (NestJS, BullMQ, multi-tenant): https://github.com/AngeloBarzolaVillamar/open-api-facturacion-sri
- S28. russellhaering/goxmldsig: https://github.com/russellhaering/goxmldsig
- S29. SSLMate/go-pkcs12: https://github.com/SSLMate/go-pkcs12
- S30. miguelangarano/open-factura: https://github.com/miguelangarano/open-factura
- S31. facturacion-electronica-ec (npm; page returned 403 — UNVERIFIED): https://www.npmjs.com/package/facturacion-electronica-ec
- S32. PeculiarVentures/xadesjs (archived): https://github.com/PeculiarVentures/xadesjs
- S33. OpenSSL issue on legacy RC2-40 PKCS#12: https://github.com/openssl/openssl/issues/12840

Secondary (used only as the route to, or a transcription of, a primary text):

- S18. Transcription of NAC-DGERCGC18-00000233 (Arts. 5, 7, 8, 9): https://www.contadoryabogado.net/2018/06/nac-dgercgc18-00000233-comprobantes.html — the original PDF on sri.gob.ec was not located in this session.
- S23. LEXIS Noticias, SRI maintenance window 2026-03-06/07 (reporting an SRI communiqué): https://www.lexis.com.ec/noticias/suspension-temporal-de-servicios-digitales-del-sri-por-mantenimiento-programado
- Reported reduction of the consumidor-final ceiling from USD 200 to USD 50 by Decreto Ejecutivo 586 (secondary; the USD 50 figure is confirmed by S1 §9.10 and S6 Q34): https://facturasrapidasec.com/facturar-a-consumidor-final-no-excedera-de-50usd/

Repository files consulted for the queue-design fit: `docs/technical-design.md`,
`docs/adr/0007-*`, `0009-*`, `0016-*`, `0024-*`, `0051-*`, `CONTEXT.md`,
`backend/internal/sales/service/{assignmentreminder,reminderpacing,reconciler,reversal}.go`,
`backend/internal/sales/repository/reversal.go`,
`backend/internal/digest/repository/repository.go`,
`backend/internal/platform/taxid.go`, `backend/internal/server/{app,routes}.go`,
`backend/migrations/{039,087}_*.sql`, `terraform/modules/ticket-pos/{assignment_reminder,reversal_reconciler}.tf`.

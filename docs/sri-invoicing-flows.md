# SRI invoicing: every way a facturación can go, and what the platform does about it

Date: 2026-08-27. Audited against `main` at `136165a` (PR #488, ADR 0061 merged).

This is the "what we built, and what we didn't" companion to
`docs/research-sri-facturacion-electronica.md` (what the SRI requires) and to
ADRs [0059](./adr/0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
[0060](./adr/0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md)
and [0061](./adr/0061-a-wrong-recipient-is-corrected-by-reissue-a-credit-note-then-a-fresh-sale-invoice-never-an-edit.md)
(what we decided). Paths are relative to the repository root; `B/` is `backend/internal/`.

## Status legend

| Label | Meaning |
|---|---|
| **Built** | Code found; the cited `file:line` is the proof. |
| **Partial** | The path exists but a branch of the flow is not handled; the note says which. |
| **Ruled out** | Deliberately not built; the ADR or issue that ruled is cited. |
| **Tracked** | Known gap with an issue number. |
| **Untracked** | Gap with neither a ruling nor an issue. Collected at the end. |

Vocabulary: *Sale Invoice* = the factura (codDoc `01`) a paid Online Sale of a House Event owes; *Credit Note* = nota de
crédito (`04`); *Drainer* = the Sale Invoice Drainer (internal endpoint + scheduler); *needs_attention* = a document the
Drainer could not settle, waiting for a Platform Operator; *Recipient Warning* = SRI advertencia 59/62 on an authorized
document.

---

## 1. Diagrams

### 1.1 Document lifecycle (one Sale Invoice or Credit Note row)

```mermaid
stateDiagram-v2
    direction LR
    [*] --> owed : Sale committed (same tx)<br/>or reversal / reissue owes it

    owed --> withdrawn : Sale reversed before signing<br/>/ Credit Note it follows died
    owed --> needs_attention : no Issuer, no certificate,<br/>certificate expired<br/>(no secuencial consumed)
    owed --> pending : signed (secuencial + clave allocated),<br/>submitted → RECIBIDA<br/>or 43 clave registrada / 70 en procesamiento

    pending --> pending : EN PROCESAMIENTO<br/>ladder 1m · 5m · 15m · hourly
    pending --> authorized : AUTORIZADO<br/>(authorization XML stored)
    pending --> needs_attention : DEVUELTA (any code)<br/>NO AUTORIZADO (any code)<br/>24 h undecided
    pending --> annulled : operator Mark annulled<br/>(portal annulment recorded)

    needs_attention --> pending : operator Resend<br/>(same clave & secuencial, re-signed)
    needs_attention --> needs_attention : still polled / retried hourly
    needs_attention --> annulled : operator Mark annulled
    needs_attention --> withdrawn : Sale reversed while<br/>still unsigned
    needs_attention --> authorized : late AUTORIZADO on a poll

    authorized --> authorized : delivery mail failed<br/>→ retried on the ladder<br/>(delivered_at stays null)
    authorized --> [*] : delivered (XML + link mailed)

    note right of authorized
        Recipient Warning (advertencia 59/62)
        is a marker, not a state. Set on
        AUTORIZADO, cleared when a Credit
        Note against this factura authorizes.
        "superseded" is a relation to the
        corrected factura, not a state.
    end note

    withdrawn --> [*]
    annulled --> [*]
```

Transport errors, HTTP 5xx and SOAP faults never change the status: an attempt row is written and the document is
rescheduled on the ladder. A document the SRI never acknowledged is resubmitted with the same bytes; one it ever held
(RECIBIDA, 43, 70) is only polled.

### 1.2 Checkout → Sale Invoice → buyer

```mermaid
sequenceDiagram
    autonumber
    actor Buyer
    participant Checkout as Checkout (sales)
    participant DB as Postgres
    participant Inv as Invoicing service
    participant Drainer as Sale Invoice Drainer
    participant SRI as SRI (celcer / cel)
    participant Mail as Resend

    Buyer->>Checkout: pay, with Tax ID (check digit validated)
    Checkout->>DB: BEGIN
    Checkout->>DB: record Sale + Tickets
    alt House Organization and amount > 0 and SALE_INVOICING_ENABLED
        Checkout->>Inv: OwePaidOnlineSale (inside the tx)
        Inv->>DB: INSERT invoice status=owed, next_attempt_at=now
    end
    Checkout->>DB: COMMIT
    Checkout-->>Buyer: Sale Confirmation mail<br/>("a factura will follow" when one is owed)
    Checkout-)Drainer: kick (post-commit, this Sale only, fire-and-forget)

    par immediate attempt
        Drainer->>DB: claim owed row (FOR UPDATE SKIP LOCKED, lease)
        Drainer->>DB: allocate secuencial, compute clave, sign XAdES-BES (same tx)
        Drainer->>SRI: validarComprobante
        SRI-->>Drainer: RECIBIDA / DEVUELTA
        Drainer->>SRI: autorizacionComprobante
        SRI-->>Drainer: AUTORIZADO / EN PROCESAMIENTO / NO AUTORIZADO
    and scheduler every 5 min (ships paused)
        Drainer->>DB: claim every due row, 60 s budget
    end

    alt AUTORIZADO
        Drainer->>DB: status=authorized, authorization XML stored
        Drainer->>Mail: XML attached + Customer Area link
        Mail-->>Buyer: factura mail
        Drainer->>DB: delivered_at
    else EN PROCESAMIENTO
        Drainer->>DB: pending, next_attempt_at on the ladder
    else refused / 24 h without answer / cannot sign
        Drainer->>DB: needs_attention (messages stored verbatim)
        Note over Drainer: Operator Dashboard counts it
    end
```

### 1.3 Reversal → Credit Note

```mermaid
flowchart TD
    R[Sale Reversal committed] --> Q{Which route?}
    Q -->|customer reversal<br/>reconciler-agreed reversal| C[reason = customer]
    Q -->|operator reversal| P[reason = platform]
    Q -->|staff reversal · import undo · correction| X[No document owed:<br/>import-channel Sales never owe one]
    C --> S{State of the Sale's<br/>current factura?}
    P --> S
    S -->|owed / unsignable| W[Withdraw unsigned<br/>SRI told nothing]
    S -->|pending at SRI| WAIT[Credit Note owed,<br/>unclaimable until the factura is terminal]
    S -->|needs_attention,<br/>number consumed| WAIT
    S -->|authorized| CN[Credit Note owed:<br/>same Recipient, full amount,<br/>motivo = route]
    S -->|superseded by a reissue| CUR[credit the current factura only]
    CUR --> CN
    WAIT -->|factura authorizes| CN
    WAIT -->|factura annulled / withdrawn| DEAD[Credit Note withdrawn]
    CN --> D[Drainer: sign, submit, poll<br/>same lifecycle as 1.1]
    D -->|authorized| M[Credit Note mailed to buyer,<br/>Recipient Warning on the factura cleared]
    D -->|refused| NA[needs_attention:<br/>operator Resend or Mark annulled]
```

A Sale Reversal is never refused or delayed by the state of its paperwork. A refused Reversal Request reverses nothing,
so it owes nothing.

### 1.4 Reissue (wrong Recipient, ADR 0061)

```mermaid
sequenceDiagram
    autonumber
    actor Op as Platform Operator
    participant Inv as Invoicing service
    participant DB as Postgres
    participant Drainer
    participant SRI
    actor Buyer

    Op->>Inv: POST reissue {tax_id_type, tax_id, legal_name, address?, note?}
    Inv->>DB: lock the Sale, re-check refusals
    Note over Inv: refused on: manual Tax Invoice, Credit Note,<br/>not authorized (owed/pending/needs_attention/withdrawn/annulled),<br/>Sale reversed, reissue in flight, already superseded, already credited
    Inv->>DB: owe Credit Note (reason=reissue, motivo "Corrección de los datos del receptor")<br/>+ owe corrected factura (lines/totals/IVA of the old one,<br/>email = the Sale's current email)
    Inv-)Drainer: kick

    Drainer->>SRI: Credit Note first
    alt Credit Note AUTORIZADO
        Drainer->>Buyer: Credit Note mail: "cancelled for a correction, a corrected factura follows"
        Note over DB: old factura: still authorized, now superseded,<br/>Recipient Warning cleared
        Drainer->>SRI: corrected factura (claimable only now)
        alt corrected factura AUTORIZADO
            Drainer->>Buyer: factura mail: "replaces the earlier one"
            Note over Buyer: Customer Area shows the chain:<br/>current · superseded (still downloadable) · credit_note
        else refused
            Note over DB: needs_attention → operator Resend,<br/>or Mark annulled → Sale has no current factura (issue 480)
        end
    else Credit Note refused, then Mark annulled
        Note over DB: corrected factura withdrawn unsigned,<br/>old factura stands current, operator may reissue again
    end

    opt Sale reversed during the reissue
        Note over Drainer: the reissue's Credit Note is the one that credits the old factura,<br/>the reversal's own Credit Note stands down, the corrected factura is withdrawn,<br/>if the reissue Credit Note authorizes after the reversal its mail reads as a reversal's
    end
```

### 1.5 Tax ID quality: what the buyer typed, and what can be done about it

```mermaid
flowchart TD
    T[Buyer types Tax ID at checkout] --> V{ValidateTaxID at checkout}
    V -->|cédula / RUC check digit wrong,<br/>bad shape| REJ[Checkout refused with a field error<br/>— never reaches the SRI]
    V -->|passport: shape only| OK
    V -->|valid| OK[Sale committed, factura owed]
    OK --> SRI[SRI authorizes]
    SRI -->|no advertencia| FINE[authorized, no marker]
    SRI -->|advertencia 59 identificación no existe<br/>or 62 identificación incorrecta| RW[authorized + Recipient Warning<br/>dashboard count · list filter<br/>backfilled from the attempts ledger]
    SRI -->|number exists but is somebody else's| SILENT[Nothing detectable —<br/>only the buyer notices]
    RW --> WRITE[Buyer writes in]
    SILENT --> WRITE
    FINE -.->|buyer notices a wrong name| WRITE
    WRITE --> REISSUE[Operator reissue — 1.4]
    T -.->|no Tax ID at all| NOCF[Cannot check out online:<br/>consumidor final 07 and exterior 08<br/>are never produced]
```

### 1.6 Operator exits from `needs_attention`

```mermaid
flowchart LR
    NA[needs_attention] --> WHY{Why is it parked?}
    WHY -->|cannot sign: no Issuer,<br/>certificate missing / expired| FIX[Fix the Issuer or re-upload the certificate]
    FIX --> AUTO[Drainer retries hourly on its own<br/>— no operator action on the document]
    WHY -->|DEVUELTA / NO AUTORIZADO<br/>any error code, messages shown verbatim| DEC{Operator decides}
    WHY -->|24 h without a definite answer| POLL[Still polled hourly,<br/>Check status button]
    DEC -->|the cause was ours and is fixed| RS[Resend: same clave & secuencial,<br/>re-signed with the current certificate]
    RS --> PEND[pending → 1.1]
    DEC -->|it can never be authorized| ANN[Mark annulled: records who / when,<br/>the portal annulment itself is manual]
    ANN --> DEADF{What was it?}
    DEADF -->|a first Sale Invoice| GAP[Sale with no current factura — issue 480]
    DEADF -->|a corrected factura of a reissue| GAP
    DEADF -->|a reissue's Credit Note| BACK[Corrected factura withdrawn,<br/>old factura stands, reissue again]
    DEADF -->|a reversal's Credit Note| UNCR[Reversed Sale whose factura is still<br/>authorized and uncredited]
```

---

## 2. Flow table

### 2.1 What owes a document

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 1 | Paid Online Sale of a House Event → Sale Invoice owed in the Sale's own transaction, immediate attempt after commit | **Built** | `B/sales/repository/payments.go:660` → `B/invoicing/service/saleinvoice.go:37` `OwePaidOnlineSale`; kick `B/sales/service/service.go:642` | Row is `owed` with `next_attempt_at = now`; the kick drains that Sale only. |
| 2 | Sale of a non-House Organization | **Ruled out** (ADR 0060) | `B/sales/repository/payments.go:697` `isHouseOrganization` | Designating an Organization House later affects future Sales only; nothing retroactive. |
| 3 | Free / zero-total Sale (ADR 0017) | **Ruled out** (ADR 0060) | `payments.go:660` (`AmountCents > 0`) | Whether the SRI accepts a USD 0 factura is unverified. |
| 4 | Manually Recorded Sale / Sale Import (ADR 0050/0052) | **Ruled out** (ADR 0060, "deliberate and visible") | `B/sales/service/service.go:617` | The seam is only wired on online approval. A row without a Tax ID would have to be consumidor final — irreversible. |
| 5 | `SALE_INVOICING_ENABLED` closed | **Built** | `B/server/app.go:576`; `B/invoicing/service/drainer.go:196` | Seam not tied → no owed rows at all; drain endpoint answers `SALE_INVOICING_UNAVAILABLE`. Rows owed before a close simply wait. |
| 6 | Issuer missing, certificate missing or expired at signing time | **Built** | `B/invoicing/service/drainer.go:586` `signOwedInvoice` | Parked `needs_attention` with a platform-typed message; **no secuencial, no SRI call, no attempt row**; retried hourly without operator action. |
| 7 | Process dies between the Sale's commit and the immediate attempt | **Built** | `B/invoicing/repository/drainer.go:69` `ClaimDueInvoice` + scheduler | The row was already due at owe-time; the next tick claims it. |
| 8 | Two Sale Invoices minted for one Ticket Sale | **Partial** | `backend/migrations/098_sale_invoices.sql:87-100` (index is non-unique) | Prevented by control flow (one call site inside the Sale's tx), not by the schema. Only `102_sale_invoice_reissue.sql:44` enforces one live successor per factura. → Untracked U3 |

### 2.2 The Recipient's Tax ID

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 9 | Tax ID type → SRI code: cédula 05, RUC 04, passport 06 | **Built** | `B/invoicing/sri/factura.go:63-65` | |
| 10 | Consumidor final (07) or identificación del exterior (08) | **Ruled out** (ADR 0060: a consumidor-final factura can never be credited) | same | Checkout requires a Tax ID, so a buyer without one cannot buy online (`B/sales/handler/checkout.go:290`). |
| 11 | Cédula / RUC with a wrong check digit | **Built** | `B/sales/handler/checkout.go:300` → `B/platform/taxid.go:242` | Refused at checkout with a field error; never reaches the SRI. Passport is shape-only. |
| 12 | SRI advertencia 59 (no existe) / 62 (incorrecta) on an authorized document → Recipient Warning | **Built** | derive `B/invoicing/service/invoice.go:421`; `B/invoicing/recipient_warning.go:32`; backfill `backend/migrations/101_recipient_warning.sql:45`; clear `B/invoicing/repository/invoice.go:313` | Status stays `authorized`. Cleared when an authorized Credit Note lands against the factura (reissue's or reversal's). Older documents were backfilled from the attempts ledger. |
| 13 | Comments say the warning clears "only when superseded"; code clears on credit | **Untracked** (stale comments, code matches ADR 0061 as reviewed) | `B/invoicing/recipient_warning.go:9-10`, `101_recipient_warning.sql:19-22`, OpenAPI text `B/invoicing/handler/invoice.go:159` | → U1 |
| 14 | Valid Tax ID that belongs to somebody else | **Ruled out** as undetectable (ADR 0061) | — | Only the buyer notices; the cure is a reissue. SRI lookup at checkout was refused (no official service, rate-limited, couples checkout to the SRI). |
| 15 | Wrong Recipient on an authorized factura → operator reissue | **Built** | see 2.5 | |
| 16 | Sale Re-addressing (ADR 0058) after the factura exists | **Partial** | frozen Recipient `B/invoicing/service/saleinvoice.go:253`; delivery `B/invoicing/service/delivery.go:72`; reissue reads the live email `B/invoicing/repository/reissue.go:73` | The Recipient snapshot is unchanged (correct: re-addressing moves the addressee, not what was transacted). But the **delivery mail goes to the email frozen on the invoice**, so a factura authorized after a re-addressing is mailed to the stranded address — while a reissue's corrected factura takes the Sale's current email. → U2 |

### 2.3 What the SRI answers

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 17 | RECIBIDA → AUTORIZADO → delivered | **Built** | `B/invoicing/sri/authority.go:56` `Submit`, `:81` `QueryOutcome`; `B/invoicing/service/drainer.go:361` | Delivery happens in the same round under the same lease. |
| 18 | EN PROCESAMIENTO, repeatedly | **Built** | `drainer.go:105` `SaleInvoiceLadder`, `:109` `SaleInvoiceAttentionAfter`, `:743` `undecidedStatus` | 1 m · 5 m · 15 m · hourly from `issued_at`; at 24 h undecided → `needs_attention` **and still polled hourly**. |
| 19 | Error 43, clave already registered (we sent it, lost the answer) | **Built** | `B/invoicing/sri/authority.go:67` (`AlreadyHeld`) | Treated as received; polled, never resent. |
| 20 | Error 70, clave en procesamiento | **Built** | `B/invoicing/sri/client.go:90`; `drainer.go:766` `heldByAuthority` | Same: poll, never resubmit. |
| 21 | Transport error, HTTP 5xx, SOAP fault, timeout | **Built** | `B/invoicing/service/invoice.go:349` `attempt`; `drainer.go:353`, `:555` `reschedule` | Attempt row written, status untouched, rescheduled on the ladder; a never-acknowledged document is resent with the same bytes. |
| 22 | Error 50, "error interno general" (SRI-side, transient) | **Partial** | only 43, 70, 60 are named constants (`sri/client.go:88-92`) | Arrives as an ordinary DEVUELTA → `needs_attention`, not retried automatically. → U4 |
| 23 | DEVUELTA on schema (35/36/47/48/49) | **Partial** | `B/invoicing/service/invoice.go:423` `applyOutcome` | → `needs_attention`, messages verbatim. Correct outcome; not distinguished by code. |
| 24 | NO AUTORIZADO on signature / certificate (39/40) | **Partial** | same | Same bucket. The operator fixes the certificate and Resends. |
| 25 | NO AUTORIZADO emisor-side (37/46/56/57/63: RUC sin autorización, no existe, establecimiento cerrado, suspendida, clausurado) | **Partial** | same | Same bucket; not fixable by resend, but nothing says so. |
| 26 | Arithmetic 52, clave mismatch 58, extemporánea 65, fecha inválida 67 | **Partial** | same | Same bucket. |
| 27 | Error 45, secuencial registrado | **Partial** | `backend/migrations/096_invoicing_invoices.sql:105-120` UNIQUE on (issuer, env, cod_doc, estab, pto_emi, secuencial) | Prevented structurally against ourselves; if the SRI returns it (another system on the same punto de emisión) it is the same bucket, with no resequence path. |
| 28 | Every definite refusal is one `needs_attention` bucket | **Untracked** (by design so far; never ruled in an ADR) | `invoice.go:423` | Messages are stored verbatim on the row and in the ledger, so a per-code taxonomy could be added without re-fetching. → U4 |
| 29 | Operator Resend from `needs_attention` | **Built** | `B/invoicing/service/invoice_actions.go:71` `ResendInvoice`, `:160` `rebuildAndSign` | Same clave and secuencial, re-signed with the current certificate and refreshed Issuer details; on `AlreadyHeld` the stored bytes are kept. |
| 30 | Operator Mark annulled | **Built** | `B/invoicing/service/attention.go:114` `AnnulInvoice`; `backend/migrations/099_invoice_annulment.sql` | From `pending` or `needs_attention`, signed documents only; records who/when; irreversible; the portal annulment itself is a manual act. |
| 31 | Reconciliation via `consultarEstadoAutorizacionComprobante` | **Untracked** (proposed in the research doc §3.3 rule 10, never ruled) | `B/invoicing/sri/client.go:231` uses `autorizacionComprobante` only | The ladder poll plus the operator's Check status is the only reconciliation. → U5 |
| 32 | Sequence numbering per (issuer, environment, cod_doc, estab, pto_emi) | **Built** | `B/invoicing/repository/invoice.go:130` `allocateSecuencial` | Allocated inside the signing tx; rollback leaves no hole; unsignable documents park before allocation. |
| 33 | Ambiente pruebas vs producción | **Built** | `B/invoicing/sri/client.go:19-20` (celcer / cel); `sri/authority.go:41` `AmbienteFor` | Environment is a column on the Issuer, copied at signing time. |
| 34 | Attempts ledger, one row per SRI request | **Built** | `B/invoicing/repository/invoice.go:327` `RecordAttempt` | Outcome, verbatim messages, error text, duration. |

### 2.4 Reversal

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 35 | Reversal while the factura is unsigned (owed, or parked unsignable) | **Built** | `B/invoicing/service/saleinvoice.go:188`, `:234` `withdrawNeverSent` | Withdrawn in the reversal's tx; SRI told nothing. |
| 36 | Reversal while the factura is pending at the SRI | **Built** | `saleinvoice.go:222`; `B/invoicing/repository/drainer.go:81-90` | Credit Note owed but unclaimable until the factura is terminal. |
| 37 | Reversal of an authorized factura | **Built** | `saleinvoice.go:253` `creditNoteOf`; motivo `B/invoicing/invoice.go` `CreditNoteMotivo` | Same Recipient, lines and totals; full amount; route as reason and motivo. |
| 38 | Reversal while the factura is `needs_attention` with a number consumed | **Built** | `saleinvoice.go:195` | Credit Note waits; if the factura later dies (annulled) the Credit Note is withdrawn (`drainer.go:378`). Waits indefinitely otherwise. |
| 39 | The Credit Note itself is refused | **Partial** | `invoice.go:423` | `needs_attention` for the operator; if annulled, the reversed Sale keeps an authorized, uncredited factura with no automatic follow-up. → U6 |
| 40 | Routes that produce a Credit Note | **Built** for customer + reconciler-agreed (`customer`) and operator (`platform`) | `B/sales/service/reversal.go:967`, `B/sales/service/operator.go:529` | Staff reversal, import undo and correction pass no seam — correct, import-channel Sales never owe a document. A refused Reversal Request reverses nothing. |
| 41 | Reversal during a reissue | **Built** | see 2.5 #47 | |

### 2.5 Reissue (ADR 0061)

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 42 | Operator reissue: Credit Note (reason `reissue`) + corrected factura owed in one transaction; corrected signed only once the Credit Note is authorized | **Built** | `B/invoicing/handler/reissue.go:56`; `B/invoicing/service/reissue.go:41`; `B/invoicing/repository/reissue.go:53`; gate `B/invoicing/repository/drainer.go:91-95` | Decided under the Sale's lock; Tax ID validated exactly as at checkout; corrected factura's email is the Sale's current one. |
| 43 | Credit Note refused, then Mark annulled | **Built** | `B/invoicing/service/attention.go:129`; `drainer.go:435` `withdrawCorrectedFacturaOfADeadCreditNote` | Corrected factura withdrawn unsigned; old factura stands current; operator may reissue again. |
| 44 | Corrected factura refused | **Built** (bucket) | `invoice.go:423` | `needs_attention`; Resend or Mark annulled. |
| 45 | Corrected factura annulled → Sale with no current factura | **Tracked** #480 | `B/invoicing/service/attention.go:177` `DocumentRoleNotCurrent` | Same terminal state as an annulled first factura (#477). |
| 46 | Reissue refused on: manual Tax Invoice, Credit Note, not-authorized document, reversed Sale, reissue in flight, superseded, already credited | **Built** | `B/invoicing/service/reissue.go:358`, `:380` `reissueRefusal`; codes in `B/invoicing/errors.go:160-167` | Each its own 409 code. |
| 47 | Sale Reversal during a reissue | **Built** | `saleinvoice.go:141-161`, `:199-222`; `drainer.go:405` `withdrawRedundantCreditNote`; `delivery.go:67` | Exactly one Credit Note reaches the SRI; the corrected factura is withdrawn; a reissue Credit Note that authorizes after the reversal is mailed with reversal wording (#484). |
| 48 | Second reissue on the corrected factura (chain) | **Built** | `B/invoicing/service/attention.go:269` `chainOrder`; `ErrReissueInFlight` `reissue.go:387` | Unbounded chain; one reissue per Sale at a time. |
| 49 | Recipient Warning cleared by the reissue | **Built** | `B/invoicing/repository/invoice.go:313` | Cleared when the reissue's Credit Note authorizes — before the corrected factura is even signed. See #13. |

### 2.6 Delivery and surfaces

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 50 | Delivery mail after authorization: signed XML attached, Customer Area link | **Built** | `B/invoicing/service/delivery.go:79`, `:87` | |
| 51 | RIDE (PDF) attached or generated for the buyer | **Untracked** (the SRI FAQ obliges XML + RIDE, S6 Q21) | `B/invoicing/sri/doc.go:21` names it as future; only a printable staff page exists (`apps/staff/app/operator/invoicing/[id]/ride/`) | → U7 |
| 52 | Delivery fails (Resend down, 429) | **Built** | `delivery.go:90-97`; claim `authorized AND delivered_at IS NULL` `B/invoicing/repository/drainer.go:80` | Authorization untouched; retried on the ladder; `MarkDelivered` is the never-twice guard. No 429-specific pacing. |
| 53 | Sale Confirmation of a House sale says a factura will follow | **Built** | `B/platform/email_content.go:524` | Only when a document was actually owed. |
| 54 | Credit Note mail copy: reversal vs reissue; corrected factura says it replaces | **Built** | `email_content.go:1885`, `:1898`, `:1910`, applied `:1938` | |
| 55 | Customer Area shows the chain: current · superseded (still downloadable, labelled) · credit_note; "on its way" before authorization | **Built** | `B/invoicing/service/customer_documents.go:53-113`; `apps/storefront/components/sale-documents.tsx` | Nothing the SRI said travels to the buyer. |
| 56 | Buyer told when a document's state changes (Res. 25-14: annulment must be communicated) | **Partial** | credit/reissue mails exist; no mail on Mark annulled | An annulled factura produces no buyer mail. → U8 |
| 57 | Operator list: kind filter, `recipient_warning` filter, superseded marker, chain on the Sale lookup; dashboard counts (`needs_attention`, Recipient Warnings) | **Built** | `B/invoicing/handler/invoice.go:176-183`; `B/invoicing/service/attention.go:69`, `:219`; `apps/staff/app/operator/invoicing/operator-invoices-client.tsx:84-91`; `operator-dashboard-client.tsx:182,277` | |

### 2.7 Ops and legal edges

| # | Flow | Status | Proof | Note |
|---|---|---|---|---|
| 58 | Drainer scheduler job | **Built**, ships paused | `terraform/modules/ticket-pos/sale_invoice_drainer.tf:58,72,112`; `variables.tf:484-501` | `*/5 * * * *` America/Guayaquil; 60 s budget < 120 s attempt deadline < 300 s, asserted by a test. Distinct from `SALE_INVOICING_ENABLED`. |
| 59 | Certificate expiry | **Built** (ADR 0063, #490) | Derivation `B/invoicing/certificate_expiry.go`; Issuer read carries `certificate_expiry`; the Drainer's tick works the 30/7/1/0 ladder above the flag (`B/invoicing/service/certificate_expiry_warning.go`, called first in `drainer.go` `DrainSaleInvoices`); ledger `backend/migrations/103_certificate_expiry_notices.sql`; mail `B/platform/email.go` `CertificateExpiryWarning`; expired → park with the date (`drainer.go` `certificateExpiredMessage`) | One mail per rung per certificate to the operator allowlist in each Staff Locale, highest-unfired-only, restarting on a new fingerprint; banners on the dashboard, the invoicing list and the Issuer page (#504). Alive while `SALE_INVOICING_ENABLED` is closed, but only while the Drainer's scheduler job (row 58) is unpaused. Rotation = re-upload. |
| 60 | Seven-year retention of signed XML + authorization XML | **Built** | `backend/migrations/098_sale_invoices.sql` columns; no DELETE anywhere in `B/` | Deliberate. |
| 61 | Customer erasure request touching invoice rows | **Ruled out** for now (ADR 0060 Consequences; deletion requests go to counsel) | — | No path exists; nothing can delete an issued document. |
| 62 | IVA: fixed 15% inside the price | **Built** | `B/invoicing/invoice.go` `SaleInvoiceIVARate = IVARate15`; `sri.BackOutIVA` | 0% RUAC (artistic events ≤ 2,000 capacity) explicitly out of scope; manual Tax Invoices may still pick 0/exento/no objeto. |
| 63 | formaPago | **Built** | `B/invoicing/sri/factura.go:200-235` | Card provider → 19; otherwise 20. |
| 64 | Anexo 26 "RUC Proveedor" campoAdicional | **Ruled out** (ADR 0059: the platform is the emisor, not a provider) | not emitted anywhere in `backend/` | Becomes relevant only if Organizations ever become emisores. |
| 65 | Platform Fee on the factura | **Ruled out** (ADR 0060: the fee is the platform's own money whichever way Fee Handling went) | `drainer.go:619` `saleFacturaParts` | One line per Ticket Sale Line as the buyer paid it. |
| 66 | RIMPE `contribuyenteRimpe` tag, `obligadoContabilidad` | **Built** for the single Issuer | `B/invoicing/sri/factura.go:462` from the Issuer regime; goldens `sri/testdata/golden/factura_rimpe.xml` | Facts of the platform's own RUC, entered once. |

---

## 3. Untracked gaps, for triage

Nothing here has an issue or a ruling. Which ones become tickets is a decision, not a finding.

| Id | Gap | Rows | Why it matters |
|---|---|---|---|
| U1 | Recipient Warning comments and OpenAPI text say "cleared only when superseded"; the code (per the #478 review) clears on credit — including a plain reversal's Credit Note | 13, 49 | Doc rot on a legal marker; an operator reading the API description will misread the filter. |
| U2 | Delivery mails the email frozen on the invoice, so a factura authorized after a Sale Re-addressing (ADR 0058) goes to the stranded address; a reissue takes the Sale's current email | 16 | The SRI obliges delivery to the buyer; the buyer is now the new addressee. Asymmetric with reissue. |
| U3 | No schema uniqueness on "one Sale Invoice per Ticket Sale" | 8 | Control-flow-only invariant; a second issuance path (#480) would have no backstop. |
| U4 | Every definite refusal is one bucket; error 50 (SRI internal error) parks instead of retrying; emisor-side codes (37/46/56/57/63) are not marked "resend won't help" | 22–28 | An SRI hiccup becomes operator work; operators cannot tell a fixable refusal from a dead one without reading the Ficha. Data is already stored to derive it. |
| U5 | No reconciliation against `consultarEstadoAutorizacionComprobante` | 31 | Research doc §3.3 rule 10 proposed a daily check; the only cross-check today is the same `autorizacionComprobante` poll. |
| U6 | A reversal's Credit Note annulled leaves a reversed Sale with an authorized, uncredited factura and nothing chasing it | 39 | Declared income for a sale that no longer stands, invisible after the operator acts. Sibling of #480. |
| U7 | No RIDE for the buyer (XML only) | 51 | S6 Q21: the emisor must deliver XML **and** RIDE; a RIDE without the número de autorización is worthless, so it can only be produced after authorization — which is when the mail goes. |
| U8 | Mark annulled sends the buyer nothing | 56 | Res. 25-14: any change to a comprobante's state must be communicated to the receptor. |
| U9 | ~~No certificate-expiry early warning~~ — closed by ADR 0063 / #490 (#500–#505) | 59 | Built: the Drainer's tick warns the operator allowlist at 30, 7, 1 and 0 days and the staff app banners it. What remains is deployment, not code: the Drainer's Cloud Scheduler job ships paused (row 58) and must be unpaused before the production certificate's first rung, and the mail's Issuer-page link takes its origin from `STAFF_BASE_URL`, which Terraform now mounts on the API from `staff_domain` (nothing to set by hand once the staff domain is mapped). |

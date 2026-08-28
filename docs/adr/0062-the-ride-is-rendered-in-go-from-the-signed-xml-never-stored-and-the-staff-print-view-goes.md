# The RIDE is rendered in Go from the signed XML, never stored, and it is the only RIDE — the staff print view goes

Specified as issue #489, the buyer-facing half that #456 deliberately left out ("the platform sends nothing this
round"). Builds on [ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md),
which promised that "when the RIDE exists, the same mail and the same Customer Area page carry both" the XML
and the RIDE, and on [ADR 0059](./0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
which made the stored signed XML the legal artifact.

## Context

The SRI obliges the emisor to deliver the XML **and** the RIDE to the buyer (FAQ S6 Q21); a RIDE without the
número de autorización "no tiene validez" (Q20); the RIDE is not archived, only the XML is kept seven years
(Q18–19). Today the delivery mail carries the signed XML alone and the only RIDE is a React print view in the
Staff app that an operator prints by hand — hardcoded "Factura", so a Credit Note's RIDE was never right. Nothing
in the repository makes a PDF, and the Cloud Run image has no browser. Delivery and the buyer's download are both
Go handlers; the `sri` kit already exposes every formatter a RIDE needs, and line arithmetic is stored on the
document precisely so no rendering ever recomputes a legal figure.

## Decision

1. **The RIDE is rendered in Go, with pure-Go libraries** — `go-pdf/fpdf` for the page and `boombuler/barcode`
   for the Code 128 clave — **from the stored signed XML and the stored authorization number and date, on demand,
   and is never persisted.** Regeneration is deterministic from stored data, so a document authorized before this
   landed gets its RIDE the first time somebody asks; nothing is re-mailed. The XML is the record; the RIDE is its
   reading.
2. **A RIDE that cannot be rendered does not retry.** Rendering happens before sending. When it fails, the
   document stays authorized and undelivered and leaves the delivery queue — the same dead end as a document with
   no Sale or no signed bytes — logged for an operator. The delivery ladder is for the transport alone. Neither
   does the platform degrade to mailing the XML by itself: `delivered_at` vouches for a compliant delivery or for
   nothing.
3. **A Credit Note's RIDE is its own document**, as the Ficha Técnica has it: headed "Nota de crédito", naming the
   Sale Invoice it credits (type, number, issue date), the motivo and the valor de modificación, with no forma de
   pago. It shares the emisor, authorization, Recipient, lines and totals sections with the factura's RIDE.
4. **There is one RIDE.** The Staff app's RIDE link becomes a download of this PDF, offered beside the signed and
   authorization XML downloads and only once the document is authorized; the React print view and the
   TypeScript Code 128 encoder are deleted. Manual Tax Invoices get the same PDF. An operator and a buyer hold
   byte-identical documents, and an unauthorized document has no RIDE to hand over by mistake — which is what
   #456's "pending" banner existed to prevent.

## Considered options

- **Render the existing React page with a headless browser** and have Go call it at delivery time. Reuses the
  page pixel for pixel, at the price of a Chromium in a container image, a cross-service call inside the Drainer's
  delivery step, and a second thing that can be down when the SRI has just said yes. Rejected: the delivery step
  must be able to finish alone.
- **Retry a failed render on the ladder**, as the issue first said. The last rung is one hour forever; a broken
  renderer would retry hourly and silently, and a deterministic failure is a bug, not weather. Rejected.
- **Mail the XML alone when the render fails** and let the buyer fetch the RIDE later. The buyer would hold the
  legally weightier artifact sooner, but nothing would re-mail the RIDE and the delivery mark would lie. Rejected.
- **Keep the print view as a preview for unauthorized documents.** Two layouts drift — the "Factura" title on a
  Credit Note is the proof — and a preview of an unauthorized document is exactly what should not exist. Rejected.

## Consequences

- The repository gains its first PDF dependency and a second RIDE layout; the barcode encoder moves from
  TypeScript to a Go library.
- An authorized, undelivered document that left the queue is visible only on its operator detail page; listing
  and counting those is a separate ticket, since the dead end predates this decision.
- `TaxDocumentDelivery` grows from one attachment to two, and its copy stops naming "the XML" alone.
- The Customer Area's document chain offers two downloads per authorized document, on `current`, `superseded`
  and `credit_note` alike.

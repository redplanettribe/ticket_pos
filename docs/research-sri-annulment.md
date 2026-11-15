# Research: what the SRI tells a machine about annulling a factura (as of 2026-11-15)

Date: 2026-11-15.
Author: research agent, for issue #711 (part of map #710).

Scope: whether a Platform Operator's annulment of an **authorized** factura can be driven or confirmed by a machine, and which rules gate it.
This note extends `docs/research-sri-facturacion-electronica.md` §2.9 and does not repeat what is verified there.
Source IDs S1-S33 are that document's; new sources are A1-A6 (see the end).

Sourcing rule: every claim is tied to a primary source (SRI resolutions, SRI PDFs on sri.gob.ec, the Ficha Técnica, gob.ec trámite sheets supplied by the SRI).
Anything that could not be confirmed at such a source is marked **UNVERIFIED**.
Law-firm and press articles were read only to look for later norms; none is the authority for any claim.

Freshness check: the SRI's *Facturación Electrónica* page (S2, fetched 2026-11-15) still links Ficha Técnica **v2.34 (27/07/2026)** as current, and its change log (S1, versions 2.31-2.34) adds no annulment service.
Searches for a 2026 resolution on annulment found none; the only 2026 SRI resolution surfaced (NAC-DGERCGC26-00000027, July 2026, reported as being about providers of invoicing systems; not read) is not about annulment by its reported title.
The regime in force is therefore Res. NAC-DGERCGC25-00000014 (A1 = S9) as amended by NAC-DGERCGC25-00000017 (A2 = S10).
"No later change" is a negative finding from search, so it is **UNVERIFIED** in the strict sense: no index of all 2026 SRI resolutions was read.

---

## Answers to the six questions

### 1. Is there a machine route for annulment?

**No.**
Res. 25-14 Art. 2 names the online routes exhaustively: "La anulación en línea podrá realizarse a través del portal web institucional o el Facturador SRI, cuando la emisión se haya realizado en este aplicativo" (A1).
The Facturador SRI route applies only to comprobantes issued in that free desktop app (A1 Art. 2; S1 §9), so it is closed to the platform, which signs its own XML.
The remaining routes are human: the SRI en línea menu *Facturación Electrónica > Producción > Anulación > Solicitud de anulación de comprobantes* (A3 §1.2.1; A4), and the trámite with a letter plus an Excel annex, in person or uploaded through *Trámites y notificaciones* (A4).
The mass-annulment trámite is reserved for more than 1,000 comprobantes in a month (A1 Disposición General Cuarta).

The Ficha v2.34 (S1) has no annulment web service for comprobantes.
It does contain a REST endpoint named `devolucionesIndividualesAnulaciones` (S1 §13.7, on `celcer.sri.gob.ec/devolucion-iva/rest/` and `srienlinea.sri.gob.ec/devolucion-iva/rest/`), and its error codes 6000-6012 are named `ANULACION_*`.
That service is **not** comprobante annulment: it cancels the *automatic IVA refund discount to older adults* (DIG) "para los casos en que no se pueda concretar la transacción", requires the beneficiary's access code, and only accepts facturas or notas de débito issued **today** (error 6005 `ANULACION_FECHA_DIFERENTE_HOY`, 6006) (S1 §13, §13.7, error table).
It does not change a comprobante's state at the SRI and is irrelevant to #710.

Conclusion: the guided-and-recorded shape stands.
A headless-browser driver would be scripting the SRI en línea login (RUC + clave) and a JSF form; no primary source offers or tolerates that as an interface. (Confidence: verified at A1, A3, A4, S1.)

### 2. Does a consulta answer differently for an annulled clave?

**Yes, but not `autorizacionComprobante`.**
Since Ficha v2.31 (27/03/2025) the SRI exposes `ConsultaComprobante?wsdl` on `celcer` and `cel`, namespace `http://ec.gob.sri.ws.consultas`, operation `consultarEstadoAutorizacionComprobante(claveAcceso)` (S1 §8.2, change log 2.31).
Its documented `estadoAutorizacion` values are exactly "AUTORIZADO", "NO AUTORIZADO", "PENDIENTE DE ANULAR" and "ANULADO" (S1 §8.2.3 and again §8.4).
The sample response for an authorized document carries `claveAcceso`, `mensajes`, `estadoAutorizacion`, `tipoComprobante` (as text, e.g. "Nota de Débito"), `rucEmisor` and `fechaAutorizacion` (S1 §8.2.3).
Two refusals are documented, both as `estadoConsulta` = "RECHAZADA" with mensaje identificador 99 "ERROR AL CONSULTAR DATOS DEL SERVICIO WEB":
- `informacionAdicional` "No es posible validar la clave de acceso ya que la fecha de emisión está fuera del rango permitido." (date outside an allowed range; the range itself is **UNVERIFIED**, the Ficha does not state it);
- `informacionAdicional` "No existen datos para los parámetros ingresados" (clave unknown to the SRI) (S1 §8.2.3).

So a Check status after the portal act can call `consultarEstadoAutorizacionComprobante` and expect "ANULADO".
For a factura the expected value is "ANULADO" straight away, never "PENDIENTE DE ANULAR", because facturas skip receptor acceptance (see question 4; A3 §1.2.1, §1.2.2.1.1).
"PENDIENTE DE ANULAR" is reachable only for retenciones, notas de crédito and notas de débito (A1 Art. 4; A3).

What `autorizacionComprobante` (the authorization service the platform already calls, `backend/internal/invoicing/sri/client.go`) returns for an annulled clave is **not documented** anywhere in S1: its states are only PPR, AUT and NAT (S1 Tabla 6 in §5.12, §7.4), and §5.11 only says it "devuelve el XML autorizado".
Whether it keeps answering AUTORIZADO after annulment is **UNVERIFIED**.
Do not use it to confirm an annulment.

Live probing was not possible: every connection to `celcer.sri.gob.ec` and `cel.sri.gob.ec` from this machine was reset on 2026-11-15, so neither the `ConsultaComprobante` WSDL nor a real response was read.
The element names above come from the Ficha's own samples, not the live schema (**UNVERIFIED** against the WSDL).
Note the samples are inconsistent: the success sample uses `estadoAutorizacion`, the refusal samples `estadoConsulta`; a parser should accept both.

Corollary: the SRI may annul a consumidor-final factura *de oficio* (A2 Disposición General Quinta, question 6), so "ANULADO" can appear without any Platform Operator act.

### 3. The exact window

**Until day 7 of the month following the month of emission, rolling forward to the next business day when day 7 is a rest day or a national or local holiday.**
Res. 25-14 Art. 3 first inciso: "se podrán anular en línea hasta el día 10 del mes siguiente al de su emisión, cuando esa fecha coincida con días de descanso obligatorio o feriados nacionales o locales, se podrán anular hasta el siguiente día hábil" (A1), with "el día 10" replaced by "el día 7 (siete)" by Res. 25-17 Art. 1(a)(1) (A2).
The SRI's own bulletin paraphrases the rollover as "si esa fecha coincide con un fin de semana o feriado, el plazo se extiende hasta el siguiente día hábil" (A5).
After the window "los comprobantes de venta electrónicos se podrán anular únicamente mediante el uso de notas de crédito" (A1 Art. 3 second inciso).
The window applies to comprobantes issued from 2025-08-01 (A2, new Disposición Transitoria Primera).
No later change was found (see the freshness check).

Whose calendar and timezone:
- The anchor is the **emission** month ("del mes siguiente al de su emisión", A1 Art. 3), i.e. the factura's `fechaEmision`, not its authorization date.
  The portal form asks for the *fecha de autorización* as an identifying field only (A3 §1.2.1).
- No primary text read states a timezone.
  The SRI's own timestamps are `-05:00` (S1 §8.2.3 sample `fechaAutorizacion` "2024-12-12T10:49:37-05:00"), consistent with mainland Ecuador (`America/Guayaquil`); that the deadline is judged in that zone is **UNVERIFIED**.
- "Feriados ... locales" makes the rollover depend on a locality that the resolution does not name (the emisor's domicile, the establecimiento, or Quito); **UNVERIFIED**.
  The national calendar is set by law and executive decree each year, so a fixed table in code would go stale.
- Recommendation for the platform: gate on day 7 of the next month in `America/Guayaquil` as a hard close, and treat days 8 onward as "possibly still open if day 7 was a holiday, check the portal" rather than computing holidays.
  This errs toward the Credit Note, which is always lawful after the window (A1 Art. 3; A2 Art. 1(c)).

### 4. Does annulling a factura need the receptor's acceptance?

**No. It is immediate.**
Res. 25-14 Art. 4 requires the receptor's acceptance, within 5 business days, only for "Los comprobantes de retención, notas de crédito y notas de débito"; silence leaves the request "sin efecto" (A1 Art. 4).
Res. 25-17 only added an exemption from acceptance for those same three types when the receptor is foreign or registered as deceased (A2 Art. 1(b)); it did not extend acceptance to facturas.
The SRI en línea guide is explicit: "En el caso de facturas, guías de remisión y liquidaciones de compra ... al presionar el botón Aceptar, la anulación se ejecutará y no será necesario ningún proceso posterior" (A3 §1.2.1), and "Las facturas ... no requieren la aceptación de solicitud de anulación, puesto que se anulan directamente" (A3 §1.2.2.1.1, and Nota Aclaratoria 3).
The SRI bulletin lists acceptance only for "comprobantes de retención, notas de crédito o notas de débito" (A5).
Some press coverage claims facturas need acceptance too; that contradicts all four primary texts and was disregarded.

There is no pending state for a factura, so the platform needs no `pending_annulment` status for this flow.
What the SRI does do on a factura annulment: it sends a notification to the receptor's *buzón* and an email to the receptor's address (A3 §1.2.1).
The form requires the receptor's identification "exactamente como consta en XML del comprobante, incluido espacios" and the receptor's email (A3 §1.2.1), so the guided flow should hand the operator both, copied from the signed XML.
Separately, the emisor must itself inform the receptor of "cualquier modificación que se realice al estado del comprobante electrónico" (A1 Disposición Reformatoria Primera §1, adding to Art. 5 of 18-233); this is `docs/sri-invoicing-flows.md` U8.

Advisory, not a gate: the SRI FAQ (2023) and guide say to obtain the receptor's consent first, so as not to annul a factura the receptor has declared (S6 Q25, Q28; A3 Nota Aclaratoria 2, 8).
Annulment is irreversible (S6 Q27; A3 Nota Aclaratoria 9).
"Pendiente de anular" documents keep tax validity and "Anulado" ones lose it (A3 Notas 5, 6).

### 5. Can a factura with an authorized Credit Note against it be annulled? Partially credited?

**No primary source rules on it; UNVERIFIED either way.**
Neither resolution, the guide, the FAQ nor the Ficha mentions annulling a factura that a nota de crédito references, and the Ficha has no error code for it.
Whether the portal refuses such a request is **UNVERIFIED**.

What the primary texts do say, which bears on it:
- A nota de crédito is itself a means of annulling a factura (A1 Art. 2), issued "para anular operaciones, aceptar devoluciones y conceder descuentos" (Reglamento Art. 15, quoted in A2's considerandos).
  A fully credited factura is therefore already annulled in law by the other route.
- Online annulment is for comprobantes "emitidos con errores o aquellos cuya operación ... no se haya producido" (A1 Art. 1), and the guide restricts factura annulment to "transacciones que no han sido realizadas, siempre y cuando estas facturas no hayan sido utilizadas por parte del emisor y receptor en sus declaraciones" (A3 Nota Aclaratoria 2).
  A partial credit (a partial refund) presupposes the operation took place, which argues against annulling the factura.
- Annulling the factura would leave an authorized nota de crédito referencing an annulled document; annulling that nota de crédito needs the receptor's acceptance within 5 business days (A1 Art. 4), which the platform cannot obtain on the buyer's behalf.

Recommendation for the platform: refuse annulment of any factura that has a Credit Note in flight or authorized, full or partial.
That is a platform rule derived from the above, not an SRI rule.

### 6. Consumidor final, and what else blocks annulment

**Confirmed: a consumidor-final factura can never be annulled, nor credited, once transmitted.**
"Las facturas electrónicas emitidas con la leyenda 'consumidor final' no se podrán anular una vez emitidas y transmitidas al Servicio de Rentas Internas. En estos casos, no procede la emisión de notas de crédito." (A1 Art. 3 last inciso.)
This inciso has applied since **2026-01-01** (A2 Art. 1(f), adding to the Disposición Final); before that date the rest of 25-14 applied from 2025-08-01.
The SRI may annul such facturas *de oficio* in control operations by fedatarios fiscales (test purchases), "en las transacciones realizadas para el efecto" (A2 Art. 1(d), new Disposición General Quinta).
A factura above the USD 50 ceiling must identify the buyer (S1 §9.10; S6 Q34), so it is not a consumidor-final factura and this block does not reach it; an identified buyer at any amount is not blocked.

Everything else that blocks, from the primary texts:
- **Outside the window**: after day 7 (rolled) only a nota de crédito (A1 Art. 3).
- **Facturas supporting a tax refund**: "Los comprobantes de venta y de retención que sustenten la devolución de impuestos no podrán ser anulados. En estos casos, tampoco procede la emisión de notas de crédito." (A1 Disposición General Tercera.) The platform cannot know whether a buyer used a factura in a refund claim.
- **Negotiated facturas comerciales negociables**: not annullable, not even by nota de crédito (A1 Disposición General Segunda). The platform does not issue them.
- **Purpose**: only for errors or an operation that did not occur (A1 Art. 1).
- **Already declared** by emisor or receptor: the FAQ and guide say a declared factura must be credited, not annulled (S6 Q25; A3 Nota Aclaratoria 2). This is SRI guidance, not text of the resolution, and whether the portal enforces it is **UNVERIFIED**.
- **Already annulled**: irreversible, and a new comprobante must be issued instead (S6 Q27; A3 Nota 9). A receptor's rejection (not applicable to facturas) can be followed by a new request (A3 Nota 10).

---

## Stale primary material to be aware of

Two SRI documents still in circulation predate Res. 25-14 and contradict it on the window:
- The FAQ PDF (S6, created 2023-06-13) Q29 says "Los emisores pueden anular hasta 90 días después de la emisión".
- The *Guía para contribuyentes - Anulación de Comprobantes Electrónicos* (A3, "actualizado marzo 2025") quotes Art. 7 of Res. NAC-DGERCGC16-00000092 and its 90-day window (Introducción; Nota Aclaratoria 7).
Res. 25-14 Disposición Reformatoria Segunda repealed that Art. 7 (A1).
The guide's mechanics (form fields, states, notifications, no acceptance for facturas) are unaffected and still match A1 Art. 4; only its 90-day figure is superseded.

---

## Sources

- A1 (= S9). SRI, Resolución NAC-DGERCGC25-00000014, 27 June 2025, published Tercer Suplemento R.O. 70, 30 June 2025: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=137046a6-787c-47fb-a2d7-176595d292dc&nombre=NAC-DGERCGC25-00000014.pdf (Arts. 1-5, Disposiciones Generales Primera-Cuarta, Reformatorias, Final; re-read 2026-11-15)
- A2 (= S10). SRI, Resolución NAC-DGERCGC25-00000017, 29 July 2025: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar?id=e98fc8a6-299e-4ea9-8de7-2f6c70dbb4f5&nombre=NAC-DGERCGC25-00000017.pdf (Art. 1(a)-(f); re-read 2026-11-15)
- A3. SRI, *Guía para contribuyentes - Anulación de Comprobantes Electrónicos*, "actualizado marzo 2025" (PDF created 2025-03-26), linked from S2: https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/c97242e6-c271-4eb8-8f6a-f687313118ba/Guia%20para%20contribuyentes%20de%20anulaci%c3%b3n%20de%20comprobantes%20electr%c3%b3nicos.pdf
- A4 (= S11). gob.ec, trámite *Anulación de comprobantes electrónicos* (SRI-014-02-07; signed sheet dated 2026-10-08, content "actualizada al 23 de June de 2025"): https://www.gob.ec/sri/tramites/anulacion-comprobantes-electronicos
- A5. SRI, Boletín NAC-COM-25-033, 10 July 2025, "SRI establece nuevas reglas para la anulación de comprobantes electrónicos": https://www.sri.gob.ec/o/sri-portlet-biblioteca-alfresco-internet/descargar/142630d3-569f-4cd2-a5a5-58557b7fc342/BOLET%C3%8DN%20033%20-%20SRI%20ESTABLECE%20NUEVAS%20REGLAS%20PARA%20LA%20ANULACI%C3%93N%20DE%20COMPROBANTES%20ELECTR%C3%93NICOS%20COMO%20PARTE%20DE%20SU%20ESTRATEGIA%20DE%20CONTROL.pdf (written before Res. 25-17, so its "día 10" and 12-month credit-note limit are superseded)
- A6. gob.ec, trámite *Confirmar anulación de comprobantes electrónicos* (the receptor's side; states no plazos): https://www.gob.ec/sri/tramites/confirmar-anulacion-comprobantes-electronicos
- S1. SRI, Ficha Técnica de Comprobantes Electrónicos Esquema Off-line v2.34 (27/07/2026), §5.11-5.12 (Tabla 6), §7.4, §8 (consultas), §13.7 and the 6000-series error table; URL in `docs/research-sri-facturacion-electronica.md` §6.
- S2, S6: as listed in `docs/research-sri-facturacion-electronica.md` §6.

Secondary sources read only to look for 2026 norms (none relied on): nmslaw.com.ec (2025-06-30, 2025-08-04, 2026-01-04 posts), primicias.ec, vistazo.com, sovos.com.

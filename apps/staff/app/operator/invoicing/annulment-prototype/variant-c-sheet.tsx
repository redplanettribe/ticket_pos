/* eslint-disable i18next/no-literal-string */
// PROTOTYPE - throwaway. Variant C for issue #715: annulment is a property of
// the document at the SRI, so it lives in the Authorization card, moved to the
// top. A window chip sits in the header. "Annul…" opens a right-hand sheet
// that mirrors the portal's own form, field for field, in the portal's
// Spanish, with the record step underneath. After the act, a sticky next-step
// bar under the header carries the follow-ups as plain buttons.
"use client";

import Link from "next/link";
import { useState } from "react";

import {
  Badge,
  Button,
  PageHeader,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  cn,
} from "@ticket-pos/ui";

import { followUps, formatDay, formatMoment, refusal } from "./fixture";
import {
  AnnulmentTrail,
  AuthorizationCard,
  ConfirmFields,
  CopyButton,
  PORTAL_URL,
  RestOfPage,
  StatusBadges,
  type VariantProps,
  VisibleWhitespace,
  noticeLine,
  portalPath,
  sriCheckLine,
  useConfirm,
  windowSentence,
} from "./prototype-kit";

function PortalField({ label, value, children, note }: { label: string; value: string; children?: React.ReactNode; note?: string }) {
  return (
    <div className="grid grid-cols-[9rem_1fr_auto] items-center gap-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="min-w-0 rounded border bg-muted/30 px-2 py-1 font-mono text-xs">
        <span className="break-all">{children ?? value}</span>
        {note ? <span className="block font-sans text-[11px] text-amber-700">{note}</span> : null}
      </div>
      <CopyButton value={value} />
    </div>
  );
}

export function VariantC({ invoice, window, onAnnul, onRecheck }: VariantProps) {
  const [open, setOpen] = useState(false);
  const confirm = useConfirm(invoice, window);
  const refused = refusal(invoice);
  const annulled = invoice.status === "annulled";
  const check = sriCheckLine(invoice.sriCheck, invoice.sriCheckedAt);
  const options = followUps(invoice);

  const chip = annulled ? null : refused ? (
    <Badge variant="outline" className="text-muted-foreground">
      Not annullable
    </Badge>
  ) : window.state === "closed" ? null : (
    <Badge
      variant="outline"
      className={cn(window.state === "last_days" ? "border-amber-500 text-amber-700" : "border-emerald-600 text-emerald-700")}
    >
      Annullable until {formatDay(window.deadline)}
    </Badge>
  );

  return (
    <div className="space-y-6">
      <PageHeader
        title={`Tax Invoice ${invoice.number}`}
        description={`${formatDay(invoice.issuedOn)}${invoice.saleRef ? ` · Sale ${invoice.saleRef}${invoice.saleStanding ? "" : " (reversed)"}` : ""}`}
        actions={<StatusBadges invoice={invoice} extra={chip} />}
      />

      {annulled ? (
        <div className="sticky top-2 z-40 flex flex-wrap items-center gap-2 rounded-lg border bg-card px-4 py-3 shadow-sm">
          <span className="w-full text-sm">
            {invoice.kind === "sale_invoice" && invoice.saleStanding ? (
              <>
                <span className="font-medium">Annulled.</span> Sale {invoice.saleRef} has no factura now.
              </>
            ) : (
              <span className="font-medium">Annulled. {options[0]?.owes}</span>
            )}
          </span>
          {options
            .filter((o) => o.action)
            .map((o, index) =>
              o.key === "reversal" ? (
                <Button key={o.key} asChild type="button" size="sm" variant="outline" title={o.owes}>
                  <Link href="#">Reverse the Sale…</Link>
                </Button>
              ) : (
                <Button key={o.key} type="button" size="sm" variant={index === 0 ? "default" : "outline"} title={o.owes}>
                  {o.action}…
                </Button>
              ),
            )}
        </div>
      ) : null}

      <AuthorizationCard invoice={invoice}>
        <div className="mt-3 border-t pt-3">
          {annulled ? (
            <div className="space-y-2">
              <AnnulmentTrail invoice={invoice} />
              <div className="flex items-center justify-between gap-3">
                <p
                  className={cn(
                    "text-sm",
                    check.tone === "ok" && "text-emerald-700",
                    check.tone === "warn" && "font-medium text-amber-700",
                    check.tone === "muted" && "text-muted-foreground",
                  )}
                >
                  {check.text}
                </p>
                {check.tone !== "ok" ? (
                  <Button type="button" size="sm" variant="outline" onClick={onRecheck}>
                    Check status
                  </Button>
                ) : null}
              </div>
              <p className="text-sm text-muted-foreground">{noticeLine(invoice, true)}</p>
            </div>
          ) : refused ? (
            <p className="text-sm text-muted-foreground">{refused}</p>
          ) : (
            <div className="flex items-center justify-between gap-3">
              <p className={cn("text-sm", window.state === "last_days" ? "font-medium text-amber-700" : "text-muted-foreground")}>
                {windowSentence(window)}
              </p>
              <Button type="button" size="sm" variant="destructive" onClick={() => { confirm.reset(); setOpen(true); }}>
                {window.state === "closed" ? "Record annulment…" : "Annul…"}
              </Button>
            </div>
          )}
        </div>
      </AuthorizationCard>

      <RestOfPage invoice={invoice} hideAuthorization />

      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="right" className="w-full max-w-xl overflow-y-auto p-6 sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>Annul {invoice.number}</SheetTitle>
            <SheetDescription>{windowSentence(window)}</SheetDescription>
          </SheetHeader>

          <section className="space-y-3 rounded-lg border p-4">
            <div className="flex items-baseline justify-between">
              <p className="text-sm font-medium">At the portal: Solicitud de anulación</p>
              <a href={PORTAL_URL} target="_blank" rel="noreferrer" className="text-xs underline underline-offset-4">
                SRI en línea ↗
              </a>
            </div>
            <p className="text-xs text-muted-foreground">{portalPath(invoice)}</p>
            <div className="space-y-2">
              <PortalField label="Tipo de comprobante" value="Factura" />
              <PortalField label="Clave de acceso" value={invoice.accessKey} />
              {invoice.status === "authorized" ? (
                <PortalField label="Fecha de autorización" value={formatMoment(invoice.authorizedAt)} />
              ) : null}
              <PortalField
                label="Identificación receptor"
                value={invoice.recipientTaxIdXml}
                note="As in the XML, spaces included"
              >
                <VisibleWhitespace value={invoice.recipientTaxIdXml} />
              </PortalField>
              <PortalField label="Correo receptor" value={invoice.recipientEmail} />
            </div>
            <p className="text-xs text-muted-foreground">Press Aceptar. A factura is annulled at once, with no acceptance.</p>
          </section>

          <section className="space-y-3 rounded-lg border border-destructive/40 p-4">
            <p className="text-sm font-medium">Here: record it</p>
            <p className="text-xs text-muted-foreground">Whether you just did it or did it days ago, this is the same record.</p>
            <ConfirmFields invoice={invoice} confirm={confirm} compact />
            <p className="text-xs text-muted-foreground">{noticeLine(invoice, false)}</p>
            <Button
              type="button"
              variant="destructive"
              className="w-full"
              disabled={!confirm.ready}
              onClick={() => {
                setOpen(false);
                onAnnul({ note: confirm.note, lateAcknowledged: confirm.late });
              }}
            >
              Mark annulled
            </Button>
          </section>
        </SheetContent>
      </Sheet>
    </div>
  );
}

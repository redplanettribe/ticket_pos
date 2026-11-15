/* eslint-disable i18next/no-literal-string */
// PROTOTYPE - throwaway. Variant B for issue #715: no dialog. An always-open
// runbook panel at the top of the page: the window as a timeline, the portal
// steps as a checklist beside an inline record form. Once recorded, the same
// panel becomes a "what now" chooser that compares the follow-ups side by side.
"use client";

import { useState } from "react";

import { Badge, Button, Card, CardContent, CardHeader, CardTitle, PageHeader, cn } from "@ticket-pos/ui";

import { type FollowUp, type Window, followUps, formatDay, formatMoment, refusal } from "./fixture";
import {
  AnnulmentTrail,
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

function WindowTimeline({ issuedOn, window }: { issuedOn: string; window: Window }) {
  const pct = Math.round(window.progress * 100);
  return (
    <div className="space-y-1">
      <div className="relative h-2 rounded-full bg-muted">
        <div
          className={cn(
            "absolute inset-y-0 left-0 rounded-full",
            window.state === "open" && "bg-emerald-500",
            window.state === "last_days" && "bg-amber-500",
            window.state === "closed" && "bg-neutral-400",
          )}
          style={{ width: `${pct}%` }}
        />
        <div className="absolute -top-1 h-4 w-0.5 bg-foreground" style={{ left: `calc(${pct}% - 1px)` }} />
      </div>
      <div className="flex justify-between text-xs text-muted-foreground">
        <span>Issued {formatDay(issuedOn)}</span>
        <span className={cn(window.state === "last_days" && "font-medium text-amber-700")}>
          {window.state === "closed" ? "Window closed" : window.daysLeft === 0 ? "Last day" : `${window.daysLeft} ${window.daysLeft === 1 ? "day" : "days"} left`}
        </span>
        <span>Deadline {formatDay(window.deadline)}</span>
      </div>
    </div>
  );
}

function Step({
  n,
  done,
  onToggle,
  title,
  children,
}: {
  n: number;
  done: boolean;
  onToggle: () => void;
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <li className="flex gap-3">
      <button
        type="button"
        onClick={onToggle}
        className={cn(
          "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-medium",
          done ? "border-emerald-600 bg-emerald-600 text-white" : "border-muted-foreground/40 text-muted-foreground",
        )}
        aria-label={`Step ${n}`}
      >
        {done ? "✓" : n}
      </button>
      <div className="min-w-0 flex-1 space-y-1">
        <p className={cn("text-sm font-medium", done && "text-muted-foreground line-through")}>{title}</p>
        {children}
      </div>
    </li>
  );
}

function Field({ label, value, children }: { label: string; value: string; children?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1">
      <div className="min-w-0">
        <p className="text-[11px] text-muted-foreground">{label}</p>
        <p className="break-all font-mono text-xs">{children ?? value}</p>
      </div>
      <CopyButton value={value} />
    </div>
  );
}

function FollowUpChooser({ options }: { options: FollowUp[] }) {
  const [chosen, setChosen] = useState<FollowUp["key"] | null>(options.length === 1 ? options[0].key : null);
  const picked = options.find((o) => o.key === chosen);
  return (
    <div className="space-y-3">
      <div className={cn("grid gap-3", options.length === 3 ? "sm:grid-cols-3" : "grid-cols-1")}>
        {options.map((option) => (
          <button
            key={option.key}
            type="button"
            onClick={() => setChosen(option.key)}
            className={cn(
              "space-y-2 rounded-lg border p-3 text-left text-sm transition-colors",
              chosen === option.key ? "border-primary ring-2 ring-primary/30" : "hover:bg-muted/50",
            )}
          >
            <p className="font-medium">{option.title}</p>
            <p className="text-xs text-muted-foreground">
              <span className="font-medium text-foreground">Owes: </span>
              {option.owes}
            </p>
            <p className="text-xs text-muted-foreground">
              <span className="font-medium text-foreground">Buyer: </span>
              {option.buyerHears}
            </p>
          </button>
        ))}
      </div>
      {picked?.action ? (
        <div className="flex items-center gap-3">
          <Button type="button">{picked.action}</Button>
          <span className="text-xs text-muted-foreground">Or leave it: the Sale stays on the needs-attention queue.</span>
        </div>
      ) : null}
    </div>
  );
}

export function VariantB({ invoice, window, onAnnul, onRecheck }: VariantProps) {
  const [done, setDone] = useState<Record<number, boolean>>({});
  const toggle = (n: number) => setDone((d) => ({ ...d, [n]: !d[n] }));
  const confirm = useConfirm(invoice, window);
  const refused = refusal(invoice);
  const annulled = invoice.status === "annulled";
  const check = sriCheckLine(invoice.sriCheck, invoice.sriCheckedAt);
  const standingSale = invoice.kind === "sale_invoice" && invoice.saleStanding;

  return (
    <div className="space-y-6">
      <PageHeader
        title={`Tax Invoice ${invoice.number}`}
        description={`${formatDay(invoice.issuedOn)}${invoice.saleRef ? ` · Sale ${invoice.saleRef}${invoice.saleStanding ? "" : " (reversed)"}` : ""}`}
        actions={<StatusBadges invoice={invoice} />}
      />

      {annulled ? (
        <Card className="border-l-4 border-l-neutral-500">
          <CardHeader className="pb-3">
            <CardTitle className="flex flex-wrap items-center gap-2">
              Annulled at the SRI
              <Badge
                variant="outline"
                className={cn(
                  check.tone === "ok" && "border-emerald-600 text-emerald-700",
                  check.tone === "warn" && "border-amber-500 text-amber-700",
                )}
              >
                {check.tone === "ok" ? "SRI: ANULADO" : check.tone === "warn" ? "SRI: AUTORIZADO" : "SRI: not confirmed"}
              </Badge>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <AnnulmentTrail invoice={invoice} />
            {check.tone !== "ok" ? (
              <div className="flex items-center justify-between gap-3 rounded-md bg-muted/60 p-3 text-sm">
                <span>{check.text}</span>
                <Button type="button" size="sm" variant="outline" onClick={onRecheck}>
                  Check status
                </Button>
              </div>
            ) : null}
            <p className="text-sm text-muted-foreground">{noticeLine(invoice, true)}</p>
            <div className="border-t pt-4">
              <p className="mb-3 text-sm font-medium">
                {standingSale ? "This Sale now has no factura. What next?" : "What next"}
              </p>
              <FollowUpChooser options={followUps(invoice)} />
            </div>
          </CardContent>
        </Card>
      ) : refused ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle>Annulment</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">{refused}</CardContent>
        </Card>
      ) : (
        <Card className={cn("border-l-4", window.state === "last_days" ? "border-l-amber-500" : window.state === "closed" ? "border-l-neutral-400" : "border-l-emerald-500")}>
          <CardHeader className="space-y-3 pb-3">
            <CardTitle>Annul instead of crediting</CardTitle>
            <WindowTimeline issuedOn={invoice.issuedOn} window={window} />
            <p className="text-sm text-muted-foreground">{windowSentence(window)}</p>
          </CardHeader>
          <CardContent>
            <div className="grid gap-6 md:grid-cols-[1fr_1fr]">
              <ol className="space-y-4">
                <Step n={1} done={!!done[1]} onToggle={() => toggle(1)} title="Sign in to SRI en línea">
                  <a href={PORTAL_URL} target="_blank" rel="noreferrer" className="text-xs underline underline-offset-4">
                    Open the portal ↗
                  </a>
                </Step>
                <Step n={2} done={!!done[2]} onToggle={() => toggle(2)} title="Open the annulment form">
                  <p className="text-xs text-muted-foreground">{portalPath(invoice)}</p>
                </Step>
                <Step n={3} done={!!done[3]} onToggle={() => toggle(3)} title="Fill it in exactly">
                  <div className="space-y-1.5">
                    <Field label="Clave de acceso" value={invoice.accessKey} />
                    {invoice.status === "authorized" ? (
                      <Field label="Fecha de autorización" value={formatMoment(invoice.authorizedAt)} />
                    ) : null}
                    <Field label="Identificación receptor (spaces included)" value={invoice.recipientTaxIdXml}>
                      <VisibleWhitespace value={invoice.recipientTaxIdXml} />
                    </Field>
                    <Field label="Correo receptor" value={invoice.recipientEmail} />
                  </div>
                </Step>
                <Step n={4} done={!!done[4]} onToggle={() => toggle(4)} title="Press Aceptar. Facturas annul at once." />
              </ol>
              <div className="space-y-3 rounded-lg bg-muted/40 p-4">
                <p className="text-sm font-medium">Record it</p>
                <p className="text-xs text-muted-foreground">
                  Done at the portal, now or earlier? Record it here. The steps on the left are optional.
                </p>
                <ConfirmFields invoice={invoice} confirm={confirm} compact />
                <p className="text-xs text-muted-foreground">{noticeLine(invoice, false)}</p>
                <Button
                  type="button"
                  variant="destructive"
                  className="w-full"
                  disabled={!confirm.ready}
                  onClick={() => onAnnul({ note: confirm.note, lateAcknowledged: confirm.late })}
                >
                  Mark annulled
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <RestOfPage invoice={invoice} />
    </div>
  );
}

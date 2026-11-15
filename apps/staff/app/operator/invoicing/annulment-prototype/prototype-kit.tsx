/* eslint-disable i18next/no-literal-string */
// PROTOTYPE - throwaway. Shared bits across the three variants for issue #715.
"use client";

import { useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  Textarea,
  cn,
} from "@ticket-pos/ui";

import { type Fixture, type SriCheck, type Window, SCENARIOS, formatDay, formatMoment } from "./fixture";

export const VARIANTS = [
  { key: "A", name: "Lever card + guided dialog" },
  { key: "B", name: "Inline runbook panel" },
  { key: "C", name: "Authorization card + portal-mirror sheet" },
] as const;

export type VariantKey = (typeof VARIANTS)[number]["key"];

// What every variant gets from the host: the document as it now stands, and
// the one act, simulated in memory.
export type VariantProps = {
  invoice: Fixture;
  today: string;
  window: Window;
  onAnnul: (input: { note: string; lateAcknowledged: boolean }) => void;
  onRecheck: () => void;
};

export function PrototypeSwitcher({
  current,
  scenario,
  onChange,
}: {
  current: VariantKey;
  scenario: string;
  onChange: (next: { variant?: VariantKey; scenario?: string }) => void;
}) {
  const index = VARIANTS.findIndex((v) => v.key === current);
  const step = (delta: number) => {
    onChange({ variant: VARIANTS[(index + delta + VARIANTS.length) % VARIANTS.length].key });
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (event.key === "ArrowLeft") step(-1);
      if (event.key === "ArrowRight") step(1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  if (process.env.NODE_ENV === "production") return null;

  return (
    <div className="fixed bottom-4 left-1/2 z-[60] flex -translate-x-1/2 items-center gap-3 rounded-full bg-neutral-900 px-3 py-2 text-sm text-white shadow-lg ring-1 ring-white/20">
      <button
        type="button"
        onClick={() => step(-1)}
        className="rounded-full px-2 py-1 hover:bg-white/15"
        aria-label="Previous variant"
      >
        ←
      </button>
      <span className="whitespace-nowrap font-mono tabular-nums">
        {current} - {VARIANTS[index]?.name}
      </span>
      <button
        type="button"
        onClick={() => step(1)}
        className="rounded-full px-2 py-1 hover:bg-white/15"
        aria-label="Next variant"
      >
        →
      </button>
      <span className="h-5 w-px bg-white/25" />
      <select
        value={scenario}
        onChange={(event) => onChange({ scenario: event.target.value })}
        className="max-w-[16rem] rounded-full bg-white/10 px-2 py-1 text-xs text-white"
        aria-label="Scenario"
      >
        {SCENARIOS.map((s) => (
          <option key={s.key} value={s.key} className="text-black">
            {s.name}
          </option>
        ))}
      </select>
    </div>
  );
}

export function PrototypeNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-dashed border-amber-400 bg-amber-50 px-3 py-2 text-xs text-amber-900">
      <span className="font-semibold">Prototype note - </span>
      {children}
    </p>
  );
}

// The portal wants the receptor ID "exactamente como consta en XML del
// comprobante, incluido espacios", so spaces are drawn, not trusted to the eye.
export function VisibleWhitespace({ value }: { value: string }) {
  return (
    <>
      {value.split("").map((char, index) =>
        char === " " ? (
          <span key={index} className="mx-px rounded-sm bg-amber-100 px-0.5 text-amber-700" title="space">
            ␣
          </span>
        ) : (
          <span key={index}>{char}</span>
        ),
      )}
    </>
  );
}

export function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      className={cn("h-7 shrink-0 px-2 text-xs", className)}
      onClick={() => {
        void navigator.clipboard?.writeText(value);
        setCopied(true);
        setTimeout(() => setCopied(false), 1200);
      }}
    >
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}

export function CopyRow({
  label,
  value,
  mono = true,
  whitespace = false,
  hint,
}: {
  label: string;
  value: string;
  mono?: boolean;
  whitespace?: boolean;
  hint?: string;
}) {
  return (
    <div className="flex items-start justify-between gap-3 py-2">
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className={cn("break-all text-sm", mono && "font-mono")}>
          {whitespace ? <VisibleWhitespace value={value} /> : value}
        </p>
        {hint ? <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p> : null}
      </div>
      <CopyButton value={value} />
    </div>
  );
}

export function portalPath(invoice: Fixture): string {
  const branch = invoice.environment === "test" ? "Pruebas" : "Producción";
  return `Facturación Electrónica › ${branch} › Anulación › Solicitud de anulación de comprobantes`;
}

export const PORTAL_URL = "https://srienlinea.sri.gob.ec/sri-en-linea/inicio/NAT";

export function windowSentence(window: Window): string {
  if (window.state === "closed") {
    return `The SRI's window closed on ${formatDay(window.deadline)}. If you annulled it at the portal anyway, you can still record that.`;
  }
  if (window.daysLeft === 0) return `Annullable until today, ${formatDay(window.deadline)}.`;
  return `Annullable until ${formatDay(window.deadline)} (${window.daysLeft} ${window.daysLeft === 1 ? "day" : "days"} left).`;
}

// The confirm, as ruled (#713 section 4): type the number, optional note of
// at most 500 characters, and past the deadline a typed acknowledgement.
export const LATE_PHRASE = "annulled at the portal";

export function useConfirm(invoice: Fixture, window: Window) {
  const [typedNumber, setTypedNumber] = useState("");
  const [note, setNote] = useState("");
  const [ack, setAck] = useState("");
  const late = window.state === "closed";
  const ready = typedNumber.trim() === invoice.number && (!late || ack.trim().toLowerCase() === LATE_PHRASE);
  const reset = () => {
    setTypedNumber("");
    setNote("");
    setAck("");
  };
  return { typedNumber, setTypedNumber, note, setNote, ack, setAck, late, ready, reset };
}

export function ConfirmFields({
  invoice,
  confirm,
  compact = false,
}: {
  invoice: Fixture;
  confirm: ReturnType<typeof useConfirm>;
  compact?: boolean;
}) {
  return (
    <div className="space-y-3">
      <FormField id="typed_number" label={`Type ${invoice.number} to confirm`}>
        <Input
          value={confirm.typedNumber}
          placeholder={invoice.number}
          className="font-mono"
          onChange={(event) => confirm.setTypedNumber(event.target.value)}
        />
      </FormField>
      {confirm.late ? (
        <FormField id="late_ack" label={`Past the deadline: type "${LATE_PHRASE}" to say the portal accepted it`}>
          <Input value={confirm.ack} onChange={(event) => confirm.setAck(event.target.value)} />
        </FormField>
      ) : null}
      <FormField id="annul_note" label="Note (optional)">
        <Textarea
          value={confirm.note}
          maxLength={500}
          rows={compact ? 2 : 3}
          placeholder="Wrong Recipient, the buyer asked by phone"
          onChange={(event) => confirm.setNote(event.target.value)}
        />
      </FormField>
      <p className="text-xs text-muted-foreground">
        The SRI refuses to annul a factura the buyer used for a tax refund or already declared, and the platform
        cannot see either. You answer for that. Recording is irreversible.
      </p>
    </div>
  );
}

export function sriCheckLine(check: SriCheck, checkedAt: string | null): { tone: "ok" | "warn" | "muted"; text: string } {
  if (check === "anulado") {
    return { tone: "ok", text: `The SRI reports ANULADO${checkedAt ? `, checked ${formatMoment(checkedAt)}` : ""}.` };
  }
  if (check === "still_authorized") {
    return {
      tone: "warn",
      text: "The SRI still reports this factura as AUTORIZADO. The annulment stands as recorded; check again once the portal act has gone through.",
    };
  }
  if (check === "unknown") {
    return { tone: "muted", text: "The SRI could not be asked just now. The annulment stands as recorded." };
  }
  return { tone: "muted", text: "Not checked with the SRI yet." };
}

export function SriCheckAlert({ invoice, onRecheck }: { invoice: Fixture; onRecheck: () => void }) {
  const line = sriCheckLine(invoice.sriCheck, invoice.sriCheckedAt);
  if (line.tone === "ok") return null;
  return (
    <Alert variant={line.tone === "warn" ? "warning" : "default"}>
      <AlertTitle>{line.tone === "warn" ? "The SRI still reports this as authorized" : "Not confirmed with the SRI"}</AlertTitle>
      <AlertDescription className="space-y-2">
        <p>{line.text}</p>
        <Button type="button" size="sm" variant="outline" onClick={onRecheck}>
          Check status
        </Button>
      </AlertDescription>
    </Alert>
  );
}

// The rest of the real detail page, condensed, so each variant is judged
// against honest density rather than in a vacuum.
export function RestOfPage({ invoice, hideAuthorization = false }: { invoice: Fixture; hideAuthorization?: boolean }) {
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Recipient</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          <p className="font-medium">{invoice.recipientName}</p>
          <p className="font-mono text-muted-foreground">{invoice.recipientTaxIdXml}</p>
          <p className="text-muted-foreground">{invoice.recipientEmail}</p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Lines</CardTitle>
        </CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                <th className="py-2 pr-4 font-medium">Description</th>
                <th className="py-2 pr-4 text-right font-medium">Qty</th>
                <th className="py-2 text-right font-medium">Subtotal</th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-b">
                <td className="py-2 pr-4">General - Noche de Jazz</td>
                <td className="py-2 pr-4 text-right tabular-nums">2</td>
                <td className="py-2 text-right tabular-nums">$ 40,00</td>
              </tr>
            </tbody>
          </table>
          <div className="mt-3 flex justify-between text-sm font-medium">
            <span>Total</span>
            <span className="tabular-nums">{invoice.totalLabel}</span>
          </div>
        </CardContent>
      </Card>
      {hideAuthorization ? null : <AuthorizationCard invoice={invoice} />}
      <Card>
        <CardHeader>
          <CardTitle>Downloads</CardTitle>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Button type="button" size="sm" variant="outline">
            XML
          </Button>
          <Button type="button" size="sm" variant="outline">
            RIDE (PDF)
          </Button>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Attempts</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          submit · received · {formatMoment(invoice.authorizedAt)} · 812 ms
          <br />
          query · authorized · {formatMoment(invoice.authorizedAt)} · 431 ms
        </CardContent>
      </Card>
    </>
  );
}

export function AuthorizationCard({ invoice, children }: { invoice: Fixture; children?: React.ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Authorization</CardTitle>
        <CardDescription className="break-all font-mono text-xs">Clave: {invoice.accessKey}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-1 text-sm">
        <div className="flex justify-between">
          <dt className="text-muted-foreground">Authorization number</dt>
          <dd className="break-all text-right font-mono text-xs">{invoice.accessKey}</dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-muted-foreground">Authorized</dt>
          <dd className="text-right">{formatMoment(invoice.authorizedAt)}</dd>
        </div>
        {children}
      </CardContent>
    </Card>
  );
}

export function StatusBadges({ invoice, extra }: { invoice: Fixture; extra?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge variant="outline">{invoice.kind === "manual" ? "Tax Invoice" : "Sale Invoice"}</Badge>
      {invoice.environment === "test" ? <Badge variant="outline">Test</Badge> : null}
      <Badge variant={invoice.status === "annulled" ? "outline" : invoice.status === "authorized" ? "default" : "secondary"}>
        {invoice.status === "annulled" ? "Annulled" : invoice.status === "authorized" ? "Authorized" : "Pending"}
      </Badge>
      {invoice.status === "annulled" && invoice.sriCheck === "still_authorized" ? (
        <Badge variant="outline" className="border-amber-500 text-amber-700">
          SRI still authorized
        </Badge>
      ) : null}
      {extra}
    </div>
  );
}

export function AnnulmentTrail({ invoice }: { invoice: Fixture }) {
  if (!invoice.annulledBy || !invoice.annulledAt) return null;
  return (
    <div className="space-y-1 text-sm text-muted-foreground">
      <p>
        Recorded as annulled at the SRI by {invoice.annulledBy} on {formatMoment(invoice.annulledAt)}
        {invoice.lateAcknowledged ? ", after the deadline, on the operator's word that the portal accepted it" : ""}.
      </p>
      {invoice.annulNote ? (
        <p>
          <span className="font-medium text-foreground">Note:</span> {invoice.annulNote}
        </p>
      ) : null}
    </div>
  );
}

// The Annulment notice (#714): a Sale Invoice's buyer is told; a manual Tax
// Invoice's is not, as its issuing sends nothing.
export function noticeLine(invoice: Fixture, recorded: boolean): string {
  if (invoice.kind === "manual") return "No mail goes to the buyer: manual Tax Invoices send none.";
  return recorded
    ? `Annulment notice queued to ${invoice.recipientEmail}.`
    : `Recording sends the buyer an Annulment notice at ${invoice.recipientEmail}. The SRI also notifies them itself.`;
}

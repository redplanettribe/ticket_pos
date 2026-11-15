/* eslint-disable i18next/no-literal-string */
// PROTOTYPE - throwaway. Variant A for issue #715: the page's house style.
// A lever card where the SRI-remedies card sits today, opening a two-step
// dialog (portal handover, then record). After the act, the follow-ups are
// separate cards stacked like today's Reissue and Issue again cards.
"use client";

import Link from "next/link";
import { useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  PageHeader,
  cn,
} from "@ticket-pos/ui";

import { followUps, formatDay, formatMoment, refusal } from "./fixture";
import {
  AnnulmentTrail,
  ConfirmFields,
  CopyRow,
  PORTAL_URL,
  RestOfPage,
  SriCheckAlert,
  StatusBadges,
  type VariantProps,
  noticeLine,
  portalPath,
  sriCheckLine,
  useConfirm,
  windowSentence,
} from "./prototype-kit";

export function VariantA({ invoice, window, onAnnul, onRecheck }: VariantProps) {
  const [open, setOpen] = useState(false);
  const [step, setStep] = useState<"portal" | "record">("portal");
  const confirm = useConfirm(invoice, window);
  const refused = refusal(invoice);
  const annulled = invoice.status === "annulled";

  const start = (at: "portal" | "record") => {
    confirm.reset();
    setStep(at);
    setOpen(true);
  };

  return (
    <div className="space-y-6">
      <PageHeader title={`Tax Invoice ${invoice.number}`} description={formatDay(invoice.issuedOn)} actions={<StatusBadges invoice={invoice} />} />

      {invoice.saleRef ? (
        <Card>
          <CardHeader>
            <CardTitle>Sale</CardTitle>
          </CardHeader>
          <CardContent className="text-sm">
            <p className="font-mono font-medium">{invoice.saleRef}</p>
            {!invoice.saleStanding ? <p className="text-muted-foreground">Reversed</p> : null}
          </CardContent>
        </Card>
      ) : null}

      {annulled ? (
        <>
          <SriCheckAlert invoice={invoice} onRecheck={onRecheck} />
          <Card>
            <CardHeader>
              <CardTitle>Annulment</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <AnnulmentTrail invoice={invoice} />
              {sriCheckLine(invoice.sriCheck, invoice.sriCheckedAt).tone === "ok" ? (
                <p className="text-sm text-emerald-700">{sriCheckLine(invoice.sriCheck, invoice.sriCheckedAt).text}</p>
              ) : null}
              <p className="text-sm text-muted-foreground">{noticeLine(invoice, true)}</p>
            </CardContent>
          </Card>
          {followUps(invoice).map((followUp) => (
            <Card key={followUp.key}>
              <CardHeader>
                <CardTitle>{followUp.title}</CardTitle>
                <CardDescription>{followUp.owes}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <p className="text-sm text-muted-foreground">Buyer: {followUp.buyerHears}</p>
                {followUp.action ? (
                  followUp.key === "reversal" ? (
                    <Link href="#" className="text-sm font-medium underline underline-offset-4">
                      {followUp.action} {invoice.saleRef}
                    </Link>
                  ) : (
                    <Button type="button" variant={followUp.key === "issue_again" ? "default" : "outline"}>
                      {followUp.action}
                    </Button>
                  )
                ) : null}
              </CardContent>
            </Card>
          ))}
        </>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Annul at the SRI</CardTitle>
            <CardDescription>
              Instead of crediting, annul this factura at the SRI en línea portal and record it here. The SRI has no
              web service for this, so the portal step is yours.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {refused ? (
              <p className="text-sm text-muted-foreground">{refused}</p>
            ) : (
              <>
                <p
                  className={cn(
                    "text-sm",
                    window.state === "last_days" && "font-medium text-amber-700",
                    window.state === "closed" && "text-muted-foreground",
                  )}
                >
                  {windowSentence(window)}
                </p>
                <div className="flex flex-wrap items-center gap-3">
                  <Button
                    type="button"
                    variant="destructive"
                    onClick={() => start(window.state === "closed" ? "record" : "portal")}
                  >
                    {window.state === "closed" ? "Record a portal annulment…" : "Annul this factura…"}
                  </Button>
                  {window.state !== "closed" ? (
                    <button
                      type="button"
                      className="text-sm text-muted-foreground underline underline-offset-4"
                      onClick={() => start("record")}
                    >
                      Already annulled it at the portal?
                    </button>
                  ) : null}
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      <RestOfPage invoice={invoice} />

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{step === "portal" ? "1 of 2 · Annul it at the SRI" : "2 of 2 · Record the annulment"}</DialogTitle>
            <DialogDescription>
              {step === "portal"
                ? `Sign in to SRI en línea and go to ${portalPath(invoice)}. Fill the form with these, exactly.`
                : "Record what you did at the portal. Nothing is sent to the SRI from here."}
            </DialogDescription>
          </DialogHeader>
          {step === "portal" ? (
            <div className="divide-y">
              <CopyRow label="Clave de acceso" value={invoice.accessKey} />
              {invoice.status === "authorized" ? (
                <CopyRow label="Fecha de autorización" value={formatMoment(invoice.authorizedAt)} mono={false} />
              ) : null}
              <CopyRow
                label="Identificación del receptor"
                value={invoice.recipientTaxIdXml}
                whitespace
                hint="Exactly as in the XML, spaces included."
              />
              <CopyRow label="Correo del receptor" value={invoice.recipientEmail} />
              <div className="pt-3">
                <a href={PORTAL_URL} target="_blank" rel="noreferrer" className="text-sm font-medium underline underline-offset-4">
                  Open SRI en línea ↗
                </a>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              {confirm.late ? (
                <Alert variant="warning">
                  <AlertTitle>Past the SRI&apos;s deadline</AlertTitle>
                  <AlertDescription>{windowSentence(window)}</AlertDescription>
                </Alert>
              ) : null}
              <ConfirmFields invoice={invoice} confirm={confirm} />
              <p className="text-xs text-muted-foreground">{noticeLine(invoice, false)}</p>
            </div>
          )}
          <DialogFooter>
            {step === "portal" ? (
              <>
                <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                  Not now
                </Button>
                <Button type="button" onClick={() => setStep("record")}>
                  The portal accepted it →
                </Button>
              </>
            ) : (
              <>
                <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                  Cancel
                </Button>
                <Button
                  type="button"
                  variant="destructive"
                  disabled={!confirm.ready}
                  onClick={() => {
                    setOpen(false);
                    onAnnul({ note: confirm.note, lateAcknowledged: confirm.late });
                  }}
                >
                  Mark annulled
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

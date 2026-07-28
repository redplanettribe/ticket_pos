"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Breadcrumb,
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
  FormField,
  Input,
  PageHeader,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import { formatEventStartDate, formatPriceCents, parsePriceToCents } from "@/lib/events-api";
import {
  type OperatorSaleLookup,
  fetchOperatorSale,
  reverseOperatorSale,
} from "@/lib/operator-api";

/**
 * The Platform Operator's view of one Ticket Sale, found by the Sale
 * Confirmation reference a support thread quoted (#124).
 *
 * Everything here exists to answer one question before anybody acts: is this
 * the sale we are talking about? Hence the Organization and the Event, which
 * the reference alone hides, the buyer as the sale snapshotted them, and the
 * money split the way the platform recorded it.
 *
 * It also carries the one action an operator has on a sale: the Operator
 * Reversal (#125), recording a refund they already made off-platform. That
 * marking never calls the payment provider and cannot be undone, so it is
 * stated plainly and confirmed before it commits.
 */

/** The longest note the API accepts on an Operator Reversal. */
const REVERSAL_NOTE_MAX_LENGTH = 500;

/** One label/value row of the detail. */
function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="font-medium">{children}</div>
    </div>
  );
}

function formatInstant(value: string | null): string {
  if (!value) {
    return "—";
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

/**
 * How a Sale Reversal came about, in the words the glossary uses. `customer` is
 * the buyer's own undo within the Reversal Window; `staff` is a Sale Import
 * undo. Anything else is shown verbatim rather than guessed at.
 */
function reversalActorLabel(reversedBy: string): string {
  switch (reversedBy) {
    case "customer":
      return "the buyer";
    case "staff":
      return "organization staff (sale import undo)";
    case "operator":
      return "the platform (refunded off-platform)";
    default:
      return reversedBy;
  }
}

export function OperatorSaleClient({ confirmationRef }: { confirmationRef: string }) {
  const [lookup, setLookup] = useState<OperatorSaleLookup | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFoundRef, setNotFoundRef] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The marking's form. Both money facts start empty on purpose: a pre-filled
  // refund invites rubber-stamping the number the platform collected rather
  // than stating what the buyer actually got, and a defaulted fee decision
  // would record a revenue choice nobody made.
  const [refunded, setRefunded] = useState("");
  const [feeKept, setFeeKept] = useState<"" | "kept" | "returned">("");
  const [note, setNote] = useState("");
  const [refundedError, setRefundedError] = useState<string | null>(null);
  const [feeKeptError, setFeeKeptError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // Set only while the operator is confirming; it carries the parsed cents so
  // the dialog can restate exactly what is about to be recorded.
  const [pendingRefundCents, setPendingRefundCents] = useState<number | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotFoundRef(false);
    try {
      setLookup(await fetchOperatorSale(confirmationRef));
    } catch (loadError) {
      const message = loadError instanceof Error ? loadError.message : "Failed to load sale";
      // A reference nothing carries is the ordinary outcome of a typo, not a
      // failure worth an alarming red box.
      const code =
        loadError && typeof loadError === "object" && "code" in loadError
          ? (loadError as { code?: string }).code
          : undefined;
      if (code === "TICKET_SALE_NOT_FOUND") {
        setNotFoundRef(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  }, [confirmationRef]);

  useEffect(() => {
    void load();
  }, [load]);

  async function submitReversal(refundedAmountCents: number, keptFee: boolean) {
    setSubmitting(true);
    try {
      await reverseOperatorSale(confirmationRef, {
        refunded_amount_cents: refundedAmountCents,
        platform_fee_kept: keptFee,
        ...(note.trim() ? { note: note.trim() } : {}),
      });
      setPendingRefundCents(null);
      toast.success("Sale reversed");
      // Re-read rather than patch locally: the status, the provenance and the
      // memo are all the server's account of what just happened.
      await load();
    } catch (submitError) {
      toast.error(submitError instanceof Error ? submitError.message : "Failed to reverse the sale");
    } finally {
      setSubmitting(false);
    }
  }

  // The form never submits straight through. The marking cannot be undone —
  // released capacity can be resold within seconds and the void email cannot be
  // unsent — so it always goes past the dialog that restates the consequences.
  function handleReverseSubmit(event: React.FormEvent) {
    event.preventDefault();
    const refundedAmountCents = parsePriceToCents(refunded);
    const amountInvalid = refundedAmountCents === null || refundedAmountCents <= 0;
    setRefundedError(amountInvalid ? "Enter the amount the buyer got back." : null);
    setFeeKeptError(feeKept === "" ? "Say whether the platform kept its fee." : null);
    if (amountInvalid || feeKept === "" || refundedAmountCents === null) {
      return;
    }
    setPendingRefundCents(refundedAmountCents);
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading sale...</p>;
  }

  if (notFoundRef) {
    return (
      <div className="space-y-6">
        <Breadcrumb items={[{ label: "Operator", href: "/operator" }, { label: confirmationRef }]} />
        <Card>
          <CardHeader>
            <CardTitle>No sale with that reference</CardTitle>
            <CardDescription>
              Nothing on the platform carries the sale confirmation reference{" "}
              <span className="font-mono">{confirmationRef}</span>. Check it against the
              confirmation email — the reference looks like <span className="font-mono">TP-…</span>.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Link href="/operator" className="text-sm underline">
              Back to the operator dashboard
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error || !lookup) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the sale</AlertTitle>
        <AlertDescription>{error ?? "Sale not found."}</AlertDescription>
      </Alert>
    );
  }

  const { sale, organization } = lookup;
  const currency = sale.currency;
  const reversed = sale.status === "reversed";
  // The marking is offered on an active Online Sale and on nothing else: money
  // for any other Sales Channel never passed through the platform, so there is
  // nothing here for an operator to assert about it. The Reversal Window is
  // deliberately absent from this condition — being past it is the reason the
  // action exists, and being inside it never blocks it.
  const markable = !reversed && sale.channel === "online";
  const memo = sale.operator_reversal;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[{ label: "Operator", href: "/operator" }, { label: sale.confirmation_ref }]}
      />

      <PageHeader
        title={sale.confirmation_ref}
        description={`${sale.event.name} · ${organization.name}`}
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle>Sale</CardTitle>
            <CardDescription>
              {sale.ticket_count} {sale.ticket_count === 1 ? "ticket" : "tickets"} ·{" "}
              {sale.ticket_types.map((line) => `${line.quantity} × ${line.ticket_type_name}`).join(", ")}
            </CardDescription>
          </div>
          <Badge variant={reversed ? "destructive" : "success"} className="capitalize">
            {sale.status}
          </Badge>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-3">
          <Fact label="Sold">{formatInstant(sale.sold_at)}</Fact>
          <Fact label="Channel">
            <span className="capitalize">{sale.channel.replace("_", " ")}</span>
            {sale.source ? (
              <span className="text-muted-foreground"> · {sale.source.replace("_", " ")}</span>
            ) : null}
          </Fact>
          <Fact label="Payment method">{sale.payment_method ?? "—"}</Fact>
          {reversed ? (
            <>
              <Fact label="Reversed">{formatInstant(sale.reversed_at)}</Fact>
              <Fact label="Reversed by">
                {sale.reversed_by ? reversalActorLabel(sale.reversed_by) : "Not recorded"}
              </Fact>
            </>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Reversal window</CardTitle>
          <CardDescription>
            The period in which the buyer could undo this sale themselves: until 20:00 Ecuador time
            on the day of purchase, or the event&apos;s start, whichever comes first.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Fact label="Status">
            {sale.reversal_window_passed ? (
              <Badge variant="warning">Passed</Badge>
            ) : (
              <Badge variant="outline">Still open</Badge>
            )}
          </Fact>
          <Fact label="Closes">
            {sale.reversal_window_closes_at
              ? formatInstant(sale.reversal_window_closes_at)
              : "This sale never had a reversal window"}
          </Fact>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Amounts</CardTitle>
          <CardDescription>
            What the buyer paid and how it was split, as this sale snapshotted it at the time.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-4">
          <Fact label="Collected">
            <span className="tabular-nums">{formatPriceCents(sale.amount_cents, currency)}</span>
          </Fact>
          <Fact label="Platform fee">
            <span className="tabular-nums">
              {formatPriceCents(sale.platform_fee_cents, currency)}
            </span>
          </Fact>
          <Fact label="Fee IVA">
            <span className="tabular-nums">{formatPriceCents(sale.fee_iva_cents, currency)}</span>
          </Fact>
          <Fact label="Net proceeds">
            <span className="tabular-nums">
              {formatPriceCents(sale.net_proceeds_cents, currency)}
            </span>
          </Fact>
        </CardContent>
      </Card>

      {memo ? (
        <Card>
          <CardHeader>
            <CardTitle>Out-of-band refund</CardTitle>
            <CardDescription>
              What the operator asserted when they recorded this reversal. Visible to platform
              operators only — the organization sees the sale as reversed by the platform and
              nothing of this.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-6 sm:grid-cols-3">
            <Fact label="Refunded to the buyer">
              <span className="tabular-nums">
                {memo.refunded_amount_cents === null
                  ? "Nothing to refund"
                  : formatPriceCents(memo.refunded_amount_cents, currency)}
              </span>
            </Fact>
            <Fact label="Platform fee">
              {memo.platform_fee_kept === null
                ? "—"
                : memo.platform_fee_kept
                  ? "Kept by the platform"
                  : "Returned"}
            </Fact>
            <Fact label="Recorded by">{memo.operator}</Fact>
            {memo.note ? (
              <div className="sm:col-span-3">
                <Fact label="Note">
                  <span className="whitespace-pre-wrap font-normal">{memo.note}</span>
                </Fact>
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {markable ? (
        <Card>
          <CardHeader>
            <CardTitle>Record an out-of-band refund</CardTitle>
            <CardDescription>
              Use this after you have already refunded the buyer yourself — in the payment
              provider&apos;s dashboard, or by bank transfer. It records what happened: no payment
              provider is called from here, and nothing is refunded by submitting this form.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="grid gap-4 sm:grid-cols-2" onSubmit={handleReverseSubmit}>
              <FormField
                id="reversal-refunded"
                label={`Refunded to the buyer (${currency})`}
                error={refundedError}
              >
                <Input
                  value={refunded}
                  onChange={(event) => setRefunded(event.target.value)}
                  inputMode="decimal"
                  placeholder="0.00"
                  disabled={submitting}
                />
                <p className="mt-1 text-xs text-muted-foreground">
                  What actually left our account. This sale collected{" "}
                  {formatPriceCents(sale.amount_cents, currency)}, which is the most it can be.
                </p>
              </FormField>
              <FormField id="reversal-fee-kept" label="Platform fee" error={feeKeptError}>
                <div className="flex flex-col gap-2 pt-1 text-sm">
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="platform-fee-kept"
                      value="kept"
                      checked={feeKept === "kept"}
                      onChange={() => setFeeKept("kept")}
                      disabled={submitting}
                    />
                    Kept — the platform keeps{" "}
                    {formatPriceCents(sale.platform_fee_cents + sale.fee_iva_cents, currency)} (fee
                    and fee IVA)
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="platform-fee-kept"
                      value="returned"
                      checked={feeKept === "returned"}
                      onChange={() => setFeeKept("returned")}
                      disabled={submitting}
                    />
                    Returned — the platform keeps nothing on this sale
                  </label>
                </div>
              </FormField>
              <div className="sm:col-span-2">
                <FormField id="reversal-note" label="Note (optional)">
                  <Textarea
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    maxLength={REVERSAL_NOTE_MAX_LENGTH}
                    rows={2}
                    placeholder="Refunded via the PayPhone dashboard; bank transfer, fee waived as goodwill; ..."
                    disabled={submitting}
                  />
                </FormField>
              </div>
              <div className="sm:col-span-2">
                <Button type="submit" variant="destructive" disabled={submitting}>
                  {submitting ? "Reversing..." : "Reverse this sale"}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Buyer</CardTitle>
          <CardDescription>The customer as this sale recorded them.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Fact label="Name">
            {`${sale.customer.first_name} ${sale.customer.last_name}`.trim() || "—"}
          </Fact>
          <Fact label="Email">{sale.customer.email}</Fact>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Event and organization</CardTitle>
          <CardDescription>Whose sale this is, and what it is for.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-3">
          <Fact label="Event">{sale.event.name}</Fact>
          <Fact label="Starts">
            {formatEventStartDate(sale.event.starts_at, sale.event.timezone)}
          </Fact>
          <Fact label="Organization">
            <Link href={`/operator/organizations/${organization.id}`} className="hover:underline">
              {organization.name}
            </Link>
          </Fact>
        </CardContent>
      </Card>

      <Dialog
        open={pendingRefundCents !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingRefundCents(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reverse {sale.confirmation_ref}?</DialogTitle>
            <DialogDescription>
              This cannot be undone. There is no un-reversal: the tickets go back on sale
              immediately and the void email cannot be unsent.
            </DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            <li>
              {sale.ticket_count} {sale.ticket_count === 1 ? "ticket returns" : "tickets return"} to{" "}
              {sale.event.name}, on sale again at once.
            </li>
            <li>
              {organization.name}&apos;s withdrawable balance drops by{" "}
              {formatPriceCents(sale.net_proceeds_cents, currency)}, going negative if this sale was
              already paid out.
            </li>
            <li>
              {sale.customer.email} is emailed that their tickets are no longer valid, quoting{" "}
              {sale.confirmation_ref}.
            </li>
            <li>
              You are recording that {formatPriceCents(pendingRefundCents ?? 0, currency)} already
              went back to the buyer, and that the platform{" "}
              {feeKept === "kept" ? "keeps" : "returns"} its fee. No payment provider is called.
            </li>
          </ul>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPendingRefundCents(null)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => {
                if (pendingRefundCents !== null && feeKept !== "") {
                  void submitReversal(pendingRefundCents, feeKept === "kept");
                }
              }}
              disabled={submitting}
            >
              {submitting ? "Reversing..." : "Reverse the sale"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

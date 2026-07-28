"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Breadcrumb,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  PageHeader,
} from "@ticket-pos/ui";

import { formatEventStartDate, formatPriceCents } from "@/lib/events-api";
import { type OperatorSaleLookup, fetchOperatorSale } from "@/lib/operator-api";

/**
 * The Platform Operator's read-only view of one Ticket Sale, found by the Sale
 * Confirmation reference a support thread quoted (#124).
 *
 * Everything here exists to answer one question before anybody acts: is this
 * the sale we are talking about? Hence the Organization and the Event, which
 * the reference alone hides, the buyer as the sale snapshotted them, and the
 * money split the way the platform recorded it.
 *
 * Read-only for now. The Operator Reversal (#123) is the action this page will
 * carry, and until it exists this page changes nothing.
 */

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
    default:
      return reversedBy;
  }
}

export function OperatorSaleClient({ confirmationRef }: { confirmationRef: string }) {
  const [lookup, setLookup] = useState<OperatorSaleLookup | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFoundRef, setNotFoundRef] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    </div>
  );
}

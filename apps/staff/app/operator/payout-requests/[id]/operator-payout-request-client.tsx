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
  cn,
} from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import {
  type OperatorPayoutRequestDetail,
  fetchOperatorPayoutRequest,
} from "@/lib/operator-api";
import { payoutRequestStatusLabel, waitingLabel } from "@/lib/payout-requests";

// One payout request, with everything needed to execute the transfer (#176,
// ADR 0026).
//
// This is the ONE page on the platform that shows a whole bank account number,
// and it shows exactly one: an operator who opened it is about to retype that
// number into a banking app. Everything else — the queue, the organization's
// detail page — masks it.
//
// The two payable balances are the reason the page exists rather than a tooltip
// on the queue. The figure on the request is what the organization could have
// asked for when it asked; the figure beside it is what it could ask for now.
// The cap was checked once, at request time, and deliberately never again: the
// operator standing at the bank is the party who decides what to do about a
// number that has moved, and this page's whole job is to put both in front of
// them.

/** One labelled fact from the snapshot. Definition lists read better than a table for six fields. */
function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className={cn("font-medium", mono && "font-mono")}>{value}</p>
    </div>
  );
}

const ACCOUNT_TYPE_LABELS: Record<string, string> = {
  ahorros: "Ahorros (savings)",
  corriente: "Corriente (current)",
};

export function OperatorPayoutRequestClient({ requestId }: { requestId: string }) {
  const [detail, setDetail] = useState<OperatorPayoutRequestDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setDetail(await fetchOperatorPayoutRequest(requestId));
      setForbidden(false);
    } catch (loadError) {
      const message = loadError instanceof Error ? loadError.message : "Failed to load payout request";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  }, [requestId]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading payout request...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>This account is not a platform operator.</AlertDescription>
      </Alert>
    );
  }

  if (error || !detail) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load payout request</AlertTitle>
        <AlertDescription>{error ?? "Payout request not found."}</AlertDescription>
      </Alert>
    );
  }

  const { request, organization } = detail;
  const currency = organization.currency;
  const profile = request.payout_profile;
  const snapshotPayable = request.payable_balance_cents;
  const livePayable = detail.payable_balance_cents;
  const outstanding = request.status === "pending";

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: "Operator", href: "/operator" },
          { label: "Payout requests", href: "/operator/payout-requests" },
          { label: organization.name },
        ]}
      />

      <PageHeader
        title={`${formatPriceCents(request.amount_cents, currency)} to ${organization.name}`}
        description={
          outstanding
            ? `Asked for by ${request.requested_by} · waiting ${waitingLabel(request.requested_at).toLowerCase()}`
            : `Asked for by ${request.requested_by} on ${new Date(request.requested_at).toLocaleDateString()}`
        }
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle>The ask</CardTitle>
            <CardDescription>
              What this organization asked for, and what it could have asked for at the time.
            </CardDescription>
          </div>
          <Badge variant={outstanding ? "default" : "secondary"} className="w-fit">
            {payoutRequestStatusLabel(request.status)}
          </Badge>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-6 sm:grid-cols-3">
            <div>
              <p className="text-sm text-muted-foreground">Requested</p>
              <p className="text-2xl font-semibold tabular-nums">
                {formatPriceCents(request.amount_cents, currency)}
              </p>
            </div>
            {/*
              The pair the whole screen is for. The snapshot cannot move; the
              live figure can and does, and the difference between them is the
              judgement the cap deliberately leaves to the operator (ADR 0026).
            */}
            <div>
              <p className="text-sm text-muted-foreground">Payable when asked</p>
              <p
                className={cn(
                  "text-2xl font-semibold tabular-nums",
                  snapshotPayable < 0 && "text-destructive",
                )}
              >
                {formatPriceCents(snapshotPayable, currency)}
              </p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Payable now</p>
              <p
                className={cn("text-2xl font-semibold tabular-nums", livePayable < 0 && "text-destructive")}
              >
                {formatPriceCents(livePayable, currency)}
              </p>
            </div>
          </div>

          <p className="text-sm text-muted-foreground">
            {request.amount_cents > livePayable
              ? `More than this organization can ask for today (${formatPriceCents(livePayable, currency)} has cleared). The request was within its balance when it was made and nothing re-checks it — paying it is your call.`
              : "Within what has cleared today. Withdrawable balance in full: " +
                formatPriceCents(detail.withdrawable_balance_cents, currency) +
                "."}
          </p>

          {request.note ? (
            <div>
              <p className="text-sm text-muted-foreground">Note from the organizer</p>
              <p>{request.note}</p>
            </div>
          ) : null}

          {/* A decline always carries its reason, and the asker is shown it. */}
          {request.decline_reason ? (
            <p className="text-sm text-destructive">Declined: {request.decline_reason}</p>
          ) : null}
          {request.resolved_by ? (
            <p className="text-sm text-muted-foreground">
              Answered by {request.resolved_by}
              {request.resolved_at ? ` on ${new Date(request.resolved_at).toLocaleDateString()}` : ""}.
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Where to pay it</CardTitle>
          <CardDescription>
            The bank details as this request recorded them. A later change to the organization&apos;s payout
            profile does not touch them — this is the account the platform was told to pay.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Detail label="Bank" value={profile.bank_name} />
          <Detail
            label="Account type"
            value={ACCOUNT_TYPE_LABELS[profile.account_type] ?? profile.account_type}
          />
          <Detail label="Account number" value={profile.account_number} mono />
          <Detail label="Account holder" value={profile.account_holder_name} />
          <Detail
            label={profile.tax_id_type === "ruc" ? "RUC" : "Cédula"}
            value={profile.tax_id_number}
            mono
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Organization</CardTitle>
          <CardDescription>Who is being paid, and everything else about their money.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Link
            href={`/operator/organizations/${organization.id}`}
            className="font-medium hover:underline"
          >
            {organization.name}
          </Link>
          <p className="text-sm text-muted-foreground">
            {organization.slug} · {currency} · withdrawable balance{" "}
            <span className={cn("tabular-nums", detail.withdrawable_balance_cents < 0 && "text-destructive")}>
              {formatPriceCents(detail.withdrawable_balance_cents, currency)}
            </span>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

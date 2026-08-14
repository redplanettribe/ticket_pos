"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
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
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, parsePriceToCents } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime, formatMoney, formatNumber } from "@/lib/format";
import {
  type OperatorSaleLookup,
  fetchOperatorSale,
  reverseOperatorSale,
} from "@/lib/operator-api";
import {
  type ReversalActor,
  type SaleChannel,
  type SaleSource,
  paymentMethodToken,
  saleChannelToken,
  saleSourceToken,
} from "@/lib/sales-api";

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
 *
 * How much of that form there is depends on the sale. One that collected
 * nothing has no money to state and refuses both money facts, so it is offered
 * a note and nothing else (#126).
 *
 * WHAT IT DOES NOT SAY IN ITS OWN WORDS: the Sales Channel, its source, the
 * Payment Method and who reversed a sale. Every one of those is vocabulary the
 * Event's own Sales tab already coined, read here from the `sales` namespace
 * through the same token narrowing lib/sales-api.ts gives that screen — because
 * an organizer and an operator discussing one sale over the phone must be using
 * one word for its channel (ADR 0041).
 */

/** The longest note the API accepts on an Operator Reversal. */
const REVERSAL_NOTE_MAX_LENGTH = 500;

/** Rendered where a fact has nothing to show. Punctuation, in every language. */
const NOTHING = "—";

/** The `sales` catalog keys for everything lib/sales-api.ts narrows. */
const CHANNEL_KEYS = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
} as const satisfies Record<SaleChannel, string>;

const SOURCE_KEYS = {
  direct: "sourceDirect",
  external_platform: "sourceExternalPlatform",
} as const satisfies Record<SaleSource, string>;

const PAYMENT_METHOD_KEYS = {
  cash: "paymentCash",
  transfer: "paymentTransfer",
  payphone: "paymentPayphone",
} as const;

/**
 * How a Sale Reversal came about, in the words the Event's Sales tab uses.
 * `customer` is the buyer's own undo within the Reversal Window; `staff` is a
 * Sale Import undo; `operator` is this very page. Anything else is shown
 * verbatim rather than guessed at.
 */
const REVERSAL_ACTOR_KEYS = {
  customer: "actorCustomer",
  staff: "actorStaff",
  operator: "actorOperator",
} as const satisfies Record<ReversalActor, string>;

/**
 * A marking the operator has stated and is being asked to confirm.
 *
 * Both money facts are null together on a free sale — absent, not zero, which
 * is the distinction the record keeps — and both are set together on a paid
 * one, because an amount with no fee decision says nothing about what the
 * platform kept.
 */
type PendingReversal = {
  refundedAmountCents: number | null;
  platformFeeKept: boolean | null;
};

/** One label/value row of the detail. */
function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="font-medium">{children}</div>
    </div>
  );
}

export function OperatorSaleClient({ confirmationRef }: { confirmationRef: string }) {
  const t = useTranslations("operator");
  const tSales = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [lookup, setLookup] = useState<OperatorSaleLookup | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFoundRef, setNotFoundRef] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The marking's form. Both money facts start empty on purpose: a pre-filled
  // refund invites rubber-stamping the number the platform collected rather
  // than stating what the buyer actually got, and a defaulted fee decision
  // would record a revenue choice nobody made. On a free sale neither field is
  // shown at all, and both stay empty.
  const [refunded, setRefunded] = useState("");
  const [feeKept, setFeeKept] = useState<"" | "kept" | "returned">("");
  const [note, setNote] = useState("");
  const [refundedError, setRefundedError] = useState<string | null>(null);
  const [feeKeptError, setFeeKeptError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // Set only while the operator is confirming; it carries the marking exactly
  // as it will be sent, so the dialog restates what is about to be recorded.
  const [pending, setPending] = useState<PendingReversal | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotFoundRef(false);
    try {
      setLookup(await fetchOperatorSale(confirmationRef));
    } catch (loadError) {
      // A reference nothing carries is the ordinary outcome of a typo, not a
      // failure worth an alarming red box — so it gets its own card below and
      // never comes through the error copy at all.
      if (loadError instanceof ApiError && loadError.code === "TICKET_SALE_NOT_FOUND") {
        setNotFoundRef(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("saleNotFound"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [confirmationRef]);

  useEffect(() => {
    void load();
  }, [load]);

  async function submitReversal(marking: PendingReversal) {
    setSubmitting(true);
    try {
      // The money facts are OMITTED on a free sale rather than sent as zeros:
      // the API refuses either of them there, and the record must keep "nothing
      // to refund" apart from "zero refunded".
      await reverseOperatorSale(confirmationRef, {
        ...(marking.refundedAmountCents !== null && marking.platformFeeKept !== null
          ? {
              refunded_amount_cents: marking.refundedAmountCents,
              platform_fee_kept: marking.platformFeeKept,
            }
          : {}),
        ...(note.trim() ? { note: note.trim() } : {}),
      });
      setPending(null);
      toast.success(t("saleReversed"));
      // Re-read rather than patch locally: the status, the provenance and the
      // memo are all the server's account of what just happened.
      await load();
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("reverseFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  // The form never submits straight through. The marking cannot be undone —
  // released capacity can be resold within seconds and the void email cannot be
  // unsent — so it always goes past the dialog that restates the consequences.
  function handleReverseSubmit(event: React.FormEvent) {
    event.preventDefault();
    // A sale that collected nothing states no money at all, so there is nothing
    // here to validate: the marking is the note and the operator's identity.
    if (lookup && lookup.sale.amount_cents === 0) {
      setPending({ refundedAmountCents: null, platformFeeKept: null });
      return;
    }
    const refundedAmountCents = parsePriceToCents(refunded);
    const amountInvalid = refundedAmountCents === null || refundedAmountCents <= 0;
    setRefundedError(amountInvalid ? t("refundedRequired") : null);
    setFeeKeptError(feeKept === "" ? t("feeKeptRequired") : null);
    if (amountInvalid || feeKept === "" || refundedAmountCents === null) {
      return;
    }
    setPending({ refundedAmountCents, platformFeeKept: feeKept === "kept" });
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("saleLoading")}</p>;
  }

  if (notFoundRef) {
    return (
      <div className="space-y-6">
        <Breadcrumb
          items={[
            { label: t("breadcrumbFindSale"), href: "/operator/sales" },
            { label: confirmationRef },
          ]}
        />
        <Card>
          <CardHeader>
            <CardTitle>{t("saleNotFoundTitle")}</CardTitle>
            <CardDescription>
              {t.rich("saleNotFoundBody", {
                reference: confirmationRef,
                ref: (chunks) => <span className="font-mono">{chunks}</span>,
              })}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Link href="/operator/sales" className="text-sm underline">
              {t("backToSaleLookup")}
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error || !lookup) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("saleLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("saleNotFound")}</AlertDescription>
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
  // A sale that collected nothing: there was no refund to make and no platform
  // fee to keep, so the marking takes neither money fact and the form does not
  // ask for them (#126). It is the amount COLLECTED that decides this, not the
  // payment method — that is the fact the API judges the marking against.
  const free = sale.amount_cents === 0;
  const memo = sale.operator_reversal;
  // The sale's own currency, and the platform's clock for every moment on this
  // page: a sale's timestamps and its Reversal Window cutoff are Ecuadorian
  // facts (ADR 0018), and only the Event's schedule belongs to the Event's zone.
  const money = (cents: number) => formatMoney(cents, currency, locale);
  const moment = (value: string | null) =>
    formatDateTime(value, PLATFORM_TIME_ZONE, locale) ?? NOTHING;
  const channelToken = saleChannelToken(sale.channel);
  const sourceToken = saleSourceToken(sale.source);
  const paymentToken = paymentMethodToken(sale.payment_method);
  const actorToken = sale.reversed_by
    ? (REVERSAL_ACTOR_KEYS[sale.reversed_by as ReversalActor] ?? null)
    : null;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbFindSale"), href: "/operator/sales" },
          { label: sale.confirmation_ref },
        ]}
      />

      {/* The reference, the Event's name and the Organization's are all data. */}
      <PageHeader
        title={sale.confirmation_ref}
        description={t("saleHeaderDescription", {
          event: sale.event.name,
          organization: organization.name,
        })}
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle>{t("saleTitle")}</CardTitle>
            <CardDescription>
              {t("saleTicketCount", {
                count: sale.ticket_count,
                lines: sale.ticket_types
                  .map((line) =>
                    t("saleTicketLine", {
                      quantity: formatNumber(line.quantity, locale),
                      name: line.ticket_type_name,
                    }),
                  )
                  .join(", "),
              })}
            </CardDescription>
          </div>
          <Badge variant={reversed ? "destructive" : "success"}>
            {reversed ? t("saleStatusReversed") : t("saleStatusActive")}
          </Badge>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-3">
          <Fact label={t("factSold")}>{moment(sale.sold_at)}</Fact>
          <Fact label={t("factChannel")}>
            {sourceToken || sale.source
              ? tSales("channelWithSource", {
                  channel: channelToken ? tSales(CHANNEL_KEYS[channelToken]) : sale.channel,
                  source: sourceToken ? tSales(SOURCE_KEYS[sourceToken]) : (sale.source ?? ""),
                })
              : channelToken
                ? tSales(CHANNEL_KEYS[channelToken])
                : sale.channel}
          </Fact>
          <Fact label={t("factPaymentMethod")}>
            {paymentToken
              ? tSales(PAYMENT_METHOD_KEYS[paymentToken])
              : (sale.payment_method ?? NOTHING)}
          </Fact>
          {reversed ? (
            <>
              <Fact label={t("factReversed")}>{moment(sale.reversed_at)}</Fact>
              <Fact label={t("factReversedBy")}>
                {actorToken
                  ? tSales(actorToken)
                  : (sale.reversed_by ?? t("reversedByNotRecorded"))}
              </Fact>
            </>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("reversalWindowTitle")}</CardTitle>
          <CardDescription>{t("reversalWindowDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Fact label={t("colStatus")}>
            {sale.reversal_window_passed ? (
              <Badge variant="warning">{t("reversalWindowPassed")}</Badge>
            ) : (
              <Badge variant="outline">{t("reversalWindowOpen")}</Badge>
            )}
          </Fact>
          <Fact label={t("reversalWindowCloses")}>
            {sale.reversal_window_closes_at
              ? moment(sale.reversal_window_closes_at)
              : t("reversalWindowNever")}
          </Fact>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("amountsTitle")}</CardTitle>
          <CardDescription>{t("amountsDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-4">
          <Fact label={t("factCollected")}>
            <span className="tabular-nums">{money(sale.amount_cents)}</span>
          </Fact>
          <Fact label={t("factPlatformFee")}>
            <span className="tabular-nums">{money(sale.platform_fee_cents)}</span>
          </Fact>
          <Fact label={t("factFeeIva")}>
            <span className="tabular-nums">{money(sale.fee_iva_cents)}</span>
          </Fact>
          <Fact label={t("factNetProceeds")}>
            <span className="tabular-nums">{money(sale.net_proceeds_cents)}</span>
          </Fact>
        </CardContent>
      </Card>

      {memo ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("memoTitle")}</CardTitle>
            <CardDescription>{t("memoDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-6 sm:grid-cols-3">
            <Fact label={t("memoRefunded")}>
              <span className="tabular-nums">
                {memo.refunded_amount_cents === null
                  ? t("memoNothingToRefund")
                  : money(memo.refunded_amount_cents)}
              </span>
            </Fact>
            <Fact label={t("factPlatformFee")}>
              {memo.platform_fee_kept === null
                ? NOTHING
                : memo.platform_fee_kept
                  ? t("memoFeeKept")
                  : t("memoFeeReturned")}
            </Fact>
            <Fact label={t("memoRecordedBy")}>{memo.operator}</Fact>
            {memo.note ? (
              <div className="sm:col-span-3">
                <Fact label={t("memoNote")}>
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
            <CardTitle>{free ? t("reverseTitleFree") : t("reverseTitlePaid")}</CardTitle>
            <CardDescription>
              {free ? t("reverseDescriptionFree") : t("reverseDescriptionPaid")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="grid gap-4 sm:grid-cols-2" onSubmit={handleReverseSubmit}>
              {free ? null : (
                <>
                  <FormField
                    id="reversal-refunded"
                    label={t("refundedLabel", { currency })}
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
                      {t("refundedHint", { amount: money(sale.amount_cents) })}
                    </p>
                  </FormField>
                  <FormField
                    id="reversal-fee-kept"
                    label={t("factPlatformFee")}
                    error={feeKeptError}
                  >
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
                        {t("feeKeptOption", {
                          amount: money(sale.platform_fee_cents + sale.fee_iva_cents),
                        })}
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
                        {t("feeReturnedOption")}
                      </label>
                    </div>
                  </FormField>
                </>
              )}
              <div className="sm:col-span-2">
                <FormField id="reversal-note" label={t("noteLabel")}>
                  <Textarea
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    maxLength={REVERSAL_NOTE_MAX_LENGTH}
                    rows={2}
                    placeholder={t("reversalNotePlaceholder")}
                    disabled={submitting}
                  />
                </FormField>
              </div>
              <div className="sm:col-span-2">
                <Button type="submit" variant="destructive" disabled={submitting}>
                  {submitting ? t("reversing") : t("reverseSubmit")}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{t("buyerTitle")}</CardTitle>
          <CardDescription>{t("buyerDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          {/* A Customer's name and address are data, never copy. */}
          <Fact label={t("buyerName")}>
            {`${sale.customer.first_name} ${sale.customer.last_name}`.trim() || NOTHING}
          </Fact>
          <Fact label={t("buyerEmail")}>{sale.customer.email}</Fact>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("eventAndOrganizationTitle")}</CardTitle>
          <CardDescription>{t("eventAndOrganizationDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-3">
          <Fact label={t("factEvent")}>{sale.event.name}</Fact>
          {/*
            THE ONE MOMENT ON THIS PAGE THAT IS NOT THE PLATFORM'S: an Event's
            schedule is drawn in the EVENT's own timezone, whichever language
            the operator reads in (ADR 0041). An Event with no zone recorded
            falls back to the platform's clock rather than to the machine's.
          */}
          <Fact label={t("factStarts")}>
            {formatDateTime(
              sale.event.starts_at,
              sale.event.timezone ?? PLATFORM_TIME_ZONE,
              locale,
            ) ?? NOTHING}
          </Fact>
          <Fact label={t("factOrganization")}>
            <Link href={`/operator/organizations/${organization.id}`} className="hover:underline">
              {organization.name}
            </Link>
          </Fact>
        </CardContent>
      </Card>

      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPending(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reverseDialogTitle", { reference: sale.confirmation_ref })}</DialogTitle>
            <DialogDescription>{t("reverseDialogBody")}</DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            <li>
              {t("reverseDialogTickets", {
                count: sale.ticket_count,
                event: sale.event.name,
              })}
            </li>
            {free ? null : (
              <li>
                {t("reverseDialogBalance", {
                  organization: organization.name,
                  amount: money(sale.net_proceeds_cents),
                })}
              </li>
            )}
            <li>
              {t("reverseDialogEmail", {
                email: sale.customer.email,
                reference: sale.confirmation_ref,
              })}
            </li>
            <li>
              {pending?.refundedAmountCents === null || pending?.platformFeeKept === null
                ? t("reverseDialogNoMoney")
                : pending?.platformFeeKept
                  ? t("reverseDialogMoneyKept", {
                      amount: money(pending?.refundedAmountCents ?? 0),
                    })
                  : t("reverseDialogMoneyReturned", {
                      amount: money(pending?.refundedAmountCents ?? 0),
                    })}
            </li>
          </ul>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPending(null)}
              disabled={submitting}
            >
              {t("cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => {
                if (pending !== null) {
                  void submitReversal(pending);
                }
              }}
              disabled={submitting}
            >
              {submitting ? t("reversing") : t("reverseDialogConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

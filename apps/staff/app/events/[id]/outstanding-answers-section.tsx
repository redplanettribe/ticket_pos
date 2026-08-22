"use client";

import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Skeleton,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatDate, formatNumber } from "@/lib/format";
import {
  SALES_CHANNEL_KEYS,
  buyerName,
  fetchOutstandingAnswers,
  type OutstandingAnswersPage,
  type TicketOwingAnswers,
} from "@/lib/outstanding-answers";

import { TicketAnswersDialog } from "./ticket-answers-dialog";

type OutstandingAnswersSectionProps = {
  eventId: string;
  /** The Event's timezone, so a sale date reads where the Event is. */
  timezone: string | null;
};

/**
 * The Event's Outstanding Answers: which Tickets still owe required Answers, and
 * which questions they owe (#313).
 *
 * WHAT THIS SCREEN IS. An Outstanding Answer is a debt, not a defect — the whole
 * meaning of "required" on this platform. Nothing was ever refused for want of
 * one, on any channel, so seeing the debt IS the feature: this is where an
 * Organization finds out how many sizes it still does not know before it orders
 * the shirts.
 *
 * IT HIDES NOTHING AND FILTERS NOTHING. Door sales and Sale Imports stand here
 * beside online ones and start out owing everything, because nobody ever put the
 * questions to those buyers — so every row names its Sales Channel, which is what
 * turns "this import owes six answers" from an alarm into a fact. There is no
 * filter control either: every row is something the Organization does not know,
 * and a filter over this list would only be a way of looking at fewer of them.
 *
 * NOTHING HERE DECIDES WHAT IS OUTSTANDING. The definition lives once, in the
 * API. This component reads a page of the answer and draws it.
 */
export function OutstandingAnswersSection({ eventId, timezone }: OutstandingAnswersSectionProps) {
  const t = useTranslations("outstandingAnswers");
  const sales = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [page, setPage] = useState(1);
  const [result, setResult] = useState<OutstandingAnswersPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  /*
    The Ticket Sale the Answers dialog is open on, or null.

    Held as the SALE and not the Ticket, because that is what the dialog #310
    built takes: a Ticket has no name and no face, staff go looking for Ana's
    order, and the Tickets hang off it. Opening on a row therefore opens the
    whole sale — which is the right amount, since a buyer's four tickets usually
    owe the same thing and answering them one dialog at a time would be four
    round trips through this list.
  */
  const [openSale, setOpenSale] = useState<{ id: string; ref: string } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setResult(await fetchOutstandingAnswers(eventId, page));
      setError(null);
    } catch (failure) {
      setResult(null);
      setError(
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ?? t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [eventId, page, errorCopy, t]);

  useEffect(() => {
    void load();
  }, [load]);

  /*
    Re-read when the dialog closes.

    The list is DERIVED on every read, so an Answer given in the dialog has
    already discharged its debt on the server — this is only the screen catching
    up. Re-reading on close rather than watching the dialog's writes keeps the
    two surfaces from having to agree about anything: whatever happened in there,
    including nothing, the next read is the truth.
  */
  const closeDialog = useCallback(
    (open: boolean) => {
      if (!open) {
        setOpenSale(null);
        void load();
      }
    },
    [load],
  );

  const rows = result?.data ?? [];
  const pagination = result?.pagination;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("title")}</CardTitle>
        <CardDescription>{t("description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : null}

        {!loading && !error && rows.length === 0 ? (
          /*
            The empty state is a CONGRATULATION and not a shrug. An Event with
            nothing outstanding is one where every required question has been
            answered on every live Ticket, which is the state the whole feature
            exists to reach — and it reads identically on an Event that has asked
            no required questions at all, which is correct: neither owes anything.
          */
          <p className="text-sm text-muted-foreground">{t("nothingOutstanding")}</p>
        ) : null}

        {!loading && !error && rows.length > 0 ? (
          <>
            {/* One message rather than two numbers glued together: which of the
                two counts leads the sentence, and both of their plurals, are the
                translator's to decide. */}
            <p className="text-sm text-muted-foreground">
              {t("summary", {
                answers: result?.outstanding_count ?? 0,
                tickets: pagination?.total ?? 0,
              })}
            </p>

            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colBuyer")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colTicket")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colSold")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colOwes")}</th>
                    <th className="py-2 font-medium">
                      <span className="sr-only">{t("colActions")}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((ticket) => (
                    <OutstandingRow
                      key={ticket.ticket_id}
                      ticket={ticket}
                      timezone={timezone}
                      onOpen={() =>
                        setOpenSale({ id: ticket.ticket_sale_id, ref: ticket.confirmation_ref })
                      }
                      answersLabel={sales("ticketAnswers")}
                      channelLabel={sales(SALES_CHANNEL_KEYS[ticket.channel])}
                    />
                  ))}
                </tbody>
              </table>
            </div>

            {pagination && pagination.total_pages > 1 ? (
              <div className="flex flex-col gap-3 border-t pt-4 text-sm sm:flex-row sm:items-center sm:justify-between">
                <span className="text-muted-foreground">
                  {t("pageSummary", {
                    page: formatNumber(pagination.page, locale),
                    pages: formatNumber(pagination.total_pages, locale),
                  })}
                </span>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pagination.page <= 1}
                    onClick={() => setPage(pagination.page - 1)}
                  >
                    {sales("previousPage")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pagination.page >= pagination.total_pages}
                    onClick={() => setPage(pagination.page + 1)}
                  >
                    {sales("nextPage")}
                  </Button>
                </div>
              </div>
            ) : null}
          </>
        ) : null}
      </CardContent>

      {/*
        The Answers dialog #310 built, reused exactly as it stands. It owns its
        own loading, its own writes and its own refusals — a second dialog for
        the same job would be a second idea of what an Answer may be.
      */}
      {openSale ? (
        <TicketAnswersDialog
          open
          onOpenChange={closeDialog}
          eventId={eventId}
          ticketSaleId={openSale.id}
          confirmationRef={openSale.ref}
        />
      ) : null}
    </Card>
  );
}

type OutstandingRowProps = {
  ticket: TicketOwingAnswers;
  timezone: string | null;
  onOpen: () => void;
  answersLabel: string;
  channelLabel: string;
};

/** One Ticket that owes, and the questions it owes. */
function OutstandingRow({
  ticket,
  timezone,
  onOpen,
  answersLabel,
  channelLabel,
}: OutstandingRowProps) {
  const t = useTranslations("outstandingAnswers");
  const locale = toAppLocale(useLocale());

  // The Event's zone when it is known, and the reader's own when it is not. A
  // date drawn in the wrong zone is off by at most a day; no date at all is off
  // by everything.
  const sold = timezone
    ? formatDate(ticket.sold_at, timezone, locale)
    : formatDate(ticket.sold_at, Intl.DateTimeFormat().resolvedOptions().timeZone, locale);

  return (
    <tr className="border-b align-top last:border-0">
      <td className="py-3 pr-4">
        <div className="font-medium">{buyerName(ticket)}</div>
        <div className="text-muted-foreground">{ticket.customer_email}</div>
      </td>
      <td className="py-3 pr-4">
        {/* The Ticket Type as the Organization named it, with the ordinal that
            tells two Tickets of one line apart, and the buyer's own reference,
            which is what staff on the phone match against. */}
        <div>{t("ticketHeading", { name: ticket.ticket_type_name, ordinal: ticket.ordinal })}</div>
        <div className="text-muted-foreground">{ticket.confirmation_ref}</div>
      </td>
      <td className="py-3 pr-4 whitespace-nowrap">
        <div>{sold}</div>
        {/* The channel EXPLAINS the row. A door sale or an import owing every
            question is a buyer who was never asked — there is no checkout form
            on either — and without this the row reads as lost data. */}
        <Badge variant="outline" className="mt-1">
          {channelLabel}
        </Badge>
      </td>
      <td className="py-3 pr-4">
        <ul className="space-y-1">
          {ticket.outstanding.map((question) => (
            // The label is the Organization's own words, rendered AS COINED in
            // both languages exactly as a Ticket Type name is (ADR 0027). Only
            // the chrome around it follows the reader's Staff Locale.
            <li key={question.question_id}>{question.label}</li>
          ))}
        </ul>
      </td>
      <td className="py-3">
        <Button type="button" variant="outline" size="sm" onClick={onOpen}>
          {answersLabel}
        </Button>
      </td>
    </tr>
  );
}

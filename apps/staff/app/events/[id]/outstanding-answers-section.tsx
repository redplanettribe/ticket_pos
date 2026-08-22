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
  fetchHolderList,
  holderBadgeVariant,
  holderListVisible,
  holderName,
  holderStateKey,
  questionsVisible,
  type HolderListPage,
  type HolderTicket,
} from "@/lib/outstanding-answers";

import { TicketAnswersDialog } from "./ticket-answers-dialog";

type HolderListSectionProps = {
  eventId: string;
  /** The Event's timezone, so a sale date reads where the Event is. */
  timezone: string | null;
};

/**
 * The Event's Holder List (#333; the Outstanding Answers screen of #313,
 * widened to the roster): every Ticket of the Event, who is coming on each,
 * and — where the Event asks Ticket Questions — what each still owes.
 *
 * WHAT THIS SCREEN IS. The Organization's answer to "who is coming": a roster,
 * one row per Ticket of every live sale. A fully answered Ticket stays on it
 * and an Event that asks nothing still has one, because the roster is the
 * point and the questions are a column on it. OUTSTANDING ANSWERS IS THE ONE
 * FILTER this list offers — the debt view it used to be — never its
 * definition.
 *
 * IT HIDES NOTHING. Door sales and Sale Imports stand here beside online ones
 * and start out owing everything, because nobody ever put the questions to
 * those buyers — so every row names its Sales Channel, which is what turns
 * "this import owes six answers" from an alarm into a fact.
 *
 * NOTHING HERE DECIDES A STATE OR A DEBT. Both definitions live once, in the
 * API. This component reads a page of the answer and draws it — including
 * which halves of the screen exist at all, read off the payload's absences
 * rather than off any flag of its own (ADR 0045): no `assignment_state`
 * anywhere means no Holder column, and no `outstanding_count` means no Owes
 * column, no debt summary and no filter.
 */
export function OutstandingAnswersSection({ eventId, timezone }: HolderListSectionProps) {
  const t = useTranslations("outstandingAnswers");
  const sales = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [page, setPage] = useState(1);
  /*
    The Outstanding Answers filter: the roster narrowed to the Tickets that
    still owe. Held here rather than in the URL because it is a working view of
    one sitting — the chase — and the page resets with it, since page 3 of the
    roster names nothing about page 3 of the owing.
  */
  const [outstandingOnly, setOutstandingOnly] = useState(false);
  const [result, setResult] = useState<HolderListPage | null>(null);
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
      setResult(await fetchHolderList(eventId, page, outstandingOnly));
      setError(null);
    } catch (failure) {
      setResult(null);
      setError(
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ?? t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [eventId, page, outstandingOnly, errorCopy, t]);

  useEffect(() => {
    void load();
  }, [load]);

  /*
    Re-read when the dialog closes.

    The debts are DERIVED on every read, so an Answer given in the dialog has
    already been discharged on the server — this is only the screen catching
    up. Re-reading on close rather than watching the dialog's writes keeps the
    two surfaces from having to agree about anything: whatever happened in
    there, including nothing, the next read is the truth.
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
  /*
    Which halves of the list exist, read off the payload's absences and nowhere
    else, exactly as the Storefront reads its flags (ADR 0045): with
    `TICKET_ASSIGNMENT_ENABLED` closed the API omits every assignment field,
    and with `TICKET_QUESTIONS_ENABLED` closed it omits everything about debts.
  */
  const showHolders = holderListVisible(rows);
  const showQuestions = result ? questionsVisible(result) : false;

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

        {/* The one filter, offered only while the Event HAS debts to filter by
            — an Event whose questions feature is dark gets a plain roster and
            no control promising a view that cannot differ. It stays visible
            while its own filtered view is empty, because unticking it is the
            way back. */}
        {showQuestions && !error && (rows.length > 0 || outstandingOnly) ? (
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={outstandingOnly}
              onChange={(event) => {
                setOutstandingOnly(event.target.checked);
                setPage(1);
              }}
            />
            {t("filterOutstanding")}
          </label>
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
            Two different empty states for two different facts. The filtered
            view emptying is a CONGRATULATION — every required question has
            been answered on every live Ticket. The roster emptying just means
            nothing has been sold yet, which is no achievement and no failure.
          */
          <p className="text-sm text-muted-foreground">
            {outstandingOnly ? t("nothingOutstanding") : t("noTickets")}
          </p>
        ) : null}

        {!loading && !error && rows.length > 0 ? (
          <>
            {/* One message rather than two numbers glued together: which of the
                two counts leads the sentence, and both of their plurals, are the
                translator's to decide. Only drawn where debts exist as a concept
                — a roster without questions has nothing to summarise. */}
            {showQuestions ? (
              <p className="text-sm text-muted-foreground">
                {t("summary", {
                  answers: result?.outstanding_count ?? 0,
                  tickets: pagination?.total ?? 0,
                })}
              </p>
            ) : null}

            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colBuyer")}</th>
                    {/* The Holder column appears only when the API sent an
                        assignment at all — see `holderListVisible`. A column of
                        blanks on a deployment where assignment is closed would
                        be a promise this platform is not yet making. */}
                    {showHolders ? <th className="py-2 pr-4 font-medium">{t("colHolder")}</th> : null}
                    <th className="py-2 pr-4 font-medium">{t("colTicket")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colSold")}</th>
                    {/* And the Owes column only where debts exist as a concept,
                        for the same reason on the other flag. */}
                    {showQuestions ? <th className="py-2 pr-4 font-medium">{t("colOwes")}</th> : null}
                    <th className="py-2 font-medium">
                      <span className="sr-only">{t("colActions")}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((ticket) => (
                    <HolderRow
                      key={ticket.ticket_id}
                      ticket={ticket}
                      timezone={timezone}
                      onOpen={() =>
                        setOpenSale({ id: ticket.ticket_sale_id, ref: ticket.confirmation_ref })
                      }
                      answersLabel={sales("ticketAnswers")}
                      channelLabel={sales(SALES_CHANNEL_KEYS[ticket.channel])}
                      showHolder={showHolders}
                      showQuestions={showQuestions}
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

type HolderRowProps = {
  ticket: HolderTicket;
  timezone: string | null;
  onOpen: () => void;
  answersLabel: string;
  channelLabel: string;
  /** Whether this Event's rows carry a Holder at all; see `holderListVisible`. */
  showHolder: boolean;
  /** Whether the questions side of the list exists; see `questionsVisible`. */
  showQuestions: boolean;
};

/** One Ticket of the Event: its buyer, its Holder, and what it owes. */
function HolderRow({
  ticket,
  timezone,
  onOpen,
  answersLabel,
  channelLabel,
  showHolder,
  showQuestions,
}: HolderRowProps) {
  const t = useTranslations("outstandingAnswers");
  const locale = toAppLocale(useLocale());

  // The Event's zone when it is known, and the reader's own when it is not. A
  // date drawn in the wrong zone is off by at most a day; no date at all is off
  // by everything.
  const sold = timezone
    ? formatDate(ticket.sold_at, timezone, locale)
    : formatDate(ticket.sold_at, Intl.DateTimeFormat().resolvedOptions().timeZone, locale);

  const outstanding = ticket.outstanding ?? [];

  return (
    <tr className="border-b align-top last:border-0">
      <td className="py-3 pr-4">
        <div className="font-medium">{buyerName(ticket)}</div>
        <div className="text-muted-foreground">{ticket.customer_email}</div>
      </td>
      {showHolder ? <HolderCell ticket={ticket} /> : null}
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
      {showQuestions ? (
        <td className="py-3 pr-4">
          {outstanding.length > 0 ? (
            <ul className="space-y-1">
              {outstanding.map((question) => (
                // The label is the Organization's own words, rendered AS COINED
                // in both languages exactly as a Ticket Type name is (ADR 0027).
                // Only the chrome around it follows the reader's Staff Locale.
                <li key={question.question_id}>{question.label}</li>
              ))}
            </ul>
          ) : (
            /* Owing nothing is an ordinary state of a roster row, said out
               loud rather than left as a blank a reader must interpret. */
            <span className="text-muted-foreground">{t("owesNothing")}</span>
          )}
        </td>
      ) : null}
      <td className="py-3">
        <Button type="button" variant="outline" size="sm" onClick={onOpen}>
          {answersLabel}
        </Button>
      </td>
    </tr>
  );
}

type HolderCellProps = { ticket: HolderTicket };

/**
 * Who is coming on one Ticket (#329, ADR 0047).
 *
 * THE STATE IS ALWAYS DRAWN AND THE PERSON IS NOT, and that asymmetry is the
 * whole cell. A name arrives only when a Holder ACCEPTS, so without the state an
 * `assigned` Ticket whose Holder never clicked would look exactly like one
 * nobody was ever named for — and those are opposite facts to an Organizer
 * deciding whether to chase, or whether to expect somebody at the door.
 *
 * AN `assigned` ROW NAMES NOBODY, INCLUDING NO ADDRESS. The API withholds it,
 * and this cell would have nothing to draw even if it wanted to: an address a
 * buyer typed and its owner never accepted has no consent moment behind it, and
 * the person may not know a ticket was bought for them.
 *
 * A PURGED ROW READS "ASSIGNED, NEVER ACCEPTED" (#334): the API sends
 * `assigned` with `never_accepted` beside it, and `holderStateKey` picks the
 * word — somebody was named and never claimed the Ticket, which after the
 * Event is a different fact from nobody having been named. It too names
 * nobody: the address is gone by definition.
 *
 * The address of an ACCEPTED Holder is shown, deliberately and at a stated cost
 * (ADR 0047) — an Organizer needs a way to reach the people attending its Event,
 * and this platform builds no surface for it to mail them.
 */
function HolderCell({ ticket }: HolderCellProps) {
  const t = useTranslations("outstandingAnswers");
  const name = holderName(ticket);

  return (
    <td className="py-3 pr-4">
      {name ? <div className="font-medium">{name}</div> : null}
      {ticket.holder_email ? (
        <div className="text-muted-foreground">{ticket.holder_email}</div>
      ) : null}
      <Badge variant={holderBadgeVariant(ticket)} className={name ? "mt-1" : undefined}>
        {t(holderStateKey(ticket))}
      </Badge>
    </td>
  );
}

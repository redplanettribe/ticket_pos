"use client";

import { useCallback, useEffect, useState } from "react";

import { useRouter } from "next/navigation";

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
  Input,
  Label,
  Skeleton,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatDate, formatNumber } from "@/lib/format";
import {
  EMPTY_HOLDER_LIST_FILTERS,
  HOLDER_LIST_CHANNELS,
  SALES_CHANNEL_KEYS,
  buyerName,
  fetchHolderList,
  hasActiveHolderListFilters,
  holderBadgeVariant,
  holderListQuery,
  holderListVisible,
  holderName,
  holderStateKey,
  isOutstandingTheOnlyFilter,
  questionsVisible,
  type HolderListFilters,
  type HolderListPage,
  type HolderSortDir,
  type HolderSortField,
  type HolderTicket,
} from "@/lib/holder-list";

import { TicketAnswersDialog } from "./ticket-answers-dialog";

type HolderListSectionProps = {
  eventId: string;
  /*
    THE VIEW, READ OFF THE URL BY THE PAGE AND HANDED DOWN (#522, ADR 0065).
    Props and not state: this component navigates to change the view and
    re-renders with the new props, which is what makes the view shareable and
    the back button work.
  */
  page: number;
  filters: HolderListFilters;
  /**
   * The Event's Ticket Types, to name the ticket-type filter's options. Loaded
   * on the server page and handed down, as the Sales list's are; an empty array
   * is the tolerated failure — the control then offers only "all", and the
   * roster is unaffected.
   */
  ticketTypes: HolderTicketTypeOption[];
  sort: HolderSortField;
  dir: HolderSortDir;
  /** The Event's timezone, so a sale date reads where the Event is. */
  timezone: string | null;
};

/**
 * One option of the Ticket Type filter: the id the URL carries, and the name
 * the Organization coined.
 *
 * Its own type rather than the whole `TicketType`, because a filter needs a
 * label and a value and has no business holding a price or a capacity — and the
 * page that fills it would then have to keep those fields correct for a control
 * that never reads them.
 */
export type HolderTicketTypeOption = { id: string; name: string };

/**
 * The `<select>` chrome, matching the Sales list's `SELECT_CLASS` exactly.
 *
 * Restated rather than exported across, because it is nine words of Tailwind on
 * a native element that the design system does not wrap: sharing it would mean
 * one screen importing a private constant from another screen's file, which is
 * a worse coupling than a duplicated class string. If a Select component ever
 * lands in @ticket-pos/ui, both call sites go at once.
 */
const HOLDER_SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

/**
 * The Event's Holder List (#333; the Outstanding Answers screen of #313,
 * widened to the roster): every Ticket of the Event, who is coming on each,
 * and — where the Event asks Ticket Questions — what each still owes.
 *
 * WHAT THIS SCREEN IS. The Organization's answer to "who is coming": a roster,
 * one row per Ticket of every live sale. A fully answered Ticket stays on it
 * and an Event that asks nothing still has one, because the roster is the
 * point and the questions are a column on it. OUTSTANDING ANSWERS IS ONE
 * FILTER OF IT — the debt view it used to be — never its definition, and since
 * #523 it stands beside three structural ones: the Ticket Type, the Sales
 * Channel and the date the sale was made. They compose, so "which VIP door
 * sales are still unclaimed" is one view rather than a page somebody reads
 * down.
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
export function HolderListSection({
  eventId,
  page,
  filters,
  ticketTypes,
  sort,
  dir,
  timezone,
}: HolderListSectionProps) {
  /*
    THE MESSAGE NAMESPACE KEEPS THE OLD NAME while the file, the module and the
    route were renamed to the Holder List (#519, ADR 0065). A namespace is not
    an address: nothing outside the two catalogs reads it, renaming it would
    rewrite every key in both Locales, and a translation diff that touches every
    string of a screen for no change in wording is a diff nobody can review. The
    words on the screen are the Holder List's; only the key they hang from is
    historical.
  */
  const t = useTranslations("outstandingAnswers");
  const sales = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const router = useRouter();

  /*
    THE VIEW LIVES IN THE URL, AND NOT IN THIS COMPONENT (#522, ADR 0065).

    It used to be `useState`, defended as a working view of one sitting. That
    held while `outstanding` was the only control and stops holding at seven
    filters plus a sort: a view held here cannot be sent to a colleague, cannot
    be bookmarked, and cannot be stepped back through — the back button leaves
    the screen instead of undoing the narrowing. And decisively, THE HOLDER
    EXPORT CANNOT HONESTLY CLAIM TO MIRROR FILTERS THAT HAVE NO ADDRESS: the
    file #529 adds is defined as "this list, as you are looking at it", which is
    a promise only an addressable view can keep.

    Accepted cost, stated here because it is a privacy decision: once search
    arrives (#526) a customer's email address will reach browser history and any
    pasted link. Accepted ONLY because the Sales list already does exactly this
    — tightening it is one change across both screens, not a special case here.
  */
  const outstandingOnly = filters.outstanding;
  const filtersActive = hasActiveHolderListFilters(filters);
  /*
    Whether an empty view would be the CONGRATULATION or just "nothing matched"
    — see the empty state below, and `isOutstandingTheOnlyFilter` for why the
    two sentences must not be shared.
  */
  const outstandingAlone = isOutstandingTheOnlyFilter(filters);

  /*
    Every navigation goes through one builder, so the address bar and the fetch
    below cannot describe different views. A FILTER CHANGE RESETS TO PAGE 1,
    because page 3 of the roster names nothing about page 3 of the owing;
    paging keeps the filters, since the reader is walking one view.

    `push` and not `replace`: each change is a history entry, which is what
    makes the back button undo a filter.
  */
  const navigate = useCallback(
    (nextPage: number, nextFilters: HolderListFilters) => {
      router.push(
        `/events/${eventId}/sales/holders${holderListQuery(nextPage, nextFilters, sort, dir)}`,
      );
    },
    [router, eventId, sort, dir],
  );

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
      setResult(await fetchHolderList(eventId, page, filters, sort, dir));
      setError(null);
    } catch (failure) {
      setResult(null);
      setError(
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ?? t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [eventId, page, filters, sort, dir, errorCopy, t]);

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

        {/*
          THE FILTER BAR, in the Sales list's shape: the controls in a grid, and
          beneath them the row that clears them — one place a reader looks to see
          how this view is narrowed, and one place #524–#527 hang the remaining
          three filters and the sort from.

          THE BAR IS NO LONGER THE QUESTIONS FEATURE'S (#523). It used to be
          drawn only under `showQuestions`, because its one control was the
          Outstanding Answers checkbox and a build with questions dark had
          nothing to put in it. The three structural filters belong to no flag —
          a Ticket Type, a Sales Channel and a sale date exist on every build —
          so the bar now stands whenever there is a roster to narrow, and only
          the CHECKBOX comes and goes with the flag.

          It stays drawn while its own filtered view is empty, because clearing
          it is the way back and a bar that vanished with the last row would
          strand the reader on a screen with no controls.
        */}
        {!error && (rows.length > 0 || filtersActive) ? (
          <div className="space-y-3 rounded-md border bg-muted/20 p-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              {/* The Ticket Type: the VIP roster apart from general admission.
                  A Ticket belongs to exactly one, so this is a single choice
                  and never a set. The options come from the server page and are
                  empty when that read failed — the control then offers only
                  "all", which is honest, rather than a list missing types. */}
              <div className="flex flex-col gap-1">
                <Label htmlFor="holder-ticket-type">{sales("ticketTypeLabel")}</Label>
                <select
                  id="holder-ticket-type"
                  className={HOLDER_SELECT_CLASS}
                  value={filters.ticketTypeId}
                  onChange={(event) =>
                    navigate(1, { ...filters, ticketTypeId: event.target.value })
                  }
                >
                  <option value="">{sales("allTicketTypes")}</option>
                  {/* A Ticket Type's name is the Organization's own word and
                      reads as coined in both languages — data, not copy. */}
                  {ticketTypes.map((type) => (
                    <option key={type.id} value={type.id}>
                      {type.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* The Sales Channel, named from the SALES catalog and never
                  re-coined here: a door sale translated per screen is how a
                  Spanish reader comes to meet two words for one thing on two
                  tabs of the same screen. */}
              <div className="flex flex-col gap-1">
                <Label htmlFor="holder-channel">{sales("channelLabel")}</Label>
                <select
                  id="holder-channel"
                  className={HOLDER_SELECT_CLASS}
                  value={filters.channel}
                  onChange={(event) => navigate(1, { ...filters, channel: event.target.value })}
                >
                  <option value="">{sales("allChannels")}</option>
                  {HOLDER_LIST_CHANNELS.map((channel) => (
                    <option key={channel} value={channel}>
                      {sales(SALES_CHANNEL_KEYS[channel])}
                    </option>
                  ))}
                </select>
              </div>

              {/* The sale date, as two calendar days INCLUSIVE OF BOTH ENDS and
                  read in the Event's timezone by the API — so "sold in January"
                  is January where the Event is. Each bounds the other so the
                  picker cannot offer a range that means nothing. */}
              <div className="flex flex-col gap-1">
                <Label htmlFor="holder-sold-from">{sales("soldFromLabel")}</Label>
                <Input
                  id="holder-sold-from"
                  type="date"
                  value={filters.soldFrom}
                  max={filters.soldTo || undefined}
                  onChange={(event) => navigate(1, { ...filters, soldFrom: event.target.value })}
                  className="h-9"
                />
              </div>

              <div className="flex flex-col gap-1">
                <Label htmlFor="holder-sold-to">{sales("soldToLabel")}</Label>
                <Input
                  id="holder-sold-to"
                  type="date"
                  value={filters.soldTo}
                  min={filters.soldFrom || undefined}
                  onChange={(event) => navigate(1, { ...filters, soldTo: event.target.value })}
                  className="h-9"
                />
              </div>
            </div>

            {/* The Outstanding Answers filter, and the ONE control that belongs
                to a feature flag: with questions dark there are no debts to
                narrow by, and a checkbox promising a view that cannot differ
                would be a promise this build is not making (ADR 0045). */}
            {showQuestions ? (
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={outstandingOnly}
                  onChange={(event) => navigate(1, { ...filters, outstanding: event.target.checked })}
                />
                {t("filterOutstanding")}
              </label>
            ) : null}

            {/*
              The clear control appears ONLY when something is narrowed — an
              offer to clear nothing reads as "rows are missing" on a screen
              where none are — and it returns the whole roster from page 1.

              It borrows the SALES catalog's `clearFilters`, exactly as this
              screen already borrows `previousPage` and the channel names: two
              words for one control is how a Spanish reader comes to meet two
              names for the same button on two tabs of one screen.

              THE DOWNLOAD CONTROL BELONGS HERE, beside the filters it obeys, so
              what pressing it will produce is legible before it is pressed —
              the Sales Export's arrangement, and the reason the Holder Export
              can claim the file mirrors the screen. #529 adds it in this row,
              after the Clear:

                {canExport ? <HolderExportButton ... /> : null}
            */}
            {filtersActive ? (
              <div className="flex flex-wrap items-center justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => navigate(1, EMPTY_HOLDER_LIST_FILTERS)}
                >
                  {sales("clearFilters")}
                </Button>
              </div>
            ) : null}
          </div>
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
            THREE empty states for three different facts, and the third is
            #523's doing (ADR 0065).

            An empty OUTSTANDING-ONLY view is a CONGRATULATION: every required
            question has been answered on every live Ticket. That sentence is
            true only while `outstanding` is the sole narrowing — under a Ticket
            Type or a date range an empty view means "nothing matched here", and
            saying "every question has been answered" would be a claim about the
            whole Event that the view does not support. An Organizer told that
            after filtering to VIP door sales in January would stop chasing.

            An empty ROSTER under any other filter is just that: nothing
            matched, and the way out is the Clear beside it.

            An empty roster under NO filter means nothing has been sold yet,
            which is no achievement and no failure.
          */
          <p className="text-sm text-muted-foreground">
            {outstandingAlone
              ? t("nothingOutstanding")
              : filtersActive
                ? t("noMatchingTickets")
                : t("noTickets")}
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
                    onClick={() => navigate(pagination.page - 1, filters)}
                  >
                    {sales("previousPage")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pagination.page >= pagination.total_pages}
                    onClick={() => navigate(pagination.page + 1, filters)}
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

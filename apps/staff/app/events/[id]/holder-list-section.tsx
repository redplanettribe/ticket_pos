"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";
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

import { SortableHeader } from "@/components/sortable-header";
import { apiErrorMessage } from "@/lib/api-errors";
import { dossierHref } from "@/lib/customer-dossier";
import { ApiError } from "@/lib/events-api";
import { formatDate, formatNumber } from "@/lib/format";
import {
  EMPTY_HOLDER_LIST_FILTERS,
  HOLDER_LIST_ASSIGNMENT_STATES,
  HOLDER_LIST_CHANNELS,
  HOLDER_STATE_VALUE_KEYS,
  SALES_CHANNEL_KEYS,
  assignmentStateFilterVisible,
  buyerName,
  defaultHolderDirFor,
  downloadHolderExport,
  fetchHolderList,
  hasActiveHolderListFilters,
  holderBadgeVariant,
  holderDossierCustomerId,
  holderListEmptyStateKey,
  holderListQuery,
  holderListVisible,
  holderName,
  holderStateKey,
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
  /**
   * The Event's Ticket Questions, to name the named-question filter's options
   * (#525) — the union across its Ticket Types, since a question belongs to a
   * type and never to the Event. Loaded on the server page and handed down, as
   * the Ticket Types are, and an empty array is the tolerated failure.
   *
   * IT IS NOT A FLAG, AND IT DOES NOT DECIDE WHETHER THE CONTROL EXISTS. That
   * is read off the payload's absences (`showQuestions`), for the reason the
   * state filter's comment below gives at length: an empty list here means "no
   * questions to offer", which is a different fact from "this build has no
   * Ticket Questions".
   */
  questions: HolderQuestionOption[];
  sort: HolderSortField;
  dir: HolderSortDir;
  /** The Event's timezone, so a sale date reads where the Event is. */
  timezone: string | null;
  /**
   * Whether this reader may download the Holder Export (#529): an Org Admin or
   * an Event Owner, never Event Staff.
   *
   * A PROP AND NOT A ROLE READ HERE. The page already computes the role for its
   * own redirect guard, and a client component that read the session again would
   * be a second opinion about who this reader is — one that could disagree with
   * the guard it stands behind. Nothing here is the security boundary either
   * way: the API refuses independently, on the same gate as the list itself.
   */
  canExport: boolean;
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
 * One option of the named-question filter: the id the URL carries, and the
 * Organization's own words.
 *
 * ITS OWN TYPE rather than the whole `TicketQuestion`, on
 * `HolderTicketTypeOption`'s terms: a filter needs a label and a value and has
 * no business holding a kind, a review status or twenty Options.
 *
 * KEYED ON THE ID AND NOT THE LABEL. A label is the Organization's wording and
 * can be corrected; the id is what the Answers are attached to, and what a
 * shared URL must keep meaning tomorrow.
 */
export type HolderQuestionOption = { id: string; label: string };

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
 * #523 it stands beside three structural ones — the Ticket Type, the Sales
 * Channel and the date the sale was made — and since #524 beside the
 * assignment state. They compose, so "which VIP door sales were named and
 * never claimed" is one view rather than a page somebody reads down.
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
  questions,
  sort,
  dir,
  timezone,
  canExport,
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
    THE SEARCH BOX IS THE ONE CONTROL THAT DOES NOT APPLY ON CHANGE (#526).
    It is held here and committed on SUBMIT, which is the Sales list's
    interaction copied exactly (`SalesFilterBar`) and not a second one invented
    for this screen: every keystroke navigating would be a history entry per
    letter, so the back button would spell the reader's search backwards instead
    of undoing it — and each keystroke would be a request, and each request a
    customer address in a URL.

    Re-synced from the URL, because the URL is the view: the back button, a
    pasted link and the Clear control all change `filters.q` underneath this
    box, and a box that kept its own last word would then disagree with the
    rows beneath it.
  */
  const [search, setSearch] = useState(filters.q);
  useEffect(() => {
    setSearch(filters.q);
  }, [filters.q]);
  /*
    WHICH SENTENCE AN EMPTY LIST SAYS, decided in the module and not here (#528).
    The rule is that the congratulation is a claim about the whole Event and so
    fires only when `outstanding` is the sole narrowing; see
    `holderListEmptyStateKey` for why, and for why the SORT does not count.
  */
  const emptyStateKey = holderListEmptyStateKey(filters);

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

  /*
    THE SORT NAVIGATES LIKE A FILTER, THROUGH THE SAME BUILDER (#527).

    Clicking the ACTIVE column flips its direction; clicking another switches to
    it in that column's own natural direction (`defaultHolderDirFor` — names
    ascend, the debt descends). Either way it RESETS TO PAGE 1, because page 3
    of one order names nothing about page 3 of another; the filters are kept,
    since the reader is reordering one view rather than choosing a new one.

    The sort lives in the URL exactly as the filters do, so an ordered roster is
    a link a colleague can be sent and the back button undoes a reordering
    instead of leaving the screen.
  */
  const toggleSort = useCallback(
    (field: HolderSortField) => {
      const nextDir: HolderSortDir =
        field === sort ? (dir === "asc" ? "desc" : "asc") : defaultHolderDirFor(field);
      router.push(`/events/${eventId}/sales/holders${holderListQuery(1, filters, field, nextDir)}`);
    },
    [router, eventId, filters, sort, dir],
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
  /*
    THE STATE FILTER'S OWN VISIBILITY IS NOT `holderListVisible(rows)`, and the
    difference is the bug this fixes. That predicate reads the CURRENT PAGE'S
    rows, so filtering to a state nobody is in emptied the page and took the
    control that produced the view with it. "The feature is dark" and "this
    filter matched nothing" are different facts and only the first is a payload
    absence; see `assignmentStateFilterVisible` for why an active narrowing on
    an EMPTY page keeps the control, and why the dark build is still safe.

    `showHolders` keeps its original meaning — whether the assignment COLUMNS
    have anything to draw — and is what the table below reads.

    THE QUESTIONS SIDE IS ALREADY IMMUNE and needs no equivalent: `showQuestions`
    is `questionsVisible(result)`, which tests `outstanding_count !== undefined`
    — a PAGE-LEVEL field the API sends whenever Ticket Questions are open,
    unaffected by the filters and present on an empty page. So the named-question
    select and the Outstanding checkbox stay drawn through an empty filtered view
    already. No other control in the bar is row-derived: the Ticket Types and the
    questions come down as props, and the Clear and the download hang off
    `filtersActive` / `canExport`.
  */
  const showHolders = holderListVisible(rows);
  const showAssignmentFilter = assignmentStateFilterVisible(rows, filters.assignmentState);
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
          how this view is narrowed, and one place #525–#527 hang the remaining
          two filters and the sort from.

          TWO OF ITS CONTROLS BELONG TO A FEATURE FLAG, and each is drawn from
          the PAYLOAD'S ABSENCES rather than from anything passed in: the state
          filter under `showHolders` and the Outstanding checkbox under
          `showQuestions`. The filters they carry are IGNORED by the API on a
          build where their feature is dark, never refused, so a URL naming one
          still returns the roster.

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
              {/*
                THE SEARCH BOX (#526): one person on the roster, by the buyer's
                name or address, by the Sale Confirmation reference, or by an
                ACCEPTED Holder's name or address.

                SEARCHABLE IF AND ONLY IF DISPLAYABLE, WHICH IS WHY THE
                PLACEHOLDER IS WORDED AS IT IS. It promises a buyer, a reference
                and an *accepted* holder — never holder addresses generally —
                because an address a buyer typed and its owner never accepted is
                named nowhere on this platform (ADR 0047) and matches nothing.
                The rule is enforced in the API's predicate and never here; the
                copy's whole job is not to promise what the query refuses.

                IT IS DRAWN ON EVERY BUILD, unlike the state select above and
                the checkbox below, and this is not an oversight: a buyer's
                name, a buyer's address and a Sale Confirmation reference are on
                every roster, so there is always something for this box to
                match. With Ticket Assignment dark the Holder branch of the
                API's predicate simply never matches, because nobody has
                accepted anything — no control comes and goes, and no flag is
                read.

                THE LABEL AND THE BUTTON ARE THE SALES CATALOG'S OWN WORDS,
                reused rather than re-coined for the reason the channel names
                are: "Search" means the same thing on both tabs, and it must not
                acquire a second Spanish word here. Only the PLACEHOLDER is this
                screen's, because only what is matched differs.
              */}
              <form
                className="flex flex-col gap-1"
                onSubmit={(event) => {
                  event.preventDefault();
                  navigate(1, { ...filters, q: search.trim() });
                }}
              >
                <Label htmlFor="holder-search">{sales("searchLabel")}</Label>
                <div className="flex gap-2">
                  <Input
                    id="holder-search"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={t("filterSearchPlaceholder")}
                    className="h-9"
                  />
                  <Button type="submit" variant="outline" size="sm">
                    {sales("searchAction")}
                  </Button>
                </div>
              </form>

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

              {/*
                WHERE EACH TICKET STANDS WITH ITS HOLDER (#524): four values
                over three states, `never_accepted` being a value of this
                filter and not a fourth state — the API derives it at read time
                from the retention purge's marker, and #331 rejected making it
                a state. "Who did I name who never claimed their ticket" is the
                morning-after question, and after the Event has started it is
                otherwise indistinguishable from "nobody was named".

                DRAWN ONLY WHERE THE ASSIGNMENT SIDE OF THE LIST EXISTS, and
                that is decided by the payload's ABSENCES — exactly as the
                Outstanding checkbox below hangs on `questionsVisible`. THE
                TEMPTING MOVE IS TO PASS A FLAG DOWN TO RENDER THIS CONTROL, AND
                IT IS THE WRONG ONE: this app holds no copy of a deployment flag
                (ADR 0045), and a prop carrying one would be that copy, wrong on
                the first deploy where the two disagree. With Ticket Assignment
                dark the API omits every assignment field, so there is nothing to
                filter by and no control — and a URL that still carries a
                WELL-FORMED `assignment_state` is IGNORED by the API rather than
                refused, so the bookmark keeps working. (A MALFORMED one is now a
                400: shape is validated on every surface here, honouring is not.
                This control can only emit the four legal values, so it never
                produces one.)

                BUT NOT `holderListVisible(rows)` DIRECTLY, WHICH WAS THE BUG.
                That reads the current page's rows, so narrowing to a state
                nobody is in emptied the page and deleted the control that
                produced the view — the reader left with a Clear and no sight of
                what they had asked for. An empty RESULT is not a payload
                ABSENCE. `assignmentStateFilterVisible` keeps the absence reading
                whenever there are rows to read it from and falls back to "this
                filter is set" only on an empty page, which is the Outstanding
                checkbox's own reasoning — unticking it is the way back.

                THE WORDS ARE THE ROWS' OWN, from `HOLDER_STATE_VALUE_KEYS`,
                because a filter that named a state differently from the badge
                beneath it would be two vocabularies for one fact.
              */}
              {showAssignmentFilter ? (
                <div className="flex flex-col gap-1">
                  <Label htmlFor="holder-assignment-state">{t("filterStateLabel")}</Label>
                  <select
                    id="holder-assignment-state"
                    className={HOLDER_SELECT_CLASS}
                    value={filters.assignmentState}
                    onChange={(event) =>
                      navigate(1, { ...filters, assignmentState: event.target.value })
                    }
                  >
                    <option value="">{t("filterStateAny")}</option>
                    {HOLDER_LIST_ASSIGNMENT_STATES.map((state) => (
                      <option key={state} value={state}>
                        {t(HOLDER_STATE_VALUE_KEYS[state])}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}

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

              {/*
                ONE NAMED TICKET QUESTION'S DEBTORS (#525). "Who still hasn't
                told me their shirt size" is a different chase from "who owes
                anything at all", and on an Event asking several questions the
                checkbox below is too blunt to work from. The two COMPOSE: this
                narrows the same debt the checkbox does, and neither replaces
                the other.

                DRAWN WHERE THE QUESTIONS SIDE OF THE LIST EXISTS, which is
                `questionsVisible(result)` — the payload's ABSENCES — exactly as
                the checkbox below hangs on it. NO FLAG PROP COMES DOWN FOR
                THIS, for the reason spelled out on the state filter above: this
                app holds no copy of a deployment flag (ADR 0045), and a prop
                carrying one would be that copy. A URL still naming a question
                on a build where Ticket Questions are dark is IGNORED by the
                API, never refused, so the bookmark keeps working.

                AND NOT ON `questions.length`, deliberately. An Event that asks
                nothing draws this control with only its "any question" option,
                exactly as it already draws the checkbox — because the
                alternative is a filter that can be in the URL and narrowing the
                roster with no control on screen saying so, which is the one
                thing a filter bar must never do. It is also what a failed
                options read looks like, and a wide roster the reader can see is
                wide beats a hidden narrowing.

                THE LABELS ARE THE ORGANIZATION'S OWN WORDS, rendered AS COINED
                in every Locale (ADR 0027) — data, not copy, exactly like the
                Ticket Type names above. Only the control's own chrome follows
                the reader's Staff Locale. Two questions may be worded alike,
                since each belongs to its own Ticket Type; the option's VALUE is
                the id, so a repeated heading picks out the right one.
              */}
              {showQuestions ? (
                <div className="flex flex-col gap-1">
                  <Label htmlFor="holder-question">{t("filterQuestionLabel")}</Label>
                  <select
                    id="holder-question"
                    className={HOLDER_SELECT_CLASS}
                    value={filters.questionId}
                    onChange={(event) => navigate(1, { ...filters, questionId: event.target.value })}
                  >
                    <option value="">{t("filterQuestionAny")}</option>
                    {questions.map((question) => (
                      <option key={question.id} value={question.id}>
                        {question.label}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}
            </div>

            {/* The Outstanding Answers filter, and the ONE control that belongs
                to a feature flag: with questions dark there are no debts to
                narrow by, and a checkbox promising a view that cannot differ
                would be a promise this build is not making (ADR 0045). Since
                #525 the named-question select above shares its flag and its
                subject — the debt — and narrows it further. */}
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
              can claim the file mirrors the screen (#529). It sits AFTER the
              Clear, and the row is now drawn whenever EITHER control has
              something to offer: the download is available on an unfiltered
              roster too, where the file is simply everybody.
            */}
            {filtersActive || canExport ? (
              <div className="flex flex-wrap items-center justify-end gap-2">
                {filtersActive ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => navigate(1, EMPTY_HOLDER_LIST_FILTERS)}
                  >
                    {sales("clearFilters")}
                  </Button>
                ) : null}
                {canExport ? (
                  <HolderExportButton eventId={eventId} filters={filters} sort={sort} dir={dir} />
                ) : null}
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
            THREE empty states for three different facts (#523, #528, ADR 0065),
            chosen by `holderListEmptyStateKey` and unit-tested there rather than
            spelled out as ternaries here: the congratulation is a claim about
            the WHOLE EVENT and holds only while `outstanding` is the sole
            narrowing, under any other filter an empty view means "nothing
            matched" with the Clear beside it as the way out, and an unfiltered
            empty roster means nothing has been sold yet.
          */
          <p className="text-sm text-muted-foreground">{t(emptyStateKey)}</p>
        ) : null}

        {!loading && !error && rows.length > 0 ? (
          <>
            {/*
                TWO COUNTS AT TWO SCOPES, AND THE SENTENCE NAMES BOTH (#528).
                `outstanding_count` is the Event's whole debt and deliberately
                does NOT follow the filters — a fact about the Event, per the API
                contract and exactly as the Sales list's reversed count behaves —
                while `total` is the count of Tickets in the view being read.
                Filtered to one Ticket Type the old wording read "47 outstanding
                answers across 12 tickets", two scopes in one clause with nothing
                saying so.

                ONE MESSAGE KEY AND NOT TWO (filtered vs not), because "across
                the whole event" and "in this view" are both true of the plain
                roster: the unfiltered view IS the whole Event, so the sentence
                degrades to a harmless restatement rather than reading wrong. A
                second key would fork the copy for the common reader's case and
                drift the moment one of them is edited. Which count leads and
                both plurals stay the translator's to decide. Only drawn where
                debts exist as a concept — a roster without questions has nothing
                to summarise. */}
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
                  {/*
                    EVERY DATA COLUMN IS SORTABLE, AND EACH HEADER IS THE ONLY
                    CONTROL ITS SORT HAS (#527). The markup, the arrow and the
                    `aria-sort` are the Sales list's `SortableHeader`, shared
                    rather than re-coined so the two staff tables are reordered
                    by the same gesture.

                    THE TWO FLAGGED SORTS NEED NO FLAG OF THEIR OWN, and this is
                    the property to preserve when touching these lines: `holder`
                    and `owes` are offered by the headers of the columns they
                    order, which are already drawn only under `showHolders` and
                    `showQuestions`. So on a build with either feature dark the
                    control is simply ABSENT — no second reading of a flag, and
                    nothing to keep in step. A URL still asking for one of them
                    is the API's problem, and it ignores it rather than refusing.
                  */}
                  <tr className="border-b text-left text-muted-foreground">
                    <SortableHeader
                      label={t("colBuyer")}
                      field="buyer"
                      sort={sort}
                      dir={dir}
                      onSort={toggleSort}
                    />
                    {/* The Holder column appears only when the API sent an
                        assignment at all — see `holderListVisible`. A column of
                        blanks on a deployment where assignment is closed would
                        be a promise this platform is not yet making. And with
                        it goes the one sort whose blanks come last in BOTH
                        directions. */}
                    {showHolders ? (
                      <SortableHeader
                        label={t("colHolder")}
                        field="holder"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                      />
                    ) : null}
                    {/* Sorted in the Event's CATALOG DISPLAY ORDER, not
                        alphabetically — the order the Organization wrote its
                        Ticket Types in is the order it means. */}
                    <SortableHeader
                      label={t("colTicket")}
                      field="ticket_type"
                      sort={sort}
                      dir={dir}
                      onSort={toggleSort}
                    />
                    <SortableHeader
                      label={t("colSold")}
                      field="sold_at"
                      sort={sort}
                      dir={dir}
                      onSort={toggleSort}
                    />
                    {/* And the Owes column only where debts exist as a concept,
                        for the same reason on the other flag — which is also
                        what keeps the `owes` sort off a build that has no
                        debts to rank. */}
                    {showQuestions ? (
                      <SortableHeader
                        label={t("colOwes")}
                        field="owes"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                      />
                    ) : null}
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
                      eventId={eventId}
                      dossierFrom={`/events/${encodeURIComponent(eventId)}/sales/holders${holderListQuery(page, filters, sort, dir)}`}
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
  eventId: string;
  /** This list's own address, filters and all, for the Dossier's Back (#640). */
  dossierFrom: string;
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
  eventId,
  dossierFrom,
}: HolderRowProps) {
  const t = useTranslations("outstandingAnswers");
  const tDossier = useTranslations("customerDossier");
  const buyer = buyerName(ticket);
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
        <div className="font-medium">
          {buyer && ticket.customer_id ? (
            <Link
              href={dossierHref(eventId, ticket.customer_id, dossierFrom)}
              className="underline-offset-2 hover:underline"
              aria-label={tDossier("openDossier", { name: buyer })}
            >
              {buyer}
            </Link>
          ) : (
            buyer
          )}
        </div>
        <div className="text-muted-foreground">{ticket.customer_email}</div>
      </td>
      {showHolder ? <HolderCell ticket={ticket} eventId={eventId} dossierFrom={dossierFrom} /> : null}
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

type HolderCellProps = { ticket: HolderTicket; eventId: string; dossierFrom: string };

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
function HolderCell({ ticket, eventId, dossierFrom }: HolderCellProps) {
  const t = useTranslations("outstandingAnswers");
  const tDossier = useTranslations("customerDossier");
  const name = holderName(ticket);
  // An unaccepted assignment offers no link (#640): see holderDossierCustomerId.
  const holderId = holderDossierCustomerId(ticket);

  return (
    <td className="py-3 pr-4">
      {name ? (
        <div className="font-medium">
          {holderId ? (
            <Link
              href={dossierHref(eventId, holderId, dossierFrom)}
              className="underline-offset-2 hover:underline"
              aria-label={tDossier("openDossier", { name })}
            >
              {name}
            </Link>
          ) : (
            name
          )}
        </div>
      ) : null}
      {ticket.holder_email ? (
        <div className="text-muted-foreground">{ticket.holder_email}</div>
      ) : null}
      <Badge variant={holderBadgeVariant(ticket)} className={name ? "mt-1" : undefined}>
        {t(holderStateKey(ticket))}
      </Badge>
    </td>
  );
}

type HolderExportButtonProps = {
  eventId: string;
  filters: HolderListFilters;
  sort: HolderSortField;
  dir: HolderSortDir;
};

/**
 * Downloads the Holder Export for the view currently on screen (#529, ADR
 * 0065), whatever its size (ADR 0075).
 *
 * IT FETCHES A BLOB RATHER THAN LINKING to the endpoint, because the endpoint
 * answers with a FILE on success and a JSON error envelope on failure: a plain
 * link would send the browser to raw JSON on a refusal, and the error would go
 * unseen. The busy state and the inline message both follow from that - a
 * large roster takes a moment to stream, and a download that fails part way
 * saves nothing and says so here.
 *
 * It follows `SalesExportButton` deliberately and almost line for line: two
 * download buttons on two tabs of one screen that behaved differently would be
 * two things for a reader to learn, and the difference would be in which errors
 * they can see.
 */
function HolderExportButton({ eventId, filters, sort, dir }: HolderExportButtonProps) {
  // TWO NAMESPACES, ON PURPOSE. The BUTTON's words are the `sales` catalog's,
  // reused verbatim — "Download .xlsx" and "Preparing…" say the same thing on
  // both tabs of this screen, and re-coining them is how a Spanish reader comes
  // to meet two names for one control. The LAST-RESORT FAILURE SENTENCE is this
  // screen's own, because "Could not download the sales" would be false about a
  // roster.
  const sales = useTranslations("sales");
  const t = useTranslations("outstandingAnswers");
  const errorCopy = useMessages().errors;
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDownload() {
    setDownloading(true);
    setError(null);
    try {
      await downloadHolderExport(eventId, filters, sort, dir);
    } catch (downloadError: unknown) {
      /*
        Two rungs. The catalog by code first: the one refusal this download
        still has before its first byte is a busy server, and that sentence is
        the catalog's, in the reader's language. Then this surface's own words,
        which are also what a download cut part way reads as - there is no
        envelope once the file has begun to stream, only a failed read (ADR
        0075).
      */
      const apiError = downloadError instanceof ApiError ? downloadError : null;
      setError(apiErrorMessage(errorCopy, apiError) ?? t("exportFailed"));
    } finally {
      setDownloading(false);
    }
  }

  return (
    <>
      {/* Its own full-width line inside the wrapping row: a whole sentence
          squeezed beside the button would be a column of two words. It stays in
          this row so it reads as an answer to the button. */}
      {error ? (
        <span role="alert" className="basis-full text-right text-sm text-destructive">
          {error}
        </span>
      ) : null}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={downloading}
        aria-busy={downloading}
        onClick={handleDownload}
      >
        {downloading ? sales("preparingDownload") : sales("downloadXlsx")}
      </Button>
    </>
  );
}

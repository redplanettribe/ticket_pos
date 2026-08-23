"use client";

import { Skeleton } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import { HeldTicketAnswers } from "@/components/held-ticket-answers";
import { TicketAssignmentRow } from "@/components/ticket-assignment-row";
import { visibleQuestionsOf } from "@/lib/ticket-questions";
import { apiErrorMessage } from "@/lib/api-errors";
import {
  hasAnythingToShow,
  heldRowFor,
  placeholderRowCount,
  saleOutstandingCount,
  withHeldRow,
  type BuyerTicket,
  type HeldTicket,
} from "@/lib/buyer-answers";
import {
  assignmentOffered,
  assignmentStateOf,
  assignmentTally,
  holderEmailOf,
  saleOffersAssignment,
  type AssignmentBody,
} from "@/lib/ticket-assignment";

/**
 * The buyer's Tickets on one of their own Ticket Sales: whose each one is, and
 * the questions on the ONE they hold (#315, ADR 0044; #324, ADR 0046; narrowed
 * by #344, ADR 0049).
 *
 * ONLY THE HOLDER ANSWERS. A Ticket the buyer does not hold is drawn as its
 * assignment row and nothing more — the address field, "Save address", and its
 * state (no address yet / for somebody, awaiting acceptance / accepted). No
 * question rows, no Answered/Outstanding badge, no link to copy: those are the
 * Holder's, on the Holder's own Customer Area, and Event Staff are the
 * backstop. The buyer's "Your ticket" — the Self-held Ticket (ADR 0048) —
 * keeps its questions, read and written through the held-ticket routes, which
 * are the same routes every Holder uses.
 *
 * TWO FETCHES, ONE COMPONENT. The sale-scoped list says which Tickets this
 * Sale has and whose they are; the held list says what the Tickets this
 * Customer holds are asking. They are joined here by id (lib/buyer-answers.ts,
 * `heldRowFor`), so a buyer who gave their own Ticket away between the two
 * sees it as an ordinary assignable row rather than as a panel with nothing in
 * it. Keeping both on one component is what keeps "Your ticket" one panel
 * rather than a section that has to be told what the section above it said.
 *
 * IT APPEARS ON THE CONFIRMATION LINK'S PAGE AND IN THE CUSTOMER AREA BECAUSE
 * THOSE ARE ONE PAGE. A Confirmation Link redeems into a session narrowed to one
 * Ticket Sale and lands on the Customer Area, so a link session sees this on its
 * one purchase and a signed-in Customer sees it on each of theirs.
 *
 * IT IS FETCHED RATHER THAN SERVER-RENDERED WITH THE REST OF THE CARD, and that
 * is a deliberate cost. The Customer Area lists every purchase a person has ever
 * made; loading every sale's Tickets and the held list to render a page where
 * most sales ask nothing would make the commonest page slower for the people it
 * has nothing to show. Fetching per card also means neither feature flag needs
 * a second copy in this app: a deployment with the feature dark answers 404 and
 * this component draws nothing at all, which is exactly what a build without
 * the feature does (ADR 0045).
 *
 * SPACE IS RESERVED FOR WHAT IS PROBABLY COMING, AND GIVEN BACK WHEN IT IS NOT
 * (#357). While the two fetches are in flight the card draws the section's
 * outline — a heading's worth, a tally line's worth, and one row per Ticket at
 * the real row's minimum height, the count taken from the Sale's own lines,
 * which the card knew before either list was asked for. When the lists land
 * the rows are replaced in place at the same height, so an expanded Sale does
 * not grow under the reader's finger once the page hydrates. When they land
 * with nothing to say — the feature dark, the read failed, a Sale that asks
 * nothing and assigns nothing — the outline goes and the card is precisely the
 * card it was before either feature existed. That is a shift too, but a
 * shrink after a pause on the uncommon card rather than a growth on every
 * common one. The fetch itself stays client-side (#345): the page must never
 * wait on the questions.
 *
 * THE TWO FEATURES HAVE SEPARATE FLAGS AND ARE READ SEPARATELY. Either half is
 * reason enough to draw the section and neither is reason to draw the other.
 * The API omits every assignment field while TICKET_ASSIGNMENT_ENABLED is
 * closed, and that absence is the only place this app learns the flag's state.
 *
 * A KNOWN COUPLING, and not one this ticket may fix: the sale-scoped GET is
 * still gated on TICKET_QUESTIONS_ENABLED, so a deployment with assignment open
 * and questions closed answers 404 here and shows nothing at all. That gate is
 * the API's and belongs to whoever unpicks it.
 *
 * THE BUYER'S OWN ROW IS THE HELD-TICKET PANEL (#345): the same
 * components/held-ticket-answers.tsx a Holder sees in their own Customer Area
 * — open while owed, autosaving, folded behind "Review or edit" once nothing
 * is. This file only decides WHICH row is theirs and what its header says.
 */
type BuyerTicketAnswersProps = {
  ticketSaleId: string;
  /** Tickets on the Sale, summed from its lines by the card: how many rows to
   * reserve before the lists arrive. Not trusted for anything else — the
   * fetched list decides what is actually drawn. */
  ticketCount: number;
};

export function BuyerTicketAnswers({ ticketSaleId, ticketCount }: BuyerTicketAnswersProps) {
  const t = useTranslations("customerArea");
  const [tickets, setTickets] = useState<BuyerTicket[] | null>(null);
  const [held, setHeld] = useState<HeldTicket[] | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const response = await fetch(
          `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tickets`,
        );
        const envelope = (await response.json()) as { data: BuyerTicket[] | null };
        if (!live) return;
        // EVERY FAILURE IS SILENCE. The commonest by far is the feature flag
        // being off; the rest are a signed-out session or a network that is
        // already breaking the page around this. None of them is worth an error
        // message beside somebody's receipt, and there is nothing they could do
        // about any of them.
        setTickets(response.ok && envelope.data ? envelope.data : []);
      } catch {
        if (live) setTickets([]);
      }
    })();
    void (async () => {
      try {
        // The held list is not scoped to this Sale — it is everything the
        // session holds — and the join by id below picks out the one row, if
        // any, that belongs on this card. A failure here leaves the buyer's own
        // row as an assignment row, which is the correct shape for a Ticket
        // whose questions this page was not given.
        const response = await fetch("/api/customer/held-tickets");
        const envelope = (await response.json()) as { data: HeldTicket[] | null };
        if (!live) return;
        setHeld(response.ok && envelope.data ? envelope.data : []);
      } catch {
        if (live) setHeld([]);
      }
    })();
    return () => {
      live = false;
    };
  }, [ticketSaleId]);

  // Both fetches must land before anything real is drawn: a section that
  // appeared without its question half and then grew one would move the page
  // under the reader's finger. Until then, the outline holds the room.
  if (tickets === null || held === null) {
    return <BuyerTicketAnswersSkeleton rows={placeholderRowCount(ticketCount)} />;
  }

  // TWO FEATURES, TWO FLAGS, ONE SECTION. Either half is reason enough to draw
  // it, and neither is reason to draw the other.
  const questions = hasAnythingToShow(tickets, held);
  const assignment = saleOffersAssignment(tickets);
  if (!questions && !assignment) {
    return null;
  }

  const outstanding = saleOutstandingCount(tickets, held);
  const tally = assignmentTally(tickets);

  return (
    <section className="mt-6 space-y-3 border-t pt-6">
      <div className="space-y-1">
        {/* The heading follows what the section is FOR. Once addresses can be
            given, "questions about these tickets" is no longer the half of it a
            buyer came here for — telling four identical Tickets apart is. */}
        <h3 className="font-medium">
          {assignment ? t("assignment.title") : t("answers.title")}
        </h3>
        {/* ONE LINE OF TALLY, never a paragraph per Ticket. The outstanding
            count is the buyer's OWN debt, on the one Ticket they hold; what the
            other Tickets owe is their Holders' business. Partial assignment
            counts and never warns — a sale of four with two addresses is
            finished as far as the buyer is concerned. */}
        <p className="text-muted-foreground text-sm">
          {[
            questions ?
              outstanding > 0 ?
                t("answers.outstanding", { count: outstanding })
              : t("answers.allDone")
            : null,
            assignment && tally.total > 0 ?
              t("assignment.tally", { assigned: tally.assigned, total: tally.total })
            : null,
          ]
            .filter(Boolean)
            .join(" ")}
        </p>
      </div>

      <div className="divide-y rounded-md border">
        {tickets.map((ticket, index) => (
          <TicketBlock
            key={ticket.ticket_id}
            ticketSaleId={ticketSaleId}
            ticket={ticket}
            heldRow={heldRowFor(ticket, held)}
            // "Ticket 2 of 4" is the whole of what can be said to tell two
            // identical tickets apart until an address is given: there are no
            // seat numbers, and the platform is never going to ask for them.
            position={index + 1}
            total={tickets.length}
            onAssigned={setTickets}
            onAnswered={(updated) => setHeld((rows) => withHeldRow(rows ?? [], updated))}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * The section's outline while its two lists are in flight. The same outer
 * spacing as the real section (`mt-6 border-t pt-6`, a heading, a tally line,
 * a bordered `divide-y` list) so the card is already the height it is about
 * to be, and each row the real summary row's box — `min-h-11 px-3 py-2.5` —
 * so the swap is invisible. Hidden from assistive tech: there is nothing to
 * read in it, and `aria-busy` on the wrapper says what it is.
 */
function BuyerTicketAnswersSkeleton({ rows }: { rows: number }) {
  return (
    <section aria-busy className="mt-6 space-y-3 border-t pt-6">
      <div aria-hidden className="space-y-1">
        {/* A `font-medium` heading line is 24px tall at the base size and the
            tally line 20px at `text-sm`; the bars sit inside those line boxes
            so the two blocks measure what the text will. */}
        <div className="flex h-6 items-center">
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="flex h-5 items-center">
          <Skeleton className="h-3 w-32" />
        </div>
      </div>
      <div aria-hidden className="divide-y rounded-md border">
        {Array.from({ length: rows }, (_, index) => (
          <div key={index} className="flex min-h-11 items-center gap-x-3 px-3 py-2.5 text-sm">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-4 w-28" />
          </div>
        ))}
      </div>
    </section>
  );
}

type TicketBlockProps = {
  ticketSaleId: string;
  ticket: BuyerTicket;
  /** The held-ticket row behind this one, or null — null for every Ticket the
   * buyer does not hold, which draws as an assignment row and nothing more. */
  heldRow: HeldTicket | null;
  position: number;
  total: number;
  onAssigned: (tickets: BuyerTicket[]) => void;
  onAnswered: (ticket: HeldTicket) => void;
};

function TicketBlock({
  ticketSaleId,
  ticket,
  heldRow,
  position,
  total,
  onAssigned,
  onAnswered,
}: TicketBlockProps) {
  const t = useTranslations("customerArea");
  const errorCopy = useMessages().errors;
  // The questions are the held row's, and a row the buyer does not hold HAS
  // none here — not "none yet", none: the sale-scoped payload never carried
  // them, so there is nothing this block could draw even by mistake.
  const questions = heldRow === null ? [] : visibleQuestionsOf(heldRow.questions);
  const assignable = assignmentOffered(ticket);

  if (questions.length === 0 && !assignable) {
    // A Ticket the buyer does not hold, in a deployment where assignment is
    // dark. Drawn as nothing rather than as an empty block: there is nothing
    // to say about it and nothing to do with it.
    return null;
  }

  /**
   * Naming the address for this Ticket (#324).
   *
   * A SEPARATE REQUEST FROM THE ANSWER SAVE and not a batched one: the API's
   * unit is one fact about one Ticket. The whole sale comes back, so a Ticket
   * the buyer just gave away is redrawn as an ordinary row at once — its held
   * row no longer matches anything on the sale, and its questions go with it.
   */
  async function assign(body: AssignmentBody): Promise<string | null> {
    try {
      const response = await fetch(
        `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}` +
          `/tickets/${encodeURIComponent(ticket.ticket_id)}/assignment`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        },
      );
      const envelope = (await response.json()) as {
        data: BuyerTicket[] | null;
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error || !envelope.data) {
        return apiErrorMessage(errorCopy, envelope.error) ?? t("assignment.saveFailed");
      }
      onAssigned(envelope.data);
      return null;
    } catch {
      return t("assignment.networkFailed");
    }
  }

  if (heldRow !== null) {
    // THE BUYER'S OWN TICKET: the held-ticket panel — the same one a Holder
    // sees in their Customer Area — with "give it to someone else" folded
    // inside it above the questions. The panel decides whether it starts open
    // (an Answer is owed), folds (the last one saved) or has no chevron at all
    // (asks nothing) — see lib/held-ticket-panel.ts. The address is the
    // buyer's own, so the field is offered only as a give-away rather than
    // drawn open with their own email in it.
    return (
      <HeldTicketAnswers ticket={heldRow} header={t("answers.ownTicket")} onAnswered={onAnswered}>
        {assignable ?
          <details className="text-sm">
            <summary className="text-muted-foreground cursor-pointer">
              {t("assignment.giveAway")}
            </summary>
            <div className="pt-3">
              <TicketAssignmentRow ticket={ticket} save={assign} />
            </div>
          </details>
        : null}
      </HeldTicketAnswers>
    );
  }

  const state = assignmentStateOf(ticket);
  const holder = holderEmailOf(ticket);

  // ONE COLLAPSED ROW PER TICKET the buyer does not hold, opened on demand.
  // The row's default reading is who the Ticket is for; the address field
  // waits behind it. No badge: a Ticket the buyer does not hold owes them
  // nothing, and says nothing.
  return (
    // `open` is left UNDEFINED: a `false` here would be re-applied by React on
    // every redraw, snapping a row shut the moment a save inside it came back.
    <details className="group/ticket">
      {/* THE WHOLE ROW IS THE TAP TARGET, at least 44px tall: the chevron is a
          hint, not the handle. Below `sm` the row is two lines — position and
          Ticket Type, then the address — because a phone is narrower than a
          full address and the page must never be wider than the phone (#353).
          At `sm` and above it is one line again, the address taking the rest. */}
      <summary className="flex min-h-11 cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 text-sm [&::-webkit-details-marker]:hidden">
        <span
          aria-hidden
          className="text-muted-foreground transition-transform group-open/ticket:rotate-90"
        >
          ▸
        </span>
        <span className="font-medium">{t("answers.ticketHeading", { position, total })}</span>
        <span className="text-muted-foreground">{ticket.ticket_type_name}</span>
        {/* The state, in the buyer's words: "accepted", never "claimed". The
            address truncates with an ellipsis rather than overflow: `basis-full`
            gives it its own line on a phone, `sm:basis-0` puts it back beside
            the Ticket Type, and `min-w-0` lets the flex item shrink at all. */}
        <span className="text-muted-foreground min-w-0 basis-full flex-1 truncate sm:basis-0">
          {state === "accepted" ?
            t("assignment.rowAccepted", { email: holder })
          : state === "assigned" ?
            t("assignment.rowAssigned", { email: holder })
          : assignable ?
            t("assignment.rowUnassigned")
          : null}
        </span>
      </summary>

      <div className="space-y-4 px-3 pb-4 pt-1">
        {/* NOT remounted when the sale redraws: the field holds what the buyer
            typed, and a remount would take the "saved" confirmation off the
            screen at the moment it was earned. */}
        {assignable ?
          <TicketAssignmentRow ticket={ticket} save={assign} />
        : null}
      </div>
    </details>
  );
}

"use client";

import { Button } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import { TicketAssignmentRow } from "@/components/ticket-assignment-row";
import { TicketQuestionRow } from "@/components/ticket-question-row";
import { visibleQuestionsOf, type AnswerBody } from "@/lib/answer-link";
import { apiErrorMessage } from "@/lib/api-errors";
import {
  hasAnswerLink,
  hasAnythingToShow,
  saleOutstandingCount,
  type BuyerTicket,
} from "@/lib/buyer-answers";
import {
  assignmentOffered,
  assignmentStateOf,
  assignmentTally,
  holderEmailOf,
  isOwnTicket,
  saleOffersAssignment,
  type AssignmentBody,
} from "@/lib/ticket-assignment";

/**
 * The buyer's Tickets on one of their own Ticket Sales, with a COPY-LINK control
 * per Ticket (#315, ADR 0044).
 *
 * THIS IS THE DISTRIBUTION SURFACE, and the reason the whole feature is shaped
 * the way it is. A buyer who bought four tickets knows one t-shirt size and not
 * the other three. The platform cannot write to the other three people, because
 * it holds no address for them and asks for none — that is ADR 0044's central
 * decision, taken so the platform never becomes a collector of third-party
 * contact details. So the buyer answers what they know here, and copies the rest
 * of the links into whatever they already use.
 *
 * IT APPEARS ON THE CONFIRMATION LINK'S PAGE AND IN THE CUSTOMER AREA BECAUSE
 * THOSE ARE ONE PAGE. A Confirmation Link redeems into a session narrowed to one
 * Ticket Sale and lands on the Customer Area, so a link session sees this on its
 * one purchase and a signed-in Customer sees it on each of theirs.
 *
 * IT IS FETCHED RATHER THAN SERVER-RENDERED WITH THE REST OF THE CARD, and that
 * is a deliberate cost. The Customer Area lists every purchase a person has ever
 * made; loading every sale's Tickets, questions and Answers to render a page
 * where most sales ask nothing would make the commonest page slower for the
 * people it has nothing to show. Fetching per card also means the Ticket
 * Question feature flag needs no second copy in this app: a deployment with the
 * feature dark answers 404 and this component draws nothing at all, which is
 * exactly what a build without the feature does (ADR 0045).
 *
 * NOTHING RENDERS UNTIL THERE IS SOMETHING TO SAY. No heading, no spinner, no
 * empty state — a sale whose Ticket Types ask nothing looks precisely as it did
 * before this feature existed, which is most sales on this platform.
 *
 * THE ANSWER LINKS NEVER REACH ANYBODY BUT THE BUYER. They arrive only on this
 * session-gated fetch, they are rendered only as the target of a copy button,
 * and they are deliberately absent from the Sale Confirmation: an Answer Link is
 * built to be forwarded and a receipt is built not to be, so a per-Ticket link
 * in that mail would make forwarding a t-shirt question and forwarding a receipt
 * the same gesture.
 *
 * IT NOW CARRIES A SECOND FEATURE ON THE SAME FETCH: the TICKET ASSIGNMENT
 * (#324, parent #322), which is what finally lets the buyer tell their four
 * Tickets apart — the Answer Links never could, since they disclose nothing by
 * design. It rides here because it rides on the same payload and belongs to the
 * same act: a buyer looking at "who is this one for" and "what has this one been
 * asked" is doing one job, and two sections would make them do it twice.
 *
 * THE TWO FEATURES HAVE SEPARATE FLAGS AND ARE READ SEPARATELY. Either half is
 * reason enough to draw the section and neither is reason to draw the other, so
 * a deployment with TICKET_ASSIGNMENT_ENABLED closed renders precisely what it
 * rendered before #324 — the API omits every assignment field, and the absence
 * is the only place this app learns the flag's state.
 *
 * A KNOWN COUPLING, and not one this ticket may fix: the GET behind this
 * component is still gated on TICKET_QUESTIONS_ENABLED, so a deployment with
 * assignment open and questions closed answers 404 here and shows nothing at
 * all. Seeing assignment state today therefore needs BOTH flags open. That gate
 * is the API's and belongs to whoever unpicks it.
 */
type BuyerTicketAnswersProps = {
  ticketSaleId: string;
};

export function BuyerTicketAnswers({ ticketSaleId }: BuyerTicketAnswersProps) {
  const t = useTranslations("customerArea");
  const [tickets, setTickets] = useState<BuyerTicket[] | null>(null);

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
    return () => {
      live = false;
    };
  }, [ticketSaleId]);

  // TWO FEATURES, TWO FLAGS, ONE SECTION. Either half is reason enough to draw
  // it, and neither is reason to draw the other: a deployment with Ticket
  // Questions and no assignment looks exactly as it did before #324, and a sale
  // whose Ticket Types ask nothing still shows who its Tickets are for.
  const questions = tickets !== null && hasAnythingToShow(tickets);
  const assignment = tickets !== null && saleOffersAssignment(tickets);
  if (tickets === null || (!questions && !assignment)) {
    return null;
  }

  const outstanding = saleOutstandingCount(tickets);
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
        {/* ONE LINE OF TALLY, never a paragraph per Ticket. The counts are read
            live at the moment somebody looks, which is the only moment they are
            true; and partial assignment counts and never warns — a sale of four
            with two addresses is finished as far as the buyer is concerned. */}
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
        {/* Said ONCE, for the whole sale, rather than once per Ticket: the
            other Tickets are their Holders' to answer, and the buyer's job
            here is to pass each one on. */}
        {questions && tickets.some((ticket) => !isOwnTicket(ticket)) ?
          <p className="text-muted-foreground text-sm">{t("answers.copyHint")}</p>
        : null}
      </div>

      <div className="divide-y rounded-md border">
        {tickets.map((ticket, index) => (
          <TicketBlock
            key={ticket.ticket_id}
            ticketSaleId={ticketSaleId}
            ticket={ticket}
            // "Ticket 2 of 4" is the whole of what can be said to tell two
            // identical tickets apart until an address is given: there are no
            // seat numbers, and the platform is never going to ask for them.
            position={index + 1}
            total={tickets.length}
            onSaved={setTickets}
          />
        ))}
      </div>
    </section>
  );
}

type TicketBlockProps = {
  ticketSaleId: string;
  ticket: BuyerTicket;
  position: number;
  total: number;
  onSaved: (tickets: BuyerTicket[]) => void;
};

function TicketBlock({ ticketSaleId, ticket, position, total, onSaved }: TicketBlockProps) {
  const t = useTranslations("customerArea");
  const errorCopy = useMessages().errors;
  const questions = visibleQuestionsOf(ticket.questions);
  const assignable = assignmentOffered(ticket);

  if (questions.length === 0 && !assignable) {
    // A Ticket whose Ticket Type asks nothing, on a sale where another Ticket
    // Type does, in a deployment where assignment is dark. Drawn as nothing
    // rather than as an empty block: it has no questions, so it needs no link
    // and there is nothing to pass on.
    return null;
  }

  async function save(questionId: string, body: AnswerBody): Promise<string | null> {
    try {
      const response = await fetch(
        `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}` +
          `/tickets/${encodeURIComponent(ticket.ticket_id)}` +
          `/answers/${encodeURIComponent(questionId)}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          // No token and no id in the body. The session cookie is the whole
          // credential on this surface, and the ids are in the address.
          body: JSON.stringify(body),
        },
      );
      const envelope = (await response.json()) as {
        data: BuyerTicket[] | null;
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error || !envelope.data) {
        return apiErrorMessage(errorCopy, envelope.error) ?? t("answers.saveFailed");
      }
      // The WHOLE sale comes back, so the outstanding count above and the row
      // that changed can never disagree — which is the shape of bug nobody
      // notices until an Organization asks why a buyer says they answered.
      onSaved(envelope.data);
      return null;
    } catch {
      return t("answers.networkFailed");
    }
  }

  /**
   * Naming the address for this Ticket (#324).
   *
   * A SEPARATE REQUEST FROM THE ANSWER SAVE and not a batched one, for the
   * reason each question carries its own button: the API's unit is one fact
   * about one Ticket, and a form that sent an address and three Answers together
   * would have to decide what to show when the address took and one Answer did
   * not. It shares the redraw — the whole sale comes back from both — so a
   * reassignment that cleared this Ticket's Answers is on the page the moment it
   * happens.
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
      onSaved(envelope.data);
      return null;
    } catch {
      return t("assignment.networkFailed");
    }
  }

  const own = isOwnTicket(ticket);
  const state = assignmentStateOf(ticket);
  const holder = holderEmailOf(ticket);
  const outstanding = ticket.outstanding_count;

  // ONE COLLAPSED ROW PER TICKET, opened on demand. A buyer of three usually
  // answers for themself and passes the other two on, so the row's default
  // reading is what they need to do that — who it is for, whether it still owes
  // anything, and the link to forward — and the fields wait behind the row.
  // The buyer's own Ticket starts OPEN: its questions are theirs to answer.
  return (
    // `open` is set only on the buyer's own row and left UNDEFINED on the rest:
    // a `false` here would be re-applied by React on every redraw, snapping a
    // row shut the moment a save inside it came back.
    <details className="group" open={own ? true : undefined}>
      <summary className="flex cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5 text-sm [&::-webkit-details-marker]:hidden">
        <span
          aria-hidden
          className="text-muted-foreground transition-transform group-open:rotate-90"
        >
          ▸
        </span>
        <span className="font-medium">
          {own ?
            t("answers.ownTicket")
          : t("answers.ticketHeading", { position, total })}
        </span>
        <span className="text-muted-foreground">{ticket.ticket_type_name}</span>
        <span className="text-muted-foreground min-w-0 flex-1 whitespace-nowrap">
          {own ? null
          : state === "accepted" ?
            t("assignment.rowAccepted", { email: holder })
          : state === "assigned" ?
            t("assignment.rowAssigned", { email: holder })
          : assignable ?
            t("assignment.rowUnassigned")
          : null}
        </span>
        {questions.length > 0 ?
          <span
            className={
              outstanding > 0 ?
                "ml-auto whitespace-nowrap text-sm font-medium"
              : "text-muted-foreground ml-auto whitespace-nowrap text-sm"
            }
          >
            {outstanding > 0 ?
              t("answers.rowOutstanding", { count: outstanding })
            : t("answers.rowAnswered")}
          </span>
        : null}
        {/* The link is about the QUESTIONS, so a Ticket asked nothing is offered
            none; and the buyer's own Ticket is offered none either — there is
            nobody to forward it to. */}
        {!own && questions.length > 0 && hasAnswerLink(ticket) ?
          <CopyAnswerLink link={ticket.answer_link} />
        : null}
      </summary>

      <div className="space-y-4 px-3 pb-4 pt-1">
        {/* ABOVE THE QUESTIONS, because it is what tells this Ticket from the
            other three. NOT remounted when the sale redraws: the field holds
            what the buyer typed, and a remount would take the "saved"
            confirmation off the screen at the moment it was earned. */}
        {/* The buyer's own Ticket: the address is theirs, so the field is
            offered only as "give it to someone else" rather than drawn open
            with their own email in it. */}
        {assignable && own ?
          <details className="text-sm">
            <summary className="text-muted-foreground cursor-pointer">
              {t("assignment.giveAway")}
            </summary>
            <div className="pt-3">
              <TicketAssignmentRow ticket={ticket} save={assign} />
            </div>
          </details>
        : assignable ?
          <TicketAssignmentRow ticket={ticket} save={assign} />
        : null}

        {questions.length > 0 && !hasAnswerLink(ticket) && !own && state !== "accepted" ?
          // No link to give. Either the Event has started or the purchase was
          // undone, and in both cases nothing anybody forwards would open.
          <p className="text-muted-foreground text-sm">{t("answers.linkClosed")}</p>
        : null}

        <div className="space-y-5">
          {questions.map((pair) => (
            <TicketQuestionRow
              key={pair.question.id}
              pair={pair}
              copy={{
                requiredMark: (question: string) => t("answers.requiredMark", { question }),
                save: t("answers.save"),
                saved: t("answers.saved"),
                nothingToSave: t("answers.nothingToSave"),
                retiredQuestion: t("answers.retiredQuestion"),
              }}
              save={(body) => save(pair.question.id, body)}
            />
          ))}
        </div>
      </div>
    </details>
  );
}

/**
 * The copy-link control: one press, one Ticket's link on the clipboard.
 *
 * A COPY BUTTON AND NOT A VISIBLE URL, because these links are long, signed and
 * unforgiving of a partial selection — a truncated one is refused by the
 * signature, which is correct and would look to the buyer like the platform
 * handing out broken links. Copying is also the actual gesture: this link's
 * whole life is being pasted into WhatsApp.
 *
 * IT DOES NOT MAIL ANYBODY, and cannot. The platform holds no holder addresses
 * and asks for none (ADR 0044); a "send to..." field here would be the
 * third-party collection problem the whole feature was shaped to avoid, wearing
 * a different coat.
 *
 * The fallback matters more than it looks. The Clipboard API needs a secure
 * context and a permission that a browser may refuse, and this control is read
 * on phones — so when the copy fails the link is REVEALED for a manual
 * selection rather than the press silently doing nothing.
 */
function CopyAnswerLink({ link }: { link: string }) {
  const t = useTranslations("customerArea");
  const [state, setState] = useState<"idle" | "copied" | "manual">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(link);
      setState("copied");
    } catch {
      setState("manual");
    }
  }

  return (
    <div
      className="space-y-2 text-right"
      // A press on the button must copy, not toggle the row it sits in.
      onClick={(event) => event.preventDefault()}
    >
      <Button size="sm" variant="outline" onClick={copy}>
        {state === "copied" ? t("answers.copied") : t("answers.copyLink")}
      </Button>
      {state === "manual" ?
        <p className="text-muted-foreground max-w-xs text-xs break-all">{link}</p>
      : null}
    </div>
  );
}

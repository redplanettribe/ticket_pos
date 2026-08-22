"use client";

import { Button } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import { TicketQuestionRow } from "@/components/ticket-question-row";
import { visibleQuestionsOf, type AnswerBody } from "@/lib/answer-link";
import { apiErrorMessage } from "@/lib/api-errors";
import {
  hasAnswerLink,
  hasAnythingToShow,
  saleOutstandingCount,
  type BuyerTicket,
} from "@/lib/buyer-answers";

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

  if (tickets === null || !hasAnythingToShow(tickets)) {
    return null;
  }

  const outstanding = saleOutstandingCount(tickets);

  return (
    <section className="mt-6 space-y-6 border-t pt-6">
      <div className="space-y-1">
        <h3 className="font-medium">{t("answers.title")}</h3>
        {/* The count is stated HERE and never in the Sale Confirmation. This
            page reads it live at the moment somebody looks, which is the only
            moment it is true; a number baked into an inbox is wrong as soon as
            one question is answered. */}
        <p className="text-muted-foreground text-sm">
          {outstanding > 0 ? t("answers.outstanding", { count: outstanding }) : t("answers.allDone")}
        </p>
      </div>

      {tickets.map((ticket, index) => (
        <TicketBlock
          key={ticket.ticket_id}
          ticketSaleId={ticketSaleId}
          ticket={ticket}
          // "Ticket 2 of 4" is the whole of what can be said to tell two
          // identical tickets apart: there are no seat numbers and no holder
          // names, and the platform is never going to ask for either.
          position={index + 1}
          total={tickets.length}
          onSaved={setTickets}
        />
      ))}
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

  if (questions.length === 0) {
    // A Ticket whose Ticket Type asks nothing, on a sale where another Ticket
    // Type does. Drawn as nothing rather than as an empty block: it has no
    // questions, so it needs no link and there is nothing to pass on.
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

  return (
    <div className="space-y-4 rounded-md border p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-medium">{t("answers.ticketHeading", { position, total })}</p>
          {/* The Ticket Type's name as the Organization wrote it. */}
          <p className="text-muted-foreground text-sm">{ticket.ticket_type_name}</p>
        </div>
        {hasAnswerLink(ticket) ?
          <CopyAnswerLink link={ticket.answer_link} />
        : null}
      </div>

      {hasAnswerLink(ticket) ?
        <p className="text-muted-foreground text-sm">{t("answers.copyHint")}</p>
      : // No link to give. Either the Event has started or the purchase was
        // undone, and in both cases nothing anybody forwards would open. Said
        // plainly, because a missing button with no explanation reads as a fault.
        <p className="text-muted-foreground text-sm">{t("answers.linkClosed")}</p>
      }

      <div className="space-y-6">
        {questions.map((pair) => (
          <TicketQuestionRow
            key={pair.question.id}
            pair={pair}
            copy={{
              requiredMark: t("answers.requiredMark"),
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
    <div className="space-y-2 text-right">
      <Button size="sm" variant="outline" onClick={copy}>
        {state === "copied" ? t("answers.copied") : t("answers.copyLink")}
      </Button>
      {state === "manual" ?
        <p className="text-muted-foreground max-w-xs text-xs break-all">{link}</p>
      : null}
    </div>
  );
}

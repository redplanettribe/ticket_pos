"use client";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Input,
  Label,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { TicketQuestionRow } from "@/components/ticket-question-row";
import { visibleQuestions } from "@/lib/answer-link";
import type { QuestionAnswer } from "@/lib/answer-link";
import {
  assignmentLinkFailure,
  holderNameIsGiven,
  type AssignmentLinkView,
} from "@/lib/assignment-link";
import { apiErrorMessage } from "@/lib/api-errors";

/**
 * Accepting a ticket somebody bought for you (#325, parent #322, ADR 0046).
 *
 * THE PRESS IS THE ACT, AND OPENING THE PAGE IS NOT — the sentence the consent
 * confirmation page turns on, and it carries more weight here than anywhere.
 * Mail security scanners open every link in every message before a human sees
 * it; a page that accepted on being fetched would mint a VERIFIED CUSTOMER
 * nobody proved and hand an email address to an Organization on the strength of
 * a robot's HTTP request. So the page renders, nothing has happened, and the
 * button below sends a POST.
 *
 * IT SHOWS NOTHING BEFORE THE PRESS, and that is not shyness. Before the press
 * this reader has proved nothing, so there is nothing they are entitled to be
 * shown — and a page that displayed a known Customer's name before the press
 * would be an oracle for whether an address is registered, which ADR 0035 is
 * explicit about avoiding. What tells them what they have is the MAIL, which
 * names the Event and the Ticket Type; what this page adds is the act.
 *
 * IT ASKS FOR A NAME AND THE QUESTIONS AND NOTHING ELSE. No password, no
 * passcode, no phone, and NEVER A TAX ID — a fact about the sale's buyer, never
 * about an attendee. Anyone adding a field here is turning a favour into a
 * registration.
 *
 * THE READER IS TOLD WHAT ACCEPTING DISCLOSES BEFORE THEY PRESS: their address
 * goes to the Organization running the Event. The mail says it too; saying it
 * again at the moment of the press is the honest place to say it.
 */
type AssignmentLinkAcceptProps = {
  /** The signed token the link carried. Never inspected here; only relayed. */
  token: string;
};

type Envelope = {
  data: AssignmentLinkView | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export function AssignmentLinkAccept({ token }: AssignmentLinkAcceptProps) {
  const t = useTranslations("assignmentLink");
  const errorCopy = useMessages().errors;

  const [busy, setBusy] = useState(false);
  const [view, setView] = useState<AssignmentLinkView | null>(null);
  const [questions, setQuestions] = useState<QuestionAnswer[]>([]);
  const [failure, setFailure] = useState<string | null>(null);

  // The name fields, seeded from the press's answer. They are STATE AND NOT
  // PROPS because the prefill only exists after the press — there is no earlier
  // moment at which this component could have been given one.
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [nameSaved, setNameSaved] = useState(false);

  function adopt(accepted: AssignmentLinkView) {
    setView(accepted);
    setQuestions(visibleQuestions(accepted));
    setFirstName(accepted.holder_first_name);
    setLastName(accepted.holder_last_name);
  }

  async function accept() {
    setBusy(true);
    setFailure(null);
    try {
      const response = await fetch("/api/assignment-link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        // The token travels in the BODY on the browser leg too: a credential in
        // a URL ends up in a Referer header on the way to wherever the reader
        // goes next, and this one asserts who somebody is.
        body: JSON.stringify({ token }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error || !envelope.data) {
        // The API's code chooses the copy, so a reassigned ticket, a reversed
        // sale and a forgery all read as "this link doesn't work" — none of them
        // says what the buyer did.
        const key = assignmentLinkFailure(envelope.error?.code);
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t(`${key}Description`));
        return;
      }
      adopt(envelope.data);
    } catch {
      setFailure(t("networkFailed"));
    } finally {
      setBusy(false);
    }
  }

  async function saveName() {
    setBusy(true);
    setFailure(null);
    setNameSaved(false);
    try {
      const response = await fetch("/api/assignment-link/name", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, first_name: firstName, last_name: lastName }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error || !envelope.data) {
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t("saveFailed"));
        return;
      }
      adopt(envelope.data);
      setNameSaved(true);
    } catch {
      setFailure(t("networkFailed"));
    } finally {
      setBusy(false);
    }
  }

  if (!view) {
    return (
      <div className="space-y-4">
        {failure ? (
          <Alert variant="destructive">
            <AlertTitle>{t("failedTitle")}</AlertTitle>
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        ) : null}
        {/* Said BEFORE the button and never after it. Somebody deciding whether
            to press is entitled to know what pressing discloses, and a sentence
            below the control is a sentence half of them meet too late. */}
        <p className="text-muted-foreground text-sm">{t("disclosure")}</p>
        <Button onClick={accept} disabled={busy}>
          {t("accept")}
        </Button>
        {/* Ignoring the mail IS the decline — there is no decline button here or
            anywhere, deliberately (ADR 0046). This is where that is said. */}
        <p className="text-muted-foreground text-sm">{t("ignoring")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <Alert>
        <AlertTitle>{t("acceptedTitle")}</AlertTitle>
        <AlertDescription>{t("acceptedDescription")}</AlertDescription>
      </Alert>

      {failure ? (
        <Alert variant="destructive">
          <AlertTitle>{t("failedTitle")}</AlertTitle>
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      ) : null}

      <section className="space-y-4">
        <div className="space-y-1">
          <h2 className="text-base font-medium">{t("nameTitle")}</h2>
          <p className="text-muted-foreground text-sm">{t("nameDescription")}</p>
        </div>
        {/* TWO FIELDS, STORED SEPARATELY (ADR 0005), and no third field of any
            kind. There is no Tax ID here and there must never be one. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="holder-first-name">{t("firstName")}</Label>
            <Input
              id="holder-first-name"
              value={firstName}
              autoComplete="given-name"
              onChange={(event) => {
                setFirstName(event.target.value);
                setNameSaved(false);
              }}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="holder-last-name">{t("lastName")}</Label>
            <Input
              id="holder-last-name"
              value={lastName}
              autoComplete="family-name"
              onChange={(event) => {
                setLastName(event.target.value);
                setNameSaved(false);
              }}
            />
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={saveName} disabled={busy || !holderNameIsGiven(firstName, lastName)}>
            {t("saveName")}
          </Button>
          {nameSaved ? <span className="text-muted-foreground text-sm">{t("saved")}</span> : null}
        </div>
      </section>

      {questions.length > 0 ? (
        <section className="space-y-8">
          <div className="space-y-1">
            <h2 className="text-base font-medium">{t("questionsTitle")}</h2>
            <p className="text-muted-foreground text-sm">{t("questionsDescription")}</p>
          </div>
          {questions.map((pair) => (
            <TicketQuestionRow
              key={pair.question.id}
              pair={pair}
              copy={{
                requiredMark: (question: string) => t("requiredMark", { question }),
                save: t("save"),
                saved: t("saved"),
                nothingToSave: t("nothingToSave"),
                retiredQuestion: t("retiredQuestion"),
              }}
              // THE SAME SHARED ROW THE ANSWER LINK'S PAGE USES, with this
              // surface's own credential. The row knows how to turn a form into
              // a body and nothing about how it is addressed, which is what
              // keeps four answering surfaces meaning one thing by an Answer.
              save={async (body) => {
                try {
                  const response = await fetch(
                    `/api/assignment-link/questions/${pair.question.id}`,
                    {
                      method: "PUT",
                      headers: { "Content-Type": "application/json" },
                      body: JSON.stringify({ token, ...body }),
                    },
                  );
                  const envelope = (await response.json()) as Envelope;
                  if (!response.ok || envelope.error || !envelope.data) {
                    return apiErrorMessage(errorCopy, envelope.error) ?? t("saveFailed");
                  }
                  // A save answers with the WHOLE Ticket, so the list is replaced
                  // from one payload rather than patched question by question.
                  adopt(envelope.data);
                  return null;
                } catch {
                  return t("networkFailed");
                }
              }}
            />
          ))}
        </section>
      ) : null}
    </div>
  );
}

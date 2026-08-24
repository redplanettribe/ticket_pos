"use client";

import { useMessages, useTranslations } from "next-intl";
import { FormEvent, useCallback, useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  Label,
  Skeleton,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, fetchEventsJSON } from "@/lib/events-api";
import {
  MAX_TICKET_QUESTION_LABEL_LENGTH,
  MAX_TICKET_QUESTION_OPTIONS,
  MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH,
  TICKET_QUESTION_KINDS,
  TICKET_QUESTION_KIND_KEYS,
  TICKET_QUESTION_REVIEW_STATUS_KEYS,
  canAddOption,
  canRetireOption,
  isValidLabel,
  liveOptions,
  liveQuestions,
  moveQuestion,
  offersOptions,
  retiredOptions,
  retiredQuestions,
  reviewReason,
  type Reviewed,
  type TicketQuestion,
  type TicketQuestionKind,
} from "@/lib/ticket-questions";

/**
 * The row's review state, read-only (ADR 0056, #405): a badge with the state's
 * word, and the Operator's reason where a verdict carries one. Nothing here
 * submits or changes a state — that is the Question Review's (#406) and the
 * Operator's (#407).
 */
function ReviewState({ item, t }: { item: Reviewed; t: ReturnType<typeof useTranslations> }) {
  const reason = reviewReason(item);
  return (
    <>
      <Badge variant={item.review_status === "approved" ? "default" : "outline"}>
        {t(TICKET_QUESTION_REVIEW_STATUS_KEYS[item.review_status])}
      </Badge>
      {reason ? (
        <span className="text-xs text-muted-foreground">
          {reason.kind === "revocation"
            ? t("questionReviewRevocationReason", { reason: reason.reason })
            : t("questionReviewRefusalReason", { reason: reason.reason })}
        </span>
      ) : null}
    </>
  );
}

const SELECT_CLASS = "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

type TicketQuestionsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventId: string;
  ticketTypeId: string;
  /** The Ticket Type's own name, drawn as coined in both languages. */
  ticketTypeName: string;
};

type QuestionFormState = {
  label: string;
  kind: TicketQuestionKind;
  required: boolean;
  /** The Options a choice question is being created with, as typed. */
  optionLabels: string[];
};

const emptyQuestionForm: QuestionFormState = {
  label: "",
  kind: "short_text",
  required: false,
  optionLabels: [""],
};

/**
 * The Ticket Question authoring surface: what an Organization wants to know
 * about whoever will hold one of this Ticket Type's tickets (#309).
 *
 * It renders only where the feature flag is on — the caller decides that from
 * the Event payload's `ticket_questions_enabled` — and the API refuses every one
 * of these requests with a 404 while the flag is off, so a stale page cannot
 * write anything (ADR 0045).
 *
 * The warning at the top is not decoration. ADR 0045 requires an Organization to
 * be told what it is asking for at the moment it authors the question, because
 * the platform cannot tell a t-shirt size from an allergy once the field is a
 * free-text box, and the Organization is the party that chose to ask.
 */
export function TicketQuestionsDialog({
  open,
  onOpenChange,
  eventId,
  ticketTypeId,
  ticketTypeName,
}: TicketQuestionsDialogProps) {
  const t = useTranslations("ticketTypes");
  const errorCopy = useMessages().errors;

  const [loading, setLoading] = useState(true);
  const [questions, setQuestions] = useState<TicketQuestion[]>([]);
  const [form, setForm] = useState<QuestionFormState>(emptyQuestionForm);
  const [saving, setSaving] = useState(false);
  // A rejection the organizer must act on stays next to the form that caused it
  // rather than in a toast that scrolls away.
  const [formError, setFormError] = useState<string | null>(null);
  const [newOptionLabels, setNewOptionLabels] = useState<Record<string, string>>({});

  const basePath = `/api/events/${eventId}/ticket-types/${ticketTypeId}/questions`;

  const reportFailure = useCallback(
    (failure: unknown) => {
      const message =
        apiErrorMessage(errorCopy, failure instanceof ApiError ? failure : null) ??
        t("questionsLoadFailed");
      toast.error(message);
      return message;
    },
    [errorCopy, t],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setQuestions(await fetchEventsJSON<TicketQuestion[]>(basePath));
    } catch (failure) {
      reportFailure(failure);
    } finally {
      setLoading(false);
    }
  }, [basePath, reportFailure]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setForm(emptyQuestionForm);
    setFormError(null);
    setNewOptionLabels({});
    void load();
  }, [open, load]);

  // Every write answers with the whole question or the whole list, so the editor
  // never reassembles state from a fragment — it re-reads, which is also what
  // keeps a concurrent edit from another tab from being papered over.
  const mutate = useCallback(
    async (path: string, init: RequestInit) => {
      try {
        await fetchEventsJSON<unknown>(path, init);
        await load();
        return true;
      } catch (failure) {
        reportFailure(failure);
        return false;
      }
    },
    [load, reportFailure],
  );

  const kindOffersOptions = offersOptions(form.kind);

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    setFormError(null);

    if (!isValidLabel(form.label, MAX_TICKET_QUESTION_LABEL_LENGTH)) {
      setFormError(t("questionLabelRequired"));
      return;
    }

    const optionLabels = kindOffersOptions
      ? form.optionLabels.map((label) => label.trim()).filter((label) => label.length > 0)
      : [];
    if (kindOffersOptions && optionLabels.length === 0) {
      setFormError(t("questionOptionsRequired"));
      return;
    }
    if (
      optionLabels.some((label) => !isValidLabel(label, MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH))
    ) {
      setFormError(t("questionOptionTooLong", { max: MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH }));
      return;
    }

    setSaving(true);
    const created = await mutate(basePath, {
      method: "POST",
      body: JSON.stringify({
        label: form.label,
        kind: form.kind,
        required: form.required,
        // v1 always writes at_checkout, and offers no control for it. The field
        // travels so the API is never guessing what the editor meant.
        timing: "at_checkout",
        option_labels: optionLabels,
      }),
    });
    setSaving(false);
    if (created) {
      setForm(emptyQuestionForm);
    }
  }

  async function renameQuestion(question: TicketQuestion, label: string) {
    if (label.trim() === question.label || !isValidLabel(label, MAX_TICKET_QUESTION_LABEL_LENGTH)) {
      return;
    }
    await mutate(`${basePath}/${question.id}`, {
      method: "PATCH",
      body: JSON.stringify({
        label,
        kind: question.kind,
        required: question.required,
        timing: question.timing,
      }),
    });
  }

  async function toggleRequired(question: TicketQuestion) {
    await mutate(`${basePath}/${question.id}`, {
      method: "PATCH",
      body: JSON.stringify({
        label: question.label,
        kind: question.kind,
        required: !question.required,
        timing: question.timing,
      }),
    });
  }

  async function move(question: TicketQuestion, direction: -1 | 1) {
    await mutate(`${basePath}/order`, {
      method: "PUT",
      body: JSON.stringify({ question_ids: moveQuestion(questions, question.id, direction) }),
    });
  }

  async function retireQuestion(question: TicketQuestion) {
    await mutate(`${basePath}/${question.id}`, { method: "DELETE" });
  }

  async function addOption(question: TicketQuestion) {
    const label = (newOptionLabels[question.id] ?? "").trim();
    if (!isValidLabel(label, MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH)) {
      return;
    }
    const added = await mutate(`${basePath}/${question.id}/options`, {
      method: "POST",
      body: JSON.stringify({ label }),
    });
    if (added) {
      setNewOptionLabels((current) => ({ ...current, [question.id]: "" }));
    }
  }

  async function renameOption(question: TicketQuestion, optionId: string, label: string) {
    if (!isValidLabel(label, MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH)) {
      return;
    }
    await mutate(`${basePath}/${question.id}/options/${optionId}`, {
      method: "PATCH",
      body: JSON.stringify({ label }),
    });
  }

  async function retireOption(question: TicketQuestion, optionId: string) {
    await mutate(`${basePath}/${question.id}/options/${optionId}`, { method: "DELETE" });
  }

  const live = liveQuestions(questions);
  const retired = retiredQuestions(questions);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          {/* The Ticket Type's name is the Organization's own words: data, not
              copy, and untranslated in both languages. */}
          <DialogTitle>{t("questionsTitle", { name: ticketTypeName })}</DialogTitle>
          <DialogDescription>{t("questionsDescription")}</DialogDescription>
        </DialogHeader>

        {/* ADR 0045: the Organization is warned, at authoring time, about what
            it is asking for. It is the party that chose to ask, and the platform
            cannot tell a t-shirt size from an allergy once the box is typed in. */}
        <Alert variant="warning">
          <AlertTitle>{t("questionsWarningTitle")}</AlertTitle>
          <AlertDescription>{t("questionsWarningBody")}</AlertDescription>
        </Alert>

        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : (
          <ul className="space-y-3">
            {live.map((question, index) => (
              <li key={question.id} className="rounded-md border p-3">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 flex-1 space-y-2">
                    <Input
                      aria-label={t("questionLabel")}
                      defaultValue={question.label}
                      maxLength={MAX_TICKET_QUESTION_LABEL_LENGTH}
                      onBlur={(event) => void renameQuestion(question, event.target.value)}
                    />
                    <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                      <Badge variant="secondary">{t(TICKET_QUESTION_KIND_KEYS[question.kind])}</Badge>
                      <ReviewState item={question} t={t} />
                      <label className="flex items-center gap-1">
                        <input
                          type="checkbox"
                          checked={question.required}
                          onChange={() => void toggleRequired(question)}
                        />
                        {t("questionRequired")}
                      </label>
                    </div>
                    {question.review_status !== "approved" ? (
                      <p className="text-xs text-muted-foreground">{t("questionReviewNotAsked")}</p>
                    ) : null}
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={index === 0}
                      aria-label={t("questionMoveUp", { label: question.label })}
                      onClick={() => void move(question, -1)}
                    >
                      <span aria-hidden>↑</span>
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={index === live.length - 1}
                      aria-label={t("questionMoveDown", { label: question.label })}
                      onClick={() => void move(question, 1)}
                    >
                      <span aria-hidden>↓</span>
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => void retireQuestion(question)}
                    >
                      {t("questionRetire")}
                    </Button>
                  </div>
                </div>

                {offersOptions(question.kind) ? (
                  <div className="mt-3 space-y-2 border-t pt-3">
                    {liveOptions(question).map((option) => (
                      <div key={option.id} className="flex items-center gap-2">
                        <Input
                          aria-label={t("questionOptionLabel")}
                          defaultValue={option.label}
                          maxLength={MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH}
                          onBlur={(event) =>
                            void renameOption(question, option.id, event.target.value)
                          }
                        />
                        {option.review_status !== "approved" ? (
                          <ReviewState item={option} t={t} />
                        ) : null}
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          disabled={!canRetireOption(question)}
                          onClick={() => void retireOption(question, option.id)}
                        >
                          {t("questionOptionRetire")}
                        </Button>
                      </div>
                    ))}

                    {/* Retired Options are shown rather than hidden: they are
                        kept on the Tickets that chose them and keep their column
                        in the Sales Export, and a row that vanished would look
                        like the platform had lost it. */}
                    {retiredOptions(question).map((option) => (
                      <p key={option.id} className="text-sm text-muted-foreground">
                        <span className="line-through">{option.label}</span>{" "}
                        <Badge variant="secondary">{t("questionOptionRetired")}</Badge>
                      </p>
                    ))}

                    <div className="flex items-center gap-2">
                      <Input
                        aria-label={t("questionOptionAdd")}
                        placeholder={t("questionOptionAdd")}
                        maxLength={MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH}
                        value={newOptionLabels[question.id] ?? ""}
                        disabled={!canAddOption(question)}
                        onChange={(event) =>
                          setNewOptionLabels((current) => ({
                            ...current,
                            [question.id]: event.target.value,
                          }))
                        }
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={!canAddOption(question)}
                        onClick={() => void addOption(question)}
                      >
                        {t("questionOptionAddAction")}
                      </Button>
                    </div>
                    {canAddOption(question) ? null : (
                      <p className="text-sm text-muted-foreground">
                        {t("questionOptionCapReached", { max: MAX_TICKET_QUESTION_OPTIONS })}
                      </p>
                    )}
                  </div>
                ) : null}
              </li>
            ))}

            {live.length === 0 ? (
              <li className="text-sm text-muted-foreground">{t("questionsEmpty")}</li>
            ) : null}

            {/* Retired questions, for the same reason retired Options are shown:
                what has already been answered still reads, on the Ticket and in
                the export. */}
            {retired.map((question) => (
              <li key={question.id} className="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
                <span className="line-through">{question.label}</span>{" "}
                <Badge variant="secondary">{t("questionRetired")}</Badge>
                {reviewReason(question) ? <ReviewState item={question} t={t} /> : null}
              </li>
            ))}
          </ul>
        )}

        <form className="space-y-3 border-t pt-4" onSubmit={handleCreate}>
          <FormField id="ticket-question-label" label={t("questionLabel")}>
            <Input
              id="ticket-question-label"
              value={form.label}
              maxLength={MAX_TICKET_QUESTION_LABEL_LENGTH}
              onChange={(event) => setForm((current) => ({ ...current, label: event.target.value }))}
            />
          </FormField>

          <FormField id="ticket-question-kind" label={t("questionKind")}>
            <select
              id="ticket-question-kind"
              className={SELECT_CLASS}
              value={form.kind}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  kind: event.target.value as TicketQuestionKind,
                }))
              }
            >
              {TICKET_QUESTION_KINDS.map((kind) => (
                <option key={kind} value={kind}>
                  {t(TICKET_QUESTION_KIND_KEYS[kind])}
                </option>
              ))}
            </select>
          </FormField>

          {kindOffersOptions ? (
            <div className="space-y-2">
              <Label>{t("questionOptions")}</Label>
              {form.optionLabels.map((label, index) => (
                <Input
                  // The row's index IS its identity here: these Options do not
                  // exist yet, so there is nothing else to key on until the
                  // question is created and the API hands each one an id.
                  key={index}
                  aria-label={t("questionOptionLabel")}
                  value={label}
                  maxLength={MAX_TICKET_QUESTION_OPTION_LABEL_LENGTH}
                  onChange={(event) =>
                    setForm((current) => {
                      const optionLabels = [...current.optionLabels];
                      optionLabels[index] = event.target.value;
                      return { ...current, optionLabels };
                    })
                  }
                />
              ))}
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={form.optionLabels.length >= MAX_TICKET_QUESTION_OPTIONS}
                onClick={() =>
                  setForm((current) => ({
                    ...current,
                    optionLabels: [...current.optionLabels, ""],
                  }))
                }
              >
                {t("questionOptionAddAction")}
              </Button>
            </div>
          ) : null}

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={form.required}
              onChange={(event) =>
                setForm((current) => ({ ...current, required: event.target.checked }))
              }
            />
            {t("questionRequired")}
          </label>
          {/* Said in full rather than left to the word "required" to imply: a
              required Ticket Question refuses nothing, ever. */}
          <p className="text-sm text-muted-foreground">{t("questionRequiredHint")}</p>

          {formError ? <p className="text-sm text-destructive">{formError}</p> : null}

          <DialogFooter>
            <Button type="submit" disabled={saving}>
              {saving ? t("questionAdding") : t("questionAdd")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

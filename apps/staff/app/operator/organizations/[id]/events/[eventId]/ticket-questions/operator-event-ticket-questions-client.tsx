"use client";

import { useCallback, useEffect, useState } from "react";

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
  PageHeader,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import {
  type OperatorTicketQuestionRow,
  fetchOperatorEventTicketQuestions,
  fetchOperatorOrganization,
  revokeOperatorTicketQuestion,
} from "@/lib/operator-api";
import { RESOLUTION_REASON_MAX_LENGTH, resolutionReasonProblem } from "@/lib/payout-requests";
import {
  TICKET_QUESTION_KIND_KEYS,
  TICKET_QUESTION_REVIEW_STATUS_KEYS,
  isAsked,
} from "@/lib/ticket-questions";

type OperatorEventTicketQuestionsClientProps = {
  organizationId: string;
  eventId: string;
};

/**
 * The Operator's view of what one Event asks, and the Revocation (#410,
 * ADR 0056).
 *
 * Every question is listed — draft, under review, approved, refused, retired —
 * because the Operator is the party answerable for what the platform collects
 * and a view that hid the rows not currently asked would hide exactly the ones
 * about to be. Only an approved, live question offers Revoke: that is the only
 * state with an approval to take back, and the API refuses the rest.
 *
 * The reason goes through the same rule as a Payout Request decline's —
 * required, trimmed, 500 characters — because it is the same kind of sentence:
 * the Operator's own words to one Organization, quoted verbatim in a mail to
 * every one of its Org Admins and shown beside the question in their editor.
 */
export function OperatorEventTicketQuestionsClient({
  organizationId,
  eventId,
}: OperatorEventTicketQuestionsClientProps) {
  const t = useTranslations("operator");
  // The kind names and review states are the Ticket Type editor's vocabulary,
  // and the Operator's screen must use the same words the Organization reads.
  const tTicketTypes = useTranslations("ticketTypes");
  const errorCopy = useMessages().errors;

  const [questions, setQuestions] = useState<OperatorTicketQuestionRow[] | null>(null);
  const [organizationName, setOrganizationName] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The question the Operator has pressed Revoke on, with the reason they are
  // typing. One at a time: a Revocation is a judgement about one question.
  const [revoking, setRevoking] = useState<OperatorTicketQuestionRow | null>(null);
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [rows, organization] = await Promise.all([
        fetchOperatorEventTicketQuestions(eventId),
        fetchOperatorOrganization(organizationId),
      ]);
      setQuestions(rows);
      setOrganizationName(organization.organization.name);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("questionsNotFound"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, organizationId]);

  useEffect(() => {
    void load();
  }, [load]);

  function startRevoking(question: OperatorTicketQuestionRow) {
    setRevoking(question);
    setReason("");
    setReasonError(null);
    setConfirming(false);
  }

  function handleRevoke(event: React.FormEvent) {
    event.preventDefault();
    const problem = resolutionReasonProblem(reason);
    setReasonError(
      problem === "missing"
        ? t("revokeReasonRequired")
        : problem === "too_long"
          ? t("reasonTooLong", { max: RESOLUTION_REASON_MAX_LENGTH })
          : null,
    );
    if (problem) {
      return;
    }
    setConfirming(true);
  }

  async function submitRevocation() {
    if (!revoking) {
      return;
    }
    setSubmitting(true);
    try {
      await revokeOperatorTicketQuestion(revoking.id, reason.trim());
      toast.success(t("revoked"));
      setRevoking(null);
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("revokeFailed"),
      );
    } finally {
      setConfirming(false);
      setSubmitting(false);
      await load();
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("questionsLoading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (error || !questions) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("questionsLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("questionsNotFound")}</AlertDescription>
      </Alert>
    );
  }

  // Grouped by Ticket Type in the order the API lists them, which is the order
  // the Ticket Types were created.
  const ticketTypes: { id: string; name: string; questions: OperatorTicketQuestionRow[] }[] = [];
  for (const question of questions) {
    const last = ticketTypes[ticketTypes.length - 1];
    if (last && last.id === question.ticket_type_id) {
      last.questions.push(question);
    } else {
      ticketTypes.push({
        id: question.ticket_type_id,
        name: question.ticket_type_name,
        questions: [question],
      });
    }
  }

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbOrganizations"), href: "/operator/organizations" },
          { label: organizationName, href: `/operator/organizations/${organizationId}` },
          { label: t("breadcrumbTicketQuestions") },
        ]}
      />

      <PageHeader title={t("questionsTitle")} description={t("questionsDescription")} />

      {ticketTypes.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("questionsEmpty")}</p>
      ) : null}

      {ticketTypes.map((ticketType) => (
        <Card key={ticketType.id}>
          <CardHeader>
            {/* A Ticket Type's name is data and reads as coined. */}
            <CardTitle>{ticketType.name}</CardTitle>
            <CardDescription>{t("questionsPerTicketType")}</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="space-y-3">
              {ticketType.questions.map((question) => {
                const asked = isAsked(question);
                return (
                  <li
                    key={question.id}
                    className={
                      question.retired
                        ? "rounded-md border border-dashed p-3 text-muted-foreground"
                        : "rounded-md border p-3"
                    }
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="space-y-1">
                        {/* The Organization's own words, as coined. */}
                        <p className={question.retired ? "line-through" : "font-medium"}>
                          {question.label}
                        </p>
                        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                          <Badge variant="secondary">
                            {tTicketTypes(TICKET_QUESTION_KIND_KEYS[question.kind])}
                          </Badge>
                          <Badge variant={question.review_status === "approved" ? "default" : "outline"}>
                            {tTicketTypes(TICKET_QUESTION_REVIEW_STATUS_KEYS[question.review_status])}
                          </Badge>
                          {question.retired ? (
                            <Badge variant="secondary">{t("questionRetired")}</Badge>
                          ) : null}
                          <span>
                            {question.required ? t("questionRequired") : t("questionOptional")}
                          </span>
                        </div>
                        {question.options.length > 0 ? (
                          <p className="text-xs text-muted-foreground">
                            {t("questionOptions", {
                              options: question.options
                                .filter((option) => !option.retired)
                                .map((option) => option.label)
                                .join(", "),
                            })}
                          </p>
                        ) : null}
                        {question.approved_by && !question.revocation_reason ? (
                          <p className="text-xs text-muted-foreground">
                            {t("questionApprovedBy", { who: question.approved_by })}
                          </p>
                        ) : null}
                        {question.refusal_reason ? (
                          <p className="text-xs text-muted-foreground">
                            {t("questionRefusedReason", { reason: question.refusal_reason })}
                          </p>
                        ) : null}
                        {question.revocation_reason ? (
                          <p className="text-xs text-muted-foreground">
                            {t("questionRevokedReason", { reason: question.revocation_reason })}
                          </p>
                        ) : null}
                      </div>
                      {/* Only a question with an approval to take back. */}
                      {asked ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() => startRevoking(question)}
                        >
                          {t("revoke")}
                        </Button>
                      ) : null}
                    </div>
                  </li>
                );
              })}
            </ul>
          </CardContent>
        </Card>
      ))}

      {/* The reason, then a second look: a Revocation is final and its reason
          reaches every Org Admin word for word. */}
      <Dialog
        open={revoking !== null && !confirming}
        onOpenChange={(open) => !open && !submitting && setRevoking(null)}
      >
        <DialogContent>
          <form className="space-y-4" onSubmit={handleRevoke}>
            <DialogHeader>
              <DialogTitle>{t("revokeDialogTitle")}</DialogTitle>
              <DialogDescription>{revoking?.label}</DialogDescription>
            </DialogHeader>
            <FormField
              id="revoke-reason"
              label={t("revokeReasonLabel")}
              error={reasonError}
              description={t("revokeReasonDescription")}
            >
              <Textarea
                id="revoke-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                maxLength={RESOLUTION_REASON_MAX_LENGTH}
                rows={3}
                placeholder={t("revokeReasonPlaceholder")}
                disabled={submitting}
              />
            </FormField>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setRevoking(null)}>
                {t("cancel")}
              </Button>
              <Button type="submit" variant="destructive">
                {t("revoke")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={confirming} onOpenChange={(open) => !open && !submitting && setConfirming(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("revokeDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("revokeDialogBody", {
                label: revoking?.label ?? "",
                organization: organizationName,
              })}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm">{reason.trim()}</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)} disabled={submitting}>
              {t("goBack")}
            </Button>
            <Button variant="destructive" onClick={() => void submitRevocation()} disabled={submitting}>
              {submitting ? t("revoking") : t("revoke")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

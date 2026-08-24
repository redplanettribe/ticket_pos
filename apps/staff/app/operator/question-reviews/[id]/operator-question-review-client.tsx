"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
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
  PageHeader,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDate, formatDateTime } from "@/lib/format";
import {
  type OperatorQuestionReviewItem,
  type OperatorQuestionReviewRow,
  answerOperatorQuestionReview,
  fetchOperatorQuestionReview,
} from "@/lib/operator-api";
import {
  EMPTY_VERDICT,
  type AnswerProblem,
  type VerdictDraft,
  answerBody,
  answerProblem,
  withReason,
  withVerdict,
} from "@/lib/operator-question-reviews";
import { RESOLUTION_REASON_MAX_LENGTH } from "@/lib/payout-requests";
import { QUESTION_REVIEW_STATUS_KEYS } from "@/lib/question-reviews";
import { TICKET_QUESTION_KIND_KEYS } from "@/lib/ticket-questions";

type OperatorQuestionReviewClientProps = {
  reviewId: string;
};

type Drafts = Record<string, VerdictDraft | undefined>;

/**
 * One Question Review, and the Operator's answer (#407, ADR 0056).
 *
 * The answer is ONE ACT WITH A VERDICT PER ITEM: every question and Option the
 * Review carries is approved, or refused with a reason the organization reads
 * — so a refusal over one question of six names the one. The form refuses the
 * same holes the API refuses, before sending: an item without a verdict, a
 * refusal without a reason. Refusing a question carries the verdict and reason
 * to its Options, which are not offered either way, so the Operator is not
 * asked six times about Chicken; an Option's own verdict moves nothing else.
 *
 * Once answered, withdrawn or lapsed the page is a record: the verdicts and
 * reasons as given, and no controls. A Review reads lapsed the moment its
 * event has started, decided by the API on this read.
 */
export function OperatorQuestionReviewClient({ reviewId }: OperatorQuestionReviewClientProps) {
  const t = useTranslations("operator");
  // The kind names and review states are the Ticket Type editor's vocabulary,
  // and the Operator's screen must use the same words the Organization reads.
  const tTicketTypes = useTranslations("ticketTypes");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [row, setRow] = useState<OperatorQuestionReviewRow | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [drafts, setDrafts] = useState<Drafts>({});
  const [problem, setProblem] = useState<AnswerProblem | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotFound(false);
    try {
      const detail = await fetchOperatorQuestionReview(reviewId);
      setRow(detail);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else if (loadError instanceof ApiError && loadError.code === "QUESTION_REVIEW_NOT_FOUND") {
        setNotFound(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("reviewLoadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reviewId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleAnswer(event: React.FormEvent) {
    event.preventDefault();
    if (!row) {
      return;
    }
    const items = row.review.items;
    const hole = answerProblem(items, drafts);
    setProblem(hole);
    setSubmitError(null);
    if (hole) {
      return;
    }
    setSubmitting(true);
    try {
      const answered = await answerOperatorQuestionReview(reviewId, answerBody(items, drafts));
      setRow(answered);
      toast.success(t("reviewAnswered"));
    } catch (answerError) {
      setSubmitError(
        (answerError instanceof ApiError ? apiErrorMessage(errorCopy, answerError) : null) ??
          t("reviewAnswerFailed"),
      );
      // The state moved under us — a colleague answered, the organization
      // withdrew, or the event started. Read it again rather than argue.
      if (answerError instanceof ApiError && answerError.code === "QUESTION_REVIEW_NOT_OUTSTANDING") {
        void load();
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("reviewLoading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (notFound || !row) {
    return (
      <div className="space-y-4">
        <Alert>
          <AlertTitle>{t("reviewNotFoundTitle")}</AlertTitle>
          <AlertDescription>{error ?? t("reviewNotFound")}</AlertDescription>
        </Alert>
        <Button asChild variant="outline">
          <Link href="/operator/question-reviews">{t("goBack")}</Link>
        </Button>
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("reviewLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  const { review, organization, event: reviewEvent } = row;
  const outstanding = review.status === "outstanding";
  const startsAt = reviewEvent.starts_at;
  const items = review.items;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbQuestionReviews"), href: "/operator/question-reviews" },
          { label: reviewEvent.name },
        ]}
      />

      <PageHeader
        title={reviewEvent.name}
        description={t("reviewDetailDescription", { organization: organization.name })}
      />

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <CardTitle>{t("reviewAskTitle")}</CardTitle>
            <Badge variant={outstanding ? "default" : "secondary"}>
              {tTicketTypes(QUESTION_REVIEW_STATUS_KEYS[review.status])}
            </Badge>
          </div>
          <CardDescription>{t("reviewAskDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <dl className="grid gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">{t("reviewOrganization")}</dt>
              <dd>
                <Link href={`/operator/organizations/${organization.id}`} className="hover:underline">
                  {organization.name}
                </Link>
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t("reviewEventStarts")}</dt>
              <dd>
                {startsAt
                  ? formatDateTime(startsAt, reviewEvent.timezone || PLATFORM_TIME_ZONE, locale)
                  : t("reviewNoStart")}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t("reviewSubmittedBy")}</dt>
              <dd>
                {review.submitted_by} · {formatDate(review.submitted_at, PLATFORM_TIME_ZONE, locale)}
              </dd>
            </div>
            {/*
              The acknowledgement is a RECORD, not a checkbox: who affirmed
              what the organization was choosing to collect, and when. It is
              where ADR 0045's "warned at authoring time" stops being a banner.
            */}
            <div>
              <dt className="text-muted-foreground">{t("reviewAcknowledged")}</dt>
              <dd>{formatDate(review.acknowledged_at, PLATFORM_TIME_ZONE, locale)}</dd>
            </div>
            {review.answered_by ? (
              <div className="sm:col-span-2">
                <dt className="text-muted-foreground">{t("reviewEndedBy")}</dt>
                <dd>
                  {review.answered_by}
                  {review.answered_at
                    ? ` · ${formatDate(review.answered_at, PLATFORM_TIME_ZONE, locale)}`
                    : null}
                </dd>
              </div>
            ) : null}
          </dl>
          {review.note ? (
            <div>
              <p className="text-muted-foreground">{t("reviewNote")}</p>
              <p className="whitespace-pre-wrap">{review.note}</p>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <form onSubmit={handleAnswer} className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("reviewItemsTitle")}</CardTitle>
            <CardDescription>
              {outstanding ? t("reviewItemsDescription") : t("reviewItemsAnsweredDescription")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {items.map((item) => (
              <ReviewItemRow
                key={item.id}
                item={item}
                outstanding={outstanding}
                draft={drafts[item.id] ?? EMPTY_VERDICT}
                problem={problem?.itemId === item.id ? problem : null}
                onVerdict={(verdict) => {
                  setDrafts((current) => withVerdict(items, current, item.id, verdict));
                  setProblem(null);
                }}
                onReason={(reason) => {
                  setDrafts((current) => withReason(items, current, item.id, reason));
                  setProblem(null);
                }}
                kindName={(kind) => tTicketTypes(TICKET_QUESTION_KIND_KEYS[kind as keyof typeof TICKET_QUESTION_KIND_KEYS])}
              />
            ))}
          </CardContent>
        </Card>

        {outstanding ? (
          <div className="space-y-3">
            {submitError ? (
              <Alert variant="destructive">
                <AlertTitle>{t("reviewAnswerFailedTitle")}</AlertTitle>
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            ) : null}
            <div className="flex items-center justify-between gap-4">
              <p className="text-sm text-muted-foreground">{t("reviewAnswerNotice")}</p>
              <Button type="submit" disabled={submitting}>
                {submitting ? t("reviewAnswering") : t("reviewAnswer")}
              </Button>
            </div>
          </div>
        ) : null}
      </form>
    </div>
  );
}

/**
 * One item: the question it names (with the Option, for an Option item) and
 * the Operator's two controls. An Option item is drawn under its question's
 * words and indented, so "Chicken" is never ruled on without knowing which
 * question offers it.
 */
function ReviewItemRow({
  item,
  outstanding,
  draft,
  problem,
  onVerdict,
  onReason,
  kindName,
}: {
  item: OperatorQuestionReviewItem;
  outstanding: boolean;
  draft: VerdictDraft;
  problem: AnswerProblem | null;
  onVerdict: (verdict: "approved" | "refused") => void;
  onReason: (reason: string) => void;
  kindName: (kind: string) => string;
}) {
  const t = useTranslations("operator");
  const isOption = Boolean(item.ticket_question_option_id);
  const label = isOption ? (item.option?.label ?? "") : (item.question?.label ?? "");
  const verdict = outstanding ? draft.verdict : (item.verdict ?? null);
  const reason = outstanding ? draft.reason : (item.reason ?? "");
  const refused = verdict === "refused";

  return (
    <div className={isOption ? "ml-6 space-y-2 border-l-2 pl-4" : "space-y-2 border-b pb-4 last:border-b-0"}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="font-medium">
            {isOption ? t("reviewOptionOf", { option: label, question: item.question?.label ?? "" }) : label}
          </p>
          {!isOption && item.question ? (
            <p className="text-xs text-muted-foreground">
              {kindName(item.question.kind)} · {item.question.required ? t("questionRequired") : t("questionOptional")} ·{" "}
              {item.question.ticket_type_name}
            </p>
          ) : null}
        </div>
        {outstanding ? (
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              variant={verdict === "approved" ? "default" : "outline"}
              onClick={() => onVerdict("approved")}
            >
              {t("approve")}
            </Button>
            <Button
              type="button"
              size="sm"
              variant={refused ? "destructive" : "outline"}
              onClick={() => onVerdict("refused")}
            >
              {t("refuse")}
            </Button>
          </div>
        ) : verdict ? (
          <Badge variant={verdict === "approved" ? "default" : "destructive"}>
            {verdict === "approved" ? t("verdictApproved") : t("verdictRefused")}
          </Badge>
        ) : null}
      </div>
      {problem?.kind === "verdict" ? (
        <p className="text-sm text-destructive">{t("verdictRequired")}</p>
      ) : null}
      {refused && outstanding ? (
        <div className="space-y-1">
          <Textarea
            value={reason}
            onChange={(event) => onReason(event.target.value)}
            placeholder={t("refuseReasonPlaceholder")}
            rows={2}
            maxLength={RESOLUTION_REASON_MAX_LENGTH + 1}
          />
          <p className="text-xs text-muted-foreground">{t("refuseReasonDescription")}</p>
          {problem?.kind === "reason" ? (
            <p className="text-sm text-destructive">{t("refuseReasonRequired")}</p>
          ) : problem?.kind === "too_long" ? (
            <p className="text-sm text-destructive">{t("reasonTooLong", { max: RESOLUTION_REASON_MAX_LENGTH })}</p>
          ) : null}
        </div>
      ) : refused && reason ? (
        <p className="text-sm text-muted-foreground">{t("questionRefusedReason", { reason })}</p>
      ) : null}
    </div>
  );
}

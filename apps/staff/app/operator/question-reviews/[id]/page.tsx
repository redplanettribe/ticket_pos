import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorQuestionReviewClient } from "./operator-question-review-client";

type PageProps = {
  params: Promise<{ id: string }>;
};

export default async function OperatorQuestionReviewPage({ params }: PageProps) {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  const { id } = await params;

  return (
    <StaffPageShell activePath="/operator/question-reviews">
      <div className="mx-auto max-w-3xl">
        <OperatorQuestionReviewClient reviewId={id} />
      </div>
    </StaffPageShell>
  );
}

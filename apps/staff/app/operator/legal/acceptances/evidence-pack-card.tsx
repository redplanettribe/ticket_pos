"use client";

import { useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { downloadEvidencePack } from "@/lib/legal-records-api";

/**
 * THE CONSENT EVIDENCE PACK'S CONTROL (#568, spec #556, ADR 0067).
 *
 * ONE COMPONENT ON BOTH RECORD SCREENS, because it is one file. A pack spans
 * both populations for one address, so the customer record and the staff record
 * generate identical bytes for one human being — and two controls with two
 * pieces of copy would be two invitations to think there are two packs.
 *
 * IT SAYS WHAT THE FILE IS BEFORE IT OFFERS TO MAKE ONE. The three sentences
 * beneath the button are the ones an operator needs before sending somebody
 * everything the platform holds about them: that it covers both populations,
 * that nothing is kept but a hash, and that THERE IS NO SELF-SERVICE DOWNLOAD —
 * the operator sending it *is* the mechanism. That last one is not a limitation
 * to apologise for: a proven address buys a withdrawal, which only ever takes
 * something away, where a pack discloses everything (ADR 0039).
 *
 * IT IS A BLOB FETCH AND NOT A LINK, the Holder Export's shape: the endpoint
 * answers with a file on success and a JSON envelope on failure, so a plain
 * anchor would navigate the operator to raw JSON on a refusal.
 */
export function EvidencePackCard({ path }: { path: string }) {
  const t = useTranslations("operator.legalRecords");
  const errorCopy = useMessages().errors;
  const [generating, setGenerating] = useState(false);

  async function generate() {
    setGenerating(true);
    try {
      await downloadEvidencePack(path);
      toast.success(t("packGenerated"));
    } catch (caught) {
      toast.error(
        (caught instanceof ApiError ? apiErrorMessage(errorCopy, caught) : null) ?? t("packFailed"),
      );
    } finally {
      setGenerating(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("packTitle")}</CardTitle>
        <CardDescription>{t("packDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Button onClick={generate} disabled={generating}>
          {generating ? t("packGenerating") : t("packGenerate")}
        </Button>
        <div className="space-y-2 text-sm text-muted-foreground">
          <p>{t("packSpansBoth")}</p>
          <p>{t("packNeverStored")}</p>
          <p>{t("packOperatorOnly")}</p>
        </div>
      </CardContent>
    </Card>
  );
}

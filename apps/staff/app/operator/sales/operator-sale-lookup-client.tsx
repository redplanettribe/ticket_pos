"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { Button, Card, CardContent, FormField, Input, PageHeader } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

/**
 * The door a support thread opens: paste a Sale Confirmation reference and land
 * on that sale, whichever Organization it belongs to (#124).
 *
 * There is no sales browser here and none is planned — the flow always starts
 * from a reference somebody was given, so with nothing entered this page shows
 * the search and nothing else (#193). The field validates nothing beyond being
 * non-empty: whether a reference names a sale is the API's answer, and the
 * lookup ignores case, so a reference quoted in lowercase resolves too.
 *
 * The placeholder is a Sale Confirmation reference and reads the same in both
 * catalogs — it is the shape of an identifier this platform issues rather than
 * a sentence — but it is a key all the same, so the one place to change it if
 * the shape ever changes is the catalog (messages/README.md).
 */
export function OperatorSaleLookupClient() {
  const t = useTranslations("operator");
  const router = useRouter();
  const [reference, setReference] = useState("");

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    const trimmed = reference.trim();
    if (!trimmed) {
      return;
    }
    router.push(`/operator/sales/${encodeURIComponent(trimmed)}`);
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("saleLookupTitle")} description={t("saleLookupDescription")} />

      <Card>
        <CardContent>
          <form className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end" onSubmit={handleSubmit}>
            <FormField id="operator-sale-reference" label={t("saleReferenceLabel")}>
              <Input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder={t("saleReferencePlaceholder")}
                autoComplete="off"
                spellCheck={false}
              />
            </FormField>
            <Button type="submit" disabled={!reference.trim()}>
              {t("findSale")}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

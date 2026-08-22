import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { StorefrontShell } from "@/components/storefront-shell";
import { BRAND_NAME } from "@/lib/brand";

export async function generateMetadata({ params }: AnswerPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "answerLink" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // Never indexed. There is nothing here for a search engine, and an old
    // address of this shape may still carry a token in its query string.
    robots: { index: false, follow: false },
  };
}

type AnswerPageProps = {
  params: Promise<{ locale: string }>;
};

/**
 * Where an old Answer Link lands (#346, ADR 0049).
 *
 * The Answer Link is RETIRED. ADR 0044's signed, proof-free link — the one a
 * buyer forwarded so that whoever would hold a Ticket could answer its Ticket
 * Questions — no longer exists: nothing mints one, its API routes are gone, and
 * a Ticket's questions are reached only through the Ticket Assignment, where
 * the Holder accepts by Assignment Link and answers from their own Customer
 * Area.
 *
 * The address survives because the links do: they were pasted into group chats
 * and sit in message histories, and somebody tapping one months later should
 * land on a sentence rather than a 404. So this is a STATIC PAGE. It reads no
 * query string — a token that may still be in the address is neither parsed
 * nor relayed anywhere — it fetches nothing, and it tells the reader the only
 * true thing: the link no longer opens, and the email from whoever bought the
 * ticket is what will.
 *
 * It is a localized page reached at the unprefixed /answer the old links
 * carried, and redirected into a language by the middleware.
 */
export default async function AnswerPage({ params }: AnswerPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const t = await getTranslations("answerLink");

  return (
    // NO customerNav. Holding an old link never made anybody a Customer, and
    // an invitation to sign in would suggest the two are connected.
    <StorefrontShell>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />
        <Alert>
          <AlertTitle>{t("retiredTitle")}</AlertTitle>
          <AlertDescription>{t("retiredDescription")}</AlertDescription>
        </Alert>
      </div>
    </StorefrontShell>
  );
}

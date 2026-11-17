import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

type UnmatchedPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: UnmatchedPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "notFound" });
  return { title: t("title") };
}

/**
 * Every address under a locale that no other route matches (#799), e.g.
 * `/es/a/b/c/d`. With no app/layout.tsx above `[locale]`, an unmatched address
 * would otherwise fall through to Next's bare, English, lang-less 404; matching
 * it here puts it under the locale layout, so the `[locale]` not-found screen
 * answers it in the address's language with `<html lang>` set. This is
 * next-intl's documented catch-all recipe. Any specific route outranks a
 * catch-all, so nothing that exists is shadowed.
 */
export default async function UnmatchedPage({ params }: UnmatchedPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  notFound();
}

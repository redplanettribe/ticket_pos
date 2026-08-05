import { PageHeader } from "@ticket-pos/ui";
import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { ExplorerFilters } from "@/components/explorer-filters";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { ExplorerResults } from "@/components/explorer-results";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import { EXPLORER_PAGE_SIZE, listPublicEvents, listPublicTags } from "@/lib/api";
import { toAppLocale } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";
import { isWhenPreset, whenToRange } from "@/lib/when";

export const dynamic = "force-dynamic";

type HomePageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ q?: string; when?: string; tags?: string }>;
};

export async function generateMetadata({ params }: HomePageProps): Promise<Metadata> {
  const { locale } = await params;
  // The title and description come from the layout and are deliberately left
  // alone; this only pairs the two explorers with each other.
  //
  // The search, date and tag filters are read from the query string and never
  // reach the canonical: every filtered view of the explorer is the explorer,
  // and naming itself would put an unbounded family of near-duplicates in the
  // index.
  const { canonical, languages } = localeAlternates("/", toAppLocale(locale), storefrontBaseUrl());
  return { alternates: { canonical, languages } };
}

export default async function HomePage({ params, searchParams }: HomePageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const filters = await searchParams;
  const q = filters.q?.trim() || undefined;
  // The render's one clock: the same instant feeds the date-preset window and
  // the Timeline's grouping, and rides into the client so re-groupings after
  // "Load more" answer to the clock the page was built with.
  const now = new Date();
  const preset = isWhenPreset(filters.when) ? filters.when : "all";
  const range = whenToRange(preset, now);
  const selectedTags = (filters.tags ?? "")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  const [page, presetTags] = await Promise.all([
    listPublicEvents({
      q,
      from: range.from,
      to: range.to,
      tags: selectedTags.length > 0 ? selectedTags : undefined,
      limit: EXPLORER_PAGE_SIZE,
    }),
    listPublicTags(),
  ]);

  const t = await getTranslations("explorer");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-6xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("subtitle")} />
        <ExplorerFilters presetTags={presetTags ?? []} />
        <ExplorerResults
          initialEvents={page?.events ?? []}
          initialCursor={page?.next_cursor ?? null}
          now={now}
          q={q}
          from={range.from}
          to={range.to}
          tags={selectedTags}
        />
      </div>
    </StorefrontShell>
  );
}

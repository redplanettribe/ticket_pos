import { PageHeader } from "@ticket-pos/ui";
import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { ExplorerFilters } from "@/components/explorer-filters";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { ExplorerResults } from "@/components/explorer-results";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import { EXPLORER_PAGE_SIZE, listPublicEvents, listPublicTags } from "@/lib/api";
import { getFollows } from "@/lib/customer-session";
import { followList } from "@/lib/follows";
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

  const [page, presetTags, follows] = await Promise.all([
    listPublicEvents({
      q,
      from: range.from,
      to: range.to,
      tags: selectedTags.length > 0 ? selectedTags : undefined,
      limit: EXPLORER_PAGE_SIZE,
    }),
    listPublicTags(),
    // What this Customer already Follows, read once for the whole chip bar
    // (#218). An anonymous visitor holds no cookie, so this costs them no API
    // call and comes back "signed-out" — which is what leaves the chips exactly
    // the filter bar they were, with no Follow control on them at all.
    getFollows(),
  ]);

  // Null when nobody is signed in or the read failed: "cannot say" rather than
  // "follows nothing", which is the difference between drawing no control and
  // drawing every control unpressed.
  const followedTagKeys =
    follows.status === "ok"
      ? followList(follows).flatMap((follow) => (follow.type === "tag" ? [follow.tag.canonical_key] : []))
      : null;

  const t = await getTranslations("explorer");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-6xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("subtitle")} />
        <ExplorerFilters presetTags={presetTags ?? []} followedTagKeys={followedTagKeys} />
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

import { PageHeader, StorefrontShell } from "@ticket-pos/ui";

import { ExplorerFilters } from "@/components/explorer-filters";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { ExplorerResults } from "@/components/explorer-results";
import { EXPLORER_PAGE_SIZE, listPublicEvents, listPublicTags } from "@/lib/api";
import { isWhenPreset, whenToRange } from "@/lib/when";

export const dynamic = "force-dynamic";

type HomePageProps = {
  searchParams: Promise<{ q?: string; when?: string; tags?: string }>;
};

export default async function HomePage({ searchParams }: HomePageProps) {
  const params = await searchParams;
  const q = params.q?.trim() || undefined;
  const preset = isWhenPreset(params.when) ? params.when : "all";
  const range = whenToRange(preset);
  const selectedTags = (params.tags ?? "")
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

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-6xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader
          title="Discover events"
          description="Find and explore upcoming events from organizers everywhere."
        />
        <ExplorerFilters presetTags={presetTags ?? []} />
        <ExplorerResults
          initialEvents={page?.events ?? []}
          initialCursor={page?.next_cursor ?? null}
          q={q}
          from={range.from}
          to={range.to}
          tags={selectedTags}
        />
      </div>
    </StorefrontShell>
  );
}

import { PageHeader, StorefrontShell } from "@ticket-pos/ui";

import { ExplorerFilters } from "@/components/explorer-filters";
import { ExplorerResults } from "@/components/explorer-results";
import { EXPLORER_PAGE_SIZE, listPublicEvents } from "@/lib/api";
import { isWhenPreset, whenToRange } from "@/lib/when";

export const dynamic = "force-dynamic";

type HomePageProps = {
  searchParams: Promise<{ q?: string; when?: string }>;
};

export default async function HomePage({ searchParams }: HomePageProps) {
  const params = await searchParams;
  const q = params.q?.trim() || undefined;
  const preset = isWhenPreset(params.when) ? params.when : "all";
  const range = whenToRange(preset);

  const page = await listPublicEvents({
    q,
    from: range.from,
    to: range.to,
    limit: EXPLORER_PAGE_SIZE,
  });

  return (
    <StorefrontShell>
      <div className="mx-auto w-full max-w-6xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader
          title="Discover events"
          description="Find and explore upcoming events from organizers everywhere."
        />
        <ExplorerFilters />
        <ExplorerResults
          initialEvents={page?.events ?? []}
          initialCursor={page?.next_cursor ?? null}
          q={q}
          from={range.from}
          to={range.to}
        />
      </div>
    </StorefrontShell>
  );
}

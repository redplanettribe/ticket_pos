import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import type { EventDetail } from "@/lib/events-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { parseSaleDir, parseSaleSort } from "@/lib/sales-api";

import { loadSession } from "../../../staff-page-shell";
import { ImportSalesSection } from "../import-sales-section";
import { SalesList } from "../sales-list";

type EventSalesPageProps = {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ page?: string; sort?: string; dir?: string }>;
};

// parsePage reads the URL page number, flooring at 1 (matches the API).
function parsePage(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "", 10);
  return Number.isFinite(parsed) && parsed >= 1 ? parsed : 1;
}

// fetchEventTimezone loads just the Event timezone (for sold-at formatting),
// tolerating failure so the list still renders in the viewer's local zone.
async function fetchEventTimezone(eventId: string, token: string): Promise<string | null> {
  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data?.timezone ?? null;
  } catch {
    return null;
  }
}

export default async function EventSalesPage({ params, searchParams }: EventSalesPageProps) {
  const { id } = await params;
  const { page, sort, dir } = await searchParams;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // The Sales list is visible to every Member of the Event (Org Admin, Event
  // Owner, Event Staff). The Sale Import tool and Import history remain owner-only.
  const canManageImports = role === "org_admin" || role === "event_owner";

  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  const timezone = token ? await fetchEventTimezone(id, token) : null;

  return (
    <div className="space-y-6">
      <SalesList
        eventId={id}
        page={parsePage(page)}
        sort={parseSaleSort(sort)}
        dir={parseSaleDir(dir)}
        timezone={timezone}
      />
      {canManageImports ? <ImportSalesSection eventId={id} /> : null}
    </div>
  );
}

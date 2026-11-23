import { cookies } from "next/headers";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { callBackend } from "@/lib/api";
import type { HomeNow } from "@/lib/home-now";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { HomeNowStrip } from "./home-now-strip";
import { loadSession, StaffPageShell } from "./staff-page-shell";

/**
 * What stands now, read on the server for the Active Member (#859). The API
 * withholds the money a role may not see, so what comes back is what is
 * drawn. A failure is null and the page says so in place of the strip.
 */
async function loadHomeNow(): Promise<HomeNow | null> {
  const token = (await cookies()).get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return null;
  }
  try {
    const envelope = await callBackend<HomeNow>("/api/v1/staff/organization/home/now", {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data ?? null;
  } catch {
    return null;
  }
}

/**
 * Home (#858, ADR 0081): the page a Member lands on after choosing an
 * Organization, always the whole Organization whichever Member reads it.
 */
export default async function StaffHomePage() {
  const t = await getTranslations("home");
  const session = await loadSession();
  const home = session?.active_member ? await loadHomeNow() : null;
  // The page's one clock read, handed down so "in 3 days" is the same on
  // the server render and on the client.
  const now = Date.now();

  return (
    <StaffPageShell activePath="/">
      <div className="mx-auto max-w-5xl space-y-6">
        <PageHeader title={t("title")} />
        {home ? (
          <HomeNowStrip home={home} role={session?.active_member?.role ?? null} now={now} />
        ) : (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            <AlertDescription>{t("loadFailed")}</AlertDescription>
          </Alert>
        )}
      </div>
    </StaffPageShell>
  );
}

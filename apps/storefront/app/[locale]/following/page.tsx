import type { Metadata } from "next";
import { getMessages, getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { FollowingList } from "@/components/following-list";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { redirect } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import { BRAND_NAME } from "@/lib/brand";
import { customerSessionToken, getFollows } from "@/lib/customer-session";
import { followList } from "@/lib/follows";

// Same posture as every other Storefront page: rendered per request with
// uncached reads.
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: FollowingPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "following" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // Private, in both languages, exactly as the Customer Area is.
    robots: { index: false, follow: false },
  };
}

type FollowingPageProps = {
  params: Promise<{ locale: string }>;
};

/**
 * The Following list: everything the signed-in Customer Follows, of both kinds,
 * in one place, with unfollow available here (#218, parent #215).
 *
 * A MANAGEMENT SURFACE AND NOT A FEED. It lists what the Customer has
 * subscribed to, not what those subscriptions have produced — ADR 0030
 * deliberately builds no Following feed, because the whole payload of a Follow
 * is the weekly Follow Digest and a second, browsable stream of the same Events
 * would be a discovery surface nobody asked for competing with the explorer that
 * already exists. What belongs here is the ability to see the list and to leave
 * it.
 *
 * One read, one list. The API returns both kinds interleaved in one order, so
 * this page never unions two answers and never re-sorts one — see lib/follows.ts.
 */
export default async function FollowingPage({ params }: FollowingPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const follows = await getFollows();

  if (follows.status === "signed-out") {
    // What a person Follows is theirs, so there is nothing to show a visitor who
    // is not signed in. The `expired` flag separates "your session ended" from
    // "you were never signed in" so the sign-in page can explain which, and
    // `next` brings them back here rather than to the explorer.
    const hadSession = Boolean(await customerSessionToken());
    return redirect({
      href: hadSession ? "/signin?expired=1&next=/following" : "/signin?next=/following",
      locale,
    });
  }

  const t = await getTranslations("following");
  // What the read failed with, in this page's language: chosen by the API's own
  // error code, falling back to the API's message for a code this catalog has
  // never heard of (ADR 0023), and to this page's own sentence when the API was
  // never reached and so said nothing at all.
  const loadFailure =
    follows.status === "error"
      ? (apiErrorMessage((await getMessages()).errors, follows) ?? t("loadNetworkFailed"))
      : null;

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />

        {follows.status === "error" ? (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            {/* Which failure it was stays the API's to say; only the words are
                this page's. */}
            <AlertDescription>{loadFailure}</AlertDescription>
          </Alert>
        ) : (
          <FollowingList follows={followList(follows)} />
        )}
      </div>
    </StorefrontShell>
  );
}

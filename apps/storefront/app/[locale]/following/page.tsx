import type { Metadata } from "next";
import { getMessages, getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { DigestToggle } from "@/components/digest-toggle";
import { FollowSuggestionsPanel } from "@/components/follow-suggestions";
import { FollowingList } from "@/components/following-list";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { redirect } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import { BRAND_NAME } from "@/lib/brand";
import { customerSessionToken, getFollowSuggestions, getFollows } from "@/lib/customer-session";
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

  // Two reads, made TOGETHER rather than one after the other: they are
  // independent, and the panel is worth a page's width of latency but not a
  // second round trip's. Suggestions are their own endpoint precisely so that
  // the hot public pages calling the listing do not run this query (ADR 0031),
  // which is also what makes reading both here cost this page alone.
  //
  // The suggestions read carries no identifier of whose suggestions they are,
  // exactly as the listing carries none: the session is the only scope.
  const [follows, suggestions] = await Promise.all([getFollows(), getFollowSuggestions()]);

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
          <>
            {/* The Follow Digest switch, ABOVE the list and drawn from the same
                read (#224). Above, because when it is off it changes what the
                list below means — those Follows stand, and nothing is being sent
                about them — and a reader who met the list first would have
                already drawn the wrong conclusion. From the same read, because
                the two must never disagree: "the digest is off and your follows
                still stand" is one sentence, and two reads that could skew is
                how it becomes untrue. */}
            <DigestToggle enabled={follows.data.digest_enabled} />
            <FollowingList follows={followList(follows)} />
          </>
        )}

        {/* Suggested Follows, BELOW the Customer's own list and in the same
            place whether or not they Follow anything (#231, ADR 0031). When they
            Follow nothing this lands beneath the empty state's copy, which stays:
            the sentence explaining what a Follow is for is what makes the
            suggestions beneath it legible, so the panel adds to that explanation
            rather than replacing it.

            Outside the error branch above, because the two reads are independent
            and neither owes the other its silence: a listing that failed does not
            make a panel that succeeded wrong. The panel draws NOTHING when its
            own read failed and nothing when both groups are empty, so this line
            is unconditional by design — there is no state in which it produces an
            error, a heading or an empty box.

            The panel takes its own read and nothing else. It used to be handed
            the listing as well, to find the name of the Tag a suggestion named
            by canonical key — which only ever worked for a producer the Customer
            Follows directly (#232), and stopped working when a producer could be
            a Tag derived from a followed Organization (#233). The reason now
            carries the Tag itself (#234), so the two reads are independent here
            in the same way they are on the API. */}
        <FollowSuggestionsPanel suggestions={suggestions} />
      </div>
    </StorefrontShell>
  );
}

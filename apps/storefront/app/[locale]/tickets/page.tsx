import type { Metadata } from "next";
import {
  getMessages,
  getTranslations,
  setRequestLocale,
} from "next-intl/server";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  PageHeader,
} from "@ticket-pos/ui";

import { EventGroup } from "@/components/event-group";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { MyInfo } from "@/components/my-info";
import { OpenSaleFromHash } from "@/components/open-sale-from-hash";
import { RetryFailedRead } from "@/components/reversal-watch";
import { SignInOtherAddressButton } from "@/components/sign-in-other-address-button";
import { StorefrontShell } from "@/components/storefront-shell";
import { Link, redirect } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import { BRAND_NAME } from "@/lib/brand";
import {
  availableTabs,
  countFor,
  groupsFor,
  tabForLinkedSale,
  tabOf,
  type CustomerAreaTab,
} from "@/lib/customer-area-tabs";
import {
  customerSessionToken,
  getCustomerArea,
  getCustomerSession,
  type CustomerArea as CustomerAreaData,
} from "@/lib/customer-session";

// Same posture as every other Storefront page: rendered per request with
// uncached reads. There is no static output here to opt out of.
export const dynamic = "force-dynamic";

/**
 * A function rather than a constant now, because the tab has to be written in
 * the language the page is served in. The `robots` line is unchanged and stays
 * unconditional: the Customer Area is private in both languages.
 */
export async function generateMetadata({
  params,
}: CustomerAreaPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "customerArea" });
  return {
    // The brand is interpolated rather than written into the catalog, so a
    // translator has a sentence to translate and not a name to render.
    title: t("metaTitle", { brand: BRAND_NAME }),
    // The Customer Area is private; keep it out of search results entirely.
    robots: { index: false, follow: false },
  };
}

type CustomerAreaPageProps = {
  params: Promise<{ locale: string }>;
  /** `?tab=` names the tab to open (#355); anything else falls back to Upcoming. */
  searchParams: Promise<{ tab?: string | string[] }>;
};

/**
 * The Customer Area: every Ticket Sale the signed-in Customer owns, upcoming and
 * past, across every Organization they have bought from.
 *
 * The read is scoped by the Customer Session and by nothing else — this page
 * passes no identifier of any kind to the API, and there is no route parameter
 * here that could name a different Customer.
 */
export default async function CustomerAreaPage({
  params,
  searchParams,
}: CustomerAreaPageProps) {
  const [{ locale }, { tab: rawTab }] = await Promise.all([params, searchParams]);
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const [area, session] = await Promise.all([
    getCustomerArea(),
    getCustomerSession(),
  ]);

  // A Confirmation Link session names the one Ticket Sale it was minted for.
  // The page reads it only to explain itself: the narrowing is enforced by the
  // API, which returns that one sale and nothing else regardless of what this
  // page believes.
  const fromConfirmationLink =
    session.status === "ok" && session.data.ticket_sale_id !== null;

  // The tab this page opens on. The URL names it so that a tab is a link that
  // can be sent, except for a Confirmation Link session: that shows one Sale,
  // so there is nothing to choose between and the Sale decides — a reversed
  // one opens on Reversed, where it is, rather than on an empty Upcoming.
  const tab: CustomerAreaTab =
    fromConfirmationLink && area.status === "ok"
      ? tabForLinkedSale(area.data)
      : tabOf(rawTab);

  if (area.status === "signed-out") {
    // A session that ran out or was signed out elsewhere sends the visitor to
    // sign-in rather than showing them an error. The `expired` flag separates
    // "your session ended" from "you were never signed in", so the sign-in page
    // can explain what happened and clear the dead cookie.
    const hadSession = Boolean(await customerSessionToken());
    // The paths stay locale-free; the redirect carries the language this page
    // was read in, so a Customer whose session ran out is not also moved into
    // another language on the way to the sign-in form.
    return redirect({
      href: hadSession
        ? "/signin?expired=1&next=/tickets"
        : "/signin?next=/tickets",
      locale,
    });
  }

  const t = await getTranslations("customerArea");
  // What the read failed with, in this page's language: chosen by the API's own
  // error code, falling back to the API's message when the code is one this
  // catalog has never heard of (ADR 0023), and to this page's own sentence when
  // the API was never reached and so said nothing at all.
  const loadFailure =
    area.status === "error"
      ? (apiErrorMessage((await getMessages()).errors, area) ??
        t("loadNetworkFailed"))
      : null;

  // Whether "My info" is on the page at all decides the shape of the page (#358).
  // With it, a laptop gets two columns: tickets left, the panel in a sticky
  // sidebar on the right. Without it — a Confirmation Link arrival, or a read
  // that came back without a session — the one column stays centred and
  // narrow, rather than leaving an empty lane where a sidebar would have been.
  const showMyInfo = session.status === "ok" && !fromConfirmationLink;

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      {/* The grid only exists from `lg` up; below it, the two children stack in
          DOM order, which is the order they have always had — tickets first,
          My info last — so the phone layout and the keyboard order are
          untouched by the laptop one. */}
      <div
        className={
          showMyInfo
            ? "mx-auto w-full max-w-6xl px-4 py-10 sm:py-12 lg:grid lg:grid-cols-[minmax(0,1fr)_320px] lg:items-start lg:gap-8"
            : "mx-auto w-full max-w-3xl px-4 py-10 sm:py-12"
        }
      >
        <div className="space-y-8">
          <PageHeader
            title={fromConfirmationLink ? t("linkedTitle") : t("title")}
            description={
              fromConfirmationLink ? t("linkedDescription") : t("description")
            }
          />

          {fromConfirmationLink &&
          area.status === "ok" &&
          linkedSaleReversed(area.data) ? (
            <ReversedSaleNotice />
          ) : null}

          {fromConfirmationLink ? <ConfirmationLinkNotice /> : null}

          {area.status === "error" ? (
            <>
              <Alert variant="destructive">
                <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
                {/* Which failure it was stays the API's to say; only the words are
                    this page's. There is always a sentence: a failure that named
                    nothing at all is still a failure the reader is owed an
                    explanation for. */}
                <AlertDescription>{loadFailure}</AlertDescription>
              </Alert>
              {/* The error replaces the cards, and with them anything watching a
                  Reversal Request resolve — so a failed read is what would
                  otherwise end the watch for good. A few silent retries make one
                  blip survivable; see RetryFailedRead. */}
              <RetryFailedRead />
            </>
          ) : (
            <CustomerArea
              area={area.data}
              tab={tab}
              // A Confirmation Link arrival reads their purchase and acts on
              // nothing (#121): the cards state the Reversal Window and offer the
              // way to sign in, never the undo itself. The email travels with it
              // so that one click is the whole of the sign-in, and it is the
              // session's own address rather than anything from the URL.
              viaConfirmationLink={fromConfirmationLink}
              customerEmail={session.status === "ok" ? session.data.email : null}
            />
          )}
        </div>
        {/* "My info" is the Customer Area's only write, and it belongs to the
            full session alone. A Confirmation Link arrival is not shown it: that
            session proves possession of a forwarded email rather than ownership
            of the address, and the API refuses the edit behind it (#102). It
            comes after the tickets because the tickets are what someone came
            for — under them on a phone, beside them on a laptop, sticky so the
            panel rides along while a long list of Event groups scrolls
            (top-24 clears the sticky header). */}
        {session.status === "ok" && showMyInfo ? (
          <aside className="mt-8 lg:sticky lg:top-24 lg:mt-0">
            <MyInfo profile={session.data} />
          </aside>
        ) : null}
      </div>
    </StorefrontShell>
  );
}

/**
 * linkedSaleReversed reports whether the single Ticket Sale a Confirmation Link
 * opened has since been reversed. A link session shows exactly one sale, so
 * either list holds it.
 */
function linkedSaleReversed(area: CustomerAreaData): boolean {
  return [...area.upcoming, ...area.past].some(
    (sale) => sale.status === "reversed",
  );
}

/**
 * A reversed purchase says so at the top of the page, not only as a badge on the
 * card. Someone opening a confirmation email at the gate must not be left to
 * infer from a small label that the tickets they think they hold are gone.
 *
 * It names no actor. Since #119 a reversal can come from either side — the
 * Customer undoing their own purchase, or the organizer undoing a batch — and
 * this notice is read by someone who may have done it themselves an hour ago.
 * Asserting the wrong one would be worse than asserting neither.
 */
async function ReversedSaleNotice() {
  const t = await getTranslations("customerArea");
  return (
    <Alert variant="destructive">
      <AlertTitle>{t("reversedTitle")}</AlertTitle>
      <AlertDescription>{t("reversedDescription")}</AlertDescription>
    </Alert>
  );
}

/**
 * What someone arriving by Confirmation Link is told: that they are seeing one
 * purchase rather than everything, and how to see the rest.
 *
 * The offer of a passcode is the obvious next step from a link, and it is the
 * only way to widen: a link proves possession of an email, a passcode proves
 * ownership of the address, and only the second earns the full history.
 */
async function ConfirmationLinkNotice() {
  const t = await getTranslations("customerArea");
  return (
    <Alert>
      <AlertTitle>{t("linkNoticeTitle")}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t("linkNoticeDescription")}</p>
        <Button asChild size="sm">
          <Link href="/signin?next=/tickets">{t("linkNoticeAction")}</Link>
        </Button>
      </AlertDescription>
    </Alert>
  );
}

async function CustomerArea({
  area,
  tab: requested,
  viaConfirmationLink,
  customerEmail,
}: {
  area: CustomerAreaData;
  tab: CustomerAreaTab;
  viaConfirmationLink: boolean;
  customerEmail: string | null;
}) {
  // The Tickets somebody gave this Customer and they accepted (#325). Empty
  // for almost everybody, and empty for a Confirmation Link session by
  // construction. They are drawn inside their Event's group, by groupsFor.
  const holding = area.holding ?? [];
  // Somebody who has bought nothing may still HOLD something: a friend bought
  // them a ticket and they accepted it (#325). Showing them the "you have no
  // purchases" page while they hold a ticket would be the platform denying the
  // thing it just mailed them.
  if (
    area.upcoming.length === 0 &&
    area.past.length === 0 &&
    holding.length === 0
  ) {
    return <NoPurchases />;
  }
  const t = await getTranslations("customerArea");
  // One instant for the whole render, so a held Ticket is filed under the same
  // tab the count on the tab bar promised.
  const now = new Date();
  const tabs = availableTabs(area, now);
  // A URL can name a tab the bar does not draw — a Past link sent before the
  // last past Sale was reversed, say. It opens on Upcoming, like any other
  // name this page has nothing for, rather than on a heading over nothing.
  const tab = tabs.includes(requested) ? requested : "upcoming";
  const groups = groupsFor(area, tab, now);

  return (
    <>
      {/* One card per Event, the Sales as rows inside it (#354) and after them
          the Tickets somebody gave this Customer (#356); a row named by
          the URL's fragment is opened by the client piece below, since a
          fragment alone cannot open a `<details>`. Mounted once for the page. */}
      <OpenSaleFromHash />

      {/* A Confirmation Link session shows one Sale, so a bar of tabs would be
          a choice between one thing and nothing; the page has already opened
          on the tab that Sale lives in. */}
      {viaConfirmationLink ? null : (
        <TabBar tabs={tabs} current={tab} area={area} now={now} />
      )}

      <section className="space-y-4" aria-labelledby="customer-area-tab-heading">
        <h2
          id="customer-area-tab-heading"
          className="text-lg font-semibold tracking-tight"
        >
          {t(TAB_HEADING[tab])}
        </h2>

        {/* Reversed is the one tab whose contents are not tickets to anything,
            so it says so once, above the cards, rather than leaving the badge
            on every row to explain the whole tab. */}
        {tab === "reversed" ? (
          <p className="text-sm text-muted-foreground">
            {t("reversedTabDescription")}
          </p>
        ) : null}

        {/* Only Upcoming can be empty: the other two are not drawn when there
            is nothing behind them, and a URL naming one fell back above. */}
        {groups.length > 0 ? (
          <ul className="space-y-4">
            {groups.map((group) => (
              <EventGroup
                key={group.event.id}
                group={group}
                badgeReversed={tab !== "reversed"}
                viaConfirmationLink={viaConfirmationLink}
                customerEmail={customerEmail}
              />
            ))}
          </ul>
        ) : (
          <div className="rounded-lg border border-dashed p-8 text-center">
            <p className="font-medium">{t("nothingUpcomingTitle")}</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {t("nothingUpcomingDescription")}
            </p>
            <Button asChild variant="secondary" className="mt-4">
              <Link href="/">{t("discoverEvents")}</Link>
            </Button>
          </div>
        )}
      </section>
    </>
  );
}

const TAB_HEADING = {
  upcoming: "upcomingHeading",
  past: "pastHeading",
  reversed: "reversedHeading",
} as const;

/**
 * The tab bar: plain links to `?tab=`, so a tab is an address — sendable,
 * bookmarkable, and back-button-able — rather than client state that a reload
 * forgets. The current one is marked as the page it is, which is also what
 * draws it selected. Each label carries its count so the reader knows what a
 * tab holds before opening it.
 *
 * The bar scrolls sideways on its own when three labelled tabs outgrow a
 * narrow phone; the page itself never widens (#353).
 */
async function TabBar({
  tabs,
  current,
  area,
  now,
}: {
  tabs: CustomerAreaTab[];
  current: CustomerAreaTab;
  area: CustomerAreaData;
  now: Date;
}) {
  const t = await getTranslations("customerArea");
  return (
    <nav aria-label={t("tabsLabel")} className="-mx-4 overflow-x-auto px-4">
      <ul className="flex min-w-max gap-1 border-b">
        {tabs.map((tab) => {
          const selected = tab === current;
          return (
            <li key={tab}>
              <Link
                href={{ pathname: "/tickets", query: { tab } }}
                aria-current={selected ? "page" : undefined}
                className={
                  "-mb-px inline-flex items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium transition-colors " +
                  (selected
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:border-border hover:text-foreground")
                }
              >
                {t("tabCount", {
                  label: t(TAB_HEADING[tab]),
                  count: countFor(area, tab, now),
                })}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

/**
 * The empty state points at the explorer rather than dead-ending. A Customer can
 * reach this legitimately: signing in creates the record if a Ticket Sale never
 * did, so "no purchases yet" is a normal first visit, not a fault.
 *
 * The second line is the other way to land here, and the more confusing one: a
 * Customer is keyed on their email exactly as normalised, so tickets bought
 * under an alias belong to a different Customer and are invisible from this
 * session (ADR 0011). Naming that case is the whole mitigation — without it the
 * page reads as lost tickets. It is phrased as a question about what the reader
 * did, never as a claim that some other address is known here; answering that
 * would hand back the oracle the passcode request endpoint refuses to be.
 *
 * Reaching the other address means leaving this session first, which is why the
 * second line ends in a button rather than a link — see
 * SignInOtherAddressButton.
 */
async function NoPurchases() {
  const t = await getTranslations("customerArea");
  return (
    <div className="rounded-lg border border-dashed p-10 text-center">
      <p className="font-medium">{t("emptyTitle")}</p>
      <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
        {t("emptyDescription")}
      </p>
      <Button asChild className="mt-6">
        <Link href="/">{t("discoverEvents")}</Link>
      </Button>
      <p className="mx-auto mt-6 max-w-md text-sm text-muted-foreground">
        {/* The button is a tag inside the sentence rather than a fragment glued
            after it, so a language that opens with the offer and explains
            afterwards can. Its own idle label is the tag's contents; the busy
            one belongs to the button, which is the only thing that knows it. */}
        {t.rich("otherAddress", {
          action: (chunks) => <SignInOtherAddressButton label={chunks} />,
        })}
      </p>
    </div>
  );
}

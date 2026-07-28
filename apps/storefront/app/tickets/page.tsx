import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { Alert, AlertDescription, AlertTitle, Button, PageHeader, StorefrontShell } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { MyInfo } from "@/components/my-info";
import { SignInOtherAddressButton } from "@/components/sign-in-other-address-button";
import { TicketSaleCard } from "@/components/ticket-sale-card";
import {
  customerSessionToken,
  getCustomerArea,
  getCustomerSession,
  type CustomerArea as CustomerAreaData,
  type TicketSale,
} from "@/lib/customer-session";

// Same posture as every other Storefront page: rendered per request with
// uncached reads. There is no static output here to opt out of.
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Your tickets · Multiticketing",
  // The Customer Area is private; keep it out of search results entirely.
  robots: { index: false, follow: false },
};

/**
 * The Customer Area: every Ticket Sale the signed-in Customer owns, upcoming and
 * past, across every Organization they have bought from.
 *
 * The read is scoped by the Customer Session and by nothing else — this page
 * passes no identifier of any kind to the API, and there is no route parameter
 * here that could name a different Customer.
 */
export default async function CustomerAreaPage() {
  const [area, session] = await Promise.all([getCustomerArea(), getCustomerSession()]);

  // A Confirmation Link session names the one Ticket Sale it was minted for.
  // The page reads it only to explain itself: the narrowing is enforced by the
  // API, which returns that one sale and nothing else regardless of what this
  // page believes.
  const fromConfirmationLink = session.status === "ok" && session.data.ticket_sale_id !== null;

  if (area.status === "signed-out") {
    // A session that ran out or was signed out elsewhere sends the visitor to
    // sign-in rather than showing them an error. The `expired` flag separates
    // "your session ended" from "you were never signed in", so the sign-in page
    // can explain what happened and clear the dead cookie.
    const hadSession = Boolean(await customerSessionToken());
    redirect(hadSession ? "/signin?expired=1&next=/tickets" : "/signin?next=/tickets");
  }

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader
          title={fromConfirmationLink ? "Your purchase" : "Your tickets"}
          description={
            fromConfirmationLink
              ? "This is the purchase your confirmation email links to."
              : "Everything you've bought, from every organizer, in one place."
          }
        />

        {fromConfirmationLink && area.status === "ok" && linkedSaleReversed(area.data) ? (
          <ReversedSaleNotice />
        ) : null}

        {fromConfirmationLink ? <ConfirmationLinkNotice /> : null}

        {area.status === "error" ? (
          <Alert variant="destructive">
            <AlertTitle>We couldn&apos;t load your tickets</AlertTitle>
            <AlertDescription>{area.message}</AlertDescription>
          </Alert>
        ) : (
          <CustomerArea upcoming={area.data.upcoming} past={area.data.past} />
        )}

        {/* "My info" is the Customer Area's only write, and it belongs to the
            full session alone. A Confirmation Link arrival is not shown it: that
            session proves possession of a forwarded email rather than ownership
            of the address, and the API refuses the edit behind it (#102). It
            sits below the tickets because the tickets are what someone came
            for. */}
        {session.status === "ok" && !fromConfirmationLink ? (
          <MyInfo profile={session.data} />
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
  return [...area.upcoming, ...area.past].some((sale) => sale.status === "reversed");
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
function ReversedSaleNotice() {
  return (
    <Alert variant="destructive">
      <AlertTitle>This purchase was reversed</AlertTitle>
      <AlertDescription>
        These tickets were released and are no longer valid for entry. Contact the organizer if you
        believe that is a mistake.
      </AlertDescription>
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
function ConfirmationLinkNotice() {
  return (
    <Alert>
      <AlertTitle>You&apos;re viewing one purchase</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>
          You opened this from a confirmation email, so it shows that purchase only. Sign in with a
          passcode to see everything else you&apos;ve bought.
        </p>
        <Button asChild size="sm">
          <Link href="/signin?next=/tickets">Sign in with a passcode</Link>
        </Button>
      </AlertDescription>
    </Alert>
  );
}

function CustomerArea({ upcoming, past }: { upcoming: TicketSale[]; past: TicketSale[] }) {
  if (upcoming.length === 0 && past.length === 0) {
    return <NoPurchases />;
  }

  return (
    <>
      <section className="space-y-4">
        <h2 className="text-lg font-semibold tracking-tight">Upcoming</h2>
        {upcoming.length > 0 ? (
          <ul className="space-y-4">
            {upcoming.map((sale) => (
              <TicketSaleCard key={sale.id} sale={sale} />
            ))}
          </ul>
        ) : (
          <div className="rounded-lg border border-dashed p-8 text-center">
            <p className="font-medium">Nothing coming up</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Your past tickets are below, and there&apos;s always something new to find.
            </p>
            <Button asChild variant="secondary" className="mt-4">
              <Link href="/">Discover events</Link>
            </Button>
          </div>
        )}
      </section>

      {past.length > 0 ? (
        <section className="space-y-4">
          <h2 className="text-lg font-semibold tracking-tight">Past</h2>
          <ul className="space-y-4">
            {past.map((sale) => (
              <TicketSaleCard key={sale.id} sale={sale} />
            ))}
          </ul>
        </section>
      ) : null}
    </>
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
function NoPurchases() {
  return (
    <div className="rounded-lg border border-dashed p-10 text-center">
      <p className="font-medium">No tickets yet</p>
      <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
        When you buy tickets, they&apos;ll show up here — from every organizer you buy from.
      </p>
      <Button asChild className="mt-6">
        <Link href="/">Discover events</Link>
      </Button>
      <p className="mx-auto mt-6 max-w-md text-sm text-muted-foreground">
        Bought with a different email address? Tickets stay with the address they were bought
        under. <SignInOtherAddressButton />.
      </p>
    </div>
  );
}

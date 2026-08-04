import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Breadcrumb, PageHeader } from "@ticket-pos/ui";

import { EmptyState, EventGrid } from "@/components/event-grid";
import { FollowButton } from "@/components/follow-button";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import { getOrganizationEvents } from "@/lib/api";
import { getFollows } from "@/lib/customer-session";
import { localizedPath, toAppLocale } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";

export const dynamic = "force-dynamic";

type OrganizationPageProps = {
  params: Promise<{ locale: string; orgSlug: string }>;
};

export async function generateMetadata({ params }: OrganizationPageProps): Promise<Metadata> {
  const { locale, orgSlug } = await params;
  const data = await getOrganizationEvents(orgSlug);
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "organization" });
  // A slug nobody owns is a 404, and a 404 has no address to canonicalise and
  // no translation to pair with.
  if (!data) return { title: t("notFoundTitle") };
  const { canonical, languages } = localeAlternates(
    `/${orgSlug}`,
    toAppLocale(locale),
    storefrontBaseUrl(),
  );
  return {
    // The Organization's name is its own and is never translated; only the
    // words around it are.
    title: t("metaTitle", { organization: data.organization.name }),
    description: t("metaDescription", { organization: data.organization.name }),
    alternates: { canonical, languages },
  };
}

export default async function OrganizationPage({ params }: OrganizationPageProps) {
  const { locale, orgSlug } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const data = await getOrganizationEvents(orgSlug);

  if (!data) {
    notFound();
  }

  const { organization, upcoming, past } = data;

  // Whether this visitor Follows this Organization, read on the server so the
  // control renders in its true state on first paint rather than flickering into
  // it. An anonymous visitor never reaches the API for this — no Customer
  // Session cookie, no call — so a signed-out reader pays nothing, and the
  // control is simply not drawn for them: following while signed out has to
  // survive a round trip through sign-in and is its own problem (#219).
  //
  // A failed read is treated as signed out for this one purpose. The page is the
  // Organization's Events; a Follow control that could not be resolved is worth
  // less than the page being served without it.
  const follows = await getFollows();
  const following =
    follows.status === "ok" &&
    follows.data.follows.some(
      (follow) => follow.type === "organization" && follow.organization.slug === organization.slug,
    );

  const t = await getTranslations("organization");
  const shell = await getTranslations("shell");
  const explorer = await getTranslations("explorer");

  return (
    <StorefrontShell
      organizationName={organization.name}
      organizationLogoUrl={organization.logo_url}
      customerNav={<HeaderCustomerNav />}
    >
      <div className="mx-auto w-full max-w-6xl space-y-10 px-4 py-10 sm:py-12">
        <Breadcrumb
          label={shell("breadcrumbLabel")}
          // The crumbs are plain anchors in the shared UI package, so their
          // addresses carry the locale explicitly rather than through the
          // navigation helpers.
          items={[
            // The explorer is named by the explorer's own title, so the crumb
            // and the page it points at cannot drift apart.
            { label: explorer("title"), href: localizedPath(toAppLocale(locale), "/") },
            { label: organization.name },
          ]}
        />
        <div className="flex flex-wrap items-start justify-between gap-4">
          <PageHeader title={organization.name} description={t("subtitle")} />
          {follows.status === "ok" ? (
            <FollowButton
              endpoint={`/api/customer/follows/organizations/${encodeURIComponent(organization.slug)}`}
              following={following}
              subjectName={organization.name}
            />
          ) : null}
        </div>

        <section className="space-y-4">
          <h2 className="text-lg font-semibold tracking-tight">{t("upcomingHeading")}</h2>
          {upcoming.length > 0 ? (
            <EventGrid events={upcoming} showOrganization={false} />
          ) : (
            <EmptyState title={t("emptyTitle")} description={t("emptyDescription")} />
          )}
        </section>

        {past.length > 0 ? (
          <section className="space-y-4">
            <h2 className="text-lg font-semibold tracking-tight">{t("pastHeading")}</h2>
            <EventGrid events={past} showOrganization={false} />
          </section>
        ) : null}
      </div>
    </StorefrontShell>
  );
}

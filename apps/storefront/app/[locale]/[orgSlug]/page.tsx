import type { Metadata } from "next";
import { setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Breadcrumb, PageHeader } from "@ticket-pos/ui";

import { EmptyState, EventGrid } from "@/components/event-grid";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { getOrganizationEvents } from "@/lib/api";
import { localizedPath, toAppLocale } from "@/lib/locale";

export const dynamic = "force-dynamic";

type OrganizationPageProps = {
  params: Promise<{ locale: string; orgSlug: string }>;
};

export async function generateMetadata({ params }: OrganizationPageProps): Promise<Metadata> {
  const { orgSlug } = await params;
  const data = await getOrganizationEvents(orgSlug);
  if (!data) return { title: "Organization not found" };
  return {
    title: `${data.organization.name} · Events`,
    description: `Upcoming events from ${data.organization.name}.`,
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

  return (
    <StorefrontShell
      organizationName={organization.name}
      organizationLogoUrl={organization.logo_url}
      customerNav={<HeaderCustomerNav />}
    >
      <div className="mx-auto w-full max-w-6xl space-y-10 px-4 py-10 sm:py-12">
        <Breadcrumb
          // The crumbs are plain anchors in the shared UI package, so their
          // addresses carry the locale explicitly rather than through the
          // navigation helpers.
          items={[
            { label: "Discover events", href: localizedPath(toAppLocale(locale), "/") },
            { label: organization.name },
          ]}
        />
        <PageHeader
          title={organization.name}
          description="Upcoming events and past highlights."
        />

        <section className="space-y-4">
          <h2 className="text-lg font-semibold tracking-tight">Upcoming events</h2>
          {upcoming.length > 0 ? (
            <EventGrid events={upcoming} showOrganization={false} />
          ) : (
            <EmptyState
              title="No upcoming events"
              description="This organizer has no events on sale right now. Check back soon."
            />
          )}
        </section>

        {past.length > 0 ? (
          <section className="space-y-4">
            <h2 className="text-lg font-semibold tracking-tight">Past events</h2>
            <EventGrid events={past} showOrganization={false} />
          </section>
        ) : null}
      </div>
    </StorefrontShell>
  );
}

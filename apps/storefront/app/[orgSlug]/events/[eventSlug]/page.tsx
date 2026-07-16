import {
  Badge,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  InProgressPanel,
  StorefrontShell,
} from "@ticket-pos/ui";

import { getPublicOrganization } from "@/lib/api";

type EventPageProps = {
  params: Promise<{
    orgSlug: string;
    eventSlug: string;
  }>;
};

function titleCase(slug: string): string {
  return slug
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export default async function EventPage({ params }: EventPageProps) {
  const { orgSlug, eventSlug } = await params;
  const eventName = titleCase(eventSlug);
  const organization = await getPublicOrganization(orgSlug);
  const organizationName = organization?.name ?? titleCase(orgSlug);

  return (
    <StorefrontShell organizationName={organizationName} organizationLogoUrl={organization?.logo_url}>
      <article className="mx-auto w-full max-w-5xl space-y-8 px-4 py-8">
        <header className="space-y-4 border-b pb-8">
          <div className="flex min-h-48 items-end rounded-xl bg-muted p-8">
            <div className="space-y-2">
              <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{eventName}</h1>
              <p className="text-muted-foreground">Presented by {organizationName}</p>
            </div>
          </div>
        </header>

        <section className="space-y-4">
          <h2 className="text-xl font-semibold">Tickets</h2>
          <Card>
            <CardHeader>
              <CardTitle>General Admission</CardTitle>
              <CardDescription>Placeholder ticket type for layout preview.</CardDescription>
            </CardHeader>
            <CardContent className="flex items-center justify-between gap-4">
              <div>
                <p className="text-2xl font-semibold">$25</p>
                <p className="text-sm text-muted-foreground">1,250 remaining</p>
              </div>
              <Badge>Preview</Badge>
            </CardContent>
          </Card>
        </section>

        <InProgressPanel
          title="Checkout is under development"
          description="Ticket selection and payment will be available here soon."
        />
      </article>
    </StorefrontShell>
  );
}

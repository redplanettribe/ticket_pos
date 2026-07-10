import { Badge, Card, CardContent, CardDescription, CardHeader, CardTitle, StorefrontShell } from "@ticket-pos/ui";

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
  const organizationName = titleCase(orgSlug);

  return (
    <StorefrontShell organizationName={organizationName}>
      <article className="mx-auto w-full max-w-5xl px-4 py-8">
        <header className="space-y-4 border-b pb-8">
          <div className="flex min-h-48 items-end rounded-xl bg-muted p-8">
            <div className="space-y-2">
              <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{eventName}</h1>
              <p className="text-muted-foreground">Presented by {organizationName}</p>
            </div>
          </div>
        </header>

        <section className="py-8">
          <h2 className="mb-4 text-xl font-semibold">Tickets</h2>
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
      </article>
    </StorefrontShell>
  );
}

import { Card, CardContent, CardDescription, CardHeader, CardTitle, StorefrontShell } from "@ticket-pos/ui";

export default function HomePage() {
  return (
    <StorefrontShell>
      <div className="mx-auto w-full max-w-5xl px-4 py-12">
        <Card>
          <CardHeader>
            <CardTitle>Ticket POS Storefront</CardTitle>
            <CardDescription>Public ticket sales for organizations and events.</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground">
              Event pages live at <code className="rounded bg-muted px-1.5 py-0.5">/{`{orgSlug}`}/events/{`{eventSlug}`}</code>.
            </p>
          </CardContent>
        </Card>
      </div>
    </StorefrontShell>
  );
}

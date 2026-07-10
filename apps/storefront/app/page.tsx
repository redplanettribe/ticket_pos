import { InProgressPanel, PageHeader, StorefrontShell } from "@ticket-pos/ui";

export default function HomePage() {
  return (
    <StorefrontShell>
      <div className="mx-auto w-full max-w-5xl px-4 py-12">
        <div className="space-y-6">
          <PageHeader
            title="Ticket POS Storefront"
            description="Public ticket sales for organizations and events."
          />
          <InProgressPanel
            title="Storefront is under development"
            description="Event pages are available for layout preview. Checkout and live ticket sales are coming soon."
          />
        </div>
      </div>
    </StorefrontShell>
  );
}

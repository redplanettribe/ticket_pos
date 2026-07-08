type EventPageProps = {
  params: Promise<{
    orgSlug: string;
    eventSlug: string;
  }>;
};

export default async function EventPage({ params }: EventPageProps) {
  const { orgSlug, eventSlug } = await params;

  return (
    <main>
      <h1>Event</h1>
      <p>Organization: {orgSlug}</p>
      <p>Event: {eventSlug}</p>
    </main>
  );
}

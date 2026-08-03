import { redirect } from "next/navigation";

type EventTagsPageProps = {
  params: Promise<{ id: string }>;
};

// Tags moved into the Event's Details page. The address stays reachable so
// bookmarks and history land on the page that now manages them; Details does
// the role guard from here.
export default async function EventTagsPage({ params }: EventTagsPageProps) {
  const { id } = await params;
  redirect(`/events/${id}`);
}

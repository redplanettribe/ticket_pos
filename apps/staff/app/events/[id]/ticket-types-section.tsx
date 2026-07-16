"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import {
  fetchEventsJSON,
  formatPriceCents,
  parsePriceToCents,
  type TicketType,
} from "@/lib/events-api";

type TicketTypesSectionProps = {
  eventId: string;
  eventStatus: string;
  onTicketTypeCountChange?: (count: number) => void;
  missingWarning?: boolean;
};

type TicketTypeFormState = {
  name: string;
  description: string;
  price: string;
  capacity: string;
};

const emptyForm: TicketTypeFormState = {
  name: "",
  description: "",
  price: "",
  capacity: "",
};

export function TicketTypesSection({
  eventId,
  eventStatus,
  onTicketTypeCountChange,
  missingWarning,
}: TicketTypesSectionProps) {
  const [loading, setLoading] = useState(true);
  const [ticketTypes, setTicketTypes] = useState<TicketType[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<TicketType | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TicketType | null>(null);
  const [form, setForm] = useState<TicketTypeFormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [organizationCurrency, setOrganizationCurrency] = useState("USD");

  const canDelete = eventStatus === "draft";

  const loadTicketTypes = useCallback(async () => {
    setLoading(true);
    try {
      const types = await fetchEventsJSON<TicketType[]>(`/api/events/${eventId}/ticket-types`);
      setTicketTypes(types);
      onTicketTypeCountChange?.(types.length);
    } catch (loadError) {
      toast.error(loadError instanceof Error ? loadError.message : "Failed to load ticket types");
    } finally {
      setLoading(false);
    }
  }, [eventId, onTicketTypeCountChange]);

  useEffect(() => {
    void loadTicketTypes();
  }, [loadTicketTypes]);

  useEffect(() => {
    async function loadCurrency() {
      try {
        const org = await fetchEventsJSON<{ currency: string }>("/api/settings/organization");
        setOrganizationCurrency(org.currency);
      } catch {
        setOrganizationCurrency("USD");
      }
    }
    void loadCurrency();
  }, []);

  function openAddDialog() {
    setForm(emptyForm);
    setAddOpen(true);
  }

  function openEditDialog(ticketType: TicketType) {
    setEditTarget(ticketType);
    setForm({
      name: ticketType.name,
      description: ticketType.description ?? "",
      price: (ticketType.price_cents / 100).toFixed(2),
      capacity: String(ticketType.capacity),
    });
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const priceCents = parsePriceToCents(form.price);
    const capacity = Number.parseInt(form.capacity, 10);
    if (priceCents === null) {
      toast.error("Enter a valid price");
      return;
    }
    if (!Number.isFinite(capacity) || capacity <= 0) {
      toast.error("Enter a valid capacity");
      return;
    }

    setSaving(true);
    try {
      await fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types`, {
        method: "POST",
        body: JSON.stringify({
          name: form.name,
          description: form.description || null,
          price_cents: priceCents,
          capacity,
        }),
      });
      setAddOpen(false);
      setForm(emptyForm);
      await loadTicketTypes();
      toast.success("Ticket type added");
    } catch (createError) {
      toast.error(createError instanceof Error ? createError.message : "Failed to add ticket type");
    } finally {
      setSaving(false);
    }
  }

  async function handleUpdate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editTarget) {
      return;
    }

    const priceCents = parsePriceToCents(form.price);
    const capacity = Number.parseInt(form.capacity, 10);
    if (priceCents === null) {
      toast.error("Enter a valid price");
      return;
    }
    if (!Number.isFinite(capacity) || capacity <= 0) {
      toast.error("Enter a valid capacity");
      return;
    }

    setSaving(true);
    try {
      await fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${editTarget.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          name: form.name,
          description: form.description || null,
          price_cents: priceCents,
          capacity,
          sort_order: editTarget.sort_order,
        }),
      });
      setEditTarget(null);
      setForm(emptyForm);
      await loadTicketTypes();
      toast.success("Ticket type updated");
    } catch (updateError) {
      toast.error(updateError instanceof Error ? updateError.message : "Failed to update ticket type");
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) {
      return;
    }

    try {
      await fetchEventsJSON<{ message: string }>(
        `/api/events/${eventId}/ticket-types/${deleteTarget.id}`,
        { method: "DELETE" },
      );
      setDeleteTarget(null);
      await loadTicketTypes();
      toast.success("Ticket type deleted");
    } catch (deleteError) {
      toast.error(deleteError instanceof Error ? deleteError.message : "Failed to delete ticket type");
    }
  }

  async function moveTicketType(ticketType: TicketType, direction: "up" | "down") {
    const index = ticketTypes.findIndex((item) => item.id === ticketType.id);
    if (index < 0) {
      return;
    }
    const swapIndex = direction === "up" ? index - 1 : index + 1;
    if (swapIndex < 0 || swapIndex >= ticketTypes.length) {
      return;
    }

    const other = ticketTypes[swapIndex];
    try {
      await Promise.all([
        fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${ticketType.id}`, {
          method: "PATCH",
          body: JSON.stringify({
            name: ticketType.name,
            description: ticketType.description,
            price_cents: ticketType.price_cents,
            capacity: ticketType.capacity,
            sort_order: other.sort_order,
          }),
        }),
        fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${other.id}`, {
          method: "PATCH",
          body: JSON.stringify({
            name: other.name,
            description: other.description,
            price_cents: other.price_cents,
            capacity: other.capacity,
            sort_order: ticketType.sort_order,
          }),
        }),
      ]);
      await loadTicketTypes();
    } catch (reorderError) {
      toast.error(reorderError instanceof Error ? reorderError.message : "Failed to reorder ticket types");
    }
  }

  function ticketTypeForm(idPrefix: string, onSubmit: (event: FormEvent<HTMLFormElement>) => void) {
    const currency = ticketTypes[0]?.currency ?? editTarget?.currency ?? organizationCurrency;

    return (
      <form className="space-y-4" onSubmit={(event) => void onSubmit(event)}>
        <FormField id={`${idPrefix}-name`} label="Name">
          <Input
            id={`${idPrefix}-name`}
            value={form.name}
            onChange={(changeEvent) => setForm((current) => ({ ...current, name: changeEvent.target.value }))}
            required
          />
        </FormField>
        <FormField id={`${idPrefix}-description`} label="Description">
          <Textarea
            id={`${idPrefix}-description`}
            value={form.description}
            onChange={(changeEvent) =>
              setForm((current) => ({ ...current, description: changeEvent.target.value }))
            }
            rows={3}
            placeholder="Optional details about what this ticket includes"
          />
        </FormField>
        <div className="grid gap-4 md:grid-cols-2">
          <FormField id={`${idPrefix}-price`} label={`Price (${currency})`}>
            <Input
              id={`${idPrefix}-price`}
              type="number"
              min="0"
              step="0.01"
              value={form.price}
              onChange={(changeEvent) => setForm((current) => ({ ...current, price: changeEvent.target.value }))}
              required
            />
          </FormField>
          <FormField id={`${idPrefix}-capacity`} label="Capacity">
            <Input
              id={`${idPrefix}-capacity`}
              type="number"
              min="1"
              step="1"
              value={form.capacity}
              onChange={(changeEvent) => setForm((current) => ({ ...current, capacity: changeEvent.target.value }))}
              required
            />
          </FormField>
        </div>
        <Button type="submit" disabled={saving}>
          {saving ? "Saving..." : "Save ticket type"}
        </Button>
      </form>
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>Ticket types</CardTitle>
          <CardDescription>
            Define what you sell for this Event. Prices use your organization currency.
          </CardDescription>
        </div>
        <Button type="button" onClick={openAddDialog}>
          Add ticket type
        </Button>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading ticket types...</p>
        ) : ticketTypes.length === 0 ? (
          <p className={`text-sm ${missingWarning ? "text-destructive" : "text-muted-foreground"}`} role={missingWarning ? "alert" : undefined}>
            No ticket types yet. Add at least one before publishing this Event.
          </p>
        ) : (
          <div className="space-y-3">
            {ticketTypes.map((ticketType, index) => (
              <div
                key={ticketType.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                <div>
                  <p className="font-medium">{ticketType.name}</p>
                  {ticketType.description ? (
                    <p className="text-sm text-muted-foreground">{ticketType.description}</p>
                  ) : null}
                  <p className="text-sm text-muted-foreground">
                    {formatPriceCents(ticketType.price_cents, ticketType.currency)} · Capacity{" "}
                    {ticketType.capacity} · Sold {ticketType.sold_count}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={index === 0}
                    onClick={() => void moveTicketType(ticketType, "up")}
                  >
                    Move up
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={index === ticketTypes.length - 1}
                    onClick={() => void moveTicketType(ticketType, "down")}
                  >
                    Move down
                  </Button>
                  <Button type="button" variant="outline" size="sm" onClick={() => openEditDialog(ticketType)}>
                    Edit
                  </Button>
                  {canDelete ? (
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      onClick={() => setDeleteTarget(ticketType)}
                    >
                      Delete
                    </Button>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add ticket type</DialogTitle>
            <DialogDescription>Create a purchasable ticket category for this Event.</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("add", handleCreate)}
        </DialogContent>
      </Dialog>

      <Dialog open={editTarget !== null} onOpenChange={(open) => !open && setEditTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit ticket type</DialogTitle>
            <DialogDescription>Update name, price, and capacity for this ticket category.</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("edit", handleUpdate)}
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete ticket type?</DialogTitle>
            <DialogDescription>
              Remove <strong>{deleteTarget?.name}</strong> from this draft Event? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button type="button" variant="destructive" onClick={() => void handleDelete()}>
              Delete ticket type
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

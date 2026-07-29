"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";

import {
  Badge,
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
  ApiError,
  dateTimeLocalToISO,
  fetchEventsJSON,
  formatEventStartDate,
  formatPriceCents,
  isoToDateTimeLocal,
  parsePriceToCents,
  type TicketType,
} from "@/lib/events-api";
import { buyerUnitPriceCents, netProceedsUnitCents, type FeeHandling, type FeeRates } from "@/lib/fees";
import {
  promotionErrorMessage,
  promotionState,
  promotionStateBadgeVariant,
  PROMOTION_STATE_LABELS,
} from "@/lib/promotions";

type TicketTypesSectionProps = {
  eventId: string;
  eventStatus: string;
  /** The Event's Fee Handling and the fee schedule it is read with (ADR 0014). */
  feeHandling: FeeHandling;
  feeRates: FeeRates;
  /** The Event's timezone: Promotion windows are typed and read in it (ADR 0021). */
  eventTimezone: string | null;
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

/** The whole Promotion, as typed: a Promotional Price and the window it holds for. */
type PromotionFormState = {
  price: string;
  startsAtLocal: string;
  endsAtLocal: string;
};

const emptyPromotionForm: PromotionFormState = {
  price: "",
  startsAtLocal: "",
  endsAtLocal: "",
};

export function TicketTypesSection({
  eventId,
  eventStatus,
  feeHandling,
  feeRates,
  eventTimezone,
  onTicketTypeCountChange,
  missingWarning,
}: TicketTypesSectionProps) {
  const [loading, setLoading] = useState(true);
  const [ticketTypes, setTicketTypes] = useState<TicketType[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<TicketType | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TicketType | null>(null);
  const [promotionTarget, setPromotionTarget] = useState<TicketType | null>(null);
  const [form, setForm] = useState<TicketTypeFormState>(emptyForm);
  const [promotionForm, setPromotionForm] = useState<PromotionFormState>(emptyPromotionForm);
  const [saving, setSaving] = useState(false);
  const [savingPromotion, setSavingPromotion] = useState(false);
  // Rejections the organizer must act on stay next to the field that caused
  // them rather than in a toast that scrolls away.
  const [ticketTypeError, setTicketTypeError] = useState<string | null>(null);
  const [promotionError, setPromotionError] = useState<string | null>(null);
  const [organizationCurrency, setOrganizationCurrency] = useState("USD");

  const canDelete = eventStatus === "draft";
  // Promotion windows are typed and read in the Event's timezone. A draft that
  // has not picked one yet falls back to UTC, and the field labels say which.
  const timezone = eventTimezone ?? "UTC";

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
    setTicketTypeError(null);
    setAddOpen(true);
  }

  function openEditDialog(ticketType: TicketType) {
    setEditTarget(ticketType);
    setTicketTypeError(null);
    setForm({
      name: ticketType.name,
      description: ticketType.description ?? "",
      price: (ticketType.price_cents / 100).toFixed(2),
      capacity: String(ticketType.capacity),
    });
  }

  function openPromotionDialog(ticketType: TicketType) {
    const promotion = ticketType.promotion;
    setPromotionTarget(ticketType);
    setPromotionError(null);
    setPromotionForm(
      promotion === null
        ? emptyPromotionForm
        : {
            price: (promotion.promotional_price_cents / 100).toFixed(2),
            startsAtLocal: isoToDateTimeLocal(promotion.starts_at, timezone),
            endsAtLocal: isoToDateTimeLocal(promotion.ends_at, timezone),
          },
    );
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
      // A List Price edit that would sink to or below a live Promotional Price
      // is rejected: say so on the form, where the price the organizer just
      // typed still is.
      const inline =
        updateError instanceof ApiError ? promotionErrorMessage(updateError.code) : null;
      if (inline) {
        setTicketTypeError(inline);
        return;
      }
      toast.error(updateError instanceof Error ? updateError.message : "Failed to update ticket type");
    } finally {
      setSaving(false);
    }
  }

  async function handleSavePromotion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!promotionTarget) {
      return;
    }

    setPromotionError(null);
    const promotionalPriceCents = parsePriceToCents(promotionForm.price);
    if (promotionalPriceCents === null) {
      setPromotionError("Enter a valid promotional price");
      return;
    }
    if (promotionalPriceCents >= promotionTarget.price_cents) {
      setPromotionError(
        promotionErrorMessage("PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE") ?? "Invalid promotional price",
      );
      return;
    }
    const endsAt = dateTimeLocalToISO(promotionForm.endsAtLocal, timezone);
    if (endsAt === null) {
      setPromotionError("Enter the date and time the promotion ends");
      return;
    }
    const startsAt = dateTimeLocalToISO(promotionForm.startsAtLocal, timezone);
    if (startsAt !== null && startsAt >= endsAt) {
      setPromotionError("The promotion must end after it starts");
      return;
    }

    setSavingPromotion(true);
    try {
      await fetchEventsJSON<TicketType>(
        `/api/events/${eventId}/ticket-types/${promotionTarget.id}/promotion`,
        {
          // The slot is either empty or filled; there is no partial edit of a
          // Promotion, so an update restates the price and the window.
          method: promotionTarget.promotion === null ? "POST" : "PATCH",
          body: JSON.stringify({
            promotional_price_cents: promotionalPriceCents,
            starts_at: startsAt,
            ends_at: endsAt,
          }),
        },
      );
      setPromotionTarget(null);
      setPromotionForm(emptyPromotionForm);
      await loadTicketTypes();
      toast.success("Promotion saved");
    } catch (saveError) {
      const inline = saveError instanceof ApiError ? promotionErrorMessage(saveError.code) : null;
      setPromotionError(
        inline ?? (saveError instanceof Error ? saveError.message : "Failed to save the promotion"),
      );
    } finally {
      setSavingPromotion(false);
    }
  }

  async function handleRemovePromotion() {
    if (!promotionTarget) {
      return;
    }

    setPromotionError(null);
    setSavingPromotion(true);
    try {
      await fetchEventsJSON<TicketType>(
        `/api/events/${eventId}/ticket-types/${promotionTarget.id}/promotion`,
        { method: "DELETE" },
      );
      setPromotionTarget(null);
      setPromotionForm(emptyPromotionForm);
      await loadTicketTypes();
      toast.success("Promotion removed");
    } catch (removeError) {
      const inline = removeError instanceof ApiError ? promotionErrorMessage(removeError.code) : null;
      setPromotionError(
        inline ?? (removeError instanceof Error ? removeError.message : "Failed to remove the promotion"),
      );
    } finally {
      setSavingPromotion(false);
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

  const currency = ticketTypes[0]?.currency ?? editTarget?.currency ?? organizationCurrency;

  // The consequence of a price being typed, derived with the same arithmetic
  // checkout uses: under pass-on what the buyer is charged, under absorb what
  // the sale leaves the Organization. A Promotional Price is the base price
  // while its window holds, so it reads through exactly the same line.
  function derivedPriceLine(priceCents: number | null): string | null {
    if (priceCents === null) {
      return null;
    }
    return feeHandling === "pass_on"
      ? `Buyers will pay ${formatPriceCents(buyerUnitPriceCents("pass_on", priceCents, feeRates), currency)}`
      : `You'll receive ${formatPriceCents(netProceedsUnitCents("absorb", priceCents, feeRates), currency)} per ticket`;
  }

  function ticketTypeForm(idPrefix: string, onSubmit: (event: FormEvent<HTMLFormElement>) => void) {
    const derivedLine = derivedPriceLine(parsePriceToCents(form.price));

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
        {derivedLine ? <p className="text-sm text-muted-foreground">{derivedLine}</p> : null}
        {ticketTypeError ? (
          <p className="text-sm text-destructive" role="alert">
            {ticketTypeError}
          </p>
        ) : null}
        <Button type="submit" disabled={saving}>
          {saving ? "Saving..." : "Save ticket type"}
        </Button>
      </form>
    );
  }

  function promotionSummary(ticketType: TicketType) {
    const promotion = ticketType.promotion;
    if (promotion === null) {
      return null;
    }
    const state = promotionState(promotion, new Date());
    return (
      <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <Badge variant={promotionStateBadgeVariant(state)}>
          Promotion · {PROMOTION_STATE_LABELS[state]}
        </Badge>
        <span>
          {formatPriceCents(promotion.promotional_price_cents, ticketType.currency)}
          {promotion.starts_at
            ? ` · From ${formatEventStartDate(promotion.starts_at, timezone)}`
            : ""}{" "}
          · Until {formatEventStartDate(promotion.ends_at, timezone)}
        </span>
      </p>
    );
  }

  const promotionDerivedLine = derivedPriceLine(parsePriceToCents(promotionForm.price));

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
                  {promotionSummary(ticketType)}
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
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => openPromotionDialog(ticketType)}
                  >
                    {ticketType.promotion === null ? "Add promotion" : "Edit promotion"}
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

      <Dialog open={promotionTarget !== null} onOpenChange={(open) => !open && setPromotionTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {promotionTarget?.promotion === null ? "Add promotion" : "Edit promotion"}
            </DialogTitle>
            <DialogDescription>
              A promotion sells <strong>{promotionTarget?.name}</strong> at a lower price for a set
              window. Outside the window the list price of{" "}
              {promotionTarget
                ? formatPriceCents(promotionTarget.price_cents, promotionTarget.currency)
                : ""}{" "}
              applies again.
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void handleSavePromotion(event)}>
            <FormField
              id="promotion-price"
              label={`Promotional price (${currency})`}
              description="Must be below the list price. Zero makes the ticket free while the promotion is live."
            >
              <Input
                id="promotion-price"
                type="number"
                min="0"
                step="0.01"
                value={promotionForm.price}
                onChange={(changeEvent) =>
                  setPromotionForm((current) => ({ ...current, price: changeEvent.target.value }))
                }
                required
              />
            </FormField>
            <div className="grid gap-4 md:grid-cols-2">
              <FormField
                id="promotion-starts-at"
                label={`Starts at (${timezone})`}
                description="Leave empty to start right away."
              >
                <Input
                  id="promotion-starts-at"
                  type="datetime-local"
                  value={promotionForm.startsAtLocal}
                  onChange={(changeEvent) =>
                    setPromotionForm((current) => ({
                      ...current,
                      startsAtLocal: changeEvent.target.value,
                    }))
                  }
                />
              </FormField>
              <FormField id="promotion-ends-at" label={`Ends at (${timezone})`}>
                <Input
                  id="promotion-ends-at"
                  type="datetime-local"
                  value={promotionForm.endsAtLocal}
                  onChange={(changeEvent) =>
                    setPromotionForm((current) => ({
                      ...current,
                      endsAtLocal: changeEvent.target.value,
                    }))
                  }
                  required
                />
              </FormField>
            </div>
            {promotionDerivedLine ? (
              <p className="text-sm text-muted-foreground">
                {promotionDerivedLine} while the promotion is live
              </p>
            ) : null}
            {promotionError ? (
              <p className="text-sm text-destructive" role="alert">
                {promotionError}
              </p>
            ) : null}
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={savingPromotion}>
                {savingPromotion ? "Saving..." : "Save promotion"}
              </Button>
              {promotionTarget?.promotion ? (
                <Button
                  type="button"
                  variant="destructive"
                  disabled={savingPromotion}
                  onClick={() => void handleRemovePromotion()}
                >
                  Remove promotion
                </Button>
              ) : null}
            </div>
          </form>
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

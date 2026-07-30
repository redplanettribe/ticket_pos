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
  Skeleton,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import {
  ApiError,
  dateTimeLocalToISO,
  fetchEventsJSON,
  formatPriceCents,
  isoToDateTimeLocal,
  parsePriceToCents,
  type TicketType,
} from "@/lib/events-api";
import { buyerUnitPriceCents, netProceedsUnitCents, type FeeHandling, type FeeRates } from "@/lib/fees";
import { promotionErrorMessage, promotionState } from "@/lib/promotions";
import {
  parsePurchaseLimit,
  purchaseLimitFormValue,
  purchaseLimitWireValue,
  PURCHASE_LIMIT_HINT,
  PURCHASE_LIMIT_INVALID_MESSAGE,
} from "@/lib/purchase-limit";

import { TicketTypeCard } from "./ticket-type-card";

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
  /**
   * The Purchase Limit as typed, "" for no Purchase Limit. A string rather than
   * a number so the absent case is the field being empty, which is what most
   * Ticket Types want.
   */
  maxPerCustomer: string;
  capacity: string;
};

// The Add and the Edit dialog share this one form state, so every field must
// appear here: a field missing from the empty form keeps whatever the last Edit
// prefilled and silently carries it into the next Ticket Type added.
const emptyForm: TicketTypeFormState = {
  name: "",
  description: "",
  price: "",
  maxPerCustomer: "",
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
  // A completed reorder, for the live region: the rows re-render in place, so
  // nothing else says it worked.
  const [reorderAnnouncement, setReorderAnnouncement] = useState("");

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
      maxPerCustomer: purchaseLimitFormValue(ticketType.max_per_customer),
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
    // The Purchase Limit is optional, so an empty field is an answer rather than
    // an error and must not go through the capacity guard above.
    const purchaseLimit = parsePurchaseLimit(form.maxPerCustomer);
    if (purchaseLimit.kind === "invalid") {
      toast.error(PURCHASE_LIMIT_INVALID_MESSAGE);
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
          max_per_customer: purchaseLimitWireValue(purchaseLimit),
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
    // Clearing the field lifts the Purchase Limit; lowering or lifting it
    // governs future checkouts only and unmakes no Ticket Sale (ADR 0025).
    const purchaseLimit = parsePurchaseLimit(form.maxPerCustomer);
    if (purchaseLimit.kind === "invalid") {
      toast.error(PURCHASE_LIMIT_INVALID_MESSAGE);
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
          max_per_customer: purchaseLimitWireValue(purchaseLimit),
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
      // The update endpoint is a full restatement, not a partial patch: an
      // omitted key clears the field. Every field a Ticket Type carries has to
      // be echoed back here or a reorder would quietly wipe it — the Purchase
      // Limit especially, since it is the one field an organizer would not think
      // to re-check after nudging a row up or down.
      await Promise.all([
        fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${ticketType.id}`, {
          method: "PATCH",
          body: JSON.stringify({
            name: ticketType.name,
            description: ticketType.description,
            price_cents: ticketType.price_cents,
            capacity: ticketType.capacity,
            max_per_customer: ticketType.max_per_customer,
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
            max_per_customer: other.max_per_customer,
            sort_order: ticketType.sort_order,
          }),
        }),
      ]);
      await loadTicketTypes();
      setReorderAnnouncement(`Moved ${ticketType.name} ${direction}`);
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

  function ticketTypeForm(
    idPrefix: string,
    onSubmit: (event: FormEvent<HTMLFormElement>) => void,
    onCancel: () => void,
  ) {
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
        {/* The Purchase Limit sits under capacity because the two are read
            together and told apart there: capacity is the Event-wide stock, the
            Purchase Limit one Customer's share of it. Deliberately empty by
            default even at a price of zero — a free RSVP for a large venue is a
            legitimate unrestricted case (ADR 0025), so nothing is suggested. */}
        <FormField
          id={`${idPrefix}-max-per-customer`}
          label="Purchase Limit"
          description={PURCHASE_LIMIT_HINT}
        >
          <Input
            id={`${idPrefix}-max-per-customer`}
            type="number"
            min="1"
            step="1"
            value={form.maxPerCustomer}
            onChange={(changeEvent) =>
              setForm((current) => ({ ...current, maxPerCustomer: changeEvent.target.value }))
            }
          />
        </FormField>
        {derivedLine ? <p className="text-sm text-muted-foreground">{derivedLine}</p> : null}
        {ticketTypeError ? (
          <p className="text-sm text-destructive" role="alert">
            {ticketTypeError}
          </p>
        ) : null}
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" disabled={saving} onClick={onCancel}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving} aria-busy={saving}>
            {saving ? "Saving..." : "Save ticket type"}
          </Button>
        </DialogFooter>
      </form>
    );
  }

  // What the card's effective price means under the fee handling. The
  // Promotional Price is the base while its window holds, so the line follows
  // whichever price the buyer would actually be charged.
  function cardPriceLine(ticketType: TicketType): string | null {
    const promotion = ticketType.promotion;
    const effectiveCents =
      promotion !== null && promotionState(promotion, new Date()) === "live"
        ? promotion.promotional_price_cents
        : ticketType.price_cents;
    return derivedPriceLine(effectiveCents);
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
          <div className="space-y-3">
            {[0, 1, 2].map((row) => (
              <Skeleton key={row} className="h-32 w-full rounded-md" />
            ))}
          </div>
        ) : ticketTypes.length === 0 ? (
          <div
            className={`rounded-md border border-dashed p-8 text-center ${missingWarning ? "border-destructive" : ""}`}
            role={missingWarning ? "alert" : undefined}
          >
            <p className={`text-sm ${missingWarning ? "text-destructive" : "text-muted-foreground"}`}>
              No ticket types yet. Add at least one before publishing this Event.
            </p>
            <Button type="button" className="mt-4" onClick={openAddDialog}>
              Add ticket type
            </Button>
          </div>
        ) : (
          <ul className="space-y-3">
            {ticketTypes.map((ticketType, index) => (
              <TicketTypeCard
                key={ticketType.id}
                ticketType={ticketType}
                eventStatus={eventStatus}
                timezone={timezone}
                buyerPriceLine={cardPriceLine(ticketType)}
                isFirst={index === 0}
                isLast={index === ticketTypes.length - 1}
                canDelete={canDelete}
                onMoveUp={() => void moveTicketType(ticketType, "up")}
                onMoveDown={() => void moveTicketType(ticketType, "down")}
                onEdit={() => openEditDialog(ticketType)}
                onPromotion={() => openPromotionDialog(ticketType)}
                onDelete={() => setDeleteTarget(ticketType)}
              />
            ))}
          </ul>
        )}
        {/* A reorder swaps two rows and refetches; without this the change is
            silent to assistive tech. */}
        <p aria-live="polite" className="sr-only">
          {reorderAnnouncement}
        </p>
      </CardContent>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add ticket type</DialogTitle>
            <DialogDescription>Create a purchasable ticket category for this Event.</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("add", handleCreate, () => setAddOpen(false))}
        </DialogContent>
      </Dialog>

      <Dialog open={editTarget !== null} onOpenChange={(open) => !open && setEditTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit ticket type</DialogTitle>
            <DialogDescription>Update name, price, and capacity for this ticket category.</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("edit", handleUpdate, () => setEditTarget(null))}
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
            <div className="grid gap-4 sm:grid-cols-2">
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
              <FormField
                id="promotion-ends-at"
                label={`Ends at (${timezone})`}
                description="Required. The list price applies again from this moment."
              >
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
            {/* Removing a Promotion sits at the far edge, away from Save: the
                two are opposite intents and a mis-click on the wrong one costs
                the organizer the whole window they just typed. */}
            <DialogFooter className="gap-2 sm:justify-between">
              {promotionTarget?.promotion ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="text-destructive hover:bg-destructive/10 hover:text-destructive sm:mr-auto"
                  disabled={savingPromotion}
                  onClick={() => void handleRemovePromotion()}
                >
                  Remove promotion
                </Button>
              ) : null}
              <Button
                type="button"
                variant="outline"
                disabled={savingPromotion}
                onClick={() => setPromotionTarget(null)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={savingPromotion} aria-busy={savingPromotion}>
                {savingPromotion ? "Saving..." : "Save promotion"}
              </Button>
            </DialogFooter>
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

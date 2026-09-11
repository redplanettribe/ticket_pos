"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";
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

import { apiErrorMessage } from "@/lib/api-errors";
import {
  ApiError,
  dateTimeLocalToISO,
  fetchEventsJSON,
  isoToDateTimeLocal,
  parsePriceToCents,
  type TicketType,
} from "@/lib/events-api";
import { buyerUnitPriceCents, netProceedsUnitCents, type FeeHandling, type FeeRates } from "@/lib/fees";
import { formatMoney } from "@/lib/format";
import { isPromotionErrorCode, promotionState } from "@/lib/promotions";
import {
  parsePurchaseLimit,
  purchaseLimitFormValue,
  purchaseLimitWireValue,
} from "@/lib/purchase-limit";

import { QuestionReviewSection } from "./question-review-section";
import { TicketQuestionsDialog } from "./ticket-questions-dialog";
import { TicketTypeCard } from "./ticket-type-card";

type TicketTypesSectionProps = {
  eventId: string;
  eventStatus: string;
  /** The Event's Fee Handling and the fee schedule it is read with (ADR 0014). */
  feeHandling: FeeHandling;
  feeRates: FeeRates;
  /** The Event's timezone: Promotion windows are typed and read in it (ADR 0021). */
  eventTimezone: string | null;
  /**
   * The platform's Ticket Question feature flag, read off the Event payload
   * (#309, ADR 0045). False hides the authoring surface entirely — no button, no
   * dialog — which is the shipped state and the reason nothing on this page
   * differs from before the feature landed.
   */
  ticketQuestionsEnabled: boolean;
  /** The Event's start: no Question Review is accepted after it (ADR 0056). */
  eventStartsAt: string | null;
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
  ticketQuestionsEnabled,
  eventStartsAt,
  onTicketTypeCountChange,
  missingWarning,
}: TicketTypesSectionProps) {
  const t = useTranslations("ticketTypes");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [loading, setLoading] = useState(true);
  const [ticketTypes, setTicketTypes] = useState<TicketType[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<TicketType | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TicketType | null>(null);
  const [promotionTarget, setPromotionTarget] = useState<TicketType | null>(null);
  // The Ticket Type whose Ticket Questions are being authored, or null. Only
  // ever set while the feature flag is on.
  const [questionsTarget, setQuestionsTarget] = useState<TicketType | null>(null);
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
      toast.error(
        apiErrorMessage(errorCopy, loadError instanceof ApiError ? loadError : null) ??
          t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [errorCopy, eventId, onTicketTypeCountChange, t]);

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
      toast.error(t("priceInvalid"));
      return;
    }
    if (!Number.isFinite(capacity) || capacity <= 0) {
      toast.error(t("capacityInvalid"));
      return;
    }
    // The Purchase Limit is optional, so an empty field is an answer rather than
    // an error and must not go through the capacity guard above.
    const purchaseLimit = parsePurchaseLimit(form.maxPerCustomer);
    if (purchaseLimit.kind === "invalid") {
      toast.error(t("purchaseLimitInvalid"));
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
      toast.success(t("addedToast"));
    } catch (createError) {
      toast.error(
        apiErrorMessage(errorCopy, createError instanceof ApiError ? createError : null) ??
          t("addFailed"),
      );
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
      toast.error(t("priceInvalid"));
      return;
    }
    if (!Number.isFinite(capacity) || capacity <= 0) {
      toast.error(t("capacityInvalid"));
      return;
    }
    // Clearing the field lifts the Purchase Limit; lowering or lifting it
    // governs future checkouts only and unmakes no Ticket Sale (ADR 0025).
    const purchaseLimit = parsePurchaseLimit(form.maxPerCustomer);
    if (purchaseLimit.kind === "invalid") {
      toast.error(t("purchaseLimitInvalid"));
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
          // Echoed unchanged. This form does not edit the Sales Cutoff yet, and
          // the endpoint is a full restatement, so omitting it here would clear
          // a cutoff set through the API the next time anybody renamed a Ticket
          // Type (ADR 0070).
          sales_cutoff_at: editTarget.sales_cutoff_at,
          sort_order: editTarget.sort_order,
        }),
      });
      setEditTarget(null);
      setForm(emptyForm);
      await loadTicketTypes();
      toast.success(t("updatedToast"));
    } catch (updateError) {
      // A List Price edit that would sink to or below a live Promotional Price
      // is rejected: say so on the form, where the price the organizer just
      // typed still is. The module decides WHICH refusals belong there; the
      // sentence for each is the catalog's, resolved by the code (ADR 0023).
      const failure = updateError instanceof ApiError ? updateError : null;
      const message = apiErrorMessage(errorCopy, failure);
      if (failure && isPromotionErrorCode(failure.code) && message) {
        setTicketTypeError(message);
        return;
      }
      toast.error(message ?? t("updateFailed"));
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
      setPromotionError(t("promotionPriceInvalid"));
      return;
    }
    if (promotionalPriceCents >= promotionTarget.price_cents) {
      // The refusal the API would give, said before the request is made — and
      // said in the API's own words, from the same catalog entry, so the form
      // and the server never word the same rule two ways.
      setPromotionError(
        apiErrorMessage(errorCopy, { code: "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE" }) ??
          t("promotionPriceInvalid"),
      );
      return;
    }
    const endsAt = dateTimeLocalToISO(promotionForm.endsAtLocal, timezone);
    if (endsAt === null) {
      setPromotionError(t("promotionEndsRequired"));
      return;
    }
    const startsAt = dateTimeLocalToISO(promotionForm.startsAtLocal, timezone);
    if (startsAt !== null && startsAt >= endsAt) {
      setPromotionError(t("promotionWindowInvalid"));
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
      toast.success(t("promotionSavedToast"));
    } catch (saveError) {
      setPromotionError(
        apiErrorMessage(errorCopy, saveError instanceof ApiError ? saveError : null) ??
          t("promotionSaveFailed"),
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
      toast.success(t("promotionRemovedToast"));
    } catch (removeError) {
      setPromotionError(
        apiErrorMessage(errorCopy, removeError instanceof ApiError ? removeError : null) ??
          t("promotionRemoveFailed"),
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
      toast.success(t("deletedToast"));
    } catch (deleteError) {
      toast.error(
        apiErrorMessage(errorCopy, deleteError instanceof ApiError ? deleteError : null) ??
          t("deleteFailed"),
      );
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
      // Limit and the Sales Cutoff especially, since neither is a field an
      // organizer would think to re-check after nudging a row up or down.
      await Promise.all([
        fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${ticketType.id}`, {
          method: "PATCH",
          body: JSON.stringify({
            name: ticketType.name,
            description: ticketType.description,
            price_cents: ticketType.price_cents,
            capacity: ticketType.capacity,
            max_per_customer: ticketType.max_per_customer,
            sales_cutoff_at: ticketType.sales_cutoff_at,
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
            sales_cutoff_at: other.sales_cutoff_at,
            sort_order: ticketType.sort_order,
          }),
        }),
      ]);
      await loadTicketTypes();
      // Two messages, not one with a direction interpolated: "up" and "down"
      // are not a value a Spanish sentence can take in the same slot.
      setReorderAnnouncement(
        direction === "up"
          ? t("movedUp", { name: ticketType.name })
          : t("movedDown", { name: ticketType.name }),
      );
    } catch (reorderError) {
      toast.error(
        apiErrorMessage(errorCopy, reorderError instanceof ApiError ? reorderError : null) ??
          t("reorderFailed"),
      );
    }
  }

  const currency = ticketTypes[0]?.currency ?? editTarget?.currency ?? organizationCurrency;

  // The consequence of a price being typed, derived with the same arithmetic
  // checkout uses: under pass-on what the buyer is charged, under absorb what
  // the sale leaves the Organization. A Promotional Price is the base price
  // while its window holds, so it reads through exactly the same line.
  function derivedPriceLine(priceCents: number | null, promotional = false): string | null {
    if (priceCents === null) {
      return null;
    }
    // The Organization's currency, whichever language is being read.
    if (feeHandling === "pass_on") {
      const amount = formatMoney(
        buyerUnitPriceCents("pass_on", priceCents, feeRates),
        currency,
        locale,
      );
      return promotional ? t("promotionBuyersWillPay", { amount }) : t("buyersWillPay", { amount });
    }
    const amount = formatMoney(
      netProceedsUnitCents("absorb", priceCents, feeRates),
      currency,
      locale,
    );
    return promotional ? t("promotionYouWillReceive", { amount }) : t("youWillReceive", { amount });
  }

  function ticketTypeForm(
    idPrefix: string,
    onSubmit: (event: FormEvent<HTMLFormElement>) => void,
    onCancel: () => void,
  ) {
    const derivedLine = derivedPriceLine(parsePriceToCents(form.price));

    return (
      <form className="space-y-4" onSubmit={(event) => void onSubmit(event)}>
        <FormField id={`${idPrefix}-name`} label={t("nameLabel")}>
          <Input
            id={`${idPrefix}-name`}
            value={form.name}
            onChange={(changeEvent) => setForm((current) => ({ ...current, name: changeEvent.target.value }))}
            required
          />
        </FormField>
        <FormField id={`${idPrefix}-description`} label={t("descriptionLabel")}>
          <Textarea
            id={`${idPrefix}-description`}
            value={form.description}
            onChange={(changeEvent) =>
              setForm((current) => ({ ...current, description: changeEvent.target.value }))
            }
            rows={3}
            placeholder={t("descriptionPlaceholder")}
          />
        </FormField>
        <div className="grid gap-4 md:grid-cols-2">
          <FormField id={`${idPrefix}-price`} label={t("priceLabel", { currency })}>
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
          <FormField id={`${idPrefix}-capacity`} label={t("capacityLabel")}>
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
          label={t("purchaseLimitLabel")}
          description={t("purchaseLimitHint")}
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
            {t("cancel")}
          </Button>
          <Button type="submit" disabled={saving} aria-busy={saving}>
            {saving ? t("saving") : t("save")}
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

  const promotionDerivedLine = derivedPriceLine(parsePriceToCents(promotionForm.price), true);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>{t("title")}</CardTitle>
          <CardDescription>{t("description")}</CardDescription>
        </div>
        <Button type="button" onClick={openAddDialog}>
          {t("add")}
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
              {t("empty")}
            </p>
            <Button type="button" className="mt-4" onClick={openAddDialog}>
              {t("add")}
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
                onQuestions={
                  ticketQuestionsEnabled ? () => setQuestionsTarget(ticketType) : undefined
                }
                onDelete={() => setDeleteTarget(ticketType)}
              />
            ))}
          </ul>
        )}
        {/* The Event's Question Review (#406, ADR 0056): the drafts on every
            Ticket Type above, submitted as one ask. Mounted only while the flag
            is on and there is a Ticket Type to carry questions. */}
        {ticketQuestionsEnabled && !loading && ticketTypes.length > 0 ? (
          <div className="mt-4">
            <QuestionReviewSection
              eventId={eventId}
              eventStartsAt={eventStartsAt}
              ticketTypeIds={ticketTypes.map((ticketType) => ticketType.id)}
            />
          </div>
        ) : null}
        {/* Ticket Question authoring (#309). Mounted only once a Ticket Type has
            been chosen AND the flag is on, so a build with the flag off never
            renders this subtree at all (ADR 0045). */}
        {ticketQuestionsEnabled && questionsTarget ? (
          <TicketQuestionsDialog
            open
            onOpenChange={(open) => {
              if (!open) {
                setQuestionsTarget(null);
              }
            }}
            eventId={eventId}
            ticketTypeId={questionsTarget.id}
            ticketTypeName={questionsTarget.name}
          />
        ) : null}
        {/* A reorder swaps two rows and refetches; without this the change is
            silent to assistive tech. */}
        <p aria-live="polite" className="sr-only">
          {reorderAnnouncement}
        </p>
      </CardContent>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("addDialogTitle")}</DialogTitle>
            <DialogDescription>{t("addDialogDescription")}</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("add", handleCreate, () => setAddOpen(false))}
        </DialogContent>
      </Dialog>

      <Dialog open={editTarget !== null} onOpenChange={(open) => !open && setEditTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("editDialogTitle")}</DialogTitle>
            <DialogDescription>{t("editDialogDescription")}</DialogDescription>
          </DialogHeader>
          {ticketTypeForm("edit", handleUpdate, () => setEditTarget(null))}
        </DialogContent>
      </Dialog>

      <Dialog open={promotionTarget !== null} onOpenChange={(open) => !open && setPromotionTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {promotionTarget?.promotion === null
                ? t("promotionAddTitle")
                : t("promotionEditTitle")}
            </DialogTitle>
            <DialogDescription>
              {t.rich("promotionDialogDescription", {
                name: promotionTarget?.name ?? "",
                price: promotionTarget
                  ? formatMoney(promotionTarget.price_cents, promotionTarget.currency, locale)
                  : "",
                em: (chunks) => <strong>{chunks}</strong>,
              })}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void handleSavePromotion(event)}>
            <FormField
              id="promotion-price"
              label={t("promotionPriceLabel", { currency })}
              description={t("promotionPriceHint")}
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
                label={t("promotionStartsLabel", { timezone })}
                description={t("promotionStartsHint")}
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
                label={t("promotionEndsLabel", { timezone })}
                description={t("promotionEndsHint")}
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
              <p className="text-sm text-muted-foreground">{promotionDerivedLine}</p>
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
                  {t("promotionRemove")}
                </Button>
              ) : null}
              <Button
                type="button"
                variant="outline"
                disabled={savingPromotion}
                onClick={() => setPromotionTarget(null)}
              >
                {t("cancel")}
              </Button>
              <Button type="submit" disabled={savingPromotion} aria-busy={savingPromotion}>
                {savingPromotion ? t("saving") : t("promotionSave")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("deleteDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t.rich("deleteDialogDescription", {
                name: deleteTarget?.name ?? "",
                em: (chunks) => <strong>{chunks}</strong>,
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteTarget(null)}>
              {t("cancel")}
            </Button>
            <Button type="button" variant="destructive" onClick={() => void handleDelete()}>
              {t("deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

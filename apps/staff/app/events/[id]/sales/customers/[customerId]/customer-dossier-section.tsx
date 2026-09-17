"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";

import { Card, CardContent, CardDescription, CardHeader, CardTitle, Skeleton } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import {
  fetchCustomerDossier,
  heldTicketsVisible,
  type CustomerDossierResult,
} from "@/lib/customer-dossier";
import { ApiError } from "@/lib/events-api";

import { TicketAnswersDialog } from "../../../ticket-answers-dialog";

import { DossierHeldTicketsSection } from "./dossier-held-tickets-section";
import { DossierIdentitySection } from "./dossier-identity-section";
import { DossierSalesSection } from "./dossier-sales-section";

type CustomerDossierSectionProps = {
  eventId: string;
  customerId: string;
  /** Already held to this Event's own pages by `dossierBackHref`. */
  backHref: string;
  /** The Event's timezone; null falls back to the platform's clock. */
  timezone: string | null;
};

type LoadState =
  | { state: "loading" }
  | { state: "error"; message: string }
  | CustomerDossierResult;

/**
 * The Customer Dossier page body: Back, the heading, and one section per thing
 * the Event knows about the person. Later tickets add their sections beneath
 * the Sales (#640 Tickets, #641 Answers) and widen each Sale's card (#639).
 */
export function CustomerDossierSection({
  eventId,
  customerId,
  backHref,
  timezone,
}: CustomerDossierSectionProps) {
  const t = useTranslations("customerDossier");
  const errorCopy = useMessages().errors;
  const [load, setLoad] = useState<LoadState>({ state: "loading" });
  /*
    THE ANSWERS DIALOG (#641). One for the page, opened on a Sale from any of
    its Tickets. Closing it refetches, because whatever was answered there —
    including nothing — the next read is the truth, and a cleared debt must show.
    The refetch keeps the current Dossier on screen rather than flashing the
    skeleton: `reload` bumps the effect, and only a first load or a new person
    shows "loading".
  */
  const [openSale, setOpenSale] = useState<{ ticketSaleId: string; confirmationRef: string } | null>(null);
  const [reload, setReload] = useState(0);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const fetchKey = `${eventId}/${customerId}`;

  const openAnswers = useCallback((ticketSaleId: string, confirmationRef: string) => {
    setOpenSale({ ticketSaleId, confirmationRef });
  }, []);

  const closeDialog = useCallback((open: boolean) => {
    if (!open) {
      setOpenSale(null);
      setReload((count) => count + 1);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (loadedKey !== fetchKey) {
      setLoad({ state: "loading" });
    }
    fetchCustomerDossier(eventId, customerId)
      .then((result) => {
        if (!cancelled) {
          setLoad(result);
          setLoadedKey(fetchKey);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setLoad({
            state: "error",
            message:
              (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("loadFailed"),
          });
        }
      });
    return () => {
      cancelled = true;
    };
    // errorCopy and t are stable per locale; loadedKey is read, not reacted to.
    // The ids are the fetch key, and `reload` asks for the same person again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, customerId, reload]);

  return (
    <div className="space-y-4">
      <Link href={backHref} className="inline-block text-sm text-muted-foreground hover:underline">
        {t("back")}
      </Link>
      <Card>
        <CardHeader>
          <CardTitle>{t("title")}</CardTitle>
          <CardDescription>{t("description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {load.state === "loading" ? (
            <div className="space-y-3" aria-busy="true">
              <Skeleton className="h-6 w-2/3 max-w-xs" />
              <Skeleton className="h-32 w-full" />
              <Skeleton className="h-32 w-full" />
            </div>
          ) : load.state === "not_found" ? (
            <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
              {t("notFound")}
            </p>
          ) : load.state === "error" ? (
            <p role="alert" className="rounded-md border border-destructive/50 p-4 text-sm text-destructive">
              {t("loadError", { message: load.message })}
            </p>
          ) : (
            <>
              <DossierIdentitySection customer={load.dossier.customer} />
              <DossierSalesSection sales={load.dossier.sales} timezone={timezone} onOpenAnswers={openAnswers} />
              {heldTicketsVisible(load.dossier) ? (
                <DossierHeldTicketsSection
                  heldTickets={load.dossier.held_tickets ?? []}
                  timezone={timezone}
                  onOpenAnswers={openAnswers}
                />
              ) : null}
            </>
          )}
        </CardContent>
      </Card>
      {openSale ? (
        <TicketAnswersDialog
          open
          onOpenChange={closeDialog}
          eventId={eventId}
          ticketSaleId={openSale.ticketSaleId}
          confirmationRef={openSale.confirmationRef}
        />
      ) : null}
    </div>
  );
}

"use client";

import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";

import {
  hasSaleDocuments,
  saleDocumentDownloadHref,
  saleDocumentLabelKey,
  saleDocumentPendingKey,
  type SaleDocument,
} from "@/lib/sale-documents";

/**
 * The Factura block on one Ticket Sale's card (#475, ADR 0060): the tax
 * invoice a paid House sale owes its buyer, and the credit note its reversal
 * will owe once that ticket lands.
 *
 * IT DRAWS NOTHING AT ALL FOR NEARLY EVERY SALE. A free, imported or non-House
 * sale owes no document and the API lists none; this component then renders
 * null and the card is exactly the card it was before the feature existed
 * (#471 story 16, 17). Only a Sale with a document shows the block.
 *
 * WHAT IT SAYS IS TWO THINGS. A document authorized by the SRI is a download —
 * the signed XML, served through this app's relay under the clave's filename.
 * Anything before that — owed, pending at the SRI, or parked for a Platform
 * Operator — is "your factura is on its way", and never a word the SRI said:
 * a platform problem is the platform's to fix and not the buyer's to be told
 * (#471 story 15). The API already speaks in those two words, so nothing here
 * has a third to show.
 *
 * IT IS FETCHED RATHER THAN SERVER-RENDERED WITH THE REST OF THE CARD, for
 * the reason the Tickets block above it is: the Customer Area lists every
 * purchase a person has ever made, and loading each sale's documents to draw
 * a block most sales do not have would make the commonest page slower for the
 * people it has nothing to show. It reserves no space, unlike that block —
 * the block is short, and it exists on the uncommon card only.
 *
 * IT APPEARS ON THE CONFIRMATION LINK'S PAGE AND IN THE CUSTOMER AREA BECAUSE
 * THOSE ARE ONE PAGE, and a forwarded receipt is exactly where "where is my
 * factura" gets asked from. The API narrows a link session to its one Sale.
 */
export function SaleDocuments({ ticketSaleId }: { ticketSaleId: string }) {
  const t = useTranslations("customerArea");
  const [documents, setDocuments] = useState<SaleDocument[] | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const response = await fetch(
          `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents`,
        );
        const envelope = (await response.json()) as { data: SaleDocument[] | null };
        if (!live) return;
        // EVERY FAILURE IS SILENCE: a signed-out session or a network that is
        // already breaking the page around this. None of them is worth an
        // error beside somebody's receipt, and the mail carries the document
        // anyway.
        setDocuments(response.ok && envelope.data ? envelope.data : []);
      } catch {
        if (live) setDocuments([]);
      }
    })();
    return () => {
      live = false;
    };
  }, [ticketSaleId]);

  if (!hasSaleDocuments(documents)) {
    return null;
  }

  return (
    <section className="mt-4 space-y-2 border-t pt-4 text-sm">
      <h3 className="font-medium">{t("documents.title")}</h3>
      <ul className="space-y-2">
        {documents.map((document) => {
          const href = saleDocumentDownloadHref(ticketSaleId, document);
          const pending = saleDocumentPendingKey(document);
          return (
            <li key={document.id} className="flex flex-wrap items-center justify-between gap-2">
              <span>{t(`documents.${saleDocumentLabelKey(document)}`)}</span>
              {href ? (
                // A plain anchor rather than a Link: the relay answers a file
                // under a Content-Disposition, and the browser saves it
                // without leaving the page.
                <a href={href} className="font-medium underline underline-offset-4" download>
                  {t("documents.download")}
                </a>
              ) : pending ? (
                <span className="text-muted-foreground">{t(`documents.${pending}`)}</span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

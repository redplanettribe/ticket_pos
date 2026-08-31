"use client";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  Markdown,
} from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

/**
 * One artifact of the draft, rendered as a reader will see it (#562).
 *
 * IT RENDERS THROUGH `Markdown` FROM @ticket-pos/ui — the exact component
 * apps/storefront/app/[locale]/privacy-policy/page.tsx renders. Not a copy of
 * it, not a Staff-side approximation of it: the same component, from the shared
 * workspace package, with the same `remark-breaks` and the same absent
 * `rehype-raw`. That is the whole claim this dialog makes — a hard-wrapped line
 * will be a <br> here for the same reason it will be one there, and raw HTML
 * will vanish here for the same reason it will vanish there. An approximation
 * would be worse than no preview, because it would be believed.
 *
 * ACCEPTED RESIDUAL, stated so nobody reopens it: this is faithful to the TEXT
 * and not to the Storefront's page chrome — its header, its width, its
 * surrounding furniture. Closing that gap means an iframe of a storefront
 * preview route, which means an unpublished edition reachable from a reader's
 * side of the platform, and the evidence is the text: the fingerprint covers the
 * words, not the layout around them.
 *
 * THE PUBLIC ROUTE IS NOT INVOLVED. Nothing here asks the Storefront for
 * anything; the text comes from the operator's own workspace read. The public
 * route resolves what is current itself and refuses to be told which edition to
 * serve, which is what keeps an unpublished draft unpublished.
 */
export function LegalPreviewDialog({
  cell,
  localeName,
  onClose,
}: {
  cell: { slug: string; locale: string; body: string } | null;
  localeName: (locale: string) => string;
  onClose: () => void;
}) {
  const t = useTranslations("operator.legal");

  return (
    <Dialog open={cell !== null} onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent className="max-h-[85vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="font-mono">{cell?.slug}</DialogTitle>
          <DialogDescription>
            {cell ? t("previewDescription", { locale: localeName(cell.locale) }) : null}
          </DialogDescription>
        </DialogHeader>
        {/* The reader's view, and nothing of the editor's: no line numbers, no
            monospace, no textarea. */}
        <div className="rounded-md border p-4">
          <Markdown>{cell?.body ?? ""}</Markdown>
        </div>
      </DialogContent>
    </Dialog>
  );
}

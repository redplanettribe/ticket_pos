import type { OperatorCertificateExpiry } from "./operator-api";

/**
 * The Alert variant the Certificate Expiry Warning is drawn in (#504, ADR
 * 0063 §5), or `null` when there is no warning to draw.
 *
 * ONE RULE FOR EVERY SURFACE. The Operator Dashboard, the invoicing list and
 * the Issuer page all read the Issuer's `certificate_expiry` block and all
 * answer the same way: `warning` while the certificate is expiring with more
 * than seven days to go, `destructive` from seven days out and once expired,
 * nothing while it is `valid` or there is `none` — an absent certificate is
 * the Issuer page's own business, a different fact with a different remedy,
 * and never a warning.
 *
 * The seven is the ladder's: the mail hardens at 7 days (ADR 0063 §2) and the
 * banner hardens with it. The count is the server's, in Ecuadorian calendar
 * days; this never derives a day from the date, and a block that says
 * `expiring` without a count is treated as nothing to say rather than guessed
 * at.
 */
export function certificateExpiryAlertVariant(
  expiry: Pick<OperatorCertificateExpiry, "state" | "days_before">,
): "warning" | "destructive" | null {
  switch (expiry.state) {
    case "expired":
      return "destructive";
    case "expiring":
      if (expiry.days_before === null) return null;
      return expiry.days_before > 7 ? "warning" : "destructive";
    default:
      return null;
  }
}

/**
 * What may reach the sign-in form's email field.
 *
 * Two places supply one: the sign-in page reading `?email=` from a URL anybody
 * can link to, and the checkout context cookie whose stored address becomes that
 * query parameter. Both are caller-controlled text on its way into an input that
 * looks like this app's own knowledge of the visitor, and both must be guarded
 * the same way — which is why the guard lives here rather than inside whichever
 * feature happened to add the first link.
 */

/**
 * safePrefillEmail guards what reaches the sign-in field.
 *
 * Its only job is to save a buyer from typing their own address. Anything that
 * is not plausibly one is dropped: a blank field is a mild inconvenience, while
 * an arbitrary string sitting in an input on a sign-in page reads as something
 * this app is asserting about the visitor.
 *
 * A prefill is never an assertion in the security sense either — the passcode
 * still has to be proved, so nothing is granted by putting an address in a
 * field.
 */
export function safePrefillEmail(email: string | null | undefined): string {
  const address = email?.trim() ?? "";
  if (address.length === 0 || address.length > 254) {
    return "";
  }
  // Deliberately loose: the API decides what an email is, and the passcode
  // decides who owns it. This only refuses what obviously is not one.
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address) ? address : "";
}

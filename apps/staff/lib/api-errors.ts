/**
 * Staff error copy, chosen by the API's own error code (ADR 0023, ADR 0041).
 *
 * The Go API answers in English and knows nothing about a Locale — no
 * `Accept-Language` on any read path, no locale parameter on any endpoint, and
 * none is being added (ADR 0027). What it does carry is `error.code`: its stable
 * statement of WHICH failure occurred. That is what this module keys a sentence
 * on, so the staff app re-renders the API's verdict in the reader's language
 * without ever re-deciding which failure applies.
 *
 * THE FLOOR IS THE LOAD-BEARING HALF. A code this catalog has never heard of
 * falls back to `error.message` verbatim — which is exactly what every staff
 * surface rendered before this module existed. So the backend can ship a new code
 * on its own, with no coordinated staff release, and the window between the two
 * shows a true English sentence rather than a blank alert. That degradation is
 * the reason this is a soft contract and not a closed mapping.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * HOW TO ADD A CODE (later tickets: this is all of it)
 *
 *   1. Find the code the API sends. It is the `code` on the envelope's `error`
 *      — visible in the browser's network tab, and defined in the backend beside
 *      the handler that refuses.
 *   2. Add it to `errors.envelope` in messages/en.json AND messages/es.json, in
 *      the same commit. lib/messages.test.ts fails on a key present in one only.
 *   3. Nothing else. There is no registry to update and no switch to extend:
 *      resolution is a lookup, so a key in the catalog is a translated failure.
 *
 *   A field-level code — the `code` on an entry of `details.fields[]` — goes in
 *   `errors.field` instead and is resolved by `fieldErrorMessages`.
 *
 *   Only catalog the codes a staff surface deliberately shows. Copy that nothing
 *   renders is copy that rots, and the floor already handles the rest.
 * ─────────────────────────────────────────────────────────────────────────────
 *
 * WHERE THE SENTENCE ENDS UP. `apiErrorMessage` answers null when there is
 * nothing to show at all — a request that never reached the API, or an envelope
 * carrying no words. The caller supplies its own sentence for that case, from its
 * OWN namespace, because only the caller knows what was being attempted:
 *
 *   const errorCopy = useMessages().errors;            // client component
 *   const errorCopy = (await getMessages()).errors;    // server component
 *   setError(apiErrorMessage(errorCopy, envelope.error) ?? t("switchFailed"));
 *
 * That `?? t(...)` is not a redundancy. It is the difference between "the API
 * refused, and here is why" and "we could not reach the API at all".
 *
 * Nothing here imports next-intl, React or the catalogs. The caller hands the
 * `errors` namespace in as plain data, which is what lets the whole resolution be
 * unit-tested against the real en.json under `node --test`.
 *
 * A separate module from the Storefront's `lib/api-errors.ts` on purpose, and the
 * two are not to be merged: the catalogs behind them are deliberately different
 * (a staff screen says *Organización* where a Storefront page says *Organizador*),
 * the reachable codes are different, and a shared resolver would be the first step
 * back towards a shared catalog.
 */

/**
 * The `errors` namespace of a message catalog: groups of copy, each keyed by an
 * API code.
 *
 * Deliberately loose about which groups exist. The compiler already holds en.json
 * to its shape through global.d.ts, and a resolver that insisted the groups were
 * present could not survive the one thing it is built for — a catalog that does
 * not know about a code yet.
 */
export type ErrorCatalog = Readonly<Record<string, Readonly<Record<string, string>> | undefined>>;

/**
 * As much of a failed envelope's `error` as choosing copy needs.
 *
 * Structurally satisfied by `APIError` from lib/api.ts, which carries `code`,
 * `message` and `details` as fields — so a caught APIError can be passed straight
 * in, as can the `error` object off a BFF envelope a client component read.
 *
 * `details` is here because a refusal is sometimes only useful with the numbers
 * it was made over. The catalog states the sentence; the API states the facts it
 * is about, and neither borrows the other's job.
 */
export type ApiError =
  | { code?: string | null; message?: string | null; details?: unknown }
  | null
  | undefined;

/**
 * usableMessage is the guard that keeps the floor from rendering as nothing.
 *
 * An absent message and a message that is only whitespace are the same thing to a
 * reader: a destructive Alert with an empty body. Both answer null so the call
 * site can show its own sentence instead. The string itself is returned untouched
 * — the API's wording is the API's, and trimming it here would be the first step
 * of rewriting it.
 */
function usableMessage(message: unknown): string | null {
  return typeof message === "string" && message.trim() !== "" ? message : null;
}

/**
 * fillDetails substitutes `{name}` in a catalog sentence with the value the API's
 * `details` gave under that name, and answers null the moment one of them has no
 * value to substitute.
 *
 * Null routes an under-supplied sentence back to the API's own message at the
 * call site below — the same degradation ADR 0023 chose for an unknown code. A
 * Member reads an English sentence that is true rather than a Spanish one with a
 * literal "{limit}" in it. The trigger is a details payload that changed shape,
 * which is precisely the silent drift the ADR records as this scheme's known gap.
 *
 * Only strings and finite numbers substitute. An object or an array has no
 * reading a sentence could want, and `details.fields[]` must never be flattened
 * into prose. Numbers go in with String(): these are small counts, and reaching
 * for Intl here would make the one module that must stay framework-free carry a
 * formatter — lib/format.ts is where a number that needs the reader's marks is
 * drawn, by the component, before it is handed over.
 *
 * The pattern is built per call rather than shared: a global RegExp carries
 * `lastIndex` between uses, which is a resolver that answers differently on its
 * second call.
 */
function fillDetails(copy: string, details: unknown): string | null {
  const placeholder = /\{(\w+)\}/g;
  const values =
    typeof details === "object" && details !== null ? (details as Record<string, unknown>) : {};
  let complete = true;
  const filled = copy.replace(placeholder, (match, name: string) => {
    const value = values[name];
    if (typeof value === "string" && value !== "") return value;
    if (typeof value === "number" && Number.isFinite(value)) return String(value);
    complete = false;
    return match;
  });
  return complete ? filled : null;
}

/**
 * apiErrorMessage returns the sentence to show for a failed envelope: this app's
 * copy for the code when it has some, the API's own English message otherwise,
 * and null when there is nothing to show at all.
 *
 * Null is not a failure — it is the answer for a call that never reached the API
 * or one whose envelope carried no words. See the module comment for what the
 * caller does with it.
 *
 * There is no surface parameter, unlike the Storefront's resolver. That one
 * exists for a single API code the backend gives two meanings to depending on the
 * operation refused; no staff-reachable code does that today. When one appears,
 * the honest fix is a sentence in the SURFACE's own namespace passed as the `??`
 * fallback, not a second key space here — the `errors` namespace is for failures
 * that belong to no single surface (messages/README.md).
 */
export function apiErrorMessage(catalog: ErrorCatalog, error: ApiError): string | null {
  if (!error) return null;
  const code = typeof error.code === "string" ? error.code : null;
  if (code !== null) {
    const copy = catalog.envelope?.[code];
    // A sentence that names facts it was not given is worse than the API's own
    // words, so fillDetails answering null falls through to them (see above).
    const filled = copy ? fillDetails(copy, error.details) : null;
    if (filled) return filled;
  }
  return usableMessage(error.message);
}

/**
 * fieldErrorMessages picks the field-level errors out of a VALIDATION_FAILED
 * envelope's `details`, keyed by the API's own field name.
 *
 * The same rule one level down: a FieldError's `code` names the RULE that failed
 * ("REQUIRED", "INVALID_EMAIL") and is stable, while its `message` is a sentence
 * that gets reworded (backend/internal/platform/validation_codes.go). A field
 * whose code the catalog does not know keeps the API's message, and a field with
 * neither is dropped rather than marked with an empty line.
 *
 * Every field the API named is returned, including ones no form on this app drew.
 * Filtering to a particular form's inputs belongs to that form, which is the only
 * thing that knows what it rendered.
 */
export function fieldErrorMessages(
  catalog: ErrorCatalog,
  details: unknown,
): Record<string, string> {
  const errors: Record<string, string> = {};
  if (typeof details !== "object" || details === null) return errors;
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) return errors;
  for (const entry of fields) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, code, message } = entry as {
      field?: unknown;
      code?: unknown;
      message?: unknown;
    };
    if (typeof field !== "string" || field === "") continue;
    const copy =
      (typeof code === "string" ? catalog.field?.[code] : undefined) ?? usableMessage(message);
    if (copy) errors[field] = copy;
  }
  return errors;
}

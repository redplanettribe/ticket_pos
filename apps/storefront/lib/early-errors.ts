/**
 * Errors thrown before the browser SDK has loaded (ADR 0079).
 *
 * The Storefront loads Sentry only after hydration, so an error thrown while
 * the page is still loading or hydrating would otherwise be lost. A minimal
 * handler, inlined at the top of the document's head by the root layout, keeps
 * them; once the SDK has started, lib/sentry-browser.ts takes them out and
 * reports each one, and the SDK's own handlers report everything after.
 * This is what Sentry's own Loader Script does for its lazy mode: it is first
 * on the page and buffers unhandled errors and rejections until the SDK
 * initialises. The Loader itself is not used: it fetches the SDK from Sentry's
 * CDN, which ad blockers drop, and it would not be the npm SDK the server uses.
 *
 * The handler is written out as the source that ships, so the tests run
 * exactly what the browser runs. It is small, holds at most
 * EARLY_ERROR_LIMIT errors, and every line of it is inside a try: a fault in it
 * can never throw into the page. It only listens; it never calls
 * preventDefault, so the browser's console still shows every error.
 *
 * It keeps an uncaught error's thrown value (`ErrorEvent.error`) and a
 * rejection's reason. An error event with no thrown value, the cross-origin
 * "Script error." a third-party script produces, is not kept: it has no stack,
 * so it could never have a frame of ours and the Storefront's before-send would
 * drop it anyway.
 */

/** Where the handler keeps its buffer, on window, until the SDK takes it. */
export const EARLY_ERRORS_GLOBAL = "__ticketPosEarlyErrors";

/** At most this many early errors are kept; later ones are not. */
export const EARLY_ERROR_LIMIT = 10;

/**
 * The inline handler, ES5 so it runs in any browser that loads the page.
 * window.__ticketPosEarlyErrors = { errors: [{ kind, value }], stop() }.
 * Its names are short because they ship in every page: w the window, b the
 * buffer, k keeps one entry, e listens for errors, r for rejections.
 */
export const EARLY_ERROR_HANDLER_SCRIPT =
  "(function(w){try{" +
  "var b=[];" +
  "function k(t,v){try{if(b.length<" + EARLY_ERROR_LIMIT + ")b.push({kind:t,value:v})}catch(_){}}" +
  "function e(v){try{if(v&&v.error!=null)k(\"error\",v.error)}catch(_){}}" +
  "function r(v){try{k(\"rejection\",v&&v.reason)}catch(_){}}" +
  "w.addEventListener(\"error\",e);" +
  "w.addEventListener(\"unhandledrejection\",r);" +
  "w." + EARLY_ERRORS_GLOBAL + "={errors:b,stop:function(){" +
  "w.removeEventListener(\"error\",e);w.removeEventListener(\"unhandledrejection\",r)}}" +
  "}catch(_){}})(window)";

export type EarlyErrorKind = "error" | "rejection";

export interface EarlyError {
  kind: EarlyErrorKind;
  value: unknown;
}

/**
 * The SDK's own mechanism for an uncaught error or rejection, so a buffered one
 * reads in Sentry exactly as if the SDK had caught it itself.
 */
export function earlyErrorMechanism(kind: EarlyErrorKind): { handled: false; type: string } {
  return {
    handled: false,
    type: kind === "error" ? "auto.browser.global_handlers.onerror" : "auto.browser.global_handlers.onunhandledrejection",
  };
}

type Capture = (value: unknown, hint: { mechanism: { handled: false; type: string } }) => unknown;

/**
 * Stops the inline handler and reports what it kept, each once, through
 * `capture` (the SDK's captureException). Call it right after the SDK has
 * started, in the same task: from then on the SDK's handlers see every error,
 * and since no error event can be dispatched between the two, none is reported
 * twice or missed. A second call, or a page without the handler, reports
 * nothing. Returns how many were reported. Never throws.
 */
export function reportEarlyErrors(win: object, capture: Capture): number {
  let errors: EarlyError[];
  try {
    const holder = win as Record<string, unknown>;
    const buffer = holder[EARLY_ERRORS_GLOBAL] as { errors: EarlyError[]; stop: () => void } | undefined;
    if (!buffer) return 0;
    delete holder[EARLY_ERRORS_GLOBAL];
    buffer.stop();
    errors = buffer.errors.slice();
  } catch {
    return 0;
  }
  for (const { kind, value } of errors) {
    try {
      capture(value, { mechanism: earlyErrorMechanism(kind) });
    } catch {
      // One that cannot be reported does not keep the rest from it.
    }
  }
  return errors.length;
}

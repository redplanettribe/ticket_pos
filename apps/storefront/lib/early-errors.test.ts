import assert from "node:assert/strict";
import test from "node:test";
import vm from "node:vm";

import {
  EARLY_ERRORS_GLOBAL,
  EARLY_ERROR_HANDLER_SCRIPT,
  EARLY_ERROR_LIMIT,
  reportEarlyErrors,
} from "./early-errors.ts";

/**
 * The inline handler is run exactly as the browser runs it: its source, in a
 * context whose window is a real EventTarget, with errors and rejections
 * dispatched at it the way the browser dispatches them.
 */

type Window = EventTarget & Record<string, unknown>;

function pageWithHandler(window: Window = new EventTarget() as Window): Window {
  vm.runInNewContext(EARLY_ERROR_HANDLER_SCRIPT, { window });
  return window;
}

function throwUncaught(window: EventTarget, error: unknown) {
  window.dispatchEvent(Object.assign(new Event("error"), { error, message: String(error) }));
}

function rejectUnhandled(window: EventTarget, reason: unknown) {
  window.dispatchEvent(Object.assign(new Event("unhandledrejection"), { reason }));
}

function capturing() {
  const calls: Array<{ value: unknown; type: string; handled: boolean }> = [];
  const capture = (value: unknown, hint: { mechanism: { handled: false; type: string } }) => {
    calls.push({ value, type: hint.mechanism.type, handled: hint.mechanism.handled });
  };
  return { calls, capture };
}

test("an error and a rejection before the SDK loads are reported once it has, exactly once", () => {
  const window = pageWithHandler();
  const thrown = new Error("hydration failed");
  const reason = new Error("load failed");
  throwUncaught(window, thrown);
  rejectUnhandled(window, reason);

  const { calls, capture } = capturing();
  assert.equal(reportEarlyErrors(window, capture), 2);
  assert.deepEqual(calls, [
    { value: thrown, type: "auto.browser.global_handlers.onerror", handled: false },
    { value: reason, type: "auto.browser.global_handlers.onunhandledrejection", handled: false },
  ]);

  // Taking them again reports nothing: they are gone from the page.
  assert.equal(reportEarlyErrors(window, capture), 0);
  assert.equal(calls.length, 2);
  assert.equal(EARLY_ERRORS_GLOBAL in window, false);
});

test("once the SDK has taken the buffer, the handler keeps nothing more: the SDK's own handlers report what follows", () => {
  const window = pageWithHandler();
  const buffer = window[EARLY_ERRORS_GLOBAL] as { errors: unknown[] };
  reportEarlyErrors(window, () => {});

  throwUncaught(window, new Error("after load"));
  rejectUnhandled(window, new Error("after load"));

  assert.equal(buffer.errors.length, 0);
});

test("it holds a bounded number of errors", () => {
  const window = pageWithHandler();
  for (let i = 0; i < EARLY_ERROR_LIMIT + 25; i++) throwUncaught(window, new Error(`error ${i}`));

  const { calls, capture } = capturing();
  assert.equal(reportEarlyErrors(window, capture), EARLY_ERROR_LIMIT);
  assert.equal(calls.length, EARLY_ERROR_LIMIT);
  assert.equal((calls[0].value as Error).message, "error 0");
});

test("an error event with no thrown value, a cross-origin Script error., is not kept", () => {
  const window = pageWithHandler();
  window.dispatchEvent(Object.assign(new Event("error"), { message: "Script error.", error: null }));
  window.dispatchEvent(new Event("error"));

  assert.equal(reportEarlyErrors(window, () => {}), 0);
});

test("the handler cannot throw into the page", () => {
  // A window whose listeners cannot be added: the script still runs to its end.
  const broken = {
    addEventListener() {
      throw new Error("no listeners here");
    },
  };
  assert.doesNotThrow(() => vm.runInNewContext(EARLY_ERROR_HANDLER_SCRIPT, { window: broken }));

  // Events it cannot read are ignored, not thrown from.
  const window = pageWithHandler();
  const hostile = new Event("error");
  Object.defineProperty(hostile, "error", {
    get() {
      throw new Error("unreadable");
    },
  });
  const listeners: Array<(event: unknown) => void> = [];
  const recording = {
    addEventListener: (_: string, listener: (event: unknown) => void) => listeners.push(listener),
    removeEventListener() {},
  };
  vm.runInNewContext(EARLY_ERROR_HANDLER_SCRIPT, { window: recording });
  for (const listener of listeners) {
    assert.doesNotThrow(() => listener(hostile));
    assert.doesNotThrow(() => listener(null));
    assert.doesNotThrow(() => listener(undefined));
  }
  window.dispatchEvent(hostile);
  assert.equal(reportEarlyErrors(window, () => {}), 0);
});

test("one capture that fails does not keep the rest from being reported, and nothing is thrown", () => {
  const window = pageWithHandler();
  throwUncaught(window, new Error("first"));
  throwUncaught(window, new Error("second"));

  const seen: string[] = [];
  const reported = reportEarlyErrors(window, (value) => {
    seen.push((value as Error).message);
    if (seen.length === 1) throw new Error("SDK fault");
  });

  assert.equal(reported, 2);
  assert.deepEqual(seen, ["first", "second"]);
});

test("a page without the handler, the dormant default, reports nothing", () => {
  assert.equal(reportEarlyErrors(new EventTarget(), () => assert.fail("nothing to report")), 0);
});

test("the handler is small: under 600 bytes of inline script", () => {
  assert.ok(EARLY_ERROR_HANDLER_SCRIPT.length < 600, `${EARLY_ERROR_HANDLER_SCRIPT.length} bytes`);
  assert.doesNotThrow(() => new vm.Script(EARLY_ERROR_HANDLER_SCRIPT));
});

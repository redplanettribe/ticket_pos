/**
 * The parts of a Sentry event these rules read or rewrite, as plain structural
 * types.
 *
 * They mirror `Event`, `Exception`, `Mechanism`, `Thread`, `StackFrame`, `Breadcrumb`,
 * `RequestEventData` and `User` in `@sentry/core` (11.x), with every field
 * optional and no index signature, so an SDK event is assignable to them and
 * the rules can be handed to `beforeSend` and `beforeSendTransaction` as they
 * are; fields not named here are carried over untouched by the spreads in
 * scrub.ts. Declaring them here instead of
 * importing them keeps this package free of the SDK altogether: the rules are
 * pure functions over plain objects, tested without it, and the two apps choose
 * the SDK version.
 */

export interface SentryStackFrame {
  filename?: string;
  abs_path?: string;
  function?: string;
  lineno?: number;
  colno?: number;
  vars?: Record<string, unknown>;
}

export interface SentryStacktrace {
  frames?: SentryStackFrame[];
}

export interface SentryException {
  type?: string;
  value?: string;
  mechanism?: { type?: string; data?: Record<string, unknown> };
  stacktrace?: SentryStacktrace;
}

export interface SentryBreadcrumb {
  category?: string;
  message?: string;
  data?: Record<string, unknown>;
}

export interface SentryRequest {
  url?: string;
  method?: string;
  data?: unknown;
  query_string?: unknown;
  cookies?: Record<string, string>;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

export interface SentrySpan {
  description?: string;
  data?: Record<string, unknown>;
}

export interface SentryEvent {
  event_id?: string;
  message?: string;
  logentry?: { message?: string; params?: unknown[] };
  transaction?: string;
  exception?: { values?: SentryException[] };
  threads?: { values: Array<{ id?: number | string; stacktrace?: SentryStacktrace }> };
  fingerprint?: string[];
  breadcrumbs?: SentryBreadcrumb[];
  request?: SentryRequest;
  user?: { id?: string | number; email?: string; ip_address?: string | null; username?: string; geo?: unknown };
  tags?: Record<string, unknown>;
  extra?: Record<string, unknown>;
  contexts?: Record<string, unknown>;
  spans?: SentrySpan[];
}

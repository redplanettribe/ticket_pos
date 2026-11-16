import assert from "node:assert/strict";
import test from "node:test";

import { scrubEvent, scrubString, type SentryEvent } from "../src/index.ts";

// The shapes ADR 0079 says never leave the platform, redacted wherever a
// string can carry them. A cédula and a RUC that pass the backend's validator
// (backend/internal/platform/taxid.go) are the seeds.
const EMAIL = "ana.perez+tickets@example.com";
const CEDULA = "1710034065";
const RUC = "1710034065001";

test("scrubString: what is redacted and what survives", () => {
  const table: Array<[name: string, input: string, want: string]> = [
    ["an email", `no sale for ${EMAIL}`, "no sale for [email]"],
    ["an uppercase email", "MAIL TO ANA@EXAMPLE.COM.", "MAIL TO [email]."],
    ["a url-encoded email", "/customers/ana%40example.com/sales", "/customers/[email]/sales"],
    ["two emails", "a@b.co, c@d.org", "[email], [email]"],
    ["a cédula", `cedula ${CEDULA} rejected`, "cedula [identity-number] rejected"],
    ["a RUC", `ruc=${RUC};`, "ruc=[identity-number];"],
    ["a hyphenated cédula", "id 171003406-5", "id [identity-number]"],
    ["a mobile number", "call 0991234567", "call [identity-number]"],
    ["a query string on an absolute URL", "GET https://api.example.com/sales?email=x&ruc=1 failed", "GET https://api.example.com/sales failed"],
    ["a query string on a path", "fetch /api/checkout?token=abc&e=1 failed", "fetch /api/checkout failed"],
    ["a fragment on a URL", "at https://shop.example.com/pay#access_token=zzz", "at https://shop.example.com/pay"],
    ["a question in prose", "is this a sale? yes", "is this a sale? yes"],
    ["a UUID", "sale 3f2504e0-4f89-11d3-9a0c-0305e82c3301 voided", "sale 3f2504e0-4f89-11d3-9a0c-0305e82c3301 voided"],
    ["an all-digit UUID segment", "id 12345678-1234-1234-1234-123456789012", "id 12345678-1234-1234-1234-123456789012"],
    [
      "an SRI clave de acceso",
      "clave 1611202601179001234500120010010000000011234567811",
      "clave 1611202601179001234500120010010000000011234567811",
    ],
    ["an amount and a count", "total 1250.00 for 12 tickets", "total 1250.00 for 12 tickets"],
    ["nine digits", "ref 171003406", "ref 171003406"],
    ["ten digits with no province", "ref 9910034065", "ref 9910034065"],
    ["digits inside a word", "chunk a1710034065b.js", "chunk a1710034065b.js"],
    ["an at-sign that is not an email", "@ticket-pos/ui and user@localhost", "@ticket-pos/ui and user@localhost"],
  ];
  for (const [name, input, want] of table) {
    assert.equal(scrubString(input), want, name);
  }
});

function pii(): SentryEvent {
  return {
    event_id: "abc",
    message: `checkout failed for ${EMAIL}`,
    logentry: { message: `tax id ${CEDULA}`, params: [EMAIL, { ruc: RUC }, 3] },
    transaction: "/customers/ana%40example.com",
    exception: {
      values: [
        {
          type: "Error",
          value: `GET https://api.example.com/v1/sales?email=${EMAIL} -> 500 for ${RUC}`,
          stacktrace: {
            frames: [
              {
                filename: "https://shop.example.com/_next/static/chunks/app/page.js?dpl=abc",
                abs_path: "https://shop.example.com/_next/static/chunks/app/page.js?dpl=abc",
                function: "submit",
                lineno: 1,
                colno: 2,
                vars: { email: EMAIL },
              },
            ],
          },
        },
      ],
    },
    breadcrumbs: [
      { category: "console", message: `typed ${EMAIL}` },
      {
        category: "fetch",
        data: {
          method: "GET",
          url: "/api/me?email=x",
          "url.query": "email=x",
          "url.fragment": "f",
          "http.query": "?email=x",
          "http.fragment": "#f",
          status_code: 500,
        },
      },
      { category: "navigation", data: { from: "/es/checkout?ruc=1", to: "/es/done#x", nested: [{ note: CEDULA }] } },
    ],
    request: {
      url: "https://shop.example.com/es/checkout?email=ana@example.com#step",
      method: "POST",
      data: { email: EMAIL },
      query_string: "email=ana@example.com",
      cookies: { session: "secret" },
      env: { REMOTE_ADDR: "203.0.113.9" },
      headers: { "User-Agent": "Mozilla/5.0", Cookie: "session=secret", "X-Forwarded-For": "203.0.113.9" },
    },
    user: { id: "c-42", email: EMAIL, ip_address: "203.0.113.9", username: "ana", geo: { city: "Quito" } },
    tags: { audience: "customer", note: EMAIL, count: 2 },
    extra: { form: { email: EMAIL, cedula: CEDULA } },
    contexts: { checkout: { buyer: EMAIL } },
    spans: [{ description: `SELECT * FROM customers WHERE email = '${EMAIL}'`, data: { "http.url": "/x?y=1" } }],
  };
}

test("scrubEvent: no seeded email, identity number, cookie, header or IP address survives anywhere", () => {
  const out = JSON.stringify(scrubEvent(pii()));
  for (const needle of [EMAIL, "ana@example.com", "ana%40example.com", CEDULA, RUC, "secret", "203.0.113.9", "Mozilla", "Quito", "?", "#step", "dpl=abc"]) {
    assert.ok(!out.includes(needle), `${needle} leaked: ${out}`);
  }
});

test("scrubEvent: each place a string travels is scrubbed, and what is not personal is kept", () => {
  const out = scrubEvent(pii());

  assert.equal(out.message, "checkout failed for [email]");
  assert.equal(out.logentry?.message, "tax id [identity-number]");
  assert.deepEqual(out.logentry?.params, ["[email]", { ruc: "[identity-number]" }, 3]);
  assert.equal(out.transaction, "/customers/[email]");

  const exception = out.exception?.values?.[0];
  assert.equal(exception?.type, "Error");
  assert.equal(exception?.value, "GET https://api.example.com/v1/sales -> 500 for [identity-number]");
  const frame = exception?.stacktrace?.frames?.[0];
  assert.equal(frame?.abs_path, "https://shop.example.com/_next/static/chunks/app/page.js");
  assert.equal(frame?.filename, "https://shop.example.com/_next/static/chunks/app/page.js");
  assert.equal(frame?.function, "submit");
  assert.equal(frame?.lineno, 1);
  assert.equal(frame?.vars, undefined);

  assert.equal(out.breadcrumbs?.[0]?.message, "typed [email]");
  assert.deepEqual(out.breadcrumbs?.[1]?.data, { method: "GET", url: "/api/me", status_code: 500 });
  assert.deepEqual(out.breadcrumbs?.[2]?.data, { from: "/es/checkout", to: "/es/done", nested: [{ note: "[identity-number]" }] });

  assert.deepEqual(out.request, { url: "https://shop.example.com/es/checkout", method: "POST" });
  assert.deepEqual(out.user, { id: "c-42" });
  assert.deepEqual(out.tags, { audience: "customer", note: "[email]", count: 2 });
  assert.deepEqual(out.extra, { form: { email: "[email]", cedula: "[identity-number]" } });
  assert.deepEqual(out.contexts, { checkout: { buyer: "[email]" } });
  assert.equal(out.spans?.[0]?.description, "SELECT * FROM customers WHERE email = '[email]'");
  assert.deepEqual(out.spans?.[0]?.data, { "http.url": "/x" });
  assert.equal(out.event_id, "abc");
});

test("scrubEvent: an event with no user keeps none, and the input is not mutated", () => {
  const input = pii();
  delete input.user;
  const before = JSON.stringify(input);
  const out = scrubEvent(input);
  assert.equal("user" in out, false);
  assert.equal(JSON.stringify(input), before);
});

test("scrubEvent: an ordinary event passes through unchanged in what it says", () => {
  const event: SentryEvent = {
    message: "Sale 3f2504e0-4f89-11d3-9a0c-0305e82c3301 could not be voided",
    exception: { values: [{ type: "TypeError", value: "x is undefined" }] },
    tags: { audience: "staff" },
  };
  assert.deepEqual(scrubEvent(event), event);
});

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { NextIntlClientProvider } from "next-intl";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { CountCard, TotalsCard } from "../app/operator/operator-dashboard-client.tsx";
import { OVERVIEW_SECTION_TITLE } from "../app/operator/overview-layout.ts";

const messages = JSON.parse(readFileSync(new URL("../messages/en.json", import.meta.url), "utf8"));

// Tailwind's text sizes in px, smallest first, for the sizes the Overview uses.
const TEXT_PX: Record<string, number> = {
  "text-xs": 12,
  "text-sm": 14,
  "text-base": 16,
  "text-lg": 18,
  "text-xl": 20,
  "text-2xl": 24,
};

function textSize(classes: string): number {
  const sizes = classes.split(/\s+/).filter((name) => name in TEXT_PX);
  assert.equal(sizes.length, 1, `one text size in "${classes}"`);
  return TEXT_PX[sizes[0]];
}

function headingClasses(markup: string): string {
  const match = /<h3[^>]*class="([^"]*)"/.exec(markup);
  assert.ok(match, "the card has an h3");
  return match[1];
}

function render(node: React.ReactNode): string {
  return renderToStaticMarkup(
    React.createElement(NextIntlClientProvider, { locale: "en", messages, timeZone: "UTC", children: node }),
  );
}

test("a count card's heading is no larger than the Platform Trends title", () => {
  const markup = render(
    React.createElement(CountCard, {
      title: "Payout requests",
      description: "Waiting",
      count: "3",
      href: "/operator/payout-requests",
      linkLabel: "Open",
    }),
  );
  assert.ok(textSize(headingClasses(markup)) <= textSize(OVERVIEW_SECTION_TITLE));
});

test("a totals card's heading is no larger than the Platform Trends title", () => {
  const markup = render(
    React.createElement(TotalsCard, {
      locale: "en",
      totals: {
        currency: "USD",
        platform_fee_cents: 100,
        fee_iva_cents: 15,
        total_owed_cents: 115,
        kept_fee_cents: 0,
        kept_fee_iva_cents: 0,
      },
    }),
  );
  assert.ok(textSize(headingClasses(markup)) <= textSize(OVERVIEW_SECTION_TITLE));
});

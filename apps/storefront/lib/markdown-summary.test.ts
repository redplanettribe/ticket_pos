import assert from "node:assert/strict";
import test from "node:test";

import { markdownSummary } from "./markdown-summary.ts";

test("prose with no notation is left as it was written", () => {
  assert.equal(markdownSummary("An evening of jazz at the old market."), "An evening of jazz at the old market.");
});

test("block markers drop out and their words stay", () => {
  const description = ["# Line-up", "", "- Doors at 20:00", "- Support: The Wrens", "", "> Bring a coat."].join("\n");
  assert.equal(markdownSummary(description), "Line-up Doors at 20:00 Support: The Wrens Bring a coat.");
});

test("emphasis is unwrapped and links keep their text, not their target", () => {
  assert.equal(
    markdownSummary("**Free** drink with every [early ticket](https://example.com/early), _really_."),
    "Free drink with every early ticket, really.",
  );
});

test("an image contributes nothing, not even its bang", () => {
  assert.equal(markdownSummary("![poster](https://cdn.example.com/poster.png) Jazz night."), "Jazz night.");
});

test("a fenced code block is dropped whole", () => {
  const description = ["Directions below.", "", "```", "not prose", "```", "", "See you there."].join("\n");
  assert.equal(markdownSummary(description), "Directions below. See you there.");
});

test("a table reads as its cells, without pipes or alignment row", () => {
  const description = ["| Set | Time |", "| --- | ---: |", "| Doors | 20:00 |"].join("\n");
  assert.equal(markdownSummary(description), "Set Time Doors 20:00");
});

test("an escaped marker is shown as the character it was hiding", () => {
  assert.equal(markdownSummary("Tickets cost 20 \\* the usual \\_nothing\\_."), "Tickets cost 20 * the usual _nothing_.");
});

test("a long description is cut between words, with an ellipsis", () => {
  const summary = markdownSummary("one two three four five six seven", 20);
  assert.equal(summary, "one two three four…");
  assert.ok(summary !== null && summary.length <= 20);
});

test("a description with nothing readable in it answers null", () => {
  // An image and a rule and nothing else: the caller falls back to its generic
  // copy exactly as it would for an Event that wrote no description at all.
  assert.equal(markdownSummary("![poster](https://cdn.example.com/poster.png)\n\n---\n"), null);
  assert.equal(markdownSummary("   \n\n  "), null);
});

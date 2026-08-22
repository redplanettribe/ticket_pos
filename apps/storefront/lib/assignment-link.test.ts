import assert from "node:assert/strict";
import test from "node:test";

import { assignmentLinkFailure, holderNameIsGiven } from "./assignment-link.ts";
import enMessages from "../messages/en.json" with { type: "json" };
import esMessages from "../messages/es.json" with { type: "json" };

// The Assignment Link page's rules (#325, ADR 0046).

test("a reassigned ticket, a reversed sale and a forgery all read the same", () => {
  // The API refuses all three as ASSIGNMENT_LINK_INVALID, deliberately: "your
  // friend gave your ticket to someone else" and "your friend cancelled the
  // purchase" are facts about the buyer's decisions, and this page never names
  // the buyer or describes what they did — the disclosure rule holds in the
  // error state too (CONTEXT.md).
  assert.equal(assignmentLinkFailure("ASSIGNMENT_LINK_INVALID"), "invalid");
  // The flag being closed answers 404 exactly as a build without the feature
  // does (ADR 0045), and the reader is told the link does not work.
  assert.equal(assignmentLinkFailure("TICKET_ASSIGNMENT_UNAVAILABLE"), "invalid");
});

test("the started event and the deployment fault are told apart", () => {
  // An Event's start is already published, so saying so explains a deadline
  // rather than implying a forgery.
  assert.equal(assignmentLinkFailure("ASSIGNMENT_LINK_EXPIRED"), "expired");
  // A missing link secret is the platform's fault, and this reader has nobody to
  // ask for a replacement: they are not told who bought the ticket.
  assert.equal(assignmentLinkFailure("ASSIGNMENT_LINK_UNAVAILABLE"), "unavailable");
});

test("an unknown code falls through to the honest floor", () => {
  // The reader could not accept their ticket, and the page has nothing truer to
  // tell them than that.
  assert.equal(assignmentLinkFailure("SOMETHING_NEW"), "invalid");
  assert.equal(assignmentLinkFailure(undefined), "invalid");
});

test("a name is two non-blank halves", () => {
  // Mirrors catalog.ParseHolderName, which is the one that decides. Both halves
  // are stored separately (ADR 0005) and both are required, because half a name
  // is half a person on the Organization's guest list.
  assert.equal(holderNameIsGiven("Carla", "Ruiz"), true);
  assert.equal(holderNameIsGiven("  ", "Ruiz"), false);
  assert.equal(holderNameIsGiven("Carla", ""), false);
  assert.equal(holderNameIsGiven(" Carla ", " Ruiz "), true);
});

test("every failure key the mapper can return has copy in both languages", () => {
  // The mapper returns a key and the page renders `${key}Description`; a key
  // with no sentence behind it is a blank alert for somebody who cannot get
  // into an event.
  for (const messages of [enMessages, esMessages]) {
    const copy = messages.assignmentLink as Record<string, string>;
    for (const key of ["invalid", "expired", "unavailable"]) {
      assert.equal(typeof copy[`${key}Description`], "string", `${key}Description is missing`);
      assert.notEqual(copy[`${key}Description`].trim(), "");
    }
  }
});

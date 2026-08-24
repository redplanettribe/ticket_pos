import assert from "node:assert/strict";
import test from "node:test";

import {
  answerBody,
  answerProblem,
  withReason,
  withVerdict,
  type VerdictDraft,
  type VerdictItem,
} from "./operator-question-reviews.ts";

const size: VerdictItem = { id: "i-size", ticket_question_id: "q-size", ticket_question_option_id: null };
const meal: VerdictItem = { id: "i-meal", ticket_question_id: "q-meal", ticket_question_option_id: null };
const chicken: VerdictItem = { id: "i-chicken", ticket_question_id: "q-meal", ticket_question_option_id: "o-chicken" };
const veg: VerdictItem = { id: "i-veg", ticket_question_id: "q-meal", ticket_question_option_id: "o-veg" };
const items = [size, meal, chicken, veg];

test("answerProblem names the first item without a verdict, in list order", () => {
  const drafts: Record<string, VerdictDraft> = {
    "i-size": { verdict: "approved", reason: "" },
    "i-chicken": { verdict: "approved", reason: "" },
  };
  assert.deepEqual(answerProblem(items, drafts), { kind: "verdict", itemId: "i-meal" });
});

test("answerProblem names a refusal whose reason is blank, and one past the bound", () => {
  const base: Record<string, VerdictDraft> = {
    "i-size": { verdict: "approved", reason: "" },
    "i-meal": { verdict: "refused", reason: "   " },
    "i-chicken": { verdict: "refused", reason: "say why" },
    "i-veg": { verdict: "refused", reason: "say why" },
  };
  assert.deepEqual(answerProblem(items, base), { kind: "reason", itemId: "i-meal" });
  assert.deepEqual(
    answerProblem(items, { ...base, "i-meal": { verdict: "refused", reason: "x".repeat(501) } }),
    { kind: "too_long", itemId: "i-meal" },
  );
  assert.equal(answerProblem(items, { ...base, "i-meal": { verdict: "refused", reason: "say why" } }), null);
});

test("answerBody sends one verdict per item, the reason trimmed and only on a refusal", () => {
  const drafts: Record<string, VerdictDraft> = {
    "i-size": { verdict: "approved", reason: "ignored" },
    "i-meal": { verdict: "refused", reason: "  say why  " },
    "i-chicken": { verdict: "refused", reason: "say why" },
    "i-veg": { verdict: "refused", reason: "say why" },
  };
  assert.deepEqual(answerBody(items, drafts), {
    verdicts: [
      { item_id: "i-size", verdict: "approved" },
      { item_id: "i-meal", verdict: "refused", reason: "say why" },
      { item_id: "i-chicken", verdict: "refused", reason: "say why" },
      { item_id: "i-veg", verdict: "refused", reason: "say why" },
    ],
  });
});

test("withVerdict carries a question's verdict to its Options and leaves other questions alone", () => {
  const drafts = withVerdict(items, { "i-size": { verdict: "approved", reason: "" } }, "i-meal", "refused");
  assert.equal(drafts["i-size"]?.verdict, "approved");
  assert.equal(drafts["i-meal"]?.verdict, "refused");
  assert.equal(drafts["i-chicken"]?.verdict, "refused");
  assert.equal(drafts["i-veg"]?.verdict, "refused");
});

test("withVerdict on an Option moves only that Option", () => {
  const drafts = withVerdict(items, {}, "i-chicken", "approved");
  assert.equal(drafts["i-chicken"]?.verdict, "approved");
  assert.equal(drafts["i-meal"], undefined);
  assert.equal(drafts["i-veg"], undefined);
});

test("withReason carries a question's reason to Options with no reason of their own", () => {
  const start = withVerdict(items, {}, "i-meal", "refused");
  const own = withReason(items, start, "i-veg", "too vague");
  const drafts = withReason(items, own, "i-meal", "say why");
  assert.equal(drafts["i-meal"]?.reason, "say why");
  assert.equal(drafts["i-chicken"]?.reason, "say why");
  assert.equal(drafts["i-veg"]?.reason, "too vague");
});

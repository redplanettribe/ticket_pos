import assert from "node:assert/strict";
import test from "node:test";

import { createBeforeSend, scrubEvent, sentryOptions } from "../src/index.ts";

const DSN = "https://publickey@o123.ingest.us.sentry.io/4507";

test("both apps' runtimes send whole traces through the scrubber and collect nothing personal", () => {
  const beforeSend = createBeforeSend({ dropBots: true });
  const options = sentryOptions({ dsn: DSN, environment: "production", release: "abc123" }, beforeSend);
  assert.deepEqual(options, {
    dsn: DSN,
    environment: "production",
    release: "abc123",
    sendDefaultPii: false,
    beforeSend,
    beforeSendTransaction: scrubEvent,
    traceLifecycle: "static",
    dataCollection: {
      userInfo: false,
      cookies: false,
      httpHeaders: { request: { allow: ["user-agent"] }, response: false },
      httpBodies: [],
      urlQueryParams: false,
      stackFrameVariables: false,
      graphQL: { document: false, variables: false },
      genAI: { inputs: false, outputs: false },
      mcp: { inputs: false, outputs: false },
      databaseQueryData: false,
      queues: false,
    },
  });
});

test("each call gets its own header list, so one runtime cannot widen another's", () => {
  const config = { dsn: DSN, environment: "production" };
  const first = sentryOptions(config, scrubEvent);
  first.dataCollection.httpHeaders.request.allow.push("x-forwarded-for");
  assert.deepEqual(sentryOptions(config, scrubEvent).dataCollection.httpHeaders.request.allow, ["user-agent"]);
});

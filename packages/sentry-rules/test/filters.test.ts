import assert from "node:assert/strict";
import test from "node:test";

import { createBeforeSend, hasFrameFromOwnBundles, isFromBot, type SentryEvent } from "../src/index.ts";

const OURS = "https://shop.example.com/_next/static/chunks/app/%5Blocale%5D/page-0a1b.js";

function thrownFrom(...absPaths: string[]): SentryEvent {
  return {
    exception: {
      values: [{ type: "TypeError", value: "boom", stacktrace: { frames: absPaths.map((abs_path) => ({ abs_path, filename: abs_path })) } }],
    },
    request: { url: "https://shop.example.com/es", headers: { "User-Agent": BROWSER } },
  };
}

const BROWSER = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Mobile Safari/537.36";

test("hasFrameFromOwnBundles: a stack is ours when any frame comes from our bundles", () => {
  const table: Array<[name: string, event: SentryEvent, want: boolean]> = [
    ["a frame from our chunks", thrownFrom(OURS), true],
    ["ours among others", thrownFrom("chrome-extension://abc/content.js", OURS), true],
    ["a relative bundle path", thrownFrom("/_next/static/chunks/main.js"), true],
    ["only an extension", thrownFrom("chrome-extension://abc/content.js"), false],
    ["only a third-party script", thrownFrom("https://www.googletagmanager.com/gtm.js"), false],
    ["another Next site's chunks", thrownFrom("https://other.example.org/_next/static/chunks/main.js"), false],
    ["a lookalike path on another host's script", thrownFrom("https://cdn.example.net/next/static/x.js"), false],
    ["no frames at all", thrownFrom(), false],
    ["no exception at all", { message: "Script error." }, false],
    [
      "a frame in a chained exception",
      { exception: { values: [{ value: "a" }, { value: "b", stacktrace: { frames: [{ filename: OURS }] } }] } },
      true,
    ],
  ];
  for (const [name, event, want] of table) {
    assert.equal(hasFrameFromOwnBundles(event), want, name);
  }
});

test("isFromBot: known crawlers, previewers and headless browsers are bots; people are not", () => {
  const bots = [
    "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
    "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
    "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
    "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)",
    "Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)",
    "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)",
    "WhatsApp/2.23.20.0",
    "TelegramBot (like TwitterBot)",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/131.0.0.0 Safari/537.36",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 Chrome-Lighthouse",
    "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)",
    "Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)",
  ];
  const people = [
    BROWSER,
    "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1",
    "Mozilla/5.0 (Linux; Android 10; CUBOT X30) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Mobile Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0",
  ];
  const withAgent = (ua: string): SentryEvent => ({ request: { headers: { "User-Agent": ua } } });
  for (const ua of bots) assert.equal(isFromBot(withAgent(ua)), true, ua);
  for (const ua of people) assert.equal(isFromBot(withAgent(ua)), false, ua);

  assert.equal(isFromBot({ request: { headers: { "user-agent": bots[0]! } } }), true, "the header name is case-insensitive");
  assert.equal(isFromBot({}), false, "no user agent is not evidence of a bot");
});

test("createBeforeSend: the Storefront's rules drop what is not ours, and scrub what is", () => {
  const storefront = createBeforeSend({ dropWithoutOwnFrame: true, dropBots: true });

  assert.equal(storefront(thrownFrom("chrome-extension://abc/content.js")), null, "an extension's error");
  const crawled = thrownFrom(OURS);
  crawled.request = { headers: { "User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" } };
  assert.equal(storefront(crawled), null, "a crawler's error");

  const ordinary = thrownFrom(OURS);
  ordinary.message = "failed for ana@example.com";
  const sent = storefront(ordinary);
  assert.ok(sent, "an ordinary event passes through");
  assert.equal(sent.message, "failed for [email]");
  assert.deepEqual(sent.request, { url: "https://shop.example.com/es" }, "headers go after the bot check has read them");
});

test("createBeforeSend: each filter is separately usable, and with none the event is only scrubbed", () => {
  const extension = thrownFrom("chrome-extension://abc/content.js");
  const bot = thrownFrom(OURS);
  bot.request = { headers: { "User-Agent": "Mozilla/5.0 (compatible; bingbot/2.0)" } };

  const onlyFrames = createBeforeSend({ dropWithoutOwnFrame: true });
  assert.equal(onlyFrames(extension), null);
  assert.ok(onlyFrames(bot));

  const onlyBots = createBeforeSend({ dropBots: true });
  assert.ok(onlyBots(extension));
  assert.equal(onlyBots(bot), null);

  const scrubOnly = createBeforeSend();
  assert.ok(scrubOnly(extension));
  assert.ok(scrubOnly(bot));
});

test("createBeforeSend: a server-side stack is judged by the bundle paths the app names", () => {
  const server = thrownFrom("/app/.next/server/app/page.js");
  assert.equal(createBeforeSend({ dropWithoutOwnFrame: true })(server), null);
  assert.ok(createBeforeSend({ dropWithoutOwnFrame: true, ownBundlePaths: ["/_next/static/", "/.next/server/"] })(server));
});

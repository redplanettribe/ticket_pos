/**
 * Flattening an Event's Markdown description into a share preview.
 *
 * The description is authored in Markdown and rendered as Markdown on the page,
 * but `<meta name="description">`, og:description and the Twitter card all take
 * plain text: a crawler prints whatever it is handed, so "## Line-up" and
 * "**free drink**" reach a search result with the syntax still attached.
 *
 * This strips the notation rather than rendering it — the result is meant to be
 * read as a sentence, not to be a faithful rendering. Structure that carries no
 * words (rules, images, code blocks) drops out; the words inside links, bold
 * and headings stay, because they are the description.
 *
 * Pure and framework-free, so node:test can reach it — the ".ts" specifier is
 * written out for the same reason (see lib/locale.ts).
 */

/**
 * How many characters of the description a preview keeps.
 *
 * Search results and share cards truncate somewhere near here anyway, and doing
 * it ourselves means the cut lands between words with an ellipsis rather than
 * mid-word behind one.
 */
const SUMMARY_LENGTH = 200;

/**
 * The plain-text reading of a Markdown description, or null when nothing
 * readable is left.
 *
 * Null rather than an empty string, so a description of nothing but an image or
 * a horizontal rule is answered the same way a missing description is, and the
 * caller falls back to its generic copy in both cases.
 */
export function markdownSummary(markdown: string, maxLength = SUMMARY_LENGTH): string | null {
  const text = collapse(strip(markdown));
  if (text === "") return null;
  return truncate(text, maxLength);
}

/** Markdown notation removed, line structure still intact. */
function strip(markdown: string): string {
  return unmaskEscapes(
    maskEscapes(markdown)
      // Fenced code is a block of source, not prose: it goes entirely, closed
      // or — the second pass, running to the end — left open.
      .replace(/^[ \t]*(```|~~~)[^\n]*\n[\s\S]*?^[ \t]*\1[ \t]*$/gm, "")
      .replace(/^[ \t]*(?:```|~~~)[\s\S]*/m, "")
      // Images before links: "![alt](src)" would otherwise leave its "!" behind.
      .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
      .replace(/!\[[^\]]*\]\[[^\]]*\]/g, "")
      // Links keep their text and lose their target, inline and reference alike.
      .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/\[([^\]]*)\]\[[^\]]*\]/g, "$1")
      // Autolinks read as the address they already are.
      .replace(/<((?:https?|mailto):[^>\s]+)>/g, "$1")
      // Horizontal rules say nothing once the line breaks are gone.
      .replace(/^[ \t]*([-*_])(?:[ \t]*\1){2,}[ \t]*$/gm, "")
      // Leading block markers: heading hashes, quote carets, list bullets and
      // numbers.
      .replace(/^[ \t]*#{1,6}[ \t]+/gm, "")
      .replace(/^[ \t]*>[ \t]?/gm, "")
      .replace(/^[ \t]*(?:[-*+]|\d+[.)])[ \t]+/gm, "")
      // A table's alignment row is pure notation; the cells around it read as
      // words in a row once the pipes between them become spaces.
      .replace(/^[ \t]*\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/gm, "")
      .replace(/[ \t]*\|[ \t]*/g, " ")
      // A setext heading's underline is notation, its text is the line above.
      .replace(/^[ \t]*(=+|-{2,})[ \t]*$/gm, "")
      // Emphasis, strikethrough and inline code, unwrapped in place.
      .replace(/(\*\*\*|___)(.+?)\1/g, "$2")
      .replace(/(\*\*|__)(.+?)\1/g, "$2")
      .replace(/(\*|_)(?!\s)(.+?)(?<!\s)\1/g, "$2")
      .replace(/~~(.+?)~~/g, "$1")
      .replace(/`+([^`]+)`+/g, "$1"),
  );
}

/**
 * An escaped marker stands in for itself while the notation is stripped.
 *
 * A "\_" is a literal underscore and must not pair off with the next one as
 * emphasis, so it is carried through the rules above as a placeholder none of
 * them can match — the character's code between two NULs — and put back once
 * they have all run.
 */
const ESCAPABLE = /\\([\\`*_{}[\]()#+\-.!>~|])/g;
const PLACEHOLDER = /\u0000(\d+)\u0000/g;

function maskEscapes(markdown: string): string {
  // A NUL already in the source could forge a placeholder. There is no reason
  // for one in a description, so it is dropped rather than reasoned about.
  return markdown
    .replaceAll("\u0000", "")
    .replace(ESCAPABLE, (_, marker: string) => `\u0000${marker.charCodeAt(0)}\u0000`);
}

function unmaskEscapes(text: string): string {
  return text.replace(PLACEHOLDER, (_, code: string) => String.fromCharCode(Number(code)));
}

/** Every run of whitespace — line breaks included — as a single space. */
function collapse(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

/** Cut at the last word boundary that fits, with an ellipsis in its place. */
function truncate(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text;

  const head = text.slice(0, maxLength - 1);
  const lastSpace = head.lastIndexOf(" ");
  // A single word longer than the whole budget has no boundary to cut on, so it
  // is cut where it is.
  const cut = lastSpace > 0 ? head.slice(0, lastSpace) : head;
  return `${cut.replace(/[,;:.\-–—]+$/, "")}…`;
}

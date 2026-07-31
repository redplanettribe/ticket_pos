import type { ComponentPropsWithoutRef, ElementType, JSX } from "react";
import ReactMarkdown, { type Components, type ExtraProps } from "react-markdown";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";

import { cn } from "../lib/utils";

/**
 * A tag rendered with the design system's classes.
 *
 * `node` — the mdast node react-markdown hands every component — is dropped
 * here rather than forwarded: React would otherwise write it into the DOM as
 * `node="[object Object]"`.
 */
function styled<Tag extends keyof JSX.IntrinsicElements>(tag: Tag, base: string) {
  function Styled({ node, className, ...props }: ComponentPropsWithoutRef<Tag> & ExtraProps) {
    void node;
    const Component = tag as ElementType;
    return <Component className={cn(base, className)} {...props} />;
  }
  Styled.displayName = `Markdown.${tag}`;
  return Styled;
}

// The copy comes from an Organization's own authoring, so raw HTML stays off:
// react-markdown skips it by default and no rehype-raw plugin is added here.
// Everything an author can write is therefore one of the node types below.
//
// Headings shift down one level: the page already owns its <h1>, and a
// description that opens with "# Line-up" means a section of that page, not a
// second title for it.
const components: Components = {
  h1: styled("h2", "mt-6 text-xl font-semibold tracking-tight first:mt-0"),
  h2: styled("h3", "mt-6 text-lg font-semibold tracking-tight first:mt-0"),
  h3: styled("h4", "mt-6 text-base font-semibold tracking-tight first:mt-0"),
  h4: styled("h5", "mt-6 text-base font-medium tracking-tight first:mt-0"),
  h5: styled("h6", "mt-6 text-sm font-medium tracking-tight first:mt-0"),
  h6: styled("h6", "mt-6 text-sm font-medium uppercase tracking-wide text-muted-foreground first:mt-0"),
  p: styled("p", "mt-4 first:mt-0"),
  ul: styled("ul", "mt-4 list-disc space-y-1 pl-6 first:mt-0"),
  ol: styled("ol", "mt-4 list-decimal space-y-1 pl-6 first:mt-0"),
  li: styled("li", "[&>ul]:mt-1 [&>ol]:mt-1"),
  blockquote: styled("blockquote", "mt-4 border-l-2 pl-4 italic text-muted-foreground first:mt-0"),
  hr: styled("hr", "my-6 border-border"),
  strong: styled("strong", "font-semibold"),
  em: styled("em", "italic"),
  del: styled("del", "line-through"),
  code: styled("code", "rounded bg-muted px-1 py-0.5 font-mono text-[0.9em]"),
  pre: styled(
    "pre",
    "mt-4 overflow-x-auto rounded-lg bg-muted p-4 text-sm first:mt-0 [&_code]:bg-transparent [&_code]:p-0",
  ),
  th: styled("th", "border border-border px-3 py-2 text-left font-medium"),
  td: styled("td", "border border-border px-3 py-2 align-top"),

  // Links point outside the platform, so they open in a new tab and are fully
  // de-referred: an author's link should not hand the opener away, and should
  // not lend the platform's standing to wherever it goes.
  a: ({ node, className, ...props }) => {
    void node;
    return (
      <a
        className={cn("font-medium underline underline-offset-4 hover:no-underline", className)}
        target="_blank"
        rel="noopener noreferrer nofollow"
        {...props}
      />
    );
  },

  // A wide table scrolls on its own rather than widening the page around it.
  table: ({ node, className, ...props }) => {
    void node;
    return (
      <div className="mt-4 overflow-x-auto first:mt-0">
        <table className={cn("w-full border-collapse text-sm", className)} {...props} />
      </div>
    );
  },

  // eslint-disable-next-line @next/next/no-img-element -- an author-supplied
  // URL on an unknown host, which the Next image loader cannot be pointed at.
  img: ({ node, className, alt, ...props }) => {
    void node;
    return (
      <img
        className={cn("mt-4 h-auto max-w-full rounded-lg first:mt-0", className)}
        alt={alt ?? ""}
        {...props}
      />
    );
  },
};

type MarkdownProps = {
  children: string;
  className?: string;
};

/**
 * Renders author-written Markdown (GitHub-flavoured, no raw HTML) with the
 * design system's typography. Block spacing lives on the elements themselves
 * rather than on the wrapper, so a single paragraph looks like plain text.
 *
 * Single newlines become line breaks (`remark-breaks`): authors write these in
 * a plain textarea, where a line they ended reads as a line ended, not as the
 * same paragraph continued.
 */
export function Markdown({ children, className }: MarkdownProps) {
  return (
    <div className={cn("leading-relaxed break-words", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkBreaks]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  );
}

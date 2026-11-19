// Module hooks that let `node --test` import the app's components, as
// packages/ui's own do (#833).
//
// Node strips types from `.ts` itself but not from `.tsx`, and it neither
// knows the `@/` alias of `tsconfig.json` nor guesses the extension of an
// import the way the bundler does. These hooks do all three: `@/` names the
// app's root, an extensionless relative or `@/` import is tried as `.ts`,
// `.tsx` and `index.ts(x)`, and a `.tsx` module is compiled by the app's own
// TypeScript with React's automatic JSX runtime. A package subpath without an
// exports map is tried as `.js`.
import { readFile } from "node:fs/promises";
import ts from "typescript";

const APP_ROOT = new URL("../../", import.meta.url);
const CANDIDATES = [".ts", ".tsx", "/index.ts", "/index.tsx"];

export async function resolve(specifier, context, nextResolve) {
  const aliased = specifier.startsWith("@/");
  if (aliased) {
    specifier = new URL(specifier.slice(2), APP_ROOT).href;
  }
  const relative = aliased || specifier.startsWith("./") || specifier.startsWith("../");
  if (/\.([cm]?[jt]sx?|json)$/.test(specifier)) {
    return nextResolve(specifier, context);
  }
  if (!relative) {
    // A package without an exports map, as Next's `next/link` is: the
    // bundler finds `link.js`, and Node needs it written out.
    try {
      return await nextResolve(specifier, context);
    } catch (error) {
      if (error?.code !== "ERR_MODULE_NOT_FOUND" || !/^[@\w][^:]*\/./.test(specifier)) {
        throw error;
      }
      return nextResolve(`${specifier}.js`, context);
    }
  }
  for (const suffix of CANDIDATES) {
    try {
      return await nextResolve(specifier + suffix, context);
    } catch {
      // Not this one; try the next.
    }
  }
  return nextResolve(specifier, context);
}

export async function load(url, context, nextLoad) {
  if (!url.endsWith(".tsx")) {
    return nextLoad(url, context);
  }
  const source = await readFile(new URL(url), "utf8");
  const { outputText } = ts.transpileModule(source, {
    fileName: url,
    compilerOptions: {
      jsx: ts.JsxEmit.ReactJSX,
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
      verbatimModuleSyntax: false,
    },
  });
  return { format: "module", source: outputText, shortCircuit: true };
}

// Module hooks that let `node --test` import the package's components.
//
// Node strips types from `.ts` itself but not from `.tsx`, and it does not
// guess the extension of a relative import the way the bundler does. These
// hooks do both: an extensionless relative import is tried as `.ts`, `.tsx`
// and `index.ts(x)`, and a `.tsx` module is compiled by the package's own
// TypeScript with the same JSX setting as `tsconfig.json`.
import { readFile } from "node:fs/promises";
import ts from "typescript";

const CANDIDATES = [".ts", ".tsx", "/index.ts", "/index.tsx"];

export async function resolve(specifier, context, nextResolve) {
  const relative = specifier.startsWith("./") || specifier.startsWith("../");
  if (!relative || /\.[cm]?[jt]sx?$/.test(specifier)) {
    return nextResolve(specifier, context);
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

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

/**
 * Every Base UI component entry point, read from its package exports in the web directory `root`. The dev server pre-bundles them all at start-up: otherwise the first page to use a component it hadn't seen makes Vite re-bundle mid-session, and a page that misses the reload that follows ends up with two copies of React ("resolveDispatcher() is null").
 */
export function baseUIEntries(root: string): string[] {
  const pkg = JSON.parse(readFileSync(resolve(root, "node_modules/@base-ui/react/package.json"), "utf8")) as {
    exports: Record<string, unknown>;
  };

  return (
    Object.keys(pkg.exports)
      // Only JavaScript entries: Vite can't pre-bundle package.json or the type-only export.
      .filter(
        (key) =>
          key.startsWith("./") &&
          !key.includes("*") &&
          !key.startsWith("./internals/") &&
          key !== "./types" &&
          !key.endsWith(".json"),
      )
      .map((key) => `@base-ui/react/${key.slice(2)}`)
  );
}

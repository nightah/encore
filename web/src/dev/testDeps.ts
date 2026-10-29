import { readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";

/** Modules the browser tests load that no source file imports by name: Testing Library loads react-dom and react-dom/client, and the JSX transform adds the runtimes. */
const LOADED_BY_DEPENDENCIES = ["react-dom", "react-dom/client", "react/jsx-dev-runtime", "react/jsx-runtime"];

/** Specifiers in import and export statements and dynamic imports, skipping type-only imports, which are erased before the browser sees them. */
const SPECIFIER =
  /(?:^|[\s;])(?:import|export)(?!\s+type\s)[^"'`]*?\sfrom\s+["']([^"']+)["']|(?:^|[\s;])import\s+["']([^"']+)["']|\bimport\(\s*["']([^"']+)["']\s*\)/gm;

/**
 * Every package the browser tests load, found by reading the sources under `root`/src that run in the test browser (all but the Node tests and src/dev, which run in Node). Vite pre-bundles them before the first test page opens: a dependency it found mid-run would make it re-bundle and reload the page, leaving it with two copies of React. Import aliases (tsconfig.json's paths), relative imports, Node built-ins and Vitest itself aren't packages to bundle.
 */
export function testDeps(root: string): string[] {
  const aliases = Object.keys(
    (
      JSON.parse(readFileSync(resolve(root, "tsconfig.json"), "utf8")) as {
        compilerOptions: { paths: Record<string, string[]> };
      }
    ).compilerOptions.paths,
  ).map((alias) => alias.replace(/\*$/, ""));

  const isPackage = (spec: string) =>
    !spec.startsWith(".") &&
    !spec.startsWith("/") &&
    !spec.startsWith("node:") &&
    !spec.startsWith("virtual:") &&
    !spec.endsWith(".css") &&
    !/^(@vitest\/|vitest($|\/))/.test(spec) &&
    !aliases.some((alias) => spec.startsWith(alias));

  const found = new Set(LOADED_BY_DEPENDENCIES);

  for (const file of browserSources(resolve(root, "src"))) {
    for (const match of readFileSync(file, "utf8").matchAll(SPECIFIER)) {
      const spec = match[1] ?? match[2] ?? match[3];
      if (spec !== undefined && isPackage(spec)) found.add(spec);
    }
  }

  return [...found].sort();
}

function browserSources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "dev" ? [] : browserSources(path);

    return /\.tsx?$/.test(entry.name) && !entry.name.endsWith(".node.test.ts") ? [path] : [];
  });
}

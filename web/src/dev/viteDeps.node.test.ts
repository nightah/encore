import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import { baseUIEntries } from "@dev/baseUI";
import { testDeps } from "@dev/testDeps";

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "generated" ? [] : sources(path);
    return /\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

// The dev server pre-bundles every Base UI component, so using a new one never makes it re-bundle mid-session (which can leave a page with two copies of React).
describe("dev server dependency pre-bundling", () => {
  it("includes every Base UI component the app imports", () => {
    const include = baseUIEntries(process.cwd());

    const imported = new Set(
      sources(resolve(process.cwd(), "src")).flatMap((file) =>
        [...readFileSync(file, "utf8").matchAll(/from "(@base-ui\/react\/[a-z-]+)"/g)].map((m) => m[1] as string),
      ),
    );

    expect(imported.size).toBeGreaterThan(0);
    for (const entry of imported) expect(include).toContain(entry);
  });

  it("leaves out entries that aren't JavaScript", () => {
    const include = baseUIEntries(process.cwd());
    expect(include).not.toContain("@base-ui/react/package.json");
    expect(include.filter((entry) => entry.endsWith(".json"))).toEqual([]);
  });
});

// The browser tests pre-bundle every package they load before the first page opens, so a dependency found mid-run never makes Vite re-bundle and reload a page.
describe("browser test dependency pre-bundling", () => {
  const roots: string[] = [];

  afterEach(() => {
    for (const root of roots.splice(0)) rmSync(root, { force: true, recursive: true });
  });

  function project(files: Record<string, string>): string {
    const root = mkdtempSync(join(tmpdir(), "encore-testdeps-"));
    roots.push(root);
    writeFileSync(
      join(root, "tsconfig.json"),
      JSON.stringify({ compilerOptions: { paths: { "@lib/*": ["./src/lib/*"], "@test/*": ["./src/test/*"] } } }),
    );
    for (const [path, body] of Object.entries(files)) {
      mkdirSync(dirname(join(root, path)), { recursive: true });
      writeFileSync(join(root, path), body);
    }

    return root;
  }

  it("finds packages imported, re-exported, imported for effect or loaded on demand", () => {
    const root = project({
      "src/app.tsx":
        'import { useQuery } from "@tanstack/react-query";\nimport { z } from "zod";\nexport { Link } from "@tanstack/react-router";\n',
      "src/test/setup.ts":
        'import "@testing-library/jest-dom/vitest";\nconst mod = await import("msw/experimental");\n',
    });

    expect(testDeps(root)).toEqual(
      expect.arrayContaining([
        "@tanstack/react-query",
        "@tanstack/react-router",
        "@testing-library/jest-dom/vitest",
        "msw/experimental",
        "zod",
      ]),
    );
  });

  it("adds what dependencies load without any source naming it", () => {
    expect(testDeps(project({ "src/app.tsx": "export const x = 1;\n" }))).toEqual([
      "react-dom",
      "react-dom/client",
      "react/jsx-dev-runtime",
      "react/jsx-runtime",
    ]);
  });

  it("leaves out aliases, relative paths, built-ins, stylesheets, type-only imports and Vitest", () => {
    const root = project({
      "src/app.tsx": [
        'import { cn } from "@lib/cn";',
        'import { helper } from "./helper";',
        'import "./app.css";',
        'import "@fontsource/ibm-plex-sans/400.css";',
        'import type { Thing } from "only-types";',
        'import { readFileSync } from "node:fs";',
        'import { describe } from "vitest";',
        'import { page } from "@vitest/browser/context";',
        "",
      ].join("\n"),
    });

    expect(testDeps(root)).toEqual(["react-dom", "react-dom/client", "react/jsx-dev-runtime", "react/jsx-runtime"]);
  });

  it("leaves out the Node tests and src/dev, which never run in the browser", () => {
    const root = project({
      "src/dev/tool.ts": 'import { glob } from "fast-glob";\n',
      "src/lib/server.node.test.ts": 'import { createServer } from "vite";\n',
    });

    expect(testDeps(root)).not.toEqual(expect.arrayContaining(["vite"]));
    expect(testDeps(root)).not.toEqual(expect.arrayContaining(["fast-glob"]));
  });

  it("covers every package the hand-kept list needed", () => {
    expect(testDeps(process.cwd())).toEqual(
      expect.arrayContaining([
        "@mswjs/interceptors/fetch",
        "@testing-library/react",
        "@testing-library/user-event",
        "i18next",
        "lucide-react",
        "msw",
        "react",
        "react-hook-form",
        "react-i18next",
      ]),
    );
  });
});

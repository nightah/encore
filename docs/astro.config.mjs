// @ts-check
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";
import starlightLinksValidator from "starlight-links-validator";
import starlightOpenAPI, { createOpenAPISidebarGroup } from "starlight-openapi";

// The admin API's operations, generated from the OpenAPI document the server commits (internal/api/openapi.json).
const apiGroup = createOpenAPISidebarGroup();

const repo = fileURLToPath(new URL("..", import.meta.url));

// The Go sources and translations the generated reference pages are built from; tests and generated output don't count.
const sources = /\/(internal\/.+(?<!_test)\.go|i18n\/.+\.ya?ml)$/;

/**
 * In `astro dev`, regenerates the OpenAPI document and the reference pages (configuration, CLI, API endpoints, voice commands) whenever the code or translations they come from change, so open pages follow without a restart. `astro build` is untouched: its script generates the pages once.
 * @returns {import("astro").AstroIntegration}
 */
function regenerateReferencePages() {
  return {
    hooks: {
      "astro:server:setup": ({ server, logger }) => {
        /** @type {NodeJS.Timeout | undefined} */
        let timer;
        let running = false;
        let again = false;

        const run = () => {
          if (running) {
            again = true;
            return;
          }
          running = true;
          const child = spawn(
            "sh",
            ["-c", "go generate ./internal/api && go generate ./internal/docsgen"],
            { cwd: repo },
          );
          let output = "";
          child.stdout.on("data", (chunk) => {
            output += chunk;
          });
          child.stderr.on("data", (chunk) => {
            output += chunk;
          });
          child.on("close", (code) => {
            running = false;
            if (code === 0) logger.info("reference pages regenerated");
            else logger.error(`regenerating the reference pages failed:\n${output}`);
            if (again) {
              again = false;
              run();
            }
          });
        };

        server.watcher.add([`${repo}internal`, `${repo}i18n`]);
        server.watcher.on("all", (_event, file) => {
          if (!sources.test(file)) return;
          clearTimeout(timer);
          timer = setTimeout(run, 300);
        });
      },
    },
    name: "encore:regenerate-reference-pages",
  };
}

// Published to GitHub Pages at https://nightah.github.io/encore/. Pages link to each other with relative links, so the base path only lives here.
export default defineConfig({
  base: "/encore",
  integrations: [
    regenerateReferencePages(),
    starlight({
      customCss: ["./src/styles/theme.css"],
      description: "Control Kodi with Alexa. Self-hosted, set up from a web interface.",
      editLink: { baseUrl: "https://github.com/nightah/encore/edit/main/docs/" },
      favicon: "/mark.svg",
      lastUpdated: false,
      logo: { alt: "Encore", src: "./brand/mark.svg" },
      plugins: [
        // Fails the build on broken internal links and anchors. The OpenAPI pages are generated routes rather than content entries, so the validator can't see them.
        starlightLinksValidator({
          exclude: ["/encore/reference/api/operations/", "/encore/reference/api/operations/**"],
        }),
        starlightOpenAPI([
          {
            base: "reference/api/operations",
            schema: "../internal/api/openapi.json",
            sidebar: { collapsed: true, group: apiGroup, label: "Operations" },
          },
        ]),
      ],
      sidebar: [
        {
          items: [
            "introduction",
            "getting-started/docker",
            "getting-started/binary",
            "getting-started/first-run",
            "tour",
          ],
          label: "Start here",
        },
        {
          items: [
            "guides/reverse-proxy",
            "guides/deploy-skill",
            "guides/kodi",
            "guides/echo-devices",
            "guides/shared-libraries",
            "guides/matching",
            "guides/request-log",
            "guides/sign-in",
            "guides/storage",
            "guides/stateless",
            "guides/health",
          ],
          label: "Guides",
        },
        {
          // The overview (index.md, sidebar order -1) then one generated page per language.
          items: [{ autogenerate: { directory: "voice-commands" } }],
          label: "Voice commands",
        },
        {
          items: ["contributing", "contributing/development", "contributing/dependencies"],
          label: "Contributing",
        },
        {
          items: [
            "reference/configuration",
            "reference/cli",
            {
              items: ["reference/api", "reference/api/endpoints", apiGroup],
              label: "Admin API",
            },
          ],
          label: "Reference",
        },
        { items: ["brand", "credits"], label: "About" },
      ],
      social: [{ href: "https://github.com/nightah/encore", icon: "github", label: "GitHub" }],
      title: "Encore",
    }),
  ],
  // brand/ is the single source of the logo and mark: it is served as the site's public directory (for the favicon) and imported by pages, never copied.
  publicDir: "./brand",
  site: "https://nightah.github.io",
  trailingSlash: "always",
});

// @ts-check
import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";
import starlightLinksValidator from "starlight-links-validator";
import starlightOpenAPI, { createOpenAPISidebarGroup } from "starlight-openapi";

// The admin API's operations, generated from the OpenAPI document the server commits (internal/api/openapi.json).
const apiGroup = createOpenAPISidebarGroup();

// Published to GitHub Pages at https://nightah.github.io/encore/. Pages link to each other with relative links, so the base path only lives here.
export default defineConfig({
  base: "/encore",
  integrations: [
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

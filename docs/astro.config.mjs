// @ts-check
import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";
import starlightLinksValidator from "starlight-links-validator";
import starlightOpenAPI, { createOpenAPISidebarGroup } from "starlight-openapi";

// The admin API's operations, generated from the OpenAPI document the server commits (internal/api/openapi.json).
const apiGroup = createOpenAPISidebarGroup();

// Published to GitHub Pages at https://nightah.github.io/encore/. Pages link to each other with relative links, so the base path only lives here.
export default defineConfig({
  site: "https://nightah.github.io",
  base: "/encore",
  trailingSlash: "always",
  // brand/ is the single source of the logo and mark: it is served as the site's public directory (for the favicon) and imported by pages, never copied.
  publicDir: "./brand",
  integrations: [
    starlight({
      title: "Encore",
      description: "Control Kodi with Alexa. Self-hosted, set up from a web interface.",
      logo: { src: "./brand/mark.svg", alt: "Encore" },
      favicon: "/mark.svg",
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/nightah/encore" }],
      editLink: { baseUrl: "https://github.com/nightah/encore/edit/main/docs/" },
      customCss: ["./src/styles/theme.css"],
      lastUpdated: false,
      plugins: [
        // Fails the build on broken internal links and anchors. The OpenAPI pages are generated routes rather than content entries, so the validator can't see them.
        starlightLinksValidator({
          exclude: ["/encore/reference/api/operations/", "/encore/reference/api/operations/**"],
        }),
        starlightOpenAPI([
          {
            base: "reference/api/operations",
            schema: "../internal/api/openapi.json",
            sidebar: { label: "Operations", group: apiGroup, collapsed: true },
          },
        ]),
      ],
      sidebar: [
        {
          label: "Start here",
          items: [
            "introduction",
            "getting-started/docker",
            "getting-started/binary",
            "getting-started/first-run",
            "tour",
          ],
        },
        {
          label: "Guides",
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
        },
        {
          label: "Voice commands",
          // The overview (index.md, sidebar order -1) then one generated page per language.
          items: [{ autogenerate: { directory: "voice-commands" } }],
        },
        {
          label: "Contributing",
          items: ["contributing", "contributing/development"],
        },
        {
          label: "Reference",
          items: [
            "reference/configuration",
            "reference/cli",
            {
              label: "Admin API",
              items: ["reference/api", "reference/api/endpoints", apiGroup],
            },
          ],
        },
        { label: "About", items: ["brand", "credits"] },
      ],
    }),
  ],
});

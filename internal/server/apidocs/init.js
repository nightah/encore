// Swagger UI for Encore's admin API. Requests go out with the admin session cookie; state-changing ones also need the CSRF token from /auth/state.
(() => {
  const spec = new URL("v1/openapi.json", window.location.href).href;
  const state = new URL("v1/auth/state", window.location.href).href;
  // The document's paths start at /api/v1; under server.base_path (e.g. /encore/) requests need that prefix, which is wherever this page is served from minus "api/".
  const base = new URL("..", new URL(".", window.location.href)).href.replace(/\/$/, "");
  let csrf;

  const refreshToken = async () => {
    try {
      const res = await fetch(state, { credentials: "same-origin" });
      if (res.ok) csrf = (await res.json()).csrf_token;
    } catch {
      csrf = undefined;
    }
  };

  const start = async () => {
    const doc = await (await fetch(spec, { credentials: "same-origin" })).json();
    doc.servers = [{ url: base, description: "This Encore server" }];

    window.SwaggerUIBundle({
      spec: doc,
      dom_id: "#swagger-ui",
      deepLinking: true,
      withCredentials: true,
      requestInterceptor: async (req) => {
        if (!["GET", "HEAD", "OPTIONS"].includes((req.method || "GET").toUpperCase())) {
          if (!csrf) await refreshToken();
          if (csrf) req.headers["X-CSRF-Token"] = csrf;
        }
        return req;
      },
    });
  };

  refreshToken();
  start().catch((err) => {
    document.getElementById("swagger-ui").textContent = `Couldn't load the API document: ${err}`;
  });
})();

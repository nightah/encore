package server

// Paths of the fixed API routes, relative to the base path.
const (
	PathAlexa   = "/api/v1/alexa"
	PathHealthz = "/api/v1/healthz"
	PathAPI     = "/api/v1/"
)

// PathAPIDocs serves Swagger UI for the admin API's OpenAPI document (/api/v1/openapi.json). Like the document, the page is public; calling the API from it needs an admin session, as anywhere else.
const PathAPIDocs = "/api/"

const apiDocsPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Encore admin API</title>
<link rel="stylesheet" href="docs/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="docs/swagger-ui-bundle.js"></script>
<script src="docs/init.js"></script>
</body>
</html>
`

// apiDocsCSP allows only the page's own assets. Swagger UI sets inline styles and renders data: images.
const apiDocsCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// nonceMarker is replaced in index.html with the per-request CSP nonce.
const nonceMarker = "{{ .CSPNonce }}"

// tagNameEnd holds the bytes that can end an HTML tag's name: the tag's close, a self-closing slash or whitespace.
const tagNameEnd = ">/ \t\n\r\f"

// hashedAssetsDir is Vite's build.assetsDir (web/vite.config.ts). Files in it carry content hashes in their names, so they can be cached forever.
const hashedAssetsDir = "static"

// placeholderIndex is served when the binary was built without the UI, or before the frontend has been built.
const placeholderIndex = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Encore</title></head>
<body><main><h1>Encore</h1><p>The web interface is not included in this build. The API is available under <code>api/v1/</code>.</p></main></body></html>
`

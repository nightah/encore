/**
 * The path Encore is mounted under, e.g. "/" or "/encore/".
 *
 * The Go server injects `<base href>` into index.html from its configured base path, so the browser's document base URI is the source of truth. Under the Vite dev server there is no injected tag and this resolves to "/".
 */
export function basePath(): string {
  if (typeof document === "undefined") return "/";

  const path = new URL(document.baseURI).pathname;

  return path.endsWith("/") ? path : path.slice(0, path.lastIndexOf("/") + 1);
}

function originOf(url: string | undefined): string {
  if (!url) return "";
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

/** The public address to start the address step from: the origin the skill's endpoint already uses, else this page's own origin when it is served over HTTPS (as through the reverse proxy Alexa would use), else nothing. */
export function suggestedAddress(endpointUrl: string | undefined, page: Pick<Location, "origin" | "protocol">): string {
  return originOf(endpointUrl) || (page.protocol === "https:" ? page.origin : "");
}

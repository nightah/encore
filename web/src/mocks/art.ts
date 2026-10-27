/**
 * Stand-in artwork for the mock's now-playing items, served where Encore serves Kodi's (/kodi/instances/{id}/art). Each poster is an original abstract design with the title, so screenshots of the mock show artwork without using anyone's.
 */
const POSTERS: Record<string, { title: string; from: string; to: string; accent: string }> = {
  "severance-poster": { accent: "#9fd6e8", from: "#0a1f2b", title: "SEVERANCE", to: "#1c5068" },
};

/** The art_url the API would report for ref on the given instance. */
export function mockArtUrl(instanceId: number, ref: string): string {
  return `/api/v1/kodi/instances/${instanceId}/art?ref=${encodeURIComponent(`image://mock/${ref}.svg/`)}`;
}

/** The SVG for an image://mock/<name>.svg/ reference, or undefined for anything else. */
export function mockArt(ref: string): string | undefined {
  const name = ref.match(/^image:\/\/mock\/([\w-]+)\.svg\/$/)?.[1];
  const poster = name ? POSTERS[name] : undefined;
  if (!poster) return undefined;
  const { title, from, to, accent } = poster;
  // A long corridor in perspective under a cold light, with the title across the foot.
  const lines = Array.from({ length: 9 }, (_, i) => {
    const x = 40 + i * 40;
    return `<line x1="200" y1="250" x2="${x}" y2="600" stroke="${accent}" stroke-opacity="0.18" stroke-width="2"/>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 600" width="400" height="600">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient>
    <radialGradient id="glow" cx="0.5" cy="0.42" r="0.5"><stop offset="0" stop-color="${accent}" stop-opacity="0.55"/><stop offset="1" stop-color="${accent}" stop-opacity="0"/></radialGradient>
  </defs>
  <rect width="400" height="600" fill="url(#bg)"/>
  <rect width="400" height="600" fill="url(#glow)"/>
  <polygon points="150,250 250,250 400,600 0,600" fill="#ffffff" fill-opacity="0.05"/>
  ${lines}
  <rect x="186" y="214" width="28" height="36" fill="${accent}" fill-opacity="0.9"/>
  <rect x="196" y="234" width="8" height="16" fill="${from}"/>
  <text x="200" y="545" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="38" font-weight="700" letter-spacing="7" fill="#ffffff">${title}</text>
</svg>`;
}

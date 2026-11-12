import type { SkillInfoContentItem } from "@api/generated/model";
import i18n from "@i18n/index";

export type Content = SkillInfoContentItem;

/** What the skill can play, in the order offered; the labels are read at render, in the current language. Leaving a group out leaves its voice commands out of the skill and its titles out of "play X"; live TV is the media centre's channels, not a library. */
export const CONTENT: Array<{ value: Content; readonly label: string; readonly hint?: string }> = [
  {
    get label() {
      return i18n.t("content.movies", { ns: "skill" });
    },
    value: "movies",
  },
  {
    get label() {
      return i18n.t("content.tv", { ns: "skill" });
    },
    value: "tv",
  },
  {
    get hint() {
      return i18n.t("content.musicHint", { ns: "skill" });
    },
    get label() {
      return i18n.t("content.music", { ns: "skill" });
    },
    value: "music",
  },
  {
    get hint() {
      return i18n.t("content.liveTVHint", { ns: "skill" });
    },
    get label() {
      return i18n.t("content.liveTV", { ns: "skill" });
    },
    value: "livetv",
  },
];

/** content with group switched on or off, in the order offered. */
export function toggleContent(content: Content[], group: Content, on: boolean): Content[] {
  return CONTENT.map((c) => c.value).filter((v) => (v === group ? on : content.includes(v)));
}

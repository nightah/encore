import {
  Clapperboard,
  LayoutDashboard,
  Library,
  type LucideIcon,
  Mic,
  ScrollText,
  Settings,
  Speaker,
} from "lucide-react";

import i18n from "@i18n/index";

export interface NavItem {
  to: "/" | "/kodi" | "/echo-devices" | "/alexa-skill" | "/request-log" | "/library" | "/settings";
  label: string;
  icon: LucideIcon;
}

const item = (
  icon: LucideIcon,
  key: "overview" | "kodi" | "echoDevices" | "alexaSkill" | "requestLog" | "library" | "settings",
  to: NavItem["to"],
): NavItem => ({
  icon,
  // A getter, so the label follows the interface language.
  get label() {
    return i18n.t(`nav.${key}`);
  },
  to,
});

export const NAV_ITEMS: readonly NavItem[] = [
  item(LayoutDashboard, "overview", "/"),
  item(Clapperboard, "kodi", "/kodi"),
  item(Speaker, "echoDevices", "/echo-devices"),
  item(Mic, "alexaSkill", "/alexa-skill"),
  item(ScrollText, "requestLog", "/request-log"),
  item(Library, "library", "/library"),
  item(Settings, "settings", "/settings"),
];

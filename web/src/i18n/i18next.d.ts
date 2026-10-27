import type auth from "@i18n/locales/en/auth.json";
import type common from "@i18n/locales/en/common.json";
import type devices from "@i18n/locales/en/devices.json";
import type kodi from "@i18n/locales/en/kodi.json";
import type library from "@i18n/locales/en/library.json";
import type overview from "@i18n/locales/en/overview.json";
import type requests from "@i18n/locales/en/requests.json";
import type settings from "@i18n/locales/en/settings.json";
import type setup from "@i18n/locales/en/setup.json";
import type skill from "@i18n/locales/en/skill.json";

// English is the source, so its files define the keys: t("…") with a key that doesn't exist is a type error.
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: {
      auth: typeof auth;
      common: typeof common;
      devices: typeof devices;
      kodi: typeof kodi;
      library: typeof library;
      overview: typeof overview;
      requests: typeof requests;
      settings: typeof settings;
      setup: typeof setup;
      skill: typeof skill;
    };
  }
}

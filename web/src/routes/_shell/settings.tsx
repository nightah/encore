import { useCallback, useState } from "react";

import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";
import { z } from "zod";

import { useSettings } from "@api/settings";
import { Page } from "@components/Page";
import { Button } from "@components/ui/Button";
import { ManagedBadge } from "@components/ui/Chip";
import { ErrorState, LoadingBlock } from "@components/ui/States";
import { Tab, TabPanel, TabsList, TabsRoot } from "@components/ui/Tabs";
import { AccessTab } from "@features/settings/AccessTab";
import { FeaturesTab } from "@features/settings/FeaturesTab";
import type { FormState } from "@features/settings/form";
import { GeneralTab } from "@features/settings/GeneralTab";
import { ImportExportTab } from "@features/settings/ImportExportTab";
import { SecretsTab } from "@features/settings/SecretsTab";
import { StorageTab } from "@features/settings/StorageTab";

/** Labelled by settings:tabs.<id>. */
const TABS = ["general", "access", "storage", "secrets", "features", "backup"] as const;
type TabId = (typeof TABS)[number];

/** Tabs whose fields are saved with the header's "Save changes" button. */
const FORM_TABS: ReadonlySet<TabId> = new Set(["general", "access", "storage"]);

const searchSchema = z.object({
  tab: z.enum(TABS).optional(),
});

export const Route = createFileRoute("/_shell/settings")({
  component: SettingsPage,
  validateSearch: searchSchema,
});

const CLEAN: FormState = { dirty: false, pending: false };

function SettingsPage() {
  const { t } = useTranslation("settings");
  const { tab = "general" } = Route.useSearch();
  const navigate = useNavigate({ from: "/settings" });
  const { data: settings, error, isPending, refetch } = useSettings();
  const [formState, setFormState] = useState<FormState>(CLEAN);
  const onFormState = useCallback((state: FormState) => setFormState(state), []);
  const formId = `settings-${tab}`;
  const hasForm = FORM_TABS.has(tab);

  return (
    <Page
      title={t("page.title")}
      width="text"
      description={
        <Trans
          t={t}
          i18nKey="page.description"
          components={{ badge: <ManagedBadge />, code: <span className="font-mono text-[13px]" /> }}
        />
      }
      actions={
        <Button
          type="submit"
          form={formId}
          variant="primary"
          disabled={!hasForm || !formState.dirty}
          pending={hasForm && formState.pending}
        >
          {t("page.save")}
        </Button>
      }
    >
      {isPending ? <LoadingBlock /> : null}
      {error ? <ErrorState error={error} onRetry={() => refetch()} /> : null}
      {settings ? (
        <TabsRoot
          value={tab}
          onValueChange={(next) => {
            setFormState(CLEAN);
            void navigate({ replace: true, search: { tab: next as TabId } });
          }}
          className="flex flex-col gap-5"
        >
          <TabsList aria-label={t("page.sections")}>
            {TABS.map((id) => (
              <Tab key={id} value={id}>
                {t(`tabs.${id}`)}
              </Tab>
            ))}
          </TabsList>
          <TabPanel value="general">
            <GeneralTab settings={settings} formId="settings-general" onFormState={onFormState} />
          </TabPanel>
          <TabPanel value="access">
            <AccessTab settings={settings} formId="settings-access" onFormState={onFormState} />
          </TabPanel>
          <TabPanel value="storage">
            <StorageTab settings={settings} formId="settings-storage" onFormState={onFormState} />
          </TabPanel>
          <TabPanel value="secrets">
            <SecretsTab settings={settings} />
          </TabPanel>
          <TabPanel value="features">
            <FeaturesTab settings={settings} />
          </TabPanel>
          <TabPanel value="backup">
            <ImportExportTab />
          </TabPanel>
        </TabsRoot>
      ) : null}
    </Page>
  );
}

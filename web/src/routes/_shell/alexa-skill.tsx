import { createFileRoute } from "@tanstack/react-router";
import { Download } from "lucide-react";
import { useTranslation } from "react-i18next";

import { skillPackageUrl, useActiveDeploy, useDeploy, useInvocationName, useSkill, useStartDeploy } from "@api/skill";
import { Page } from "@components/Page";
import { AnchorButton, Button } from "@components/ui/Button";
import { Progress } from "@components/ui/Meter";
import { ErrorState, LoadingBlock } from "@components/ui/States";
import {
  AmazonCard,
  AutoDeployPausedCard,
  AutoDeployRetryCard,
  LocalesPanel,
  ModelTitlesCard,
  Timeline,
  WarningsCard,
} from "@features/skill/SkillPanels";
import { deployFraction } from "@features/skill/timeline";

export const Route = createFileRoute("/_shell/alexa-skill")({
  component: AlexaSkillPage,
});

function AlexaSkillPage() {
  const { t } = useTranslation("skill");
  const { data: skill, error, refetch, isPending } = useSkill();
  const invocation = useInvocationName();
  const start = useStartDeploy();
  const [deployId, setDeployId] = useActiveDeploy(skill);
  const { data: deploy } = useDeploy(deployId);
  const running = start.isPending || deploy?.state === "in_progress";
  const fraction = deployFraction(deploy);
  const lastEvent = deploy?.events.at(-1);

  let status = "";
  if (running) {
    status = t("page.deploy.running", { percent: fraction, step: lastEvent?.message || t("page.deploy.starting") });
  } else if (deploy?.state === "succeeded") {
    status = t("page.deploy.succeeded");
  } else if (deploy?.state === "failed") {
    status = t("page.deploy.failed", { reason: lastEvent?.message || t("page.deploy.failedStep") });
  }

  return (
    <Page
      title={t("page.title")}
      description={invocation ? t("page.description", { name: invocation }) : undefined}
      actions={
        <>
          <AnchorButton href={skillPackageUrl()} download="encore-skill.zip">
            <Download aria-hidden="true" className="size-4" />
            {t("page.export")}
          </AnchorButton>
          <Button
            variant="primary"
            pending={running}
            disabled={!skill?.amazon.connected}
            onClick={() =>
              start.mutate(skill?.needs_deploy ? undefined : true, {
                onSuccess: ({ id }) => setDeployId(id),
              })
            }
          >
            {running ? t("page.deploy.deploying") : t("page.deploy.redeploy")}
          </Button>
        </>
      }
    >
      {isPending ? <LoadingBlock /> : null}
      {error ? <ErrorState error={error} onRetry={() => refetch()} /> : null}
      {skill ? (
        <>
          <Timeline skill={skill} deploy={deploy} />
          <div aria-live="polite" className="empty:hidden">
            {status ? (
              <div className="flex flex-col gap-1.5">
                <span className={deploy?.state === "failed" ? "text-bad" : "font-semibold"}>{status}</span>
                {running ? (
                  <Progress value={fraction} label={t("page.deploy.progress", { percent: fraction })} />
                ) : null}
              </div>
            ) : null}
          </div>
          {running ? null : <AutoDeployPausedCard skill={skill} />}
          {running ? null : <AutoDeployRetryCard skill={skill} />}
          <WarningsCard warnings={skill.last_deploy?.warnings ?? []} />
          <div className="flex flex-wrap items-start gap-4.5">
            <LocalesPanel skill={skill} deploying={running} />
            <div className="flex min-w-0 flex-[2_1_320px] flex-col gap-4.5">
              <ModelTitlesCard skill={skill} />
              <AmazonCard skill={skill} />
            </div>
          </div>
        </>
      ) : null}
    </Page>
  );
}

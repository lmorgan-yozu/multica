"use client";

import { useState } from "react";
import { ArrowLeft, Plus, Play, ArrowRight } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workflowDetailOptions } from "@multica/core/workflows/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { WorkflowStepDialog } from "./workflow-step-dialog";
import { BindWorkflowDialog } from "./bind-workflow-dialog";
import { useT } from "../../i18n";

export function WorkflowDetailPage({ workflowId }: { workflowId: string }) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data, isLoading } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [addStepOpen, setAddStepOpen] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);

  const agentName = (id: string) =>
    agents.find((a) => a.id === id)?.name ?? t(($) => $.detail.unknown_agent);

  if (isLoading) {
    return (
      <div className="space-y-2 p-5">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-11 w-full" />
        <Skeleton className="h-11 w-full" />
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }

  const { workflow, steps } = data;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex min-w-0 items-center gap-2">
          <AppLink href={wsPaths.workflows()} className="text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
          </AppLink>
          <h1 className="truncate text-sm font-medium">{workflow.name}</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setAddStepOpen(true)}>
            <Plus className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.detail.add_step)}
          </Button>
          <Button size="sm" onClick={() => setBindOpen(true)} disabled={steps.length === 0}>
            <Play className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.detail.bind_issue)}
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto p-5">
        {workflow.description && (
          <p className="mb-4 text-sm text-muted-foreground">{workflow.description}</p>
        )}
        <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.detail.steps_title)}
        </h2>
        {steps.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_steps)}</p>
        ) : (
          <ol className="flex flex-col gap-2">
            {steps.map((step) => (
              <li key={step.id} className="flex items-center gap-3 rounded-md border px-3 py-2.5">
                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium tabular-nums">
                  {step.step_order}
                </span>
                <ActorAvatar actorType="agent" actorId={step.agent_id} size={20} showStatusDot />
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-sm font-medium">
                    {step.name || agentName(step.agent_id)}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {agentName(step.agent_id)}
                  </span>
                </div>
                <div className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="rounded bg-muted px-1.5 py-0.5">{step.start_status}</span>
                  <ArrowRight className="h-3 w-3" />
                  <span className="rounded bg-muted px-1.5 py-0.5">{step.advance_status}</span>
                </div>
              </li>
            ))}
          </ol>
        )}
      </div>

      {addStepOpen && (
        <WorkflowStepDialog workflowId={workflowId} open={addStepOpen} onOpenChange={setAddStepOpen} />
      )}
      {bindOpen && (
        <BindWorkflowDialog workflowId={workflowId} open={bindOpen} onOpenChange={setBindOpen} />
      )}
    </div>
  );
}

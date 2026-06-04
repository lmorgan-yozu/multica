"use client";

import { useState } from "react";
import { Plus, Workflow as WorkflowIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workflowListOptions } from "@multica/core/workflows/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { WorkflowDialog } from "./workflow-dialog";
import { useT } from "../../i18n";

export function WorkflowsPage() {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: workflows = [], isLoading } = useQuery(workflowListOptions(wsId));
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <WorkflowIcon className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && workflows.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{workflows.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.page.new_workflow)}
        </Button>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="space-y-1 p-5 pt-3">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-11 w-full" />
            ))}
          </div>
        ) : workflows.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-center">
            <WorkflowIcon className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm font-medium">{t(($) => $.page.empty_title)}</p>
            <p className="max-w-sm text-xs text-muted-foreground">{t(($) => $.page.empty_hint)}</p>
          </div>
        ) : (
          <div className="p-5 pt-3">
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-3 text-xs text-muted-foreground sm:flex">
              <span className="flex-1">{t(($) => $.table.name)}</span>
              <span className="w-16 shrink-0 text-right">{t(($) => $.table.created)}</span>
            </div>
            {workflows.map((wf) => (
              <AppLink
                key={wf.id}
                href={wsPaths.workflowDetail(wf.id)}
                className="flex items-center gap-2 border-b px-3 py-3 text-sm hover:bg-muted/40"
              >
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-medium">{wf.name}</span>
                  {wf.description && (
                    <span className="truncate text-xs text-muted-foreground">{wf.description}</span>
                  )}
                </div>
                <span className="w-16 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
                  {new Date(wf.created_at).toLocaleDateString()}
                </span>
              </AppLink>
            ))}
          </div>
        )}
      </div>

      {createOpen && <WorkflowDialog open={createOpen} onOpenChange={setCreateOpen} />}
    </div>
  );
}

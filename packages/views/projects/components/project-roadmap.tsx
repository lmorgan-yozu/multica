"use client";

import { AlertTriangle, CalendarDays, CircleDot, GitBranch, ListTree, Milestone } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { ProjectRoadmap, RoadmapEpic, RoadmapMilestone } from "@multica/core/types";
import { projectRoadmapOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

type RoadmapViewState =
  | { status: "loading" }
  | { status: "error"; onRetry?: () => void }
  | { status: "ready"; roadmap: ProjectRoadmap };

function progressPercent(progress: { done: number; total: number }): number {
  if (progress.total <= 0) return 0;
  return Math.round((progress.done / progress.total) * 100);
}

function formatDate(date: string | null): string | null {
  if (!date) return null;
  const parsed = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return date;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  }).format(parsed);
}

function isPastDate(date: string | null): boolean {
  if (!date) return false;
  const today = new Date();
  const todayUtc = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate());
  const target = new Date(`${date}T00:00:00Z`).getTime();
  return Number.isFinite(target) && target < todayUtc;
}

function ProgressBar({ progress, label }: { progress: { done: number; total: number }; label: string }) {
  const pct = progressPercent(progress);
  return (
    <div className="min-w-0">
      <div className="mb-1 flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="truncate">{label}</span>
        <span className="shrink-0 tabular-nums">{progress.done}/{progress.total}</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={cn("h-full rounded-full transition-all", pct === 100 ? "bg-emerald-500" : "bg-primary")}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

function DateBadge({ date, target }: { date: string | null; target?: boolean }) {
  const { t } = useT("projects");
  const formatted = formatDate(date);
  if (!formatted) return null;
  const overdue = isPastDate(date);
  return (
    <Badge
      variant="outline"
      className={cn("min-w-0 gap-1 text-xs font-normal", overdue && "border-destructive/40 text-destructive")}
    >
      <CalendarDays className="size-3 shrink-0" />
      <span className="truncate">{target ? t(($) => $.roadmap.date_target) : t(($) => $.roadmap.date_due)} {formatted}</span>
    </Badge>
  );
}

function DependencyCue({ epic, epicById }: { epic: RoadmapEpic; epicById: Map<string, RoadmapEpic> }) {
  const { t } = useT("projects");
  if (epic.depends_on.length === 0) return null;
  const labels = epic.depends_on.map((id) => epicById.get(id)?.identifier ?? id.slice(0, 8));
  return (
    <div className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
      <GitBranch className="size-3 shrink-0" />
      <span className="truncate">{t(($) => $.roadmap.depends_on, { labels: labels.join(", ") })}</span>
    </div>
  );
}

function EpicCard({ epic, epicById }: { epic: RoadmapEpic; epicById: Map<string, RoadmapEpic> }) {
  const { t } = useT("projects");
  const paths = useWorkspacePaths();
  const pct = progressPercent(epic.progress);
  const blocked = epic.blocked_count > 0 || epic.status === "blocked";

  return (
    <AppLink
      href={paths.issueDetail(epic.id)}
      className="block rounded-md border bg-background p-3 outline-none transition-colors hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-1.5">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="shrink-0 text-xs font-medium text-muted-foreground">{epic.identifier}</span>
            <span className="min-w-0 break-words text-sm font-medium leading-snug">{epic.title}</span>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Badge variant="outline" className="text-xs font-normal capitalize">{epic.status.replaceAll("_", " ")}</Badge>
            {blocked && (
              <Badge variant="destructive" className="gap-1 text-xs font-normal">
                <AlertTriangle className="size-3" />
                {epic.blocked_count > 0
                  ? t(($) => $.roadmap.blocked_count, { count: epic.blocked_count })
                  : t(($) => $.roadmap.blocked)}
              </Badge>
            )}
            <DateBadge date={epic.due_date} />
            <DependencyCue epic={epic} epicById={epicById} />
          </div>
        </div>
        <div className="w-full shrink-0 sm:w-36">
          <ProgressBar progress={epic.progress} label={`${pct}%`} />
        </div>
      </div>
    </AppLink>
  );
}

function MilestoneGroup({
  milestone,
  epics,
  epicById,
}: {
  milestone: RoadmapMilestone;
  epics: RoadmapEpic[];
  epicById: Map<string, RoadmapEpic>;
}) {
  const { t } = useT("projects");

  return (
    <section className="rounded-md border bg-card">
      <div className="flex min-w-0 flex-col gap-3 border-b p-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex min-w-0 items-center gap-2">
            <Milestone className="size-4 shrink-0 text-muted-foreground" />
            <h3 className="min-w-0 break-words text-sm font-semibold">{milestone.name}</h3>
          </div>
          {milestone.description && (
            <p className="break-words text-xs text-muted-foreground">{milestone.description}</p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <DateBadge date={milestone.target_date} target />
            {milestone.blocked_count > 0 && (
              <Badge variant="destructive" className="gap-1 text-xs font-normal">
                <AlertTriangle className="size-3" />
                {t(($) => $.roadmap.blocked_count, { count: milestone.blocked_count })}
              </Badge>
            )}
          </div>
        </div>
        <div className="w-full shrink-0 lg:w-48">
          <ProgressBar progress={milestone.progress} label={t(($) => $.roadmap.progress)} />
        </div>
      </div>
      <div className="space-y-2 p-3">
        {epics.map((epic) => (
          <EpicCard key={epic.id} epic={epic} epicById={epicById} />
        ))}
      </div>
    </section>
  );
}

function UngroupedEpics({ epics, epicById }: { epics: RoadmapEpic[]; epicById: Map<string, RoadmapEpic> }) {
  const { t } = useT("projects");
  if (epics.length === 0) return null;
  return (
    <section className="rounded-md border bg-card">
      <div className="flex items-center gap-2 border-b p-4">
        <ListTree className="size-4 shrink-0 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{t(($) => $.roadmap.ungrouped_title)}</h3>
      </div>
      <div className="space-y-2 p-3">
        {epics.map((epic) => (
          <EpicCard key={epic.id} epic={epic} epicById={epicById} />
        ))}
      </div>
    </section>
  );
}

export function ProjectRoadmapView(props: RoadmapViewState) {
  const { t } = useT("projects");

  if (props.status === "loading") {
    return (
      <section className="border-b bg-background px-4 py-4 md:px-6" aria-label={t(($) => $.roadmap.title)}>
        <div className="space-y-3">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      </section>
    );
  }

  if (props.status === "error") {
    return (
      <section className="border-b bg-background px-4 py-4 md:px-6" aria-label={t(($) => $.roadmap.title)}>
        <div className="rounded-md border border-destructive/30 bg-destructive/5 p-4">
          <div className="flex items-start gap-3">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />
            <div className="min-w-0 space-y-2">
              <p className="text-sm font-medium">{t(($) => $.roadmap.error_title)}</p>
              <p className="text-xs text-muted-foreground">{t(($) => $.roadmap.error_hint)}</p>
              {props.onRetry && (
                <Button size="sm" variant="outline" onClick={props.onRetry}>
                  {t(($) => $.roadmap.retry)}
                </Button>
              )}
            </div>
          </div>
        </div>
      </section>
    );
  }

  const roadmap = props.roadmap;
  const epicById = new Map(roadmap.epics.map((epic) => [epic.id, epic]));
  const epicsInMilestones = new Set(roadmap.milestones.flatMap((milestone) => milestone.epic_ids));
  const ungroupedEpics = roadmap.epics.filter((epic) => !epicsInMilestones.has(epic.id));

  return (
    <section className="border-b bg-background px-4 py-4 md:px-6" aria-label={t(($) => $.roadmap.title)}>
      <div className="mb-4 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <CircleDot className="size-4 text-muted-foreground" />
            {t(($) => $.roadmap.title)}
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.roadmap.summary, {
              epicCount: roadmap.epics.length,
              milestoneCount: roadmap.milestones.length,
            })}
          </p>
        </div>
        {roadmap.cycle_detected && (
          <Badge variant="destructive" className="w-fit gap-1 text-xs font-normal">
            <AlertTriangle className="size-3" />
            {t(($) => $.roadmap.cycle_detected)}
          </Badge>
        )}
      </div>

      {roadmap.epics.length === 0 ? (
        <div className="flex min-h-32 flex-col items-center justify-center gap-2 rounded-md border border-dashed text-center text-muted-foreground">
          <ListTree className="size-8 text-muted-foreground/50" />
          <p className="text-sm">{t(($) => $.roadmap.empty_title)}</p>
          <p className="max-w-md px-4 text-xs">{t(($) => $.roadmap.empty_hint)}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {roadmap.milestones.map((milestone) => (
            <MilestoneGroup
              key={milestone.id}
              milestone={milestone}
              epics={milestone.epic_ids.map((id) => epicById.get(id)).filter((epic): epic is RoadmapEpic => !!epic)}
              epicById={epicById}
            />
          ))}
          <UngroupedEpics epics={ungroupedEpics} epicById={epicById} />
        </div>
      )}
    </section>
  );
}

export function ProjectRoadmapSection({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const roadmapQuery = useQuery(projectRoadmapOptions(wsId, projectId));

  if (roadmapQuery.isLoading) {
    return <ProjectRoadmapView status="loading" />;
  }

  if (roadmapQuery.isError || !roadmapQuery.data) {
    return <ProjectRoadmapView status="error" onRetry={() => void roadmapQuery.refetch()} />;
  }

  return <ProjectRoadmapView status="ready" roadmap={roadmapQuery.data} />;
}

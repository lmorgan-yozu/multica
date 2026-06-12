"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, CalendarClock, GitBranch, Map as MapIcon, Milestone, OctagonAlert, Plus } from "lucide-react";
import type { ProjectRoadmap, RoadmapEpic, RoadmapMilestone } from "@multica/core/types";
import { dateOnlyToUTCDate, formatDateOnly, isPastDateOnly } from "@multica/core/issues/date";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Progress } from "@multica/ui/components/ui/progress";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@multica/ui/components/ui/sheet";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { AppLink } from "../../navigation";
import { StatusIcon } from "../../issues/components/status-icon";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { useT } from "../../i18n";

const MS_PER_DAY = 24 * 60 * 60 * 1000;
const ROW_HEIGHT = 48;
const DAY_PX = 14;

function startOfDayUTC(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
}

function addDays(d: Date, days: number): Date {
  return new Date(d.getTime() + days * MS_PER_DAY);
}

function daysBetween(a: Date, b: Date): number {
  return Math.round((b.getTime() - a.getTime()) / MS_PER_DAY);
}

function parseDay(value: string | null): Date | null {
  return dateOnlyToUTCDate(value);
}

function progressPct(progress: { done: number; total: number }): number {
  if (progress.total <= 0) return 0;
  return Math.round((progress.done / progress.total) * 100);
}

function computeRange(epics: RoadmapEpic[], milestones: RoadmapMilestone[]) {
  const today = startOfDayUTC(new Date());
  let minTs = addDays(today, -30).getTime();
  let maxTs = addDays(today, 90).getTime();
  for (const epic of epics) {
    for (const date of [parseDay(epic.start_date), parseDay(epic.due_date)]) {
      if (!date) continue;
      minTs = Math.min(minTs, date.getTime());
      maxTs = Math.max(maxTs, date.getTime());
    }
  }
  for (const milestone of milestones) {
    const date = parseDay(milestone.target_date);
    if (!date) continue;
    minTs = Math.min(minTs, date.getTime());
    maxTs = Math.max(maxTs, date.getTime());
  }
  return {
    start: addDays(startOfDayUTC(new Date(minTs)), -14),
    end: addDays(startOfDayUTC(new Date(maxTs)), 14),
    today,
  };
}

function RoadmapLoading() {
  return (
    <div className="flex min-h-0 flex-1">
      <div className="hidden w-72 shrink-0 border-r p-3 md:block">
        {Array.from({ length: 7 }, (_, i) => (
          <Skeleton key={i} className="mb-4 h-5 w-[80%]" />
        ))}
      </div>
      <div className="flex-1 p-4">
        <Skeleton className="mb-4 h-8 w-full" />
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="mb-4 h-7" style={{ width: `${55 + i * 7}%` }} />
        ))}
      </div>
    </div>
  );
}

function RoadmapEmpty({ projectId }: { projectId: string }) {
  const { t } = useT("projects");

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
      <MapIcon className="h-10 w-10 text-muted-foreground/40" />
      <p className="text-sm">{t(($) => $.roadmap.empty_title)}</p>
      <p className="max-w-sm text-center text-xs">{t(($) => $.roadmap.empty_hint)}</p>
      <Button
        variant="outline"
        size="sm"
        className="mt-1"
        onClick={() => useModalStore.getState().open("create-issue", { project_id: projectId })}
      >
        <Plus className="mr-1.5 size-3.5" />
        {t(($) => $.roadmap.new_epic)}
      </Button>
    </div>
  );
}

function RoadmapError({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("projects");

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
      <AlertTriangle className="h-9 w-9 text-destructive/80" />
      <p className="text-sm text-foreground">{t(($) => $.roadmap.error_title)}</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        {t(($) => $.roadmap.retry)}
      </Button>
    </div>
  );
}

function EpicPopover({
  epic,
  dependsOn,
}: {
  epic: RoadmapEpic;
  dependsOn: RoadmapEpic[];
}) {
  const { t } = useT("projects");
  const p = useWorkspacePaths();
  const pct = progressPct(epic.progress);
  const overdue = !!epic.due_date && isPastDateOnly(epic.due_date) && pct < 100;

  return (
    <div className="w-72 space-y-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <StatusIcon status={epic.status} className="size-3.5" />
          <PriorityIcon priority={epic.priority} />
          <AppLink href={p.issueDetail(epic.id)} className="min-w-0 truncate text-sm font-medium hover:underline">
            {epic.identifier} {epic.title}
          </AppLink>
        </div>
        <div className="mt-1 flex flex-wrap gap-1.5">
          <Badge variant="secondary">{t(($) => $.roadmap.pct_done, { pct })}</Badge>
          {epic.blocked_count > 0 && <Badge variant="destructive">{t(($) => $.roadmap.blocked_count, { count: epic.blocked_count })}</Badge>}
          {overdue && <Badge variant="destructive">{t(($) => $.roadmap.overdue)}</Badge>}
        </div>
      </div>
      <Progress value={pct} />
      <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
        <span>{t(($) => $.roadmap.start_value, { date: epic.start_date ? formatDateOnly(epic.start_date) : "-" })}</span>
        <span>{t(($) => $.roadmap.target_value, { date: epic.due_date ? formatDateOnly(epic.due_date) : "-" })}</span>
        <span>{t(($) => $.roadmap.issues_done, { done: epic.progress.done, total: epic.progress.total })}</span>
        <span>{t(($) => $.roadmap.child_issues, { count: epic.child_count })}</span>
      </div>
      {dependsOn.length > 0 && (
        <div className="space-y-1 rounded-md bg-accent/40 p-2 text-xs">
          <div className="flex items-center gap-1.5 font-medium text-foreground">
            <GitBranch className="size-3.5" />
            {t(($) => $.roadmap.blocked_by)}
          </div>
          {dependsOn.map((dep) => (
            <AppLink key={dep.id} href={p.issueDetail(dep.id)} className="block truncate text-muted-foreground hover:text-foreground hover:underline">
              {dep.identifier} {dep.title}
            </AppLink>
          ))}
        </div>
      )}
    </div>
  );
}

function EpicProgress({ epic }: { epic: RoadmapEpic }) {
  return <ProgressMeter progress={epic.progress} />;
}

function ProgressMeter({ progress }: { progress: { done: number; total: number } }) {
  const pct = progressPct(progress);
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-emerald-500" style={{ width: `${pct}%` }} />
      </div>
      <span className="w-9 text-right text-xs tabular-nums text-muted-foreground">{pct}%</span>
    </div>
  );
}

function MobileRoadmap({
  milestones,
  unscheduled,
  epicsById,
}: {
  milestones: Array<RoadmapMilestone & { epics: RoadmapEpic[] }>;
  unscheduled: RoadmapEpic[];
  epicsById: Map<string, RoadmapEpic>;
}) {
  const { t } = useT("projects");

  return (
    <div className="flex flex-col gap-3 overflow-y-auto p-4 md:hidden">
      {milestones.map((milestone) => (
        <section key={milestone.id} className="rounded-lg border p-3">
          <div className="flex min-w-0 items-start justify-between gap-3">
            <div className="min-w-0">
              <h3 className="truncate text-sm font-medium">{milestone.name}</h3>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.roadmap.target_value, { date: milestone.target_date ? formatDateOnly(milestone.target_date) : "-" })}
              </p>
            </div>
            <ProgressMeter progress={milestone.progress} />
          </div>
          <div className="mt-3 space-y-2">
            {milestone.epics.map((epic) => (
              <Popover key={epic.id}>
                <PopoverTrigger
                  render={
                    <button className="flex w-full min-w-0 items-center justify-between gap-2 rounded-md bg-accent/50 p-2 text-left">
                      <span className="min-w-0 flex-1 truncate text-sm">{epic.title}</span>
                      <div className="flex shrink-0 items-center gap-1.5">
                        {epic.blocked_count > 0 && <OctagonAlert className="size-3.5 text-destructive" />}
                        {!!epic.due_date && isPastDateOnly(epic.due_date) && progressPct(epic.progress) < 100 && (
                          <CalendarClock className="size-3.5 text-destructive" />
                        )}
                        <span className="text-xs tabular-nums text-muted-foreground">{progressPct(epic.progress)}%</span>
                      </div>
                    </button>
                  }
                />
                <PopoverContent align="start">
                  <EpicPopover epic={epic} dependsOn={epic.depends_on.map((id) => epicsById.get(id)).filter(Boolean) as RoadmapEpic[]} />
                </PopoverContent>
              </Popover>
            ))}
          </div>
        </section>
      ))}
      {unscheduled.length > 0 && (
        <section className="rounded-lg border border-dashed p-3">
          <h3 className="text-sm font-medium">{t(($) => $.roadmap.unscheduled)}</h3>
          <div className="mt-2 space-y-2">
            {unscheduled.map((epic) => (
              <div key={epic.id} className="flex min-w-0 items-center justify-between gap-2 rounded-md bg-accent/30 p-2">
                <span className="truncate text-sm">{epic.title}</span>
                <EpicProgress epic={epic} />
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

export function RoadmapView({
  projectId,
  roadmap,
  isLoading,
  isError,
  onRetry,
}: {
  projectId: string;
  roadmap?: ProjectRoadmap;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
}) {
  const { t } = useT("projects");
  const [selectedMilestoneId, setSelectedMilestoneId] = useState<string | null>(null);
  const p = useWorkspacePaths();

  const epics = useMemo(() => roadmap?.epics ?? [], [roadmap?.epics]);
  const milestones = useMemo(() => roadmap?.milestones ?? [], [roadmap?.milestones]);
  const epicsById = useMemo(() => new Map(epics.map((epic) => [epic.id, epic])), [epics]);
  const scheduled = useMemo(() => epics.filter((epic) => epic.start_date || epic.due_date), [epics]);
  const unscheduled = useMemo(() => epics.filter((epic) => !epic.start_date && !epic.due_date), [epics]);
  const range = useMemo(() => computeRange(scheduled, milestones), [scheduled, milestones]);
  const totalDays = daysBetween(range.start, range.end);
  const width = Math.max(totalDays * DAY_PX, 720);
  const todayOffset = daysBetween(range.start, range.today) * DAY_PX;
  const groupedMilestones = useMemo(
    () => {
      const milestoneIds = new Set(milestones.map((milestone) => milestone.id));
      const grouped = milestones.map((milestone) => ({
        ...milestone,
        epics: milestone.epic_ids.map((id) => epicsById.get(id)).filter(Boolean) as RoadmapEpic[],
      }));
      const noMilestoneEpics = scheduled.filter((epic) => !epic.milestone_id || !milestoneIds.has(epic.milestone_id));
      if (noMilestoneEpics.length > 0) {
        grouped.push({
          id: "__no_milestone__",
          name: t(($) => $.roadmap.no_milestone),
          description: "",
          target_date: null,
          position: Number.MAX_SAFE_INTEGER,
          progress: {
            done: noMilestoneEpics.reduce((sum, epic) => sum + epic.progress.done, 0),
            total: noMilestoneEpics.reduce((sum, epic) => sum + epic.progress.total, 0),
          },
          blocked_count: noMilestoneEpics.reduce((sum, epic) => sum + epic.blocked_count, 0),
          epic_ids: noMilestoneEpics.map((epic) => epic.id),
          created_at: "",
          updated_at: "",
          epics: noMilestoneEpics,
        });
      }
      return grouped;
    },
    [epicsById, milestones, scheduled, t],
  );
  const selectedMilestone = groupedMilestones.find((milestone) => milestone.id === selectedMilestoneId) ?? null;

  if (isLoading) return <RoadmapLoading />;
  if (isError) return <RoadmapError onRetry={onRetry} />;
  if (!roadmap || (roadmap.epics.length === 0 && roadmap.milestones.length === 0)) return <RoadmapEmpty projectId={projectId} />;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {roadmap.cycle_detected && (
        <div className="mx-4 mb-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {t(($) => $.roadmap.cycle_warning)}
        </div>
      )}
      <MobileRoadmap milestones={groupedMilestones} unscheduled={unscheduled} epicsById={epicsById} />
      <div className="hidden min-h-0 flex-1 md:flex">
        <div className="w-[280px] shrink-0 overflow-y-auto border-r">
          <div className="sticky top-0 z-10 flex h-12 items-center border-b bg-background px-3 text-xs font-medium text-muted-foreground">
            {t(($) => $.roadmap.header)}
          </div>
          {groupedMilestones.map((milestone) => (
            <div key={milestone.id}>
              <button
                className="flex h-10 w-full min-w-0 items-center gap-2 border-b bg-accent/25 px-3 text-left text-sm font-medium hover:bg-accent/45"
                onClick={() => setSelectedMilestoneId(milestone.id)}
              >
                <Milestone className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{milestone.name}</span>
                {milestone.blocked_count > 0 && <OctagonAlert className="ml-auto size-3.5 shrink-0 text-destructive" />}
              </button>
              {milestone.epics.map((epic) => (
                <AppLink
                  key={epic.id}
                  href={p.issueDetail(epic.id)}
                  className="flex min-w-0 items-center gap-2 border-b px-3 pl-8 text-sm hover:bg-accent/30"
                  style={{ height: ROW_HEIGHT }}
                >
                  <StatusIcon status={epic.status} className="size-3.5" />
                  <span className="w-16 shrink-0 truncate text-xs text-muted-foreground">{epic.identifier}</span>
                  <Tooltip>
                    <TooltipTrigger render={<span className="min-w-0 flex-1 truncate">{epic.title}</span>} />
                    <TooltipContent>{epic.title}</TooltipContent>
                  </Tooltip>
                </AppLink>
              ))}
            </div>
          ))}
          {unscheduled.length > 0 && (
            <div>
              <div className="flex h-10 items-center gap-2 border-b bg-muted/40 px-3 text-sm font-medium">
                <CalendarClock className="size-4 text-muted-foreground" />
                {t(($) => $.roadmap.unscheduled)}
              </div>
              {unscheduled.map((epic) => (
                <AppLink key={epic.id} href={p.issueDetail(epic.id)} className="flex h-10 min-w-0 items-center gap-2 border-b px-3 pl-8 text-sm hover:bg-accent/30">
                  <span className="truncate">{epic.title}</span>
                </AppLink>
              ))}
            </div>
          )}
        </div>
        <div className="relative min-w-0 flex-1 overflow-auto">
          <div className="sticky top-0 z-10 h-12 border-b bg-background" style={{ width }}>
            <div className="relative h-full">
              {Array.from({ length: totalDays }, (_, i) => {
                const date = addDays(range.start, i);
                const show = date.getUTCDate() === 1 || date.getUTCDay() === 1;
                return (
                  <div key={i} className="absolute top-0 flex h-full items-center border-l border-foreground/10 px-1 text-[10px] text-muted-foreground" style={{ left: i * DAY_PX, width: DAY_PX }}>
                    {show && (
                      <span className="whitespace-nowrap">
                        {date.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
          <div className="relative" style={{ width, minHeight: groupedMilestones.reduce((sum, m) => sum + 40 + m.epics.length * ROW_HEIGHT, 0) }}>
            <div className="absolute top-0 bottom-0 z-[2] border-l-2 border-primary border-dashed" style={{ left: todayOffset }} />
            <span className="absolute z-[3] rounded bg-primary px-1.5 py-0.5 text-[10px] text-primary-foreground" style={{ left: todayOffset + 4, top: 4 }}>
              {t(($) => $.roadmap.now)}
            </span>
            {groupedMilestones.map((milestone, milestoneIndex) => {
              const top =
                groupedMilestones.slice(0, milestoneIndex).reduce((sum, item) => sum + 40 + item.epics.length * ROW_HEIGHT, 0);
              const marker = parseDay(milestone.target_date);
              return (
                <div key={milestone.id}>
                  <div className="absolute left-0 right-0 border-b bg-accent/10" style={{ top, height: 40 }} />
                  {marker && (
                    <button
                      className="absolute top-2 z-[3] flex -translate-x-1/2 items-center gap-1 rounded-full border bg-background px-2 py-1 text-xs shadow-sm hover:bg-accent"
                      style={{ left: daysBetween(range.start, marker) * DAY_PX }}
                      onClick={() => setSelectedMilestoneId(milestone.id)}
                    >
                      <Milestone className="size-3.5" />
                      <span className="max-w-32 truncate">{milestone.name}</span>
                    </button>
                  )}
                  {milestone.epics.map((epic, epicIndex) => {
                    const start = parseDay(epic.start_date) ?? parseDay(epic.due_date);
                    const due = parseDay(epic.due_date) ?? parseDay(epic.start_date);
                    if (!start || !due) return null;
                    const left = Math.max(0, daysBetween(range.start, start) * DAY_PX);
                    const days = Math.max(1, daysBetween(start, due) + 1);
                    const pct = progressPct(epic.progress);
                    const overdue = !!epic.due_date && isPastDateOnly(epic.due_date) && pct < 100;
                    return (
                      <Popover key={epic.id}>
                        <PopoverTrigger
                          render={
                            <button
                              className={cn(
                                "absolute h-7 overflow-hidden rounded-md border bg-accent text-left text-xs shadow-sm hover:bg-accent/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                overdue && "border-destructive text-destructive",
                              )}
                              style={{ left, top: top + 40 + epicIndex * ROW_HEIGHT + 10, width: Math.max(days * DAY_PX, 52) }}
                            >
                              <span className="absolute inset-y-0 left-0 bg-emerald-500/25" style={{ width: `${pct}%` }} />
                              <span className="relative flex h-full min-w-0 items-center gap-1.5 px-2">
                                {epic.blocked_count > 0 && <OctagonAlert className="size-3.5 shrink-0 text-destructive" />}
                                <span className="truncate">{epic.title}</span>
                              </span>
                            </button>
                          }
                        />
                        <PopoverContent align="start">
                          <EpicPopover epic={epic} dependsOn={epic.depends_on.map((id) => epicsById.get(id)).filter(Boolean) as RoadmapEpic[]} />
                        </PopoverContent>
                      </Popover>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </div>
      </div>
      <Sheet open={!!selectedMilestone} onOpenChange={(open) => !open && setSelectedMilestoneId(null)}>
        <SheetContent side="right" className="w-full sm:max-w-lg">
          {selectedMilestone && (
            <>
              <SheetHeader>
                <SheetTitle>{selectedMilestone.name}</SheetTitle>
                <SheetDescription>
                  {t(($) => $.roadmap.milestone_sheet_desc, {
                    date: selectedMilestone.target_date ? formatDateOnly(selectedMilestone.target_date) : "-",
                    pct: progressPct(selectedMilestone.progress),
                  })}
                </SheetDescription>
              </SheetHeader>
              <div className="mt-5 space-y-4">
                {selectedMilestone.description && <p className="text-sm text-muted-foreground">{selectedMilestone.description}</p>}
                <Progress value={progressPct(selectedMilestone.progress)} />
                <div className="space-y-2">
                  {selectedMilestone.epics.map((epic) => (
                    <AppLink key={epic.id} href={p.issueDetail(epic.id)} className="flex min-w-0 items-center gap-2 rounded-md border p-2 text-sm hover:bg-accent/40">
                      <StatusIcon status={epic.status} className="size-3.5" />
                      <span className="w-16 shrink-0 text-xs text-muted-foreground">{epic.identifier}</span>
                      <span className="min-w-0 flex-1 truncate">{epic.title}</span>
                      <EpicProgress epic={epic} />
                    </AppLink>
                  ))}
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

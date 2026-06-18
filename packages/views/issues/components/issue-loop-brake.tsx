"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Ban, CheckCircle2, ChevronRight, ShieldOff } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import type { IssueLoopBrake } from "@multica/core/types";
import { issueLoopBrakeOptions } from "@multica/core/issues/queries";
import { useClearIssueLoopBrake } from "@multica/core/issues/mutations";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import { toast } from "sonner";

interface IssueLoopBrakeIndicatorProps {
  issueId: string;
  className?: string;
}

export function IssueLoopBrakeIndicator({ issueId, className }: IssueLoopBrakeIndicatorProps) {
  const { t } = useT("issues");
  const { data: brake } = useQuery(issueLoopBrakeOptions(issueId));

  if (!isActiveBrake(brake)) return null;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className={cn(
              "inline-flex shrink-0 items-center gap-1 rounded-full border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 text-[11px] font-medium text-destructive",
              className,
            )}
          >
            <Ban className="h-3 w-3" />
            <span>{t(($) => $.loop_brake.paused_badge)}</span>
          </span>
        }
      />
      <TooltipContent side="top">
        {brake.reason || t(($) => $.loop_brake.paused_tooltip)}
      </TooltipContent>
    </Tooltip>
  );
}

interface IssueLoopBrakePanelProps {
  issueId: string;
  canClear: boolean;
}

export function IssueLoopBrakePanel({ issueId, canClear }: IssueLoopBrakePanelProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [reason, setReason] = useState("");
  const [lastCleared, setLastCleared] = useState<IssueLoopBrake | null>(null);
  const clearBrake = useClearIssueLoopBrake();
  const {
    data: brake,
    isLoading,
    isError,
  } = useQuery(issueLoopBrakeOptions(issueId));

  const active = isActiveBrake(brake);
  const cleared = !active && isClearedBrake(lastCleared);
  const displayBrake = active ? brake : cleared ? lastCleared : brake;
  const evidence = displayBrake?.evidence;
  const metricRows = useMemo(() => brakeMetrics(evidence), [evidence]);

  if (isLoading) {
    return (
      <section>
        <div className="mb-2 flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
          {t(($) => $.loop_brake.section)}
        </div>
        <div className="space-y-2 pl-2">
          <Skeleton className="h-4 w-36" />
          <Skeleton className="h-4 w-44" />
        </div>
      </section>
    );
  }

  if (!active && !cleared && !isError) return null;

  const submitClear = () => {
    clearBrake.mutate(
      { id: issueId, reason: reason.trim() || undefined },
      {
        onSuccess: (next) => {
          setLastCleared(next);
          setReason("");
          toast.success(t(($) => $.loop_brake.clear_success));
        },
        onError: (err) => {
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.loop_brake.clear_failed),
          );
        },
      },
    );
  };

  return (
    <section>
      <button
        type="button"
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.loop_brake.section)}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
        <span
          className={cn(
            "ml-auto rounded px-1.5 py-0.5 text-[11px]",
            active
              ? "bg-destructive/10 text-destructive"
              : cleared
                ? "bg-success/10 text-success"
                : "bg-muted text-muted-foreground",
          )}
        >
          {active
            ? t(($) => $.loop_brake.state_paused)
            : cleared
              ? t(($) => $.loop_brake.state_cleared)
              : t(($) => $.loop_brake.state_clear)}
        </span>
      </button>
      {open && (
        <div className="space-y-3 pl-2 text-xs">
          {isError ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-2 py-2 text-destructive">
              {t(($) => $.loop_brake.load_failed)}
            </div>
          ) : (
            <>
              <div
                className={cn(
                  "rounded-md border px-2 py-2",
                  active
                    ? "border-destructive/30 bg-destructive/5"
                    : "border-success/30 bg-success/5",
                )}
              >
                <div className="flex items-center gap-1.5 font-medium">
                  {active ? (
                    <ShieldOff className="h-3.5 w-3.5 shrink-0 text-destructive" />
                  ) : (
                    <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
                  )}
                  <span>
                    {active
                      ? t(($) => $.loop_brake.paused_title)
                      : t(($) => $.loop_brake.cleared_title)}
                  </span>
                </div>
                {displayBrake?.reason && (
                  <p className="mt-1 text-muted-foreground">{displayBrake.reason}</p>
                )}
                {displayBrake?.clear_reason && (
                  <p className="mt-1 text-muted-foreground">
                    {t(($) => $.loop_brake.clear_reason, {
                      reason: displayBrake.clear_reason,
                    })}
                  </p>
                )}
              </div>

              {metricRows.length > 0 && (
                <div className="grid grid-cols-2 gap-2">
                  {metricRows.map((row) => (
                    <Metric key={row.label} label={row.label} value={row.value} />
                  ))}
                </div>
              )}

              {(displayBrake?.window_started_at || displayBrake?.window_ended_at) && (
                <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
                  {displayBrake.window_started_at && (
                    <>
                      <dt className="text-muted-foreground">{t(($) => $.loop_brake.window_start)}</dt>
                      <dd className="text-right">{formatDateTime(displayBrake.window_started_at)}</dd>
                    </>
                  )}
                  {displayBrake.window_ended_at && (
                    <>
                      <dt className="text-muted-foreground">{t(($) => $.loop_brake.window_end)}</dt>
                      <dd className="text-right">{formatDateTime(displayBrake.window_ended_at)}</dd>
                    </>
                  )}
                </dl>
              )}

              {(evidence?.missing_progress_signals?.length ?? 0) > 0 && (
                <div className="rounded-md border px-2 py-2">
                  <div className="mb-1 flex items-center gap-1.5 font-medium">
                    <AlertTriangle className="h-3.5 w-3.5 text-warning" />
                    <span>{t(($) => $.loop_brake.missing_progress)}</span>
                  </div>
                  <ul className="space-y-0.5 text-muted-foreground">
                    {evidence!.missing_progress_signals!.map((signal) => (
                      <li key={signal}>{progressSignalLabel(signal, t)}</li>
                    ))}
                  </ul>
                </div>
              )}

              {active && (
                <div className="space-y-2">
                  {canClear ? (
                    <>
                      <Textarea
                        value={reason}
                        onChange={(event) => setReason(event.target.value)}
                        placeholder={t(($) => $.loop_brake.clear_reason_placeholder)}
                        className="min-h-20 resize-none text-xs"
                      />
                      <Button
                        type="button"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={submitClear}
                        disabled={clearBrake.isPending}
                      >
                        {clearBrake.isPending
                          ? t(($) => $.loop_brake.clearing_action)
                          : t(($) => $.loop_brake.clear_action)}
                      </Button>
                    </>
                  ) : (
                    <div className="rounded-md border px-2 py-2 text-muted-foreground">
                      {t(($) => $.loop_brake.clear_unauthorised)}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </section>
  );
}

function isActiveBrake(brake: IssueLoopBrake | undefined | null): brake is IssueLoopBrake {
  return brake?.state === "active";
}

function isClearedBrake(brake: IssueLoopBrake | undefined | null): brake is IssueLoopBrake {
  return brake?.state === "cleared";
}

function brakeMetrics(evidence: IssueLoopBrake["evidence"] | undefined) {
  if (!evidence) return [];
  const rows: { label: string; value: string }[] = [];
  if (evidence.run_count !== undefined) rows.push({ label: "Runs", value: String(evidence.run_count) });
  if (evidence.total_tokens !== undefined) {
    rows.push({ label: "Tokens", value: formatTokens(evidence.total_tokens) });
  }
  if (evidence.estimated_cost_usd !== undefined) {
    rows.push({ label: "Cost", value: formatCurrency(evidence.estimated_cost_usd) });
  }
  const queued = evidence.existing_queued_tasks ?? 0;
  const running = evidence.existing_running_tasks ?? 0;
  if (queued > 0 || running > 0) rows.push({ label: "Queued/running", value: `${queued}/${running}` });
  return rows;
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-muted/20 px-2 py-1.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="font-mono text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function formatCurrency(value: number) {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: value >= 1 ? 2 : 4,
  }).format(value);
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function progressSignalLabel(signal: string, t: ReturnType<typeof useT<"issues">>["t"]) {
  switch (signal) {
    case "issue_status_or_field_update":
      return t(($) => $.loop_brake.signal_issue_update);
    case "comment_or_handoff":
      return t(($) => $.loop_brake.signal_comment);
    case "attachment":
      return t(($) => $.loop_brake.signal_attachment);
    default:
      return signal.replace(/_/g, " ");
  }
}

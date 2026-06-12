"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask, IssueUsageSummary } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import { estimateCost, formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";

interface IssueSpendPanelProps {
  issueId: string;
  usage: IssueUsageSummary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type UsageRow = Pick<
  IssueUsageSummary["task_breakdown"][number],
  "model" | "input_tokens" | "output_tokens" | "cache_read_tokens" | "cache_write_tokens"
>;

export function IssueSpendPanel({ issueId, usage, open, onOpenChange }: IssueSpendPanelProps) {
  const { getActorName } = useActorName();
  const { t } = useT("issues");
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const tasksById = useMemo(
    () => new Map(tasks.map((task) => [task.id, task])),
    [tasks],
  );
  const totalTokens =
    usage.total_input_tokens +
    usage.total_output_tokens +
    usage.total_cache_read_tokens +
    usage.total_cache_write_tokens;
  const totalCost = sumCost(usage.task_breakdown);

  if (usage.task_count === 0) return null;

  return (
    <section>
      <button
        type="button"
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => onOpenChange(!open)}
      >
        {t(($) => $.detail.section_spend)}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
        <span className="ml-auto font-mono tabular-nums text-muted-foreground">
          {formatCurrency(totalCost)}
        </span>
      </button>
      {open && (
        <div className="space-y-3 pl-2">
          <div className="grid grid-cols-2 gap-2">
            <Metric label={t(($) => $.detail.spend_cost)} value={formatCurrency(totalCost)} />
            <Metric label={t(($) => $.detail.spend_tokens)} value={formatTokens(totalTokens)} />
          </div>
          <TokenBuckets
            input={usage.total_input_tokens}
            output={usage.total_output_tokens}
            cacheRead={usage.total_cache_read_tokens}
            cacheWrite={usage.total_cache_write_tokens}
            inputLabel={t(($) => $.detail.prop_input)}
            outputLabel={t(($) => $.detail.prop_output)}
            cacheReadLabel={t(($) => $.detail.spend_cache_read)}
            cacheWriteLabel={t(($) => $.detail.spend_cache_write)}
          />
          {usage.usage_status !== "complete" && (
            <div className="rounded-md border border-warning/30 bg-warning/10 px-2 py-1.5 text-xs text-warning">
              {t(($) => $.detail.spend_missing_summary, {
                missing: usage.missing_usage_task_count,
                total: usage.task_count,
              })}
            </div>
          )}
          {usage.agent_breakdown.length > 0 && (
            <div className="space-y-1.5">
              <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                {t(($) => $.detail.spend_by_agent)}
              </div>
              <div className="space-y-1">
                {usage.agent_breakdown.map((row) => (
                  <AgentUsageRow
                    key={`${row.agent_id}:${row.provider ?? ""}:${row.model ?? ""}`}
                    name={getActorName("agent", row.agent_id)}
                    row={row}
                    runsLabel={t(($) => $.detail.spend_runs_fraction, {
                      used: row.usage_task_count,
                      total: row.task_count,
                    })}
                    usageLabel={t(($) => $.detail.spend_usage_present)}
                    missingLabel={t(($) => $.detail.spend_usage_missing_badge)}
                  />
                ))}
              </div>
            </div>
          )}
          {usage.task_breakdown.length > 0 && (
            <div className="space-y-1.5">
              <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                {t(($) => $.detail.spend_by_session)}
              </div>
              <div className="max-h-72 space-y-1 overflow-y-auto pr-1">
                {usage.task_breakdown.map((row) => (
                  <TaskUsageRow
                    key={`${row.task_id}:${row.provider ?? ""}:${row.model ?? ""}`}
                    row={row}
                    task={tasksById.get(row.task_id)}
                    agentName={getActorName("agent", row.agent_id)}
                    usageMissingLabel={t(($) => $.detail.spend_usage_missing)}
                    usageLabel={t(($) => $.detail.spend_usage_present)}
                    missingLabel={t(($) => $.detail.spend_usage_missing_badge)}
                    sessionLabel={t(($) => $.detail.spend_session_label)}
                    taskLabel={t(($) => $.detail.spend_task_label)}
                    unknownModelLabel={t(($) => $.detail.spend_unknown_model)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-muted/20 px-2 py-1.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="font-mono text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function TokenBuckets({
  input,
  output,
  cacheRead,
  cacheWrite,
  inputLabel,
  outputLabel,
  cacheReadLabel,
  cacheWriteLabel,
}: {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  inputLabel: string;
  outputLabel: string;
  cacheReadLabel: string;
  cacheWriteLabel: string;
}) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-xs">
      <Bucket label={inputLabel} value={input} />
      <Bucket label={outputLabel} value={output} />
      <Bucket label={cacheReadLabel} value={cacheRead} />
      <Bucket label={cacheWriteLabel} value={cacheWrite} />
    </dl>
  );
}

function Bucket({ label, value }: { label: string; value: number }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right font-mono tabular-nums">{formatTokens(value)}</dd>
    </>
  );
}

function AgentUsageRow({
  name,
  row,
  runsLabel,
  usageLabel,
  missingLabel,
}: {
  name: string;
  row: IssueUsageSummary["agent_breakdown"][number];
  runsLabel: string;
  usageLabel: string;
  missingLabel: string;
}) {
  const cost = estimateCost(priceable(row));
  return (
    <div className="rounded-md border px-2 py-1.5 text-xs">
      <div className="flex min-w-0 items-center gap-2">
        <ActorAvatar actorType="agent" actorId={row.agent_id} size={18} />
        <span className="min-w-0 flex-1 truncate">{name}</span>
        <span className="font-mono tabular-nums">{formatCurrency(cost)}</span>
      </div>
      <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className="truncate">{modelLabel(row.provider, row.model)}</span>
        <span aria-hidden>·</span>
        <span>{runsLabel}</span>
        {row.usage_status !== "complete" && (
          <UsageBadge
            status={row.usage_status}
            usageLabel={usageLabel}
            missingLabel={missingLabel}
          />
        )}
      </div>
    </div>
  );
}

function TaskUsageRow({
  row,
  task,
  agentName,
  usageMissingLabel,
  usageLabel,
  missingLabel,
  sessionLabel,
  taskLabel,
  unknownModelLabel,
}: {
  row: IssueUsageSummary["task_breakdown"][number];
  task?: AgentTask;
  agentName: string;
  usageMissingLabel: string;
  usageLabel: string;
  missingLabel: string;
  sessionLabel: string;
  taskLabel: string;
  unknownModelLabel: string;
}) {
  const tokens =
    row.input_tokens + row.output_tokens + row.cache_read_tokens + row.cache_write_tokens;
  const when = formatTaskTime(task);
  return (
    <div
      className={cn(
        "rounded-md border px-2 py-1.5 text-xs",
        row.usage_status === "missing" && "border-warning/30 bg-warning/5",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate">
          {row.session_id
            ? `${sessionLabel} ${shortId(row.session_id)}`
            : `${taskLabel} ${shortId(row.task_id)}`}
        </span>
        <UsageBadge
          status={row.usage_status}
          usageLabel={usageLabel}
          missingLabel={missingLabel}
        />
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11px] text-muted-foreground">
        <span className="truncate">{agentName}</span>
        <span aria-hidden>·</span>
        <span>{task?.status ?? row.status}</span>
        {when && (
          <>
            <span aria-hidden>·</span>
            <span>{when}</span>
          </>
        )}
      </div>
      {row.usage_status === "missing" ? (
        <div className="mt-1 text-[11px] text-warning">{usageMissingLabel}</div>
      ) : (
        <div className="mt-1 flex items-center justify-between gap-2 text-[11px]">
          <span className="truncate text-muted-foreground">
            {modelLabel(row.provider, row.model, unknownModelLabel)}
          </span>
          <span className="shrink-0 font-mono tabular-nums">{formatTokens(tokens)}</span>
        </div>
      )}
    </div>
  );
}

function UsageBadge({
  status,
  usageLabel,
  missingLabel,
}: {
  status: string;
  usageLabel: string;
  missingLabel: string;
}) {
  if (status === "complete") {
    return (
      <span className="shrink-0 rounded bg-success/10 px-1.5 py-0.5 text-[10px] font-medium text-success">
        {usageLabel}
      </span>
    );
  }
  return (
    <span className="shrink-0 rounded bg-warning/10 px-1.5 py-0.5 text-[10px] font-medium text-warning">
      {missingLabel}
    </span>
  );
}

function sumCost(rows: readonly UsageRow[]) {
  return rows.reduce((total, row) => total + estimateCost(priceable(row)), 0);
}

function priceable(row: UsageRow) {
  return {
    model: row.model ?? "",
    input_tokens: row.input_tokens,
    output_tokens: row.output_tokens,
    cache_read_tokens: row.cache_read_tokens,
    cache_write_tokens: row.cache_write_tokens,
  };
}

function modelLabel(provider?: string, model?: string, unknownLabel = "Unknown model") {
  if (provider && model) return `${provider} / ${model}`;
  return model || provider || unknownLabel;
}

function shortId(id: string) {
  return id.slice(0, 8);
}

function formatTaskTime(task?: AgentTask) {
  if (!task) return null;
  const start = task.started_at ?? task.dispatched_at ?? task.created_at;
  const end = task.completed_at;
  if (!start) return null;
  const startLabel = formatShortDateTime(start);
  if (!end) return startLabel;
  return `${startLabel} - ${formatShortDateTime(end)}`;
}

function formatShortDateTime(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatCurrency(value: number) {
  if (value > 0 && value < 0.01) return "<$0.01";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value);
}

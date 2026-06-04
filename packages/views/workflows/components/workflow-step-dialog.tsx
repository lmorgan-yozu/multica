"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCreateWorkflowStep } from "@multica/core/workflows/mutations";
import { AgentPicker, type AssigneeSelection } from "../../autopilots/components/pickers/agent-picker";
import { useT } from "../../i18n";

// Issue statuses an issue can hold (server/migrations/001_init.up.sql).
const ISSUE_STATUSES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
] as const;

export function WorkflowStepDialog({
  workflowId,
  open,
  onOpenChange,
}: {
  workflowId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workflows");
  const [assignee, setAssignee] = useState<AssigneeSelection | null>(null);
  const [name, setName] = useState("");
  const [startStatus, setStartStatus] = useState("todo");
  const [advanceStatus, setAdvanceStatus] = useState("in_review");
  const createStep = useCreateWorkflowStep();

  const agentId = assignee?.type === "agent" ? assignee.id : "";

  const handleSubmit = async () => {
    if (!agentId) return;
    await createStep.mutateAsync({
      workflowId,
      agent_id: agentId,
      name: name.trim(),
      start_status: startStatus,
      advance_status: advanceStatus,
    });
    toast.success(t(($) => $.step.toast_added));
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.step.title)}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label>{t(($) => $.step.agent_label)}</Label>
            <AgentPicker assignee={assignee} onChange={setAssignee} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="step-name">{t(($) => $.step.name_label)}</Label>
            <Input
              id="step-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.step.name_placeholder)}
            />
          </div>
          <div className="flex gap-3">
            <div className="flex flex-1 flex-col gap-1.5">
              <Label>{t(($) => $.step.start_status_label)}</Label>
              <Select value={startStatus} onValueChange={(v) => v && setStartStatus(v)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ISSUE_STATUSES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {s}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-1 flex-col gap-1.5">
              <Label>{t(($) => $.step.advance_status_label)}</Label>
              <Select value={advanceStatus} onValueChange={(v) => v && setAdvanceStatus(v)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ISSUE_STATUSES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {s}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.step.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!agentId || createStep.isPending}>
            {t(($) => $.step.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

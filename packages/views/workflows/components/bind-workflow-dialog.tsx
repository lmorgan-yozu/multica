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
import { useBindWorkflow } from "@multica/core/workflows/mutations";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

export function BindWorkflowDialog({
  workflowId,
  open,
  onOpenChange,
}: {
  workflowId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workflows");
  const [issue, setIssue] = useState("");
  const bind = useBindWorkflow();

  const handleSubmit = async () => {
    const ref = issue.trim();
    if (!ref) return;
    // Resolve a key (e.g. TAU-123) or UUID to the canonical issue id; the
    // bind endpoint requires the UUID.
    const resolved = await api.getIssue(ref);
    await bind.mutateAsync({ workflowId, issueId: resolved.id });
    toast.success(t(($) => $.bind.toast_started));
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.bind.title)}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="bind-issue">{t(($) => $.bind.issue_label)}</Label>
            <Input
              id="bind-issue"
              value={issue}
              onChange={(e) => setIssue(e.target.value)}
              placeholder={t(($) => $.bind.issue_placeholder)}
              autoFocus
            />
            <p className="text-xs text-muted-foreground">{t(($) => $.bind.help)}</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.bind.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!issue.trim() || bind.isPending}>
            {t(($) => $.bind.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

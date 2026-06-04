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
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { useCreateWorkflow } from "@multica/core/workflows/mutations";
import { useT } from "../../i18n";

export function WorkflowDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workflows");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const createWorkflow = useCreateWorkflow();

  const handleSubmit = async () => {
    if (!name.trim()) return;
    await createWorkflow.mutateAsync({ name: name.trim(), description: description.trim() });
    toast.success(t(($) => $.create.toast_created));
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.create.title)}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="wf-name">{t(($) => $.create.name_label)}</Label>
            <Input
              id="wf-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.create.name_placeholder)}
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="wf-desc">{t(($) => $.create.description_label)}</Label>
            <Textarea
              id="wf-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.create.description_placeholder)}
              rows={3}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.create.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!name.trim() || createWorkflow.isPending}>
            {t(($) => $.create.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

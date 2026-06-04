import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workflowKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateWorkflowRequest,
  CreateWorkflowStepRequest,
  ListWorkflowsResponse,
} from "../types";

export function useCreateWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateWorkflowRequest) => api.createWorkflow(data),
    onSuccess: (created) => {
      qc.setQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId), (old) =>
        old && !old.workflows.some((w) => w.id === created.id)
          ? { ...old, workflows: [...old.workflows, created], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useCreateWorkflowStep() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ workflowId, ...data }: { workflowId: string } & CreateWorkflowStepRequest) =>
      api.createWorkflowStep(workflowId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, vars.workflowId) });
    },
  });
}

export function useBindWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ workflowId, issueId }: { workflowId: string; issueId: string }) =>
      api.bindWorkflow(workflowId, issueId),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, vars.workflowId) });
    },
  });
}

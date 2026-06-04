// Handoff workflows — ordered, role-based agent handoff chains.
// Backend: server/internal/handler/workflow.go + issue_workflow.go.

export interface Workflow {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface WorkflowStep {
  id: string;
  workflow_id: string;
  step_order: number;
  agent_id: string;
  name: string;
  start_status: string;
  advance_status: string;
}

export interface CreateWorkflowRequest {
  name: string;
  description?: string;
}

export interface CreateWorkflowStepRequest {
  agent_id: string;
  name?: string;
  start_status?: string;
  advance_status?: string;
}

export interface BindWorkflowRequest {
  issue_id: string;
}

export interface BindWorkflowResponse {
  run_id: string;
  workflow_id: string;
  issue_id: string;
  current_step_id: string;
  state: string;
}

export interface ListWorkflowsResponse {
  workflows: Workflow[];
  total: number;
}

export interface GetWorkflowResponse {
  workflow: Workflow;
  steps: WorkflowStep[];
}

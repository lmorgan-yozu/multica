import type { Label } from "./label";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

/**
 * Per-issue metadata is a flat KV map agents use to record pipeline state
 * (PR number, pipeline_status, waiting_on, ...). Values are primitives only —
 * string / number / bool — enforced by both the API and the DB. Always
 * present in responses (empty object when unset) so reads don't need a
 * nil guard on the parent field.
 */
export type IssueMetadataValue = string | number | boolean;
export type IssueMetadata = Record<string, IssueMetadataValue>;

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  milestone_id?: string | null;
  position: number;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @multica/core/issues/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  metadata: IssueMetadata;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
}

export interface QualityGateTransition {
  from: IssueStatus | string;
  to: IssueStatus | string;
}

export interface QualityGateEvent {
  actor_type: string;
  actor_id: string | null;
  created_at: string;
}

export interface QualityGateState {
  key: string;
  name: string;
  order: number;
  required_actor_type?: string;
  required_role?: string;
  independent: boolean;
  transition: QualityGateTransition;
  complete: boolean;
  blocked: boolean;
  reason?: string;
  next_actor?: string;
  event?: QualityGateEvent;
}

export interface IssueQualityGatesResponse {
  enabled: boolean;
  project_id?: string;
  issue_id?: string;
  gates: QualityGateState[];
}

export interface QualityGateOverrideResponse {
  issue: Issue;
  override: {
    reason: string;
    skipped_gates: {
      key: string;
      name: string;
      transition: QualityGateTransition;
      next_actor: string;
    }[];
  };
}

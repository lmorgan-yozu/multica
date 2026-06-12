import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { ProjectRoadmap } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectRoadmapView } from "./project-roadmap";

const roadmap: ProjectRoadmap = {
  project_id: "project-1",
  project_title: "Ada Bootstrapping",
  cycle_detected: false,
  milestones: [
    {
      id: "milestone-1",
      name: "Client demo",
      description: "First reviewable delivery path",
      target_date: "2026-07-20",
      position: 1,
      progress: { done: 2, total: 4 },
      blocked_count: 1,
      epic_ids: ["epic-2", "epic-1"],
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-01T00:00:00Z",
    },
  ],
  epics: [
    {
      id: "epic-1",
      identifier: "ADA-1",
      number: 1,
      title: "Long foundation epic title that wraps without covering the progress meter",
      status: "done",
      priority: "high",
      milestone_id: "milestone-1",
      start_date: null,
      due_date: "2026-07-10",
      position: 1,
      progress: { done: 2, total: 2 },
      blocked_count: 0,
      depends_on: [],
      child_count: 3,
    },
    {
      id: "epic-2",
      identifier: "ADA-2",
      number: 2,
      title: "Delivery workflow",
      status: "blocked",
      priority: "urgent",
      milestone_id: "milestone-1",
      start_date: null,
      due_date: null,
      position: 2,
      progress: { done: 0, total: 2 },
      blocked_count: 1,
      depends_on: ["epic-1"],
      child_count: 2,
    },
    {
      id: "epic-3",
      identifier: "ADA-3",
      number: 3,
      title: "Derived fallback epic",
      status: "todo",
      priority: "medium",
      milestone_id: null,
      start_date: null,
      due_date: null,
      position: 3,
      progress: { done: 0, total: 1 },
      blocked_count: 0,
      depends_on: [],
      child_count: 0,
    },
  ],
};

function navigationAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/projects/project-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path: string) => `https://example.test${path}`,
    prefetch: vi.fn(),
  };
}

function renderRoadmap(ui: React.ReactElement) {
  return renderWithI18n(
    <NavigationProvider value={navigationAdapter()}>
      <WorkspaceSlugProvider slug="test-workspace">{ui}</WorkspaceSlugProvider>
    </NavigationProvider>,
  );
}

describe("ProjectRoadmapView", () => {
  it("renders milestones and epics in projection order with progress, blockers, dependencies, and links", () => {
    renderRoadmap(<ProjectRoadmapView status="ready" roadmap={roadmap} />);

    expect(screen.getByRole("heading", { name: "Roadmap" })).toBeInTheDocument();
    expect(screen.getByText("Client demo")).toBeInTheDocument();
    const epicLinks = screen.getAllByRole("link");
    expect(epicLinks.map((link) => link.textContent)).toEqual([
      expect.stringContaining("ADA-2"),
      expect.stringContaining("ADA-1"),
      expect.stringContaining("ADA-3"),
    ]);
    expect(epicLinks[0]).toHaveAttribute("href", "/test-workspace/issues/epic-2");
    expect(screen.getAllByText("1 blocked")).toHaveLength(2);
    expect(screen.getByText("Depends on ADA-1")).toBeInTheDocument();
    expect(screen.getByText("Project path")).toBeInTheDocument();
  });

  it("renders an empty roadmap without issue links", () => {
    renderRoadmap(
      <ProjectRoadmapView
        status="ready"
        roadmap={{ project_id: "project-1", project_title: "Empty", milestones: [], epics: [], cycle_detected: false }}
      />,
    );

    expect(screen.getByText("No roadmap yet")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders loading and error states", () => {
    const { rerender } = renderRoadmap(<ProjectRoadmapView status="loading" />);
    expect(screen.getByLabelText("Roadmap")).toBeInTheDocument();

    rerender(
      <NavigationProvider value={navigationAdapter()}>
        <WorkspaceSlugProvider slug="test-workspace">
          <ProjectRoadmapView status="error" onRetry={vi.fn()} />
        </WorkspaceSlugProvider>
      </NavigationProvider>,
    );

    expect(screen.getByText("Roadmap could not load")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

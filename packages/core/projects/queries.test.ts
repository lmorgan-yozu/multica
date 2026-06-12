import { describe, expect, it, vi } from "vitest";
import { projectKeys, projectRoadmapOptions } from "./queries";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api";

describe("project roadmap queries", () => {
  it("uses the project roadmap key and API client", async () => {
    const getProjectRoadmap = vi.fn().mockResolvedValue({
      project_id: "project-1",
      project_title: "Ada",
      milestones: [],
      epics: [],
      cycle_detected: false,
    });
    setApiInstance({ getProjectRoadmap } as unknown as ApiClient);

    const options = projectRoadmapOptions("ws-1", "project-1");

    expect(options.queryKey).toEqual(projectKeys.roadmap("ws-1", "project-1"));
    await expect(options.queryFn?.({ queryKey: options.queryKey } as never)).resolves.toMatchObject({ project_id: "project-1" });
    expect(getProjectRoadmap).toHaveBeenCalledWith("project-1");
  });
});

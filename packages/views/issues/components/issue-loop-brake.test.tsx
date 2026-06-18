import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueLoopBrake } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueLoopBrakeIndicator, IssueLoopBrakePanel } from "./issue-loop-brake";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockGetIssueLoopBrake = vi.hoisted(() => vi.fn());
const mockClearIssueLoopBrake = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getIssueLoopBrake: (...args: unknown[]) => mockGetIssueLoopBrake(...args),
    clearIssueLoopBrake: (...args: unknown[]) => mockClearIssueLoopBrake(...args),
  },
}));

const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: mockToast,
}));

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={createQueryClient()}>
        {ui}
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function activeBrake(overrides: Partial<IssueLoopBrake> = {}): IssueLoopBrake {
  return {
    issue_id: "issue-1",
    workspace_id: "ws-1",
    state: "active",
    reason: "loop detector paused new agent runs",
    window_started_at: "2026-06-12T10:00:00Z",
    window_ended_at: "2026-06-12T10:30:00Z",
    triggered_at: "2026-06-12T10:30:00Z",
    evidence: {
      run_count: 4,
      total_tokens: 3200,
      estimated_cost_usd: 0.042,
      missing_progress_signals: ["issue_status_or_field_update", "comment_or_handoff"],
    },
    ...overrides,
  };
}

beforeEach(() => {
  mockGetIssueLoopBrake.mockReset();
  mockClearIssueLoopBrake.mockReset();
  mockToast.success.mockReset();
  mockToast.error.mockReset();
});

describe("IssueLoopBrakeIndicator", () => {
  it("renders nothing when the issue has no active brake", async () => {
    mockGetIssueLoopBrake.mockResolvedValue({ state: "clear" });

    const { container } = renderWithProviders(<IssueLoopBrakeIndicator issueId="issue-1" />);

    await waitFor(() => expect(mockGetIssueLoopBrake).toHaveBeenCalledWith("issue-1"));
    expect(container).toBeEmptyDOMElement();
  });

  it("renders a paused badge for active brakes", async () => {
    mockGetIssueLoopBrake.mockResolvedValue(activeBrake());

    renderWithProviders(<IssueLoopBrakeIndicator issueId="issue-1" />);

    expect(await screen.findByText("Paused")).toBeInTheDocument();
  });
});

describe("IssueLoopBrakePanel", () => {
  it("shows brake evidence and unauthorised clear state", async () => {
    mockGetIssueLoopBrake.mockResolvedValue(activeBrake());

    renderWithProviders(<IssueLoopBrakePanel issueId="issue-1" canClear={false} />);

    expect(await screen.findByText("New agent runs are paused")).toBeInTheDocument();
    expect(screen.getByText("loop detector paused new agent runs")).toBeInTheDocument();
    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByText("3.2K")).toBeInTheDocument();
    expect(screen.getByText("$0.042")).toBeInTheDocument();
    expect(screen.getByText("Issue status or field update")).toBeInTheDocument();
    expect(screen.getByText("Comment or handoff")).toBeInTheDocument();
    expect(screen.getByText("Only workspace owners and admins can clear this brake.")).toBeInTheDocument();
  });

  it("clears an active brake and shows the audit result", async () => {
    mockGetIssueLoopBrake
      .mockResolvedValueOnce(activeBrake())
      .mockResolvedValue({ state: "clear" });
    mockClearIssueLoopBrake.mockResolvedValue(
      activeBrake({
        state: "cleared",
        cleared_at: "2026-06-12T10:40:00Z",
        clear_reason: "Human reviewed the run loop",
      }),
    );

    renderWithProviders(<IssueLoopBrakePanel issueId="issue-1" canClear />);

    fireEvent.change(await screen.findByPlaceholderText("Why is it safe to resume agent runs?"), {
      target: { value: "Human reviewed the run loop" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Clear brake" }));

    await waitFor(() =>
      expect(mockClearIssueLoopBrake).toHaveBeenCalledWith("issue-1", {
        reason: "Human reviewed the run loop",
      }),
    );
    expect(await screen.findByText("Brake cleared")).toBeInTheDocument();
    expect(screen.getByText("Clear reason: Human reviewed the run loop")).toBeInTheDocument();
    expect(mockToast.success).toHaveBeenCalledWith("Loop brake cleared");
  });
});

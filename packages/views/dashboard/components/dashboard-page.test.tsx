import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

// The viewing timezone flows: auth store `user.timezone` → useViewingTimezone()
// → every dashboard query key. This test pins that chain: when the stored
// timezone changes, the dashboard report query keys must change, which is
// what makes TanStack Query refetch under the new tz.

// Capture every queryKey passed to useQuery. queryOptions() inside the
// dashboard options builders runs for real, so the key is the production key.
const queryKeys = vi.hoisted(() => [] as unknown[][]);
const queryData = vi.hoisted(() => ({
  daily: undefined as unknown,
  byAgent: undefined as unknown,
  agentRuntime: undefined as unknown,
  runtimeDaily: undefined as unknown,
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      const key = opts.queryKey;
      if (key[0] === "projects") return { data: [], isLoading: false };
      if (key[0] === "workspaces" && key[2] === "agents") {
        return { data: [{ id: "agent-1", name: "Ada" }], isLoading: false };
      }
      if (key[0] === "dashboard") {
        const kind = key[2];
        const data =
          kind === "daily"
            ? queryData.daily
            : kind === "by-agent"
              ? queryData.byAgent
              : kind === "agent-runtime"
                ? queryData.agentRuntime
                : kind === "runtime-daily"
                  ? queryData.runtimeDaily
                  : undefined;
        return { data, isLoading: data == null };
      }
      return { data: undefined, isLoading: true };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const tzRef = vi.hoisted(() => ({ current: "UTC" as string | null }));

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone: string | null } | null };
  const state = (): AuthState => ({ user: { timezone: tzRef.current } });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const state = () => ({ pricings: {} });
  const useCustomPricingStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return {
    getCustomPricing: () => undefined,
    useCustomPricingStore,
  };
});

import { DashboardPage } from "./dashboard-page";

describe("DashboardPage — viewing timezone drives the query key", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    queryData.daily = undefined;
    queryData.byAgent = undefined;
    queryData.agentRuntime = undefined;
    queryData.runtimeDaily = undefined;
    cleanup();
  });

  // The `tz` segment is the last element of every dashboard key
  // (see dashboardKeys in @multica/core/dashboard/queries).
  function tzSegments(): unknown[] {
    return queryKeys
      .filter((k) => k[0] === "dashboard")
      .map((k) => k[k.length - 1]);
  }

  it("uses the stored timezone in every dashboard query key", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);

    const tzs = tzSegments();
    expect(tzs.length).toBeGreaterThan(0);
    expect(tzs.every((tz) => tz === "UTC")).toBe(true);
  });

  it("flips the query key when the stored timezone changes", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);
    const utcKeys = queryKeys.filter((k) => k[0] === "dashboard");

    queryKeys.length = 0;
    cleanup();

    tzRef.current = "Asia/Tokyo";
    renderWithI18n(<DashboardPage />);
    const tokyoKeys = queryKeys.filter((k) => k[0] === "dashboard");

    expect(utcKeys.length).toBe(tokyoKeys.length);
    expect(utcKeys.length).toBeGreaterThan(0);
    // Same number of dashboard queries, but no key is shared between the
    // two timezones — so TanStack Query treats every series as a fresh
    // fetch and refetches under the new tz.
    for (let i = 0; i < utcKeys.length; i++) {
      expect(utcKeys[i]).not.toEqual(tokyoKeys[i]);
    }
  });

  it("surfaces unpriced models instead of silently under-counting cost", () => {
    queryData.daily = [
      {
        date: "2026-06-11",
        model: "unknown-dashboard-model",
        input_tokens: 1000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ];
    queryData.byAgent = [];
    queryData.agentRuntime = [];
    queryData.runtimeDaily = [
      {
        date: "2026-06-11",
        total_seconds: 30,
        task_count: 1,
        failed_count: 0,
      },
    ];

    renderWithI18n(<DashboardPage />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "1 model has no maintained price",
    );
    expect(screen.getByText("unknown-dashboard-model")).toBeInTheDocument();
  });
});

/**
 * TestApiClient — lightweight API helper for E2E test data setup/teardown.
 *
 * Uses raw fetch so E2E tests have zero build-time coupling to the web app.
 */

import "./env";
import pg from "pg";
import type { Page } from "@playwright/test";

// `||` (not `??`) so an empty `NEXT_PUBLIC_API_URL=` in .env still falls
// back to localhost. dotenv sets unset-vs-empty both as "" — treating them
// the same matches user intent.
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

interface TestWorkspace {
  id: string;
  name: string;
  slug: string;
}

export class TestApiClient {
  private token: string | null = null;
  private workspaceSlug: string | null = null;
  private workspaceId: string | null = null;
  private createdIssueIds: string[] = [];

  /**
   * Request a verification code for the email and read it from the database.
   * Cleans up codes for the email both before (rate-limit reset) and after
   * the caller verifies, via the returned `done` callback.
   */
  private async requestCode(email: string): Promise<{ code: string; done: () => Promise<void> }> {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      // Keep each E2E login isolated so previous test runs do not trip the
      // per-email send-code rate limit.
      await client.query("DELETE FROM verification_code WHERE email = $1", [email]);

      const sendRes = await fetch(`${API_BASE}/auth/send-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!sendRes.ok) {
        throw new Error(`send-code failed: ${sendRes.status}`);
      }

      const result = await client.query(
        "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
        [email],
      );
      if (result.rows.length === 0) {
        throw new Error(`No verification code found for ${email}`);
      }
      const code: string = result.rows[0].code;

      return {
        code,
        done: async () => {
          const cleanup = new pg.Client(DATABASE_URL);
          await cleanup.connect();
          try {
            await cleanup.query("DELETE FROM verification_code WHERE email = $1", [email]);
          } finally {
            await cleanup.end();
          }
        },
      };
    } finally {
      await client.end();
    }
  }

  private async applyLogin(data: { token?: string; user?: { name?: string } }, name: string) {
    if (!data.token) throw new Error("verify-code response had no token");
    this.token = data.token;

    // Update user name if needed
    if (name && data.user?.name !== name) {
      await this.authedFetch("/api/me", {
        method: "PATCH",
        body: JSON.stringify({ name }),
      });
    }
  }

  /** Log in via the API only (no browser session). Use for data setup/teardown. */
  async login(email: string, name: string) {
    const { code, done } = await this.requestCode(email);
    try {
      const verifyRes = await fetch(`${API_BASE}/auth/verify-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code }),
      });
      if (!verifyRes.ok) {
        throw new Error(`verify-code failed: ${verifyRes.status}`);
      }
      const data = await verifyRes.json();
      await this.applyLogin(data, name);
      return data;
    } finally {
      await done();
    }
  }

  /**
   * Log in AND authenticate the browser session in one flow.
   *
   * Sends verify-code through the page's request context (same-origin via
   * the Next.js rewrite), so the backend's HttpOnly auth + CSRF cookies land
   * in the browser's cookie jar — the same cookie-auth session a real user
   * gets. The localStorage `multica_token` path is the deprecated legacy
   * mode and must not be used for new tests: a transient fetch failure
   * during auth init deletes the injected token mid-navigation.
   *
   * Also keeps the Bearer token on this client for API setup/teardown.
   */
  async loginInBrowser(page: Page, email: string, name: string) {
    const { code, done } = await this.requestCode(email);
    try {
      const verifyRes = await page.request.post("/auth/verify-code", {
        data: { email, code },
      });
      if (!verifyRes.ok()) {
        throw new Error(`verify-code failed: ${verifyRes.status()}`);
      }
      const data = await verifyRes.json();
      await this.applyLogin(data, name);
      return data;
    } finally {
      await done();
    }
  }

  /**
   * Mark the logged-in user as onboarded. Workspace routes hard-gate on
   * `onboarded_at`; fresh e2e users must call this before visiting
   * /{slug}/... pages. Records a skipped-everything questionnaire first —
   * without a resolved source the SourceBackfillModal floats over the
   * workspace and intercepts clicks. Idempotent. Onboarding specs
   * deliberately skip this method.
   */
  async completeOnboarding() {
    const patchRes = await this.authedFetch("/api/me/onboarding", {
      method: "PATCH",
      body: JSON.stringify({
        questionnaire: {
          source_skipped: true,
          role_skipped: true,
          use_case_skipped: true,
          version: 2,
        },
      }),
    });
    if (!patchRes.ok) {
      throw new Error(`patch onboarding questionnaire failed: ${patchRes.status}`);
    }
    const res = await this.authedFetch("/api/me/onboarding/complete", {
      method: "POST",
    });
    if (!res.ok) {
      throw new Error(`complete onboarding failed: ${res.status}`);
    }
  }

  async getWorkspaces(): Promise<TestWorkspace[]> {
    const res = await this.authedFetch("/api/workspaces");
    return res.json();
  }

  async getMe(): Promise<{ id: string; email: string; name: string }> {
    const res = await this.authedFetch("/api/me");
    if (!res.ok) {
      throw new Error(`get me failed: ${res.status}`);
    }
    return res.json();
  }

  setWorkspaceId(id: string) {
    this.workspaceId = id;
  }

  setWorkspaceSlug(slug: string) {
    this.workspaceSlug = slug;
  }

  async ensureWorkspace(name = "E2E Workspace", slug = "e2e-workspace") {
    const workspaces = await this.getWorkspaces();
    const workspace = workspaces.find((item) => item.slug === slug) ?? workspaces[0];
    if (workspace) {
      this.workspaceId = workspace.id;
      this.workspaceSlug = workspace.slug;
      return workspace;
    }

    const res = await this.authedFetch("/api/workspaces", {
      method: "POST",
      body: JSON.stringify({ name, slug }),
    });
    if (res.ok) {
      const created = (await res.json()) as TestWorkspace;
      this.workspaceId = created.id;
      return created;
    }

    const refreshed = await this.getWorkspaces();
    const created = refreshed.find((item) => item.slug === slug) ?? refreshed[0];
    if (created) {
      this.workspaceId = created.id;
      return created;
    }

    throw new Error(`Failed to ensure workspace ${slug}: ${res.status} ${res.statusText}`);
  }

  async createIssue(title: string, opts?: Record<string, unknown>) {
    const res = await this.authedFetch("/api/issues", {
      method: "POST",
      body: JSON.stringify({ title, ...opts }),
    });
    const issue = await res.json();
    this.createdIssueIds.push(issue.id);
    return issue;
  }

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  /** Clean up all issues created during this test. */
  async cleanup() {
    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];
  }

  getToken() {
    return this.token;
  }

  private async authedFetch(path: string, init?: RequestInit) {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...((init?.headers as Record<string, string>) ?? {}),
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    if (this.workspaceSlug) headers["X-Workspace-Slug"] = this.workspaceSlug;
    else if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return fetch(`${API_BASE}${path}`, { ...init, headers });
  }
}

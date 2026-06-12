import { type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

// Per-worker identity. The send-code rate limit (1 code / 60s / email) is
// reset by deleting the email's codes before each request, but parallel
// workers sharing one email race each other: worker A's pre-send DELETE
// wipes the code worker B just requested. A worker-scoped email removes the
// contention entirely; tests within a worker run serially.
const WORKER = process.env.TEST_PARALLEL_INDEX ?? "0";
const DEFAULT_E2E_NAME = "E2E User";
const DEFAULT_E2E_EMAIL = `e2e-w${WORKER}@multica.ai`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-w${WORKER}`;

/**
 * Log in as this worker's E2E user with a real cookie-auth browser session,
 * mark the user onboarded (workspace routes hard-gate on `onboarded_at`),
 * ensure the workspace exists, and land on its issues page.
 *
 * Returns the workspace slug so callers can build workspace-scoped URLs.
 */
export async function loginAsDefault(page: Page): Promise<string> {
  const api = new TestApiClient();
  await api.loginInBrowser(page, DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.completeOnboarding();
  const workspace = await api.ensureWorkspace(
    "E2E Workspace",
    DEFAULT_E2E_WORKSPACE,
  );

  // The floating chat window defaults OPEN for new users and overlaps
  // bottom-right content (e.g. the settings Save button), intercepting
  // clicks. Seed the explicit "closed" preference the product persists.
  await page.addInitScript(() => {
    localStorage.setItem("multica:chat:isOpen", "false");
  });

  await page.goto(`/${workspace.slug}/issues`);
  await page.waitForURL("**/issues", { timeout: 15000 });
  return workspace.slug;
}

/**
 * Create a TestApiClient logged in as this worker's E2E user (API only).
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.completeOnboarding();
  await api.ensureWorkspace("E2E Workspace", DEFAULT_E2E_WORKSPACE);
  return api;
}

export async function openWorkspaceMenu(page: Page) {
  // The workspace switcher is the sidebar button carrying the workspace name.
  await page.getByRole("button", { name: /E2E Workspace/ }).click();
  // The dropdown always ends with the Log out item — visible means open.
  await page
    .getByRole("menuitem", { name: "Log out" })
    .waitFor({ state: "visible" });
}

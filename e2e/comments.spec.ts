import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.createIssue("E2E Comment Test " + Date.now());
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("can add a comment on an issue", async ({ page }) => {
    // Wait for issues to load and click first one. `*=` matches both legacy
    // `/issues/{id}` and URL-refactored `/{slug}/issues/{id}` hrefs.
    const issueLink = page.locator('a[href*="/issues/"]').first();
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    // Wait for issue detail to load
    await expect(page.getByText("Properties")).toBeVisible();

    // Type a comment into the rich-text editor (Tiptap contenteditable,
    // exposed as a textbox named by its placeholder). The issue chat panel
    // has its own editor + Send — the placeholder disambiguates.
    const commentText = "E2E comment " + Date.now();
    const commentInput = page.getByRole("textbox", {
      name: "Leave a comment...",
    });
    await commentInput.click();
    await commentInput.fill(commentText);

    // Submit with the comment editor's keyboard shortcut — the page also
    // carries the chat panel's Send button, so the shortcut is unambiguous.
    await commentInput.press("ControlOrMeta+Enter");

    // Comment should appear in the activity section.
    await expect(page.getByText(commentText).first()).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    const issueLink = page.locator('a[href*="/issues/"]').first();
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    await expect(page.getByText("Properties")).toBeVisible();

    // Submit button should be disabled when the editor is empty. The
    // comment section renders before the chat panel, so first() is the
    // comment Send.
    await expect(
      page.getByRole("button", { name: "Send" }).first(),
    ).toBeDisabled();
  });
});

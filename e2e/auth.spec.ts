import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.goto("/login");

    await expect(page.getByText("Sign in to Multica")).toBeVisible();
    const emailInput = page.getByRole("textbox", { name: "Email" });
    await expect(emailInput).toBeVisible();

    // Continue is disabled until an email is entered.
    const continueButton = page.getByRole("button", { name: "Continue" });
    await expect(continueButton).toBeDisabled();
    await emailInput.fill("someone@example.com");
    await expect(continueButton).toBeEnabled();
  });

  test("login and redirect to /issues", async ({ page }) => {
    const slug = await loginAsDefault(page);

    await expect(page).toHaveURL(new RegExp(`/${slug}/issues`));
    // The workspace shell rendered: sidebar shows the workspace switcher.
    await expect(
      page.getByRole("button", { name: /E2E Workspace/ }),
    ).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page }) => {
    // Visit a workspace-scoped route with no session; the workspace layout
    // guard should redirect to /login. The slug need not exist — the guard
    // runs before workspace resolution for unauthenticated users.
    await page.goto("/e2e-workspace/issues");
    await page.waitForURL("**/login", { timeout: 10000 });
  });

  test("logout redirects to /login", async ({ page }) => {
    await loginAsDefault(page);

    // Log out lives in the workspace switcher dropdown.
    await openWorkspaceMenu(page);
    await page.getByRole("menuitem", { name: "Log out" }).click();

    await page.waitForURL("**/login", { timeout: 10000 });
    await expect(page).toHaveURL(/\/login/);
  });
});

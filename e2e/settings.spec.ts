import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page);

    // The workspace switcher (first button in the sidebar header) shows the
    // current name. Locate by slot, not name — the name changes mid-test.
    const sidebarName = page
      .locator('[data-slot="sidebar-header"] button')
      .first();
    await expect(sidebarName).toContainText("E2E Workspace");

    // Navigate to settings → workspace General tab.
    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings**");
    await page.getByRole("tab", { name: "General" }).click();

    // Change workspace name. Capture the saved value for the restore step —
    // the sidebar button's text also contains the avatar initial, so the
    // input is the clean source of truth.
    const nameInput = page.getByRole("textbox", { name: "Name" }).first();
    await expect(nameInput).not.toHaveValue("");
    const originalName = await nameInput.inputValue();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    // Save and wait for the confirmation toast.
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Workspace settings saved")).toBeVisible({
      timeout: 5000,
    });

    // Sidebar should reflect the new name WITHOUT page refresh.
    await expect(sidebarName).toContainText(newName);

    // Restore original name so other tests aren't affected.
    await nameInput.fill(originalName);
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(sidebarName).toContainText(originalName);
  });
});

import { beforeEach, describe, expect, it, vi } from "vitest";
import { openLink } from "./link-handler";

describe("openLink", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it.each([
    ["/library", "/acme/library"],
    ["/workflows", "/acme/workflows"],
    ["/squads", "/acme/squads"],
  ])("prefixes legacy workspace route %s with the current slug", (href, path) => {
    const dispatchEvent = vi.spyOn(window, "dispatchEvent");

    openLink(href, "acme");

    const event = dispatchEvent.mock.calls[0]?.[0] as CustomEvent | undefined;
    expect(event?.type).toBe("multica:navigate");
    expect(event?.detail).toEqual({ path });
  });
});

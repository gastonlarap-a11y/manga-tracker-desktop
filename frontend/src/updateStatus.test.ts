import { describe, expect, it } from "bun:test";
import type { main } from "../wailsjs/go/models";
import { canCheckAgain, noticeFor, updateLine } from "./updateStatus";

function status(overrides: Partial<main.UpdateStatus>): main.UpdateStatus {
  return {
    state: "current",
    current: "v0.1.20",
    latest: "v0.1.20",
    dismissed: "",
    problem: "",
    ...overrides,
  };
}

describe("noticeFor", () => {
  it("announces a newer release", () => {
    expect(noticeFor(status({ state: "available", latest: "v0.1.21" }))).toBe(
      "v0.1.21",
    );
  });

  it("stays quiet for a release whose notice was closed, not for the next", () => {
    expect(
      noticeFor(
        status({ state: "available", latest: "v0.1.21", dismissed: "v0.1.21" }),
      ),
    ).toBeNull();
    expect(
      noticeFor(
        status({ state: "available", latest: "v0.1.22", dismissed: "v0.1.21" }),
      ),
    ).toBe("v0.1.22");
  });

  it("shows nothing when up to date, unknown, off, a development build or not asked yet", () => {
    for (const state of ["current", "unknown", "off", "development"]) {
      expect(noticeFor(status({ state }))).toBeNull();
    }
    expect(noticeFor(null)).toBeNull();
  });
});

describe("updateLine", () => {
  it("names both versions when one is newer", () => {
    expect(
      updateLine(status({ state: "available", latest: "v0.1.21" })),
    ).toBe("Hay una versión nueva: v0.1.21 (tenés v0.1.20).");
  });

  it("never calls an unanswered question up to date", () => {
    expect(updateLine(status({ state: "unknown" }))).toBe(
      "No pude comprobar si hay una versión nueva.",
    );
    // A code this window does not know yet reads the same way.
    expect(updateLine(status({ state: "something-new" }))).toBe(
      "No pude comprobar si hay una versión nueva.",
    );
    expect(updateLine(status({ state: "current" }))).toBe("Estás al día.");
  });
});

describe("canCheckAgain", () => {
  it("offers to ask again only where asking could change the answer", () => {
    expect(canCheckAgain(status({ state: "unknown" }))).toBe(true);
    expect(canCheckAgain(status({ state: "current" }))).toBe(true);
    expect(canCheckAgain(status({ state: "off" }))).toBe(false);
    expect(canCheckAgain(status({ state: "development" }))).toBe(false);
  });
});

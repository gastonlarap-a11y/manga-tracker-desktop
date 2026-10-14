import { describe, expect, it } from "bun:test";
import type { main } from "../wailsjs/go/models";
import { backupLine, sizeLabel } from "./backupOutcome";

function outcome(overrides: Partial<main.ExportOutcome>): main.ExportOutcome {
  return {
    state: "saved",
    problem: "",
    path: "/Users/someone/Documents/manga-tracker-2026-10-13.json",
    bytes: 2_500_000,
    detail: "",
    ...overrides,
  };
}

describe("backupLine", () => {
  it("says where the file went and how big it is", () => {
    expect(backupLine(outcome({}))).toEqual({
      tone: "good",
      text: "Guardado en /Users/someone/Documents/manga-tracker-2026-10-13.json (2,4 MB).",
    });
  });

  it("says nothing when the dialog was cancelled", () => {
    expect(backupLine(outcome({ state: "cancelled", path: "" }))).toBeNull();
  });

  it.each([
    ["no-backend", "El servicio no está respondiendo"],
    ["unsupported", "todavía no sabe exportar"],
    ["download", "no se guardó nada"],
    ["write", "Probá en otra carpeta"],
  ])("words the %s failure", (problem, words) => {
    const line = backupLine(outcome({ state: "failed", problem }));
    expect(line?.tone).toBe("bad");
    expect(line?.text).toContain(words);
  });

  it("never reads an unknown outcome as saved", () => {
    expect(backupLine(outcome({ state: "failed", problem: "new-code" }))).toEqual(
      { tone: "bad", text: "No se pudo exportar la biblioteca." },
    );
    expect(backupLine(outcome({ state: "something-new" }))?.tone).toBe("bad");
  });
});

describe("sizeLabel", () => {
  it.each([
    [512, "512 B"],
    [2048, "2 kB"],
    [3 * 1024 * 1024 + 300_000, "3,3 MB"],
    [240 * 1024 * 1024, "240 MB"],
  ])("writes %d bytes as %s", (bytes, label) => {
    expect(sizeLabel(bytes)).toBe(label);
  });
});

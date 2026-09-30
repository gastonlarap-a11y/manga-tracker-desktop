import { describe, expect, it } from "bun:test";
import {
  ACTION_FAILED,
  fileManagerLabel,
  NOT_CONNECTED,
  outcomeOf,
  type SyncOutcomeFields,
  sinceLabel,
} from "./syncOutcome";

const outcome = (fields: Partial<SyncOutcomeFields>): SyncOutcomeFields => ({
  settled: true,
  connected: false,
  usesSrv: false,
  converted: false,
  host: "",
  secretInConfig: false,
  lastError: "",
  ...fields,
});

describe("outcomeOf", () => {
  it("keeps 'still restarting' apart from 'did not connect'", () => {
    // Saving restarts the service; looking too early is the common case, and
    // calling it a failure told people a working sync had broken.
    expect(outcomeOf("save", outcome({ settled: false }))).toEqual({
      kind: "unsettled",
      action: "save",
    });
  });

  it("carries the action, so a retry is never reported as a save", () => {
    const failed = outcomeOf("retry", outcome({ lastError: "timed out" }));

    expect(failed).toEqual({
      kind: "failed",
      action: "retry",
      detail: "timed out",
    });
  });

  it("names the address actually stored when a srv one was converted", () => {
    const connected = outcomeOf(
      "save",
      outcome({ connected: true, converted: true, host: "h1:27017,h2:27017" }),
    );

    expect(connected).toMatchObject({
      kind: "connected",
      convertedTo: "h1:27017,h2:27017",
    });
  });

  it("names nothing when what was stored is what was typed", () => {
    const connected = outcomeOf(
      "save",
      outcome({ connected: true, host: "h1:27017" }),
    );

    expect(connected).toMatchObject({ kind: "connected", convertedTo: "" });
  });
});

describe("the sentences", () => {
  it("never says a retry saved anything", () => {
    expect(NOT_CONNECTED.retry).not.toContain("guard");
  });

  it("says the action failed, not that it did not connect", () => {
    // Every rejection used to read "Se guardó, pero no pudo conectar".
    for (const sentence of Object.values(ACTION_FAILED)) {
      expect(sentence).not.toContain("Se guardó");
    }
  });
});

describe("sinceLabel", () => {
  const now = Date.parse("2026-09-30T12:00:00.000Z");

  it.each([
    ["2026-09-30T11:59:40.000Z", "recién"],
    ["2026-09-30T11:59:00.000Z", "hace 1 minuto"],
    ["2026-09-30T11:15:00.000Z", "hace 45 minutos"],
    ["2026-09-30T11:00:00.000Z", "hace 1 hora"],
    ["2026-09-30T02:00:00.000Z", "hace 10 horas"],
    ["2026-09-29T12:00:00.000Z", "hace 1 día"],
    ["2026-09-27T12:00:00.000Z", "hace 3 días"],
  ])("%s reads as %s", (iso, expected) => {
    expect(sinceLabel(iso, now)).toBe(expected);
  });

  it("says nothing about a timestamp it cannot read", () => {
    expect(sinceLabel("", now)).toBe("");
  });
});

describe("fileManagerLabel", () => {
  it.each([
    ["darwin", "mostrar en el Finder"],
    ["windows", "mostrar en el Explorador de archivos"],
    // Before the platform is known, and anywhere else: true everywhere.
    ["", "mostrar la carpeta"],
  ])("%p → %s", (platform, expected) => {
    expect(fileManagerLabel(platform)).toBe(expected);
  });
});

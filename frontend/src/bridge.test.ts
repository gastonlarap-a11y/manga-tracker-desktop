import { describe, expect, it } from "bun:test";
import { hello, readFrameMessage } from "./bridge";

describe("hello", () => {
  it("says whether the window behind the frame is translucent", () => {
    expect(hello(true)).toEqual({
      type: "manga-tracker:embed-hello",
      translucent: true,
    });
  });
});

describe("readFrameMessage", () => {
  it("reads a link the dashboard asks this window to open", () => {
    expect(
      readFrameMessage({
        type: "manga-tracker:open-external",
        url: "https://lectorxd.com/x",
      }),
    ).toEqual({ kind: "openExternal", url: "https://lectorxd.com/x" });
  });

  it("refuses a link message without a url", () => {
    expect(readFrameMessage({ type: "manga-tracker:open-external" })).toBeNull();
  });

  it("reads the dashboard's answer, keeping only the names it lists", () => {
    expect(
      readFrameMessage({
        type: "manga-tracker:embed-ready",
        features: ["settings", 3, null],
      }),
    ).toEqual({ kind: "ready", features: ["settings"] });
  });

  it("reads a request for the settings", () => {
    expect(readFrameMessage({ type: "manga-tracker:open-settings" })).toEqual({
      kind: "openSettings",
    });
  });

  it.each([null, "texto", 7, {}, { type: "otra-cosa" }])(
    "ignores %p, which is not one of ours",
    (data) => {
      expect(readFrameMessage(data)).toBeNull();
    },
  );
});

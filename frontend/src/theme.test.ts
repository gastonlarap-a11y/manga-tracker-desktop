import { describe, expect, it } from "bun:test";
import {
  type DarkQuery,
  followSystemTheme,
  type Rgb,
  WINDOW_BACKGROUND,
} from "./theme";

function query(matches: boolean) {
  let listener: (() => void) | null = null;
  const fake = {
    matches,
    addEventListener: (_type: "change", next: () => void) => {
      listener = next;
    },
    removeEventListener: () => {
      listener = null;
    },
  };
  return {
    fake: fake satisfies DarkQuery,
    switchTo(dark: boolean) {
      fake.matches = dark;
      listener?.();
    },
    listening: () => listener !== null,
  };
}

describe("followSystemTheme", () => {
  it("paints the current theme's colour straight away", () => {
    const applied: Rgb[] = [];
    followSystemTheme(query(false).fake, (colour) => applied.push(colour));

    expect(applied).toEqual([WINDOW_BACKGROUND.light]);
  });

  it("follows the system when it switches theme", () => {
    const applied: Rgb[] = [];
    const system = query(true);
    followSystemTheme(system.fake, (colour) => applied.push(colour));

    system.switchTo(false);

    expect(applied).toEqual([WINDOW_BACKGROUND.dark, WINDOW_BACKGROUND.light]);
  });

  it("stops listening once told to", () => {
    const system = query(true);
    const stop = followSystemTheme(system.fake, () => {});

    stop();

    expect(system.listening()).toBe(false);
  });
});

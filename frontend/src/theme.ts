/**
 * The colour behind the page on Windows, kept on the theme the system is in.
 *
 * Wails paints it while the page loads and while the window is resized; fixed
 * at start-up to the dark one, a light system flashed dark behind a light
 * dashboard. These are the dashboard's own two backgrounds. macOS needs none of
 * this: its window shows the system material, which follows the appearance by
 * itself (main.go).
 */

export interface Rgb {
  readonly r: number;
  readonly g: number;
  readonly b: number;
}

export const WINDOW_BACKGROUND: { readonly dark: Rgb; readonly light: Rgb } = {
  dark: { r: 17, g: 18, b: 24 },
  light: { r: 246, g: 246, b: 250 },
};

export function windowBackgroundFor(prefersDark: boolean): Rgb {
  return prefersDark ? WINDOW_BACKGROUND.dark : WINDOW_BACKGROUND.light;
}

/** The part of a MediaQueryList this needs, so a test can drive it. */
export interface DarkQuery {
  readonly matches: boolean;
  addEventListener(type: "change", listener: () => void): void;
  removeEventListener(type: "change", listener: () => void): void;
}

/**
 * Applies the right colour now and again on every theme change, until the
 * returned function is called.
 */
export function followSystemTheme(
  query: DarkQuery,
  apply: (colour: Rgb) => void,
): () => void {
  const update = () => apply(windowBackgroundFor(query.matches));
  update();
  query.addEventListener("change", update);
  return () => query.removeEventListener("change", update);
}

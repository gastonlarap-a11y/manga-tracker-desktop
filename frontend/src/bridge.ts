/**
 * What this window and the dashboard in its frame say to each other. The other
 * half is manga-tracker-dashboard's src/lib/embed.ts.
 *
 * Both ship in the same release, but this window can still meet a dashboard
 * older than a message — a backend run from a checkout answers on the same
 * ports — so `ready` is waited for, never assumed: until it arrives, nothing
 * here may depend on the dashboard doing its part.
 */

const EMBED_HELLO = "manga-tracker:embed-hello";
const OPEN_EXTERNAL = "manga-tracker:open-external";
const EMBED_READY = "manga-tracker:embed-ready";
const OPEN_SETTINGS = "manga-tracker:open-settings";

/**
 * The greeting that turns the dashboard's side on. `translucent` tells it the
 * window behind it is the system's material (macOS vibrancy), so it can stop
 * painting a background of its own.
 */
export function hello(translucent: boolean): {
  type: string;
  translucent: boolean;
} {
  return { type: EMBED_HELLO, translucent };
}

export type FrameMessage =
  | { readonly kind: "openExternal"; readonly url: string }
  | { readonly kind: "ready"; readonly features: readonly string[] }
  | { readonly kind: "openSettings" };

/**
 * A message from the frame, or null for anything that is not one of ours. The
 * caller has already checked the origin; this only reads the shape, since a
 * page can post any value at all.
 */
export function readFrameMessage(data: unknown): FrameMessage | null {
  if (typeof data !== "object" || data === null) {
    return null;
  }
  // Casts justified: each field is read as unknown and narrowed right here.
  const message = data as { type?: unknown; url?: unknown; features?: unknown };
  switch (message.type) {
    case OPEN_EXTERNAL:
      return typeof message.url === "string"
        ? { kind: "openExternal", url: message.url }
        : null;
    case EMBED_READY:
      return {
        kind: "ready",
        features: Array.isArray(message.features)
          ? message.features.filter(
              (feature): feature is string => typeof feature === "string",
            )
          : [],
      };
    case OPEN_SETTINGS:
      return { kind: "openSettings" };
    default:
      return null;
  }
}

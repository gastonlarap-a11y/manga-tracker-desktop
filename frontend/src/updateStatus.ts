import type { main } from "../wailsjs/go/models";

/**
 * What the window says about newer releases, from the codes `CheckForUpdate`
 * returns. Pure, so every state is tested without rendering.
 */

/**
 * The release the corner notice announces, or null for no notice: only a newer
 * release, and not one whose notice was already closed. "Could not find out"
 * shows nothing there — the settings screen is where it is said.
 */
export function noticeFor(status: main.UpdateStatus | null): string | null {
  if (status === null || status.state !== "available") {
    return null;
  }
  return status.dismissed === status.latest ? null : status.latest;
}

/** The settings screen's line for each state. */
export function updateLine(status: main.UpdateStatus): string {
  switch (status.state) {
    case "available":
      return `Hay una versión nueva: ${status.latest} (tenés ${status.current}).`;
    case "current":
      return "Estás al día.";
    case "development":
      return "Compilación de desarrollo: no se compara con las versiones publicadas.";
    case "off":
      return "No se buscan actualizaciones.";
    default:
      // "unknown", and any code this window does not know yet: never "al día".
      return "No pude comprobar si hay una versión nueva.";
  }
}

/** Whether the settings screen offers to ask again. */
export function canCheckAgain(status: main.UpdateStatus): boolean {
  return status.state === "unknown" || status.state === "current";
}
